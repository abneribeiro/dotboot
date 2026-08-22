package downloader

import (
	"context"
	"os"
	"path/filepath"
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
