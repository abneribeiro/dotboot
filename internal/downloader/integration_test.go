//go:build integration

// Testes que executam mesmo o yt-dlp e o ffmpeg, contra a rede. Ficam fora do
// "go test ./..." por precisarem dos binários e de ligação à internet:
//
//	make test-integration-docker   (dentro da imagem)
package downloader

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// testURLCurto é um vídeo de ~19s, para exercitar o caminho completo sem
// descarregar um filme inteiro.
const testURLCurto = "https://www.youtube.com/watch?v=C0DPdy98e4c"

// assertPlayableOnPhones confirma que o ficheiro é H.264 8 bits + AAC dentro de
// MP4 — o único par que o iPhone, o iOS Fotos e o Android leem sem conversão.
func assertPlayableOnPhones(t *testing.T, ctx context.Context, path string) {
	t.Helper()

	vcodec, acodec, pixFmt, err := probeStreams(ctx, path)
	if err != nil {
		t.Fatalf("ffprobe falhou em %s: %v", path, err)
	}
	if !mp4Compatible(filepath.Ext(path), vcodec, acodec, pixFmt) {
		t.Errorf("%s não é reproduzível num telemóvel: ext=%s vcodec=%s acodec=%s pix_fmt=%s",
			filepath.Base(path), filepath.Ext(path), vcodec, acodec, pixFmt)
	}
	t.Logf("codecs: %s / %s (%s)", vcodec, acodec, pixFmt)
}

// makeTestVideo gera localmente um ficheiro com os codecs pedidos. É preferível
// a descarregar um vídeo real: não depende da rede nem de o YouTube continuar a
// servir determinado formato, e produz exatamente a combinação a testar.
func makeTestVideo(t *testing.T, ctx context.Context, path string, ffmpegArgs ...string) {
	t.Helper()

	args := []string{
		"-f", "lavfi", "-i", "testsrc=duration=3:size=320x240:rate=25",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=3",
	}
	args = append(args, ffmpegArgs...)
	args = append(args, "-y", path)

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("não foi possível gerar %s: %v\n%s", path, err, out)
	}
}

// TestEnsureMP4RecodificaFonteSemH264 exercita o caso que o -S não resolve: um
// VP9/Opus, que é o que o YouTube serve acima de 1080p. Também confirma que o
// progresso do ffmpeg chega aos hooks.
func TestEnsureMP4RecodificaFonteSemH264(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	dir := t.TempDir()
	source := filepath.Join(dir, "so-vp9.webm")
	makeTestVideo(t, ctx, source, "-c:v", "libvpx-vp9", "-b:v", "300k", "-c:a", "libopus")

	var started, reencode bool
	var last float64
	got, err := EnsureMP4(ctx, source, ConvertHooks{
		Start:    func(r bool, _ string) { started, reencode = true, r },
		Progress: func(f float64) { last = f },
	})
	if err != nil {
		t.Fatalf("EnsureMP4 falhou: %v", err)
	}
	assertPlayableOnPhones(t, ctx, got)
	if _, err := os.Stat(source); err == nil {
		t.Errorf("o .webm original %s devia ter sido removido", source)
	}
	if !started || !reencode {
		t.Errorf("Start devia anunciar uma recodificação (started=%v reencode=%v)", started, reencode)
	}
	if last < 0.9 {
		t.Errorf("o progresso devia chegar perto de 1, ficou em %v", last)
	}
}

// TestEnsureMP4Recodifica10Bits fixa a armadilha do pixel format: um AV1/VP9 a
// 10 bits convertido sem -pix_fmt dá H.264 High10, que tem os codecs certos e
// mesmo assim não abre num iPhone.
func TestEnsureMP4Recodifica10Bits(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	dir := t.TempDir()
	source := filepath.Join(dir, "dez-bits.mp4")
	makeTestVideo(t, ctx, source, "-c:v", "libx264", "-pix_fmt", "yuv420p10le", "-c:a", "aac")

	got, err := EnsureMP4(ctx, source, ConvertHooks{})
	if err != nil {
		t.Fatalf("EnsureMP4 falhou: %v", err)
	}
	_, _, pixFmt, err := probeStreams(ctx, got)
	if err != nil {
		t.Fatal(err)
	}
	if pixFmt != mp4PixFmt {
		t.Errorf("pix_fmt = %q, esperado %q", pixFmt, mp4PixFmt)
	}
	assertPlayableOnPhones(t, ctx, got)
}

// TestEnsureMP4RemuxaSemPerdas verifica o caminho barato: quando os codecs já
// estão certos e só o contentor está errado, o ficheiro é remuxado em segundos.
func TestEnsureMP4RemuxaSemPerdas(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	dir := t.TempDir()
	source := filepath.Join(dir, "fonte.mkv")
	makeTestVideo(t, ctx, source, "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac")

	got, err := EnsureMP4(ctx, source, ConvertHooks{})
	if err != nil {
		t.Fatalf("EnsureMP4 falhou: %v", err)
	}
	if filepath.Ext(got) != ".mp4" {
		t.Errorf("EnsureMP4 = %q, esperado um .mp4", got)
	}
	if _, err := os.Stat(source); err == nil {
		t.Errorf("o ficheiro original %s devia ter sido removido", source)
	}
	assertPlayableOnPhones(t, ctx, got)

	// Idempotência: correr outra vez sobre um ficheiro já conforme não pode
	// tocar em nada.
	again, err := EnsureMP4(ctx, got, ConvertHooks{})
	if err != nil {
		t.Fatalf("EnsureMP4 (2ª passagem) falhou: %v", err)
	}
	if again != got {
		t.Errorf("EnsureMP4 devolveu %q na 2ª passagem, esperado o mesmo %q", again, got)
	}
}

// TestRunReportaProgresso descarrega um vídeo real e confirma que o
// --progress-template chega ao callback: é disto que a barra da TUI depende.
func TestRunReportaProgresso(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	dir := t.TempDir()
	var mu sync.Mutex
	var updates int
	var maxFrac float64
	var merged bool
	err := Run(ctx, Options{
		URLs:           []string{testURLCurto},
		FormatSort:     MP4Sort("res:360"),
		MergeFormat:    "mp4",
		OutputTemplate: filepath.Join(dir, "%(id)s.%(ext)s"),
		NoPlaylist:     true,
		Progress: func(p Progress) {
			mu.Lock()
			defer mu.Unlock()
			if p.Phase == PhaseMerge {
				merged = true
			}
			if f := p.Fraction(); f >= 0 {
				updates++
				maxFrac = max(maxFrac, f)
			}
		},
	})
	if err != nil {
		t.Fatalf("Run falhou: %v", err)
	}
	if updates == 0 || maxFrac < 0.99 {
		t.Errorf("progresso insuficiente: %d atualizações, máximo %v", updates, maxFrac)
	}
	t.Logf("%d atualizações de progresso, merge=%v", updates, merged)

	files, _ := filepath.Glob(filepath.Join(dir, "*.mp4"))
	if len(files) != 1 {
		t.Fatalf("esperava um .mp4, há %v", files)
	}
	assertPlayableOnPhones(t, ctx, files[0])
}
