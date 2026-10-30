//go:build integration

// Separação a sério, com o audio-separator e o ffmpeg. Fica fora do
// "go test ./..." porque descarrega um modelo (~80MB) na primeira execução e
// precisa de vários minutos de CPU:
//
//	make test-integration-stems   (dentro da imagem de stems)
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

	sel, err := Select(p, nil, []string{"vozes"})
	if err != nil {
		t.Fatal(err)
	}
	files, err := Export(ctx, tracks, sel, filepath.Join(dir, "saida"), "mp3", "Teste")
	if err != nil {
		t.Fatalf("Export falhou: %v", err)
	}
	// As 4 pistas mais a faixa sem vozes.
	if len(files) != 5 {
		t.Fatalf("Export criou %d ficheiros, esperado 5: %v", len(files), files)
	}
	for _, f := range files {
		info, err := os.Stat(f)
		if err != nil || info.Size() == 0 {
			t.Errorf("%s vazio ou inexistente (%v)", f, err)
		}
		t.Logf("%s (%d bytes)", filepath.Base(f), info.Size())
	}
}
