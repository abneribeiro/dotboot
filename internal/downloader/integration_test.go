//go:build integration

// Testes que executam mesmo o yt-dlp e o ffmpeg, contra a rede. Ficam fora do
// "go test ./..." por precisarem dos binários e de ligação à internet:
//
//	go test -tags integration -v -timeout 15m ./internal/downloader
package downloader

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testURL é um vídeo Creative Commons da Blender Foundation.
const testURL = "https://www.youtube.com/watch?v=aqz-KE-bpKQ"

// TestDownloadForDiscordCabeNoLimite verifica o caminho feliz: o vídeo já cabe
// no limite, por isso não há recompressão.
func TestDownloadForDiscordCabeNoLimite(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	tempDir := t.TempDir()
	sec := &Section{Start: "00:10", End: "00:20"}

	res, err := DownloadForDiscord(ctx, testURL, sec, DiscordOpts{
		MaxFileSize: 20 * mib,
		TempDir:     tempDir,
	})
	if res.WorkDir != "" {
		defer os.RemoveAll(res.WorkDir)
	}
	if err != nil {
		t.Fatalf("DownloadForDiscord falhou: %v", err)
	}

	info, err := os.Stat(res.Path)
	if err != nil {
		t.Fatalf("ficheiro final inexistente: %v", err)
	}
	if info.Size() > 20*mib {
		t.Errorf("ficheiro tem %d bytes, acima do limite de %d", info.Size(), 20*mib)
	}
	if res.Name != filepath.Base(res.Path) {
		t.Errorf("Name = %q, esperado %q", res.Name, filepath.Base(res.Path))
	}
	// O bot força mp4 com --merge-output-format; um .webm rotulado .mp4 era
	// exatamente o bug que partia a reprodução inline no Discord.
	if !strings.HasSuffix(res.Name, ".mp4") {
		t.Errorf("Name = %q, esperado terminar em .mp4", res.Name)
	}
	// A extensão certa não chega: um MP4 com AV1/VP9 + Opus não abre num
	// iPhone. Isto verifica o que está mesmo lá dentro.
	assertPlayableOnPhones(t, ctx, res.Path)
	t.Logf("ficheiro: %s (%d bytes)", res.Name, info.Size())
}

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
// VP9/Opus, que é o que o YouTube serve acima de 1080p e o que saía deste
// projeto como .webm. Só a recodificação o torna reproduzível num iPhone.
func TestEnsureMP4RecodificaFonteSemH264(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	dir := t.TempDir()
	source := filepath.Join(dir, "so-vp9.webm")
	makeTestVideo(t, ctx, source,
		"-c:v", "libvpx-vp9", "-b:v", "300k", "-c:a", "libopus")

	got, err := EnsureMP4(ctx, source, nil)
	if err != nil {
		t.Fatalf("EnsureMP4 falhou: %v", err)
	}
	assertPlayableOnPhones(t, ctx, got)
	if _, err := os.Stat(source); err == nil {
		t.Errorf("o .webm original %s devia ter sido removido", source)
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
	makeTestVideo(t, ctx, source,
		"-c:v", "libx264", "-pix_fmt", "yuv420p10le", "-c:a", "aac")

	got, err := EnsureMP4(ctx, source, nil)
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

	// Um MKV com H.264/AAC é exatamente o que um merge incompatível do yt-dlp
	// produz: os codecs já estão certos, só o contentor é que não.
	makeTestVideo(t, ctx, source, "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac")

	got, err := EnsureMP4(ctx, source, nil)
	if err != nil {
		t.Fatalf("EnsureMP4 falhou: %v", err)
	}
	if filepath.Ext(got) != ".mp4" {
		t.Errorf("EnsureMP4 = %q, esperado um .mp4", got)
	}
	// O original tem de desaparecer: deixá-lo duplicaria o espaço em disco de
	// cada download do CLI.
	if _, err := os.Stat(source); err == nil {
		t.Errorf("o ficheiro original %s devia ter sido removido", source)
	}
	assertPlayableOnPhones(t, ctx, got)

	// Idempotência: correr outra vez sobre um ficheiro já conforme não pode
	// tocar em nada.
	again, err := EnsureMP4(ctx, got, nil)
	if err != nil {
		t.Fatalf("EnsureMP4 (2ª passagem) falhou: %v", err)
	}
	if again != got {
		t.Errorf("EnsureMP4 devolveu %q na 2ª passagem, esperado o mesmo %q", again, got)
	}
}

// testURLCurto é um vídeo de ~19s, para exercitar o caminho completo sem
// descarregar um filme inteiro.
const testURLCurto = "https://www.youtube.com/watch?v=C0DPdy98e4c"

// TestDownloadForDiscordSemRecorte cobre o caso mais comum do bot — um
// /baixar sem inicio/fim — que todos os outros testes saltavam por passarem
// sempre uma Section.
func TestDownloadForDiscordSemRecorte(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	res, err := DownloadForDiscord(ctx, testURLCurto, nil, DiscordOpts{
		MaxFileSize: 50 * mib,
		TempDir:     t.TempDir(),
	})
	if res.WorkDir != "" {
		defer os.RemoveAll(res.WorkDir)
	}
	if err != nil {
		t.Fatalf("DownloadForDiscord falhou: %v", err)
	}

	if !strings.HasSuffix(res.Name, ".mp4") {
		t.Errorf("Name = %q, esperado terminar em .mp4", res.Name)
	}
	assertPlayableOnPhones(t, ctx, res.Path)
}

// TestDownloadForDiscordComprime força a compressão com um limite apertado.
func TestDownloadForDiscordComprime(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	tempDir := t.TempDir()
	sec := &Section{Start: "00:10", End: "00:40"}
	const limit = 1 * mib

	res, err := DownloadForDiscord(ctx, testURL, sec, DiscordOpts{
		MaxFileSize: limit,
		TempDir:     tempDir,
	})
	if res.WorkDir != "" {
		defer os.RemoveAll(res.WorkDir)
	}
	if err != nil {
		t.Fatalf("DownloadForDiscord falhou: %v", err)
	}

	info, err := os.Stat(res.Path)
	if err != nil {
		t.Fatalf("ficheiro final inexistente: %v", err)
	}
	if info.Size() > limit {
		t.Errorf("ficheiro comprimido tem %d bytes, acima do limite de %d", info.Size(), limit)
	}
	t.Logf("comprimido para %d bytes (limite %d)", info.Size(), limit)
}

// TestDownloadForDiscordLimpaTudo garante que o WorkDir devolvido remove todo
// o trabalho intermédio — a fuga de ficheiros temporários que existia antes.
func TestDownloadForDiscordLimpaTudo(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	tempDir := t.TempDir()
	sec := &Section{Start: "00:10", End: "00:40"}

	res, err := DownloadForDiscord(ctx, testURL, sec, DiscordOpts{
		MaxFileSize: 1 * mib,
		TempDir:     tempDir,
	})
	if err != nil {
		t.Fatalf("DownloadForDiscord falhou: %v", err)
	}
	if err := os.RemoveAll(res.WorkDir); err != nil {
		t.Fatalf("RemoveAll falhou: %v", err)
	}

	entries, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("sobraram ficheiros em TempDir depois da limpeza: %v", names)
	}
}

// TestDownloadForDiscordVideoImpossivel verifica que um vídeo longo demais
// falha depressa, com uma mensagem útil, em vez de codificar em vão.
func TestDownloadForDiscordVideoImpossivel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	tempDir := t.TempDir()
	// Um clip curto com um orçamento minúsculo exercita exatamente o mesmo
	// caminho que um vídeo de duas horas em 20MB, sem descarregar 100MB.
	sec := &Section{Start: "00:10", End: "01:10"} // 1 minuto
	const limit = 16 * 1024                       // ~2 kbps: impossível

	res, err := DownloadForDiscord(ctx, testURL, sec, DiscordOpts{
		MaxFileSize: limit,
		TempDir:     tempDir,
	})
	if res.WorkDir != "" {
		defer os.RemoveAll(res.WorkDir)
	}
	if err == nil {
		t.Fatal("esperava ErrTooLarge para 1 minuto em 16KB")
	}

	var dlErr *DownloadError
	if !errors.As(err, &dlErr) || dlErr.Kind != ErrTooLarge {
		t.Fatalf("erro = %v, esperado ErrTooLarge", err)
	}
	// A mensagem tem de dizer ao utilizador o que fazer a seguir.
	if !strings.Contains(dlErr.Error(), "kbps") {
		t.Errorf("mensagem não explica o bitrate necessário: %q", dlErr.Error())
	}
	t.Logf("erro esperado: %v", dlErr)
}
