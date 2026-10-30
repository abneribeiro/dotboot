// Package jobs é o motor do dotboot: valida um pedido (descarregar vídeo,
// extrair áudio, separar stems), executa-o e relata o progresso por eventos.
// Não conhece a web — quem chama decide como mostrar cada Event.
package jobs

import (
	"errors"
	"fmt"
	"math"
	"os"
	"time"

	"github.com/abneribeiro/dotboot/internal/config"
)

// ErrPartial sinaliza que parte do trabalho terminou e parte falhou — o
// normal numa playlist com um vídeo privado pelo meio. O Result diz o que
// ficou feito.
var ErrPartial = errors.New("alguns itens falharam")

// Stage é a etapa em que um trabalho está, já na forma de mostrar.
type Stage string

const (
	StageInfo     Stage = "a obter informação"
	StageDownload Stage = "a descarregar"
	StageMerge    Stage = "a juntar vídeo e áudio"
	StageExtract  Stage = "a extrair o áudio"
	StageConvert  Stage = "a converter para MP4"
	StageSeparate Stage = "a separar as pistas"
	StageExport   Stage = "a preparar o estúdio"
)

// Event é uma atualização de um trabalho. Os campos vazios não mudam o que
// já se sabia: um Event só com Log é só uma linha de log.
type Event struct {
	// Stage, quando não vazio, é a nova etapa.
	Stage Stage
	// Fraction entre 0 e 1; negativa = sem informação (barra indeterminada).
	// Só tem significado num Event com Progress=true.
	Fraction float64
	Progress bool
	// Speed (bytes/s) e ETA (negativo = desconhecido) do download.
	Speed float64
	ETA   time.Duration
	// Item é o ficheiro ou a música em que se está a trabalhar.
	Item string
	// Index e Count: posição numa lista (playlist, várias músicas).
	Index int
	Count int
	// Log é uma linha de output (yt-dlp, ffmpeg, audio-separator).
	Log string
}

// Reporter recebe os eventos de um trabalho. Pode ser chamado de várias
// goroutines (stdout e stderr dos processos), por isso tem de ser seguro.
type Reporter func(Event)

// Result descreve o que um trabalho produziu.
type Result struct {
	// OutputDir é a pasta onde ficaram os ficheiros.
	OutputDir string
	// Files são os ficheiros finais criados ou atualizados.
	Files []string
	// Title é o nome da música separada (vazio nos downloads).
	Title string
}

// Env é o que os trabalhos precisam da configuração.
type Env struct {
	TempDir     string
	ModelsDir   string
	JSRuntimes  string
	CookiesFile string
}

// EnvFrom extrai o Env de uma configuração carregada.
func EnvFrom(cfg *config.Config) Env {
	return Env{
		TempDir:     cfg.TempDir,
		ModelsDir:   cfg.ModelsDir,
		JSRuntimes:  cfg.JSRuntimes,
		CookiesFile: cfg.CookiesFile,
	}
}

// FormatBytes escreve um tamanho para humanos (1.4 MB, 820 KB).
func FormatBytes(b float64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%.0f B", b)
	}
	exp := int(math.Log(b) / math.Log(unit))
	exp = min(exp, 4)
	return fmt.Sprintf("%.1f %cB", b/math.Pow(unit, float64(exp)), "KMGT"[exp-1])
}

// FormatETA escreve um tempo restante (0:42, 1:05:09); vazio se desconhecido.
func FormatETA(d time.Duration) string {
	if d < 0 {
		return ""
	}
	s := int(d.Round(time.Second).Seconds())
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

// PrepareOutputDir cria a pasta de saída e confirma que se pode escrever
// nela. Corre antes do trabalho: sem isto, uma pasta só de leitura só falhava
// no fim, depois de minutos de download e de separação.
func PrepareOutputDir(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("não é possível gravar em %s: %w", dir, err)
	}
	f, err := os.CreateTemp(dir, ".dotboot-teste-*")
	if err != nil {
		return fmt.Errorf("não é possível gravar em %s: %w", dir, err)
	}
	f.Close()
	_ = os.Remove(f.Name()) // um ficheiro vazio esquecido não faz mal
	return nil
}
