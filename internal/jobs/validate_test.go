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

func TestResolveOutputDir(t *testing.T) {
	wd, _ := os.Getwd()
	home, _ := os.UserHomeDir()
	tests := []struct {
		name, dir, base, want string
	}{
		{"absoluto ignora a base", "/da/flag", "/do/env", "/da/flag"},
		{"relativo fica dentro da base", "musica", "/do/env", "/do/env/musica"},
		{"relativo sem base fica relativo", "musica", "", "musica"},
		{"sem -o usa a base", "", "/do/env", "/do/env"},
		{"sem -o nem base é o diretório atual", "", "", wd},
		{"~ de um formulário é expandido", "~/Musica", "/do/env", filepath.Join(home, "Musica")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveOutputDir(tt.dir, tt.base)
			if err != nil || got != tt.want {
				t.Errorf("ResolveOutputDir(%q, %q) = %q, %v; esperado %q", tt.dir, tt.base, got, err, tt.want)
			}
		})
	}
}

func TestNewDownloadValidacao(t *testing.T) {
	urls := tempFile(t, "urls.txt", "https://youtu.be/a\nhttps://youtu.be/b\n")
	one := []string{"https://youtu.be/a"}

	tests := []struct {
		name    string
		in      DownloadInput
		wantErr bool
	}{
		{"um URL", DownloadInput{URLs: one}, false},
		{"ficheiro de URLs", DownloadInput{BatchFile: urls}, false},
		{"recorte", DownloadInput{URLs: one, Start: "01:00", End: "02:00"}, false},
		{"qualidade", DownloadInput{URLs: one, Quality: "720"}, false},
		{"mp3", DownloadInput{URLs: one, Format: "mp3"}, false},
		{"mp3 com best", DownloadInput{URLs: one, Format: "mp3", Quality: "best"}, false},

		{"sem nada", DownloadInput{}, true},
		{"URL sem esquema", DownloadInput{URLs: []string{"youtube.com/x"}}, true},
		{"início sem fim", DownloadInput{URLs: one, Start: "01:00"}, true},
		{"recorte com dois URLs", DownloadInput{URLs: []string{"https://a.pt/1", "https://a.pt/2"}, Start: "01:00", End: "02:00"}, true},
		{"recorte com ficheiro de URLs", DownloadInput{BatchFile: urls, Start: "01:00", End: "02:00"}, true},
		{"recorte invertido", DownloadInput{URLs: one, Start: "02:00", End: "01:00"}, true},
		{"ficheiro de URLs inexistente", DownloadInput{BatchFile: "/nao/existe"}, true},
		{"qualidade inválida", DownloadInput{URLs: one, Quality: "hd"}, true},
		{"qualidade com áudio", DownloadInput{URLs: one, Format: "mp3", Quality: "720"}, true},
		{"formato desconhecido", DownloadInput{URLs: one, Format: "webm"}, true},
		{"cookies inexistentes", DownloadInput{URLs: one, Cookies: "/nao/existe.txt"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewDownload(tt.in, "/videos")
			if (err != nil) != tt.wantErr {
				t.Errorf("NewDownload(%+v) err = %v, wantErr = %v", tt.in, err, tt.wantErr)
			}
		})
	}
}

func TestNewDownloadTraduzQualidadeEFormato(t *testing.T) {
	d, err := NewDownload(DownloadInput{URLs: []string{"https://youtu.be/a"}, Quality: "2160"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if d.Format != FormatMP4 || d.FormatSort != "res:2160" || d.Height != 2160 || d.Audio() {
		t.Errorf("Download = %+v", d)
	}

	d, err = NewDownload(DownloadInput{URLs: []string{"https://youtu.be/a"}, Format: "m4a"}, "")
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

func TestValidateBatchFile(t *testing.T) {
	tests := []struct {
		name, content string
		wantErr       bool
	}{
		{"URLs válidos", "https://youtu.be/a\nhttps://youtu.be/b\n", false},
		{"vazias e comentários são ignorados", "# lista\n\nhttps://youtu.be/a\n", false},
		{"URL inválido", "https://youtu.be/a\nyoutube.com/x\n", true},
		{"sem URLs", "# só comentários\n\n", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateBatchFile(tempFile(t, "urls.txt", tt.content))
			if (err != nil) != tt.wantErr {
				t.Errorf("validateBatchFile = %v, wantErr = %v", err, tt.wantErr)
			}
		})
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
		{"ficheiro", StemsInput{Inputs: []string{song}}, false},
		{"URL", StemsInput{Inputs: []string{"https://youtu.be/a"}}, false},
		{"karaoke", StemsInput{Inputs: []string{song}, Model: "voz", Only: []string{"instrumental"}}, false},
		{"sem guitarra", StemsInput{Inputs: []string{song}, Without: []string{"guitarra"}, Format: "flac"}, false},
		{"recorte", StemsInput{Inputs: []string{song}, Start: "00:30", End: "01:00"}, false},

		{"sem entradas", StemsInput{}, true},
		{"ficheiro inexistente", StemsInput{Inputs: []string{"/nao/existe.mp3"}}, true},
		{"pasta", StemsInput{Inputs: []string{dir}}, true},
		{"modelo desconhecido", StemsInput{Inputs: []string{song}, Model: "12s"}, true},
		{"formato desconhecido", StemsInput{Inputs: []string{song}, Format: "ogg"}, true},
		{"pista que o modelo não tem", StemsInput{Inputs: []string{song}, Model: "4s", Without: []string{"guitarra"}}, true},
		{"recorte com duas músicas", StemsInput{Inputs: []string{song, song}, Start: "00:30", End: "01:00"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewStems(tt.in, "")
			if (err != nil) != tt.wantErr {
				t.Errorf("NewStems(%+v) err = %v, wantErr = %v", tt.in, err, tt.wantErr)
			}
		})
	}
}

func TestNewStemsDefaults(t *testing.T) {
	s, err := NewStems(StemsInput{Inputs: []string{"https://youtu.be/a"}}, "/base")
	if err != nil {
		t.Fatal(err)
	}
	if s.Preset.Name != "6s" || s.Format != "mp3" || s.OutputDir != "/base" {
		t.Errorf("Stems = %+v", s)
	}
	if !slices.Equal(s.Selection.Keep, s.Preset.Stems) || s.Selection.MixName != "" {
		t.Errorf("seleção por omissão = %+v", s.Selection)
	}
}
