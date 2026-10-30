//go:build integration

// Separação a sério, com o audio-separator e o ffmpeg. Fica fora do
// "go test ./..." porque descarrega um modelo (~80MB) na primeira execução e
// precisa de vários minutos de CPU:
//
//	make test-integration-docker   (dentro da imagem)
package stems

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestSepararEExportar(t *testing.T) {
	if _, err := exec.LookPath("audio-separator"); err != nil {
		t.Skip("audio-separator não instalado")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	dir := t.TempDir()
	// Uma "música" sintética de 8s — um tom e ruído rosa — gerada localmente:
	// não depende da rede nem de direitos de autor, e chega para exercitar o
	// caminho todo.
	input := filepath.Join(dir, "musica.wav")
	gen := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "sine=frequency=220:duration=8",
		"-f", "lavfi", "-i", "anoisesrc=color=pink:duration=8:amplitude=0.2",
		"-filter_complex", "amix=inputs=2", "-ac", "2", "-ar", "44100", "-y", input)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Fatalf("não foi possível gerar a música de teste: %v\n%s", err, out)
	}

	modelDir := os.Getenv("DOTBOOT_MODELS_DIR")
	if modelDir == "" {
		modelDir = t.TempDir()
	}

	// O htdemucs simples é o mais rápido dos Demucs, e produz as mesmas
	// pistas do 4s.
	p := Preset{Name: "teste", Model: "htdemucs.yaml", Stems: []string{"vozes", "bateria", "baixo", "outros"}}
	var last float64
	tracks, err := Separate(ctx, input, filepath.Join(dir, "pistas"), modelDir, p,
		func(line string) { t.Log(line) }, func(f float64) { last = f })
	if err != nil {
		t.Fatalf("Separate falhou: %v", err)
	}
	if last < 0.99 {
		t.Errorf("o progresso do modelo devia chegar a 100%%, ficou em %v", last)
	}
	for _, s := range p.Stems {
		if tracks[s] == "" {
			t.Errorf("falta a pista %q: %v", s, tracks)
		}
	}

	files, err := Export(ctx, tracks, p.Stems, filepath.Join(dir, "sessao"), "flac", "Teste")
	if err != nil {
		t.Fatalf("Export falhou: %v", err)
	}
	if len(files) != len(p.Stems) {
		t.Fatalf("Export criou %d ficheiros, esperado %d: %v", len(files), len(p.Stems), files)
	}

	// A forma de onda de uma pista real: 8s e picos entre 0 e 1.
	peaks, err := MeasurePeaks(ctx, files[0], 500)
	if err != nil {
		t.Fatalf("MeasurePeaks falhou: %v", err)
	}
	if peaks.Duration < 7.9 || peaks.Duration > 8.1 || len(peaks.Values) != 500 {
		t.Errorf("peaks = %.2fs com %d pontos, esperado 8s com 500", peaks.Duration, len(peaks.Values))
	}

	// O estúdio: baixo + bateria numa só faixa, com o baixo a meio volume,
	// e uma pista sozinha em MP3.
	mix := filepath.Join(dir, "baixo-bateria.wav")
	err = Render(ctx, []Track{{files[2], 0.5}, {files[1], 1}}, mix, "wav", "Teste (baixo + bateria)")
	if err != nil {
		t.Fatalf("Render da mistura falhou: %v", err)
	}
	single := filepath.Join(dir, "vozes.mp3")
	if err := Render(ctx, []Track{{files[0], 1}}, single, "mp3", "Teste (vozes)"); err != nil {
		t.Fatalf("Render de uma pista falhou: %v", err)
	}
	for _, f := range append(files, mix, single) {
		info, err := os.Stat(f)
		if err != nil || info.Size() == 0 {
			t.Errorf("%s vazio ou inexistente (%v)", f, err)
			continue
		}
		t.Logf("%s (%d bytes)", filepath.Base(f), info.Size())
	}
}
