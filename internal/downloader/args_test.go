package downloader

import (
	"slices"
	"testing"
)

func TestBuildArgsBotMode(t *testing.T) {
	got := BuildArgs(Options{
		URLs:           []string{"https://youtu.be/abc"},
		OutputTemplate: "/tmp/dotboot_123.%(ext)s",
		Format:         "best[filesize<25M]/best",
		NoPlaylist:     true,
		MergeFormat:    "mp4",
		Quiet:          true,
	})

	want := []string{
		"-f", "best[filesize<25M]/best",
		"--merge-output-format", "mp4",
		"--no-playlist",
		"-o", "/tmp/dotboot_123.%(ext)s",
		"--no-warnings", "--quiet",
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
		Format:         "best[filesize<50M]/best",
		NoPlaylist:     true,
		MergeFormat:    "mp4",
		Quiet:          true,
		Section:        &Section{Start: "01:30", End: "02:00"},
	})

	want := []string{
		"-f", "best[filesize<50M]/best",
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

func TestSortForQuality(t *testing.T) {
	tests := []struct {
		name    string
		q       string
		want    string
		wantErr bool
	}{
		{"vazio é melhor qualidade (sem -S)", "", "", false},
		{"best é melhor qualidade (sem -S)", "best", "", false},
		{"altura prefere até 720", "720", "res:720", false},
		{"altura 1080", "1080", "res:1080", false},
		{"texto inválido", "abc", "", true},
		{"zero inválido", "0", "", true},
		{"negativo inválido", "-720", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SortForQuality(tt.q)
			if (err != nil) != tt.wantErr {
				t.Fatalf("SortForQuality(%q) err = %v, wantErr = %v", tt.q, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("SortForQuality(%q) = %q, esperado %q", tt.q, got, tt.want)
			}
		})
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
