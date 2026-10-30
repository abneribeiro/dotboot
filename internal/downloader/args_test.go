package downloader

import (
	"slices"
	"strings"
	"testing"
)

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
	// Recorte: força --no-playlist e omite o archive — um recorte não pode
	// marcar o vídeo completo como já descarregado.
	got := BuildArgs(Options{
		URLs:           []string{"https://youtu.be/abc"},
		OutputTemplate: "/videos/%(title)s [%(id)s].%(ext)s",
		Continue:       true,
		IgnoreErrors:   true,
		NoPlaylist:     true,
		Section:        &Section{Start: "00:10", End: "00:20"},
	})

	if !slices.Contains(got, "--no-playlist") {
		t.Error("BuildArgs (recorte) devia conter --no-playlist")
	}
	if slices.Contains(got, "--download-archive") {
		t.Error("BuildArgs (recorte) não devia conter --download-archive")
	}
	if !slices.Contains(got, "--download-sections") || !slices.Contains(got, "*00:10-00:20") {
		t.Errorf("BuildArgs (recorte) devia conter a secção: %v", got)
	}
}

// TestBuildArgsCLIModeMP4 fixa o modo por omissão: contentor mp4 e a
// preferência de codecs. Sem isto o yt-dlp escolhia AV1/VP9 + Opus e o
// resultado não abria num iPhone.
func TestBuildArgsCLIModeMP4(t *testing.T) {
	got := BuildArgs(Options{
		URLs:           []string{"https://youtu.be/abc"},
		FormatSort:     MP4Sort(""),
		MergeFormat:    "mp4",
		OutputTemplate: "/videos/%(title)s [%(id)s].%(ext)s",
		PrintToFile:    "/tmp/paths.txt",
		Continue:       true,
		IgnoreErrors:   true,
	})

	want := []string{
		"-S", "res,hdr:sdr,vcodec:h264,acodec:aac",
		"--merge-output-format", "mp4",
		"-o", "/videos/%(title)s [%(id)s].%(ext)s",
		"--continue",
		"--ignore-errors",
		"--newline",
		"--print-to-file", "after_move:filepath", "/tmp/paths.txt", "--no-simulate",
		"https://youtu.be/abc",
	}
	if !slices.Equal(got, want) {
		t.Errorf("BuildArgs (CLI mp4) =\n%v\nesperado\n%v", got, want)
	}
}

func TestBuildArgsOpcoesDeAudioCookiesERuntime(t *testing.T) {
	got := BuildArgs(Options{
		URLs:          []string{"https://youtu.be/abc"},
		Format:        "ba/b",
		ExtractAudio:  "mp3",
		EmbedMetadata: true,
		CookiesFile:   "/tmp/work/cookies.txt",
		JSRuntimes:    "node",
	})

	want := []string{
		"-f", "ba/b",
		"-x", "--audio-format", "mp3", "--audio-quality", "0",
		"--embed-metadata", "--embed-thumbnail",
		"--cookies", "/tmp/work/cookies.txt",
		"--js-runtimes", "node",
		"--newline",
		"https://youtu.be/abc",
	}
	if !slices.Equal(got, want) {
		t.Errorf("BuildArgs (áudio) =\n%v\nesperado\n%v", got, want)
	}
}

// Sem cookies nem runtime configurados, as flags não podem aparecer: um
// "--cookies" sem valor engoliria o URL seguinte.
func TestBuildArgsOmiteOpcoesVazias(t *testing.T) {
	got := BuildArgs(Options{URLs: []string{"https://youtu.be/abc"}})
	for _, flag := range []string{"--cookies", "--js-runtimes", "-x", "--embed-metadata", "--progress-template"} {
		if slices.Contains(got, flag) {
			t.Errorf("BuildArgs não devia conter %q sem a opção definida: %v", flag, got)
		}
	}
}

// Com um callback de progresso, o yt-dlp tem de receber o template que o
// ParseLine sabe ler.
func TestBuildArgsComProgresso(t *testing.T) {
	got := BuildArgs(Options{URLs: []string{"u"}, Progress: func(Progress) {}})
	i := slices.Index(got, "--progress-template")
	if i < 0 || got[i+1] != progressTemplate {
		t.Fatalf("falta o --progress-template: %v", got)
	}
	if !strings.Contains(progressTemplate, progressMarker) {
		t.Error("o template tem de levar o marcador")
	}
}

func TestQualitySort(t *testing.T) {
	tests := []struct {
		name    string
		q       string
		want    string
		wantErr bool
	}{
		{"vazio é melhor qualidade", "", "", false},
		{"best é melhor qualidade", "best", "", false},
		{"limite de 720", "720", "res:720", false},
		{"limite de 1080", "1080", "res:1080", false},
		{"texto inválido", "abc", "", true},
		{"zero inválido", "0", "", true},
		{"negativo inválido", "-720", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := QualitySort(tt.q)
			if (err != nil) != tt.wantErr {
				t.Fatalf("QualitySort(%q) err = %v, wantErr = %v", tt.q, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("QualitySort(%q) = %q, esperado %q", tt.q, got, tt.want)
			}
		})
	}
}

// O limite é pela "res" do yt-dlp (o lado menor), nunca pela altura: um
// filtro [height<=720] deitava fora o 720x1280 de um vídeo vertical e
// entregava 360x640.
func TestQualitySortNaoFiltraPelaAltura(t *testing.T) {
	got, err := QualitySort("720")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if strings.Contains(got, "height") {
		t.Errorf("QualitySort não pode limitar pela altura: %q", got)
	}
}

func TestMP4Sort(t *testing.T) {
	tests := []struct {
		name    string
		resSort string
		want    string
	}{
		{"sem limite pede a maior resolução", "", "res,hdr:sdr,vcodec:h264,acodec:aac"},
		{"com limite", "res:720", "res:720,hdr:sdr,vcodec:h264,acodec:aac"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MP4Sort(tt.resSort); got != tt.want {
				t.Errorf("MP4Sort(%q) = %q, esperado %q", tt.resSort, got, tt.want)
			}
		})
	}
}

// A resolução tem de vir ANTES do vcodec: com o vcodec à frente, um H.264 a
// 480p ganhava a um AV1 a 2160p e o -q 2160 era silenciosamente ignorado.
func TestMP4SortPrioritizaResolucaoSobreCodec(t *testing.T) {
	got := MP4Sort("res:2160")
	if !strings.HasPrefix(got, "res:2160,") {
		t.Errorf("MP4Sort deve começar pela resolução: %q", got)
	}
}
