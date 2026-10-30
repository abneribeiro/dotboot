package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/abneribeiro/dotboot/internal/downloader"
	"github.com/abneribeiro/dotboot/internal/stems"
)

// fakeBin instala no PATH do teste um executável com o script sh dado.
func fakeBin(t *testing.T, dir, name, script string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
}

// fakePath cria uma pasta só com os binários falsos e põe-na no PATH (com
// /bin e /usr/bin, para o sh dos scripts ter o básico).
func fakePath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("PATH", dir+":/usr/bin:/bin")
	return dir
}

// fakeYtdlp imita o yt-dlp: escreve progresso no formato do template, cria o
// ficheiro de saída a partir do -o e regista-o no --print-to-file.
const fakeYtdlp = `
out=""; print=""; ext=mp4
while [ $# -gt 0 ]; do
  case "$1" in
    -o) out="$2"; shift ;;
    --print-to-file) print="$3"; shift 2 ;;
    --audio-format) ext="$2"; shift ;;
  esac
  shift
done
file=$(echo "$out" | sed "s/%(title)s/Teste/; s/%(id)s/abc/; s/%(ext)s/$ext/")
echo "[download] Destination: $file"
echo "dotboot-progress 512 1024 NA 2048.0 3"
echo "dotboot-progress 1024 1024 NA 2048.0 0"
echo "WARNING: só um aviso" >&2
printf 'x' > "$file"
[ -n "$print" ] && echo "$file" >> "$print"
exit 0
`

// fakeFFprobe diz que tudo é H.264/AAC 8 bits: o EnsureMP4 não converte.
const fakeFFprobe = `
case "$*" in
  *pix_fmt*) echo yuv420p ;;
  *a:0*) echo aac ;;
  *v:0*) echo h264 ;;
  *duration*) echo 10.0 ;;
esac
`

// collector guarda os eventos de um Reporter.
type collector struct {
	mu     sync.Mutex
	events []Event
}

func (c *collector) report(e Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, e)
}

func (c *collector) has(pred func(Event) bool) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.ContainsFunc(c.events, pred)
}

func TestDownloadRunComBinariosFalsos(t *testing.T) {
	bin := fakePath(t)
	fakeBin(t, bin, "yt-dlp", fakeYtdlp)
	fakeBin(t, bin, "ffprobe", fakeFFprobe)

	out := t.TempDir()
	d, err := NewDownload(DownloadInput{URLs: []string{"https://youtu.be/abc"}, OutputDir: out})
	if err != nil {
		t.Fatal(err)
	}

	var c collector
	res, err := d.Run(context.Background(), Env{TempDir: t.TempDir()}, c.report)
	if err != nil {
		t.Fatalf("Run falhou: %v", err)
	}
	want := filepath.Join(out, "Teste [abc].mp4")
	if !slices.Equal(res.Files, []string{want}) {
		t.Errorf("Files = %v, esperado [%s]", res.Files, want)
	}
	// Tudo convertido: a lista de pendentes não pode ficar para trás.
	if _, err := os.Stat(filepath.Join(out, pendingFileName)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a lista de pendentes devia ter sido apagada (%v)", err)
	}

	if !c.has(func(e Event) bool { return e.Progress && e.Fraction == 0.5 && e.Speed == 2048 }) {
		t.Error("faltou o evento de 50%")
	}
	if !c.has(func(e Event) bool { return e.Progress && e.Fraction == 1 }) {
		t.Error("faltou o evento de 100%")
	}
	if !c.has(func(e Event) bool { return e.Item == "Teste [abc].mp4" }) {
		t.Error("o ficheiro em curso devia chegar como Item")
	}
	if !c.has(func(e Event) bool { return e.Log == "WARNING: só um aviso" }) {
		t.Error("o stderr devia chegar como Log")
	}
	// As linhas do template são só para a barra, não para o log.
	if c.has(func(e Event) bool { return strings.Contains(e.Log, "dotboot-progress") }) {
		t.Error("as linhas do template não deviam chegar ao log")
	}
}

func TestDownloadRunAudio(t *testing.T) {
	bin := fakePath(t)
	fakeBin(t, bin, "yt-dlp", fakeYtdlp)

	out := t.TempDir()
	d, err := NewDownload(DownloadInput{URLs: []string{"https://youtu.be/abc"}, OutputDir: out, Format: "mp3"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := d.Run(context.Background(), Env{TempDir: t.TempDir()}, func(Event) {})
	if err != nil {
		t.Fatalf("Run falhou: %v", err)
	}
	if !slices.Equal(res.Files, []string{filepath.Join(out, "Teste [abc].mp3")}) {
		t.Errorf("Files = %v", res.Files)
	}
}

func TestDownloadRunFalhaSemNadaFeito(t *testing.T) {
	bin := fakePath(t)
	fakeBin(t, bin, "yt-dlp", `echo "ERROR: [youtube] abc: Video unavailable" >&2; exit 1`)

	d, err := NewDownload(DownloadInput{URLs: []string{"https://youtu.be/abc"}, OutputDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = d.Run(context.Background(), Env{TempDir: t.TempDir()}, func(Event) {})
	var dlErr *downloader.DownloadError
	if !errors.As(err, &dlErr) || dlErr.Kind != downloader.ErrUnavailable {
		t.Fatalf("erro = %v, esperado ErrUnavailable", err)
	}
	if errors.Is(err, ErrPartial) {
		t.Error("sem nada descarregado não é parcial")
	}
}

func TestDownloadRunCancelado(t *testing.T) {
	bin := fakePath(t)
	fakeBin(t, bin, "yt-dlp", `exec sleep 5`)

	d, err := NewDownload(DownloadInput{URLs: []string{"https://youtu.be/abc"}, OutputDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, err := d.Run(ctx, Env{TempDir: t.TempDir()}, func(Event) {}); !errors.Is(err, context.Canceled) {
		t.Errorf("erro = %v, esperado context.Canceled", err)
	}
}

// fakeSeparator imita o audio-separator: progresso no stderr com "\r", como
// o tqdm, e um WAV por pista do modelo 4s com os nomes em português.
const fakeSeparator = `
dir=""
while [ $# -gt 0 ]; do
  [ "$1" = "--output_dir" ] && dir="$2"
  shift
done
printf '  0%%|     | 0/4\r 50%%|██   | 2/4\r100%%|█████| 4/4\n' >&2
printf '  0%%|     | 0/4\r100%%|█████| 4/4\n' >&2
echo "INFO - Separation complete!"
for s in vozes bateria baixo outros; do printf 'x' > "$dir/$s.wav"; done
`

// fakeFFmpeg cria o ficheiro de saída (o último argumento) ou, quando lhe
// pedem PCM no stdout (a forma de onda), escreve 4 amostras.
const fakeFFmpeg = `for last; do :; done
if [ "$last" = "-" ]; then printf '\000\100\000\300\377\177\000\000'; exit 0; fi
printf 'x' > "$last"`

func TestStemsRunComBinariosFalsos(t *testing.T) {
	bin := fakePath(t)
	fakeBin(t, bin, "audio-separator", fakeSeparator)
	fakeBin(t, bin, "ffmpeg", fakeFFmpeg)

	// O upload tem um nome aleatório; o título vem do nome original.
	upload := filepath.Join(t.TempDir(), "a1b2c3.mp3")
	if err := os.WriteFile(upload, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "sessao")
	s, err := NewStems(StemsInput{Input: upload, Name: "A Minha Música.mp3", OutputDir: out, Model: "4s"})
	if err != nil {
		t.Fatal(err)
	}

	var c collector
	res, err := s.Run(context.Background(), Env{TempDir: t.TempDir(), ModelsDir: t.TempDir()}, c.report)
	if err != nil {
		t.Fatalf("Run falhou: %v", err)
	}
	if res.Title != "A Minha Música" {
		t.Errorf("Title = %q", res.Title)
	}

	var want []string
	for _, name := range []string{"vozes", "bateria", "baixo", "outros"} {
		want = append(want, filepath.Join(out, name+".flac"))
	}
	if !slices.Equal(res.Files, want) {
		t.Errorf("Files =\n%v\nesperado\n%v", res.Files, want)
	}
	for _, f := range want {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("falta %s", f)
		}
	}

	data, err := os.ReadFile(filepath.Join(out, PeaksFile))
	if err != nil {
		t.Fatal(err)
	}
	var peaks map[string]stems.Peaks
	if err := json.Unmarshal(data, &peaks); err != nil {
		t.Fatal(err)
	}
	// 4 amostras a 8 kHz: 0, 0.5, ~1 e 0.
	if p := peaks["baixo"]; len(peaks) != 4 || len(p.Values) != 4 || p.Values[1] != 0.5 || p.Duration != 0.0005 {
		t.Errorf("peaks = %+v", peaks)
	}

	if !c.has(func(e Event) bool { return e.Stage == StageSeparate }) {
		t.Error("faltou a etapa de separação")
	}
	if !c.has(func(e Event) bool { return e.Progress && e.Fraction == 0.5 }) {
		t.Error("o progresso do tqdm (50%) devia chegar")
	}
	// A segunda barra do tqdm é um segundo passo, não um recomeço.
	if !c.has(func(e Event) bool { return e.Stage == StageSeparate+" (passo 2)" }) {
		t.Error("a segunda passagem do modelo devia aparecer como passo 2")
	}
	if !c.has(func(e Event) bool { return e.Item == "A Minha Música" }) {
		t.Error("o título devia chegar como Item")
	}
	if !c.has(func(e Event) bool { return e.Stage == StageExport }) {
		t.Error("faltou a etapa de preparar o estúdio")
	}
}

func TestStemsRunFicheiroIlegivel(t *testing.T) {
	bin := fakePath(t)
	fakeBin(t, bin, "audio-separator", fakeSeparator)
	fakeBin(t, bin, "ffmpeg", `echo "Invalid data found when processing input" >&2; exit 1`)

	song := tempFile(t, "má.mp3", "x")
	s, err := NewStems(StemsInput{Input: song, OutputDir: filepath.Join(t.TempDir(), "s"), Model: "4s"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Run(context.Background(), Env{TempDir: t.TempDir(), ModelsDir: t.TempDir()}, func(Event) {})
	if err == nil || !strings.Contains(err.Error(), "Invalid data") {
		t.Fatalf("erro = %v, esperado a causa do ffmpeg", err)
	}
}

func TestStemsRunCancelado(t *testing.T) {
	bin := fakePath(t)
	fakeBin(t, bin, "audio-separator", `exec sleep 5`)
	fakeBin(t, bin, "ffmpeg", fakeFFmpeg)

	song := tempFile(t, "song.mp3", "x")
	s, err := NewStems(StemsInput{Input: song, OutputDir: filepath.Join(t.TempDir(), "s"), Model: "4s"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if _, err := s.Run(ctx, Env{TempDir: t.TempDir(), ModelsDir: t.TempDir()}, func(Event) {}); !errors.Is(err, context.Canceled) {
		t.Errorf("erro = %v, esperado context.Canceled", err)
	}
}

func TestFormatBytesEETA(t *testing.T) {
	for in, want := range map[float64]string{512: "512 B", 2048: "2.0 KB", 3.5 * 1024 * 1024: "3.5 MB"} {
		if got := FormatBytes(in); got != want {
			t.Errorf("FormatBytes(%v) = %q, esperado %q", in, got, want)
		}
	}
	for in, want := range map[time.Duration]string{-1: "", 42 * time.Second: "0:42", 3909 * time.Second: "1:05:09"} {
		if got := FormatETA(in); got != want {
			t.Errorf("FormatETA(%v) = %q, esperado %q", in, got, want)
		}
	}
}

func TestPrepareOutputDir(t *testing.T) {
	tmp := t.TempDir()
	readOnly := filepath.Join(tmp, "so-leitura")
	if err := os.Mkdir(readOnly, 0o555); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(tmp, "ficheiro")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		dir     string
		wantErr bool
	}{
		{"cria a pasta", filepath.Join(tmp, "nova", "sub"), false},
		{"pasta que já existe", tmp, false},
		{"pasta só de leitura falha logo", readOnly, true},
		{"um ficheiro no caminho", filepath.Join(file, "sub"), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.wantErr && tt.dir == readOnly && os.Geteuid() == 0 {
				t.Skip("o root escreve em pastas só de leitura")
			}
			err := PrepareOutputDir(tt.dir)
			if (err != nil) != tt.wantErr {
				t.Fatalf("PrepareOutputDir(%q) = %v, wantErr %v", tt.dir, err, tt.wantErr)
			}
			if err == nil {
				entries, _ := os.ReadDir(tt.dir)
				for _, e := range entries {
					if strings.HasPrefix(e.Name(), ".dotboot-teste-") {
						t.Errorf("ficou o ficheiro de teste %s", e.Name())
					}
				}
			}
		})
	}
}
