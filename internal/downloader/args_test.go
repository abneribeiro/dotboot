package downloader

import (
	"slices"
	"strings"
	"testing"
)

func TestBuildArgsBotMode(t *testing.T) {
	got := BuildArgs(Options{
		URLs:              []string{"https://youtu.be/abc"},
		OutputTemplate:    "/tmp/dotboot_123.%(ext)s",
		Format:            "bv*+ba/b",
		FormatSort:        "res:720",
		MaxFileSize:       500 * 1024 * 1024,
		RestrictFilenames: true,
		PrintToFile:       "/tmp/work/final.path",
		NoPlaylist:        true,
		MergeFormat:       "mp4",
		Quiet:             true,
	})

	want := []string{
		"-f", "bv*+ba/b",
		"-S", "res:720",
		"--merge-output-format", "mp4",
		"--max-filesize", "524288000",
		"--restrict-filenames",
		"--no-playlist",
		"-o", "/tmp/dotboot_123.%(ext)s",
		"--no-warnings", "--quiet",
		"--print-to-file", "after_move:filepath", "/tmp/work/final.path", "--no-simulate",
		"https://youtu.be/abc",
	}
	if !slices.Equal(got, want) {
		t.Errorf("BuildArgs (bot) =\n%v\nesperado\n%v", got, want)
	}
}

func TestBuildArgsBotModeWithSection(t *testing.T) {
	got := BuildArgs(Options{
		URLs:           []string{"https://youtu.be/abc"},
		OutputTemplate: "/tmp/dotboot_123.%(ext)s",
		Format:         "bv*+ba/b",
		NoPlaylist:     true,
		MergeFormat:    "mp4",
		Quiet:          true,
		Section:        &Section{Start: "01:30", End: "02:00"},
	})

	want := []string{
		"-f", "bv*+ba/b",
		"--merge-output-format", "mp4",
		"--no-playlist",
		"-o", "/tmp/dotboot_123.%(ext)s",
		"--no-warnings", "--quiet",
		"--download-sections", "*01:30-02:00", "--force-keyframes-at-cuts",
		"https://youtu.be/abc",
	}
	if !slices.Equal(got, want) {
		t.Errorf("BuildArgs (bot+recorte) =\n%v\nesperado\n%v", got, want)
	}
}

func TestBuildArgsCLIMode(t *testing.T) {
	got := BuildArgs(Options{
		URLs:           []string{"https://youtu.be/abc", "https://x.com/u/status/1"},
		OutputTemplate: "/videos/%(title)s [%(id)s].%(ext)s",
		ArchiveFile:    "/videos/.dotboot-archive.txt",
		Continue:       true,
		IgnoreErrors:   true,
	})

	want := []string{
		"-o", "/videos/%(title)s [%(id)s].%(ext)s",
		"--download-archive", "/videos/.dotboot-archive.txt",
		"--continue",
		"--ignore-errors",
		"--newline",
		"https://youtu.be/abc",
		"https://x.com/u/status/1",
	}
	if !slices.Equal(got, want) {
		t.Errorf("BuildArgs (CLI) =\n%v\nesperado\n%v", got, want)
	}

	// Sem limites do Discord: nada de -f, --quiet ou --merge-output-format.
	for _, banned := range []string{"-f", "--quiet", "--no-warnings", "--merge-output-format", "--no-playlist"} {
		if slices.Contains(got, banned) {
			t.Errorf("BuildArgs (CLI) não devia conter %q", banned)
		}
	}
}

func TestBuildArgsCLIModeBatchFile(t *testing.T) {
	got := BuildArgs(Options{
		BatchFile:      "/home/user/urls.txt",
		OutputTemplate: "/videos/%(title)s [%(id)s].%(ext)s",
		ArchiveFile:    "/videos/.dotboot-archive.txt",
		Continue:       true,
		IgnoreErrors:   true,
	})

	want := []string{
		"-o", "/videos/%(title)s [%(id)s].%(ext)s",
		"--download-archive", "/videos/.dotboot-archive.txt",
		"--continue",
		"--ignore-errors",
		"--newline",
		"--batch-file", "/home/user/urls.txt",
	}
	if !slices.Equal(got, want) {
		t.Errorf("BuildArgs (CLI batch) =\n%v\nesperado\n%v", got, want)
	}
}

func TestBuildArgsCLIModeWithSection(t *testing.T) {
	// Recorte no CLI: força --no-playlist e omite o archive — um recorte
	// não pode marcar o vídeo completo como já descarregado.
	got := BuildArgs(Options{
		URLs:           []string{"https://youtu.be/abc"},
		OutputTemplate: "/videos/%(title)s [%(id)s].%(ext)s",
		Continue:       true,
		IgnoreErrors:   true,
		NoPlaylist:     true,
		Section:        &Section{Start: "00:10", End: "00:20"},
	})

	if !slices.Contains(got, "--no-playlist") {
		t.Error("BuildArgs (CLI recorte) devia conter --no-playlist")
	}
	if slices.Contains(got, "--download-archive") {
		t.Error("BuildArgs (CLI recorte) não devia conter --download-archive")
	}
	if !slices.Contains(got, "--download-sections") || !slices.Contains(got, "*00:10-00:20") {
		t.Errorf("BuildArgs (CLI recorte) devia conter a secção: %v", got)
	}
}

func TestQualitySelectors(t *testing.T) {
	tests := []struct {
		name       string
		q          string
		wantFormat string
		wantSort   string
		wantErr    bool
	}{
		{"vazio é melhor qualidade", "", "", "", false},
		{"best é melhor qualidade", "best", "", "", false},
		{"altura limita a 720", "720", "bv*[height<=?720]+ba/b[height<=?720]/bv*+ba/b", "res:720", false},
		{"altura 1080", "1080", "bv*[height<=?1080]+ba/b[height<=?1080]/bv*+ba/b", "res:1080", false},
		{"texto inválido", "abc", "", "", true},
		{"zero inválido", "0", "", "", true},
		{"negativo inválido", "-720", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			format, sort, err := QualitySelectors(tt.q)
			if (err != nil) != tt.wantErr {
				t.Fatalf("QualitySelectors(%q) err = %v, wantErr = %v", tt.q, err, tt.wantErr)
			}
			if format != tt.wantFormat {
				t.Errorf("QualitySelectors(%q) format = %q, esperado %q", tt.q, format, tt.wantFormat)
			}
			if sort != tt.wantSort {
				t.Errorf("QualitySelectors(%q) sort = %q, esperado %q", tt.q, sort, tt.wantSort)
			}
		})
	}
}

// O "?" é o que impede o limite de altura de excluir sites que não expõem a
// resolução dos formatos (ex: Instagram) — sem ele o -q partia esses sites.
func TestQualitySelectorsToleraAlturaDesconhecida(t *testing.T) {
	format, _, err := QualitySelectors("720")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if !strings.Contains(format, "height<=?720") {
		t.Errorf("o filtro devia usar height<=?720 (com ?): %q", format)
	}
	if !strings.HasSuffix(format, "/bv*+ba/b") {
		t.Errorf("o formato devia terminar num fallback sem limite: %q", format)
	}
}

func TestSortForBudget(t *testing.T) {
	tests := []struct {
		name string
		size int64
		want string
	}{
		{"limite gratuito prefere 720", 20 * 1024 * 1024, "res:720"},
		{"boost nível 2 prefere 1080", 50 * 1024 * 1024, "res:1080"},
		{"boost nível 3 prefere 1080", 100 * 1024 * 1024, "res:1080"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sortForBudget(tt.size); got != tt.want {
				t.Errorf("sortForBudget(%d) = %q, esperado %q", tt.size, got, tt.want)
			}
		})
	}
}

// O --max-filesize é um travão de sanidade, não o limite de upload: tem de
// ficar bem acima dele, senão os vídeos grandes eram rejeitados em vez de
// comprimidos.
func TestDownloadCapDeixaMargemParaCompressao(t *testing.T) {
	for _, limit := range []int64{20 * 1024 * 1024, 100 * 1024 * 1024} {
		if got := downloadCap(limit); got <= limit*10 {
			t.Errorf("downloadCap(%d) = %d, demasiado perto do limite de upload", limit, got)
		}
	}
}

func TestBuildArgsFormatSort(t *testing.T) {
	got := BuildArgs(Options{
		URLs:       []string{"https://youtu.be/abc"},
		FormatSort: "res:720",
	})
	if !slices.Contains(got, "-S") || !slices.Contains(got, "res:720") {
		t.Errorf("BuildArgs devia conter -S res:720: %v", got)
	}
}
