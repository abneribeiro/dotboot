package downloader

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestBitratesFor(t *testing.T) {
	const mb = 1024 * 1024

	tests := []struct {
		name        string
		durationSec float64
		budget      int64
		wantOK      bool
		wantAudio   int
	}{
		{"clip curto em 20MB tem folga de sobra", 60, 20 * mb, true, 128},
		{"10 minutos em 20MB ainda cabe com áudio cheio", 600, 20 * mb, true, 128},
		{"10 minutos em 100MB (boost 3)", 600, 100 * mb, true, 128},
		// Quando o orçamento aperta, o áudio cede degrau a degrau antes de
		// se desistir do vídeo.
		{"~11 minutos em 20MB desce o áudio para 96", 650, 20 * mb, true, 96},
		{"~12 minutos em 20MB desce o áudio para 64", 730, 20 * mb, true, 64},
		// Acima disto não sobra bitrate para vídeo reconhecível.
		{"15 minutos em 20MB é impossível", 900, 20 * mb, false, 0},
		{"2 horas em 20MB é impossível", 7200, 20 * mb, false, 0},
		// O mesmo vídeo cabe folgadamente num servidor com boost.
		{"15 minutos em 100MB cabe", 900, 100 * mb, true, 128},
		{"duração desconhecida falha", 0, 20 * mb, false, 0},
		{"orçamento nulo falha", 600, 0, false, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			video, audio, ok := bitratesFor(tt.durationSec, tt.budget)
			if ok != tt.wantOK {
				t.Fatalf("bitratesFor(%v, %d) ok = %v, esperado %v (video=%d audio=%d)",
					tt.durationSec, tt.budget, ok, tt.wantOK, video, audio)
			}
			if !ok {
				return
			}
			if audio != tt.wantAudio {
				t.Errorf("audioKbps = %d, esperado %d", audio, tt.wantAudio)
			}
			if video < minVideoKbps {
				t.Errorf("videoKbps = %d, abaixo do mínimo utilizável %d", video, minVideoKbps)
			}
		})
	}
}

// O bitrate escolhido tem mesmo de caber no orçamento: é esta aritmética que
// decide se o upload passa ou é rejeitado com 413.
func TestBitratesForCabeNoOrcamento(t *testing.T) {
	const budget = 20 * 1024 * 1024
	const duration = 600.0

	video, audio, ok := bitratesFor(duration, budget)
	if !ok {
		t.Fatal("esperava um plano viável")
	}

	predicted := int64(float64(video+audio) * 1000 / 8 * duration)
	if predicted > budget {
		t.Errorf("bitrate previsto produz %d bytes, acima do orçamento %d", predicted, budget)
	}
}

// Menos orçamento nunca pode dar mais bitrate.
func TestBitratesForMonotonico(t *testing.T) {
	const duration = 300.0
	small, _, okSmall := bitratesFor(duration, 20*1024*1024)
	large, _, okLarge := bitratesFor(duration, 100*1024*1024)

	if !okSmall || !okLarge {
		t.Fatal("esperava planos viáveis para ambos os orçamentos")
	}
	if small >= large {
		t.Errorf("20MB deu %d kbps e 100MB deu %d kbps", small, large)
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		sec  float64
		want string
	}{
		{0, "0s"},
		{45, "45s"},
		{60, "1min"},
		{90, "1min"},
		{600, "10min"},
		{3600, "1h00min"},
		{7325, "2h02min"},
	}
	for _, tt := range tests {
		if got := formatDuration(tt.sec); got != tt.want {
			t.Errorf("formatDuration(%v) = %q, esperado %q", tt.sec, got, tt.want)
		}
	}
}

// A compressão tem de produzir o mesmo H.264 8 bits que o EnsureMP4: é isso
// que permite ao ensureMP4WithinBudget saltar a conversão quando o ficheiro
// vai ser comprimido de qualquer forma.
func TestEncodeArgsGaranteH264OitoBits(t *testing.T) {
	got := encodeArgs("/w/in.webm", "/w/out.mp4", "scale=-2:720", 1200, 128)

	want := []string{
		"-i", "/w/in.webm",
		"-map", "0:v:0", "-map", "0:a:0?",
		"-vf", "scale=-2:720",
		"-c:v", "libx264",
		"-b:v", "1200k",
		"-maxrate", "1200k",
		"-bufsize", "2400k",
		"-preset", compressPreset,
		"-pix_fmt", "yuv420p",
		"-c:a", "aac",
		"-b:a", "128k",
		"-movflags", "+faststart",
		"-y", "/w/out.mp4",
	}
	if !slices.Equal(got, want) {
		t.Errorf("encodeArgs =\n%v\nesperado\n%v", got, want)
	}
}

func TestEncodeArgsSemEscalaNaoTemFiltro(t *testing.T) {
	got := encodeArgs("/w/in.mp4", "/w/out.mp4", "", 800, 96)
	if slices.Contains(got, "-vf") {
		t.Errorf("sem escala não devia haver -vf: %v", got)
	}
}

func TestScaleFilter(t *testing.T) {
	tests := []struct {
		name          string
		width, height int
		target        int
		want          string
	}{
		{"horizontal 1080p para 720", 1920, 1080, 720, "scale=-2:720"},
		// O caso que estava errado: escalar a altura dava 405x720.
		{"vertical 1080x1920 para 720 fica 720x1280", 1080, 1920, 720, "scale=720:-2"},
		{"vertical para 480", 1080, 1920, 480, "scale=480:-2"},
		{"quadrado conta como horizontal", 1080, 1080, 720, "scale=-2:720"},
		{"já está em 720: nada a reduzir", 1280, 720, 720, ""},
		{"vertical já em 720: nada a reduzir", 720, 1280, 720, ""},
		{"abaixo do alvo: nada a reduzir", 854, 480, 720, ""},
		{"alvo 0 mantém a resolução", 1920, 1080, 0, ""},
		{"dimensões desconhecidas assumem horizontal", 0, 0, 720, "scale=-2:720"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := scaleFilter(tt.width, tt.height, tt.target); got != tt.want {
				t.Errorf("scaleFilter(%d, %d, %d) = %q, esperado %q", tt.width, tt.height, tt.target, got, tt.want)
			}
		})
	}
}

func TestParseDimensions(t *testing.T) {
	tests := []struct {
		in            string
		width, height int
	}{
		{"1920x1080", 1920, 1080},
		{"1080x1920", 1080, 1920},
		{" 640 x 360 ", 640, 360},
		{"", 0, 0},
		{"1920", 0, 0},
		{"Nx1080", 0, 0},
	}
	for _, tt := range tests {
		w, h := parseDimensions(tt.in)
		if w != tt.width || h != tt.height {
			t.Errorf("parseDimensions(%q) = %dx%d, esperado %dx%d", tt.in, w, h, tt.width, tt.height)
		}
	}
}

// O limite chega sem a margem do envelope (20MB - 256KB); truncar mostrava
// "19MB" a quem sabe que o limite do Discord é 20MB.
func TestRoundMB(t *testing.T) {
	const mb = 1024 * 1024
	tests := []struct {
		bytes int64
		want  int64
	}{
		{20*mb - 256*1024, 20},
		{50 * mb, 50},
		{mb - 256*1024, 1},
		{0, 0},
	}
	for _, tt := range tests {
		if got := roundMB(tt.bytes); got != tt.want {
			t.Errorf("roundMB(%d) = %d, esperado %d", tt.bytes, got, tt.want)
		}
	}
}

// A mensagem de ErrTooLarge é o que o utilizador lê quando o vídeo não cabe:
// nunca pode conter um bitrate negativo, por muito apertado que seja o
// orçamento.
func TestMensagemDeErroNuncaTemBitrateNegativo(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "grande.mp4")
	if err := os.WriteFile(path, make([]byte, 4096), 0o644); err != nil {
		t.Fatal(err)
	}

	// Sem ffprobe disponível o caminho é o de "não foi possível analisar";
	// com ele, é o da escada esgotada. Ambos passam pela mesma formatação.
	_, err := validateAndProcessVideo(context.Background(), path, 16, dir)
	if err == nil {
		t.Fatal("esperava erro para um ficheiro acima do limite")
	}
	if strings.Contains(err.Error(), "-") && strings.Contains(err.Error(), "kbps") {
		t.Errorf("mensagem contém um valor negativo: %q", err.Error())
	}
}
