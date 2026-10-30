package jobs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/abneribeiro/dotboot/internal/downloader"
)

// tempFile cria um ficheiro temporário com o conteúdo dado.
func tempFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestNewDownloadValidacao(t *testing.T) {
	one := []string{"https://youtu.be/a"}

	tests := []struct {
		name    string
		in      DownloadInput
		wantErr bool
	}{
		{"um URL", DownloadInput{URLs: one}, false},
		{"vários URLs", DownloadInput{URLs: []string{"https://a.pt/1", "https://a.pt/2"}}, false},
		{"recorte", DownloadInput{URLs: one, Start: "01:00", End: "02:00"}, false},
		{"qualidade", DownloadInput{URLs: one, Quality: "720"}, false},
		{"mp3", DownloadInput{URLs: one, Format: "mp3"}, false},
		{"mp3 com best", DownloadInput{URLs: one, Format: "mp3", Quality: "best"}, false},

		{"sem nada", DownloadInput{}, true},
		{"URL sem esquema", DownloadInput{URLs: []string{"youtube.com/x"}}, true},
		{"início sem fim", DownloadInput{URLs: one, Start: "01:00"}, true},
		{"recorte com dois URLs", DownloadInput{URLs: []string{"https://a.pt/1", "https://a.pt/2"}, Start: "01:00", End: "02:00"}, true},
		{"recorte invertido", DownloadInput{URLs: one, Start: "02:00", End: "01:00"}, true},
		{"qualidade inválida", DownloadInput{URLs: one, Quality: "hd"}, true},
		{"qualidade com áudio", DownloadInput{URLs: one, Format: "mp3", Quality: "720"}, true},
		{"formato desconhecido", DownloadInput{URLs: one, Format: "webm"}, true},
		{"pasta relativa", DownloadInput{URLs: one, OutputDir: "videos"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.in.OutputDir == "" {
				tt.in.OutputDir = "/videos"
			}
			_, err := NewDownload(tt.in)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewDownload(%+v) err = %v, wantErr = %v", tt.in, err, tt.wantErr)
			}
		})
	}
}

func TestNewDownloadTraduzQualidadeEFormato(t *testing.T) {
	d, err := NewDownload(DownloadInput{URLs: []string{"https://youtu.be/a"}, Quality: "2160", OutputDir: "/v"})
	if err != nil {
		t.Fatal(err)
	}
	if d.Format != FormatMP4 || d.FormatSort != "res:2160" || d.Height != 2160 || d.Audio() {
		t.Errorf("Download = %+v", d)
	}

	d, err = NewDownload(DownloadInput{URLs: []string{"https://youtu.be/a"}, Format: "m4a", OutputDir: "/v"})
	if err != nil {
		t.Fatal(err)
	}
	if !d.Audio() || d.FormatSort != "" {
		t.Errorf("Download (m4a) = %+v", d)
	}
}

func TestOutputTemplate(t *testing.T) {
	full := outputTemplate("/videos", nil)
	if full != "/videos/%(title)s [%(id)s].%(ext)s" {
		t.Errorf("outputTemplate sem recorte = %q", full)
	}
	// Com o mesmo nome do vídeo completo, o yt-dlp dizia "already
	// downloaded" e o archive ficava com o recorte no lugar do vídeo.
	trimmed := outputTemplate("/videos", &downloader.Section{Start: "01:30", End: "00:02:00"})
	if want := "/videos/%(title)s [%(id)s] (01.30-00.02.00).%(ext)s"; trimmed != want {
		t.Errorf("outputTemplate com recorte = %q, esperado %q", trimmed, want)
	}
	if strings.Contains(filepath.Base(trimmed), ":") {
		t.Errorf("o nome não pode ter ':' (inválido no Windows/macOS): %q", trimmed)
	}
}

func TestPrintedPathsSemRepeticoes(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "video.mp4")
	if err := os.WriteFile(existing, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	list := filepath.Join(dir, "paths.txt")
	content := existing + "\n\n" + filepath.Join(dir, "apagado.mkv") + "\n" + existing + "\n"
	if err := os.WriteFile(list, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := printedPaths(list); !slices.Equal(got, []string{existing}) {
		t.Errorf("printedPaths = %v", got)
	}
	if got := printedPaths(filepath.Join(dir, "nao-existe.txt")); got != nil {
		t.Errorf("printedPaths(inexistente) = %v, esperado nil", got)
	}
}

func TestCountArchive(t *testing.T) {
	if countArchive("") != 0 || countArchive("/nao/existe") != 0 {
		t.Error("sem archive conta zero")
	}
	path := tempFile(t, "archive.txt", "youtube abc\nyoutube def\n")
	if got := countArchive(path); got != 2 {
		t.Errorf("countArchive = %d, esperado 2", got)
	}
}

// Um CTRL+C antes de chegar a vez de um ficheiro mantém a lista de
// pendentes, para o pedido seguinte o converter.
func TestEnsurePlayableInterrompidoMantemPendentes(t *testing.T) {
	dir := t.TempDir()
	video := tempFile(t, "video.webm", "x")
	pending := filepath.Join(dir, pendingFileName)
	if err := os.WriteFile(pending, []byte(video+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ensurePlayable(ctx, pending, &reporterSink{report: func(Event) {}}); !errors.Is(err, context.Canceled) {
		t.Errorf("ensurePlayable = %v, esperado context.Canceled", err)
	}
	if _, err := os.Stat(pending); err != nil {
		t.Errorf("a lista de pendentes tinha de ficar: %v", err)
	}
}

func TestNewStemsValidacao(t *testing.T) {
	song := tempFile(t, "song.mp3", "x")
	dir := filepath.Dir(song)

	tests := []struct {
		name    string
		in      StemsInput
		wantErr bool
	}{
		{"ficheiro", StemsInput{Input: song}, false},
		{"URL", StemsInput{Input: "https://youtu.be/a"}, false},
		{"karaoke", StemsInput{Input: song, Model: "voz"}, false},
		{"recorte", StemsInput{Input: song, Start: "00:30", End: "01:00"}, false},

		{"sem entrada", StemsInput{}, true},
		{"ficheiro inexistente", StemsInput{Input: "/nao/existe.mp3"}, true},
		{"pasta", StemsInput{Input: dir}, true},
		{"URL sem esquema", StemsInput{Input: "youtube.com/x"}, true},
		{"modelo desconhecido", StemsInput{Input: song, Model: "12s"}, true},
		{"início sem fim", StemsInput{Input: song, Start: "00:30"}, true},
		{"sessão relativa", StemsInput{Input: song, OutputDir: "sessao"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.in.OutputDir == "" {
				tt.in.OutputDir = "/sessao"
			}
			_, err := NewStems(tt.in)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewStems(%+v) err = %v, wantErr = %v", tt.in, err, tt.wantErr)
			}
		})
	}
}

func TestNewStemsDefaults(t *testing.T) {
	s, err := NewStems(StemsInput{Input: " https://youtu.be/a ", OutputDir: "/sessao"})
	if err != nil {
		t.Fatal(err)
	}
	if s.Preset.Name != "6s" || s.Input != "https://youtu.be/a" || s.Section != nil {
		t.Errorf("Stems = %+v", s)
	}
}
