// Package cli implementa os subcomandos de terminal (download e stems): lê as
// flags, entrega o pedido ao motor partilhado (internal/jobs) e mostra o
// progresso em texto. É a face para scripts; a TUI usa o mesmo motor.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/charmbracelet/x/term"

	"github.com/abneribeiro/dotboot/internal/config"
	"github.com/abneribeiro/dotboot/internal/jobs"
)

var (
	// errHelp sinaliza que o utilizador pediu a ajuda. Pedir ajuda não é um
	// erro: o Run imprime o texto e devolve nil, para o processo sair a 0.
	errHelp = errors.New("ajuda pedida")

	// ErrUsage sinaliza argumentos inválidos já reportados ao utilizador.
	// O main sai com código 2 sem voltar a imprimir a mensagem.
	ErrUsage = errors.New("argumentos inválidos")

	// ErrPartial sinaliza que parte do trabalho terminou e parte falhou —
	// o normal numa playlist com um vídeo privado pelo meio. Já foi
	// reportado; o main apenas ajusta o código de saída.
	ErrPartial = errors.New("alguns downloads falharam")

	// ErrInterrupted sinaliza um CTRL+C. Não é uma falha: o que ficou a meio
	// retoma-se ao repetir o comando. O main sai com 130, como as shells.
	ErrInterrupted = errors.New("interrompido")
)

const usageText = `Uso: dotboot download [flags] URL...

Flags (podem vir antes ou depois dos URLs):
  -o DIR          Pasta de saída; relativa é resolvida sob $DOTBOOT_OUTPUT_DIR
                  quando definido (default: $DOTBOOT_OUTPUT_DIR ou o diretório atual)
  -a FICHEIRO     Ficheiro com um URL por linha
  -q QUALIDADE    best (default) ou a resolução máxima, ex: 1080, 720, 480.
                  Conta o lado menor: -q 720 num vídeo vertical dá 720x1280
  -formato FMT    mp4 (default) garante MP4 com H.264/AAC, que o iPhone e o
                  Android leem sem conversão; original guarda o ficheiro tal
                  como o site o serve (pode ser .webm ou .mkv); mp3 ou m4a
                  guardam só o áudio, com título e capa
  -cookies FICH   Cookies (formato Netscape) para vídeos que pedem login ou a
                  verificação "não és um robô" (default: $DOTBOOT_COOKIES_FILE)
  -start TEMPO    Início do recorte, MM:SS ou HH:MM:SS (exige -end e um único URL)
  -end TEMPO      Fim do recorte

Exemplos:
  dotboot download https://youtube.com/watch?v=xxxx
  dotboot download -o ~/Videos https://youtube.com/playlist?list=yyyy
  dotboot download -a urls.txt -o ~/Videos
  dotboot download -q 720 -o aulas https://youtube.com/watch?v=xxxx
  dotboot download -formato mp3 -o musica https://youtube.com/watch?v=xxxx
  dotboot download -start 01:30 -end 02:00 https://x.com/user/status/123

Repetir o mesmo comando salta o que já foi descarregado e retoma o que
ficou a meio, incluindo conversões para MP4 interrompidas.
`

// parseArgs interpreta as flags do subcomando download e valida o pedido.
// outputBase é o DOTBOOT_OUTPUT_DIR.
func parseArgs(args []string, outputBase string) (*jobs.Download, error) {
	var in jobs.DownloadInput

	fs := flag.NewFlagSet("download", flag.ContinueOnError)
	// O FlagSet não imprime nada: quem reporta erros e ajuda é o Run, para
	// não duplicar mensagens nem escrever a ajuda no stderr.
	fs.SetOutput(io.Discard)
	fs.StringVar(&in.OutputDir, "o", "", "pasta de saída")
	fs.StringVar(&in.BatchFile, "a", "", "ficheiro com um URL por linha")
	fs.StringVar(&in.Quality, "q", "", "qualidade: best ou resolução")
	fs.StringVar(&in.Format, "formato", "", "mp4 (default), original, mp3 ou m4a")
	fs.StringVar(&in.Cookies, "cookies", "", "ficheiro de cookies")
	fs.StringVar(&in.Start, "start", "", "início do recorte")
	fs.StringVar(&in.End, "end", "", "fim do recorte")

	urls, err := parseInterspersed(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil, errHelp
		}
		return nil, err
	}
	in.URLs = urls
	return jobs.NewDownload(in, outputBase)
}

// parseInterspersed faz o parsing de flags em qualquer posição. O flag da
// stdlib para no primeiro argumento que não é flag, por isso "dotboot
// download URL -o pasta" tratava o "-o" como um URL. Depois de "--", tudo é
// posicional, como manda a convenção.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return positional, nil
		}
		// O Parse consome o "--" sem o devolver: se foi o último argumento
		// consumido, o resto é todo posicional.
		if consumed := len(args) - len(rest); consumed > 0 && args[consumed-1] == "--" {
			return append(positional, rest...), nil
		}
		positional = append(positional, rest[0])
		args = rest[1:]
	}
}

// Run executa o subcomando download.
func Run(ctx context.Context, args []string, cfg *config.Config, log *slog.Logger) error {
	d, err := parseArgs(args, cfg.OutputDir)
	if errors.Is(err, errHelp) {
		fmt.Print(usageText)
		return nil
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n\n%s", err, usageText)
		return ErrUsage
	}

	log.Debug("a iniciar downloads", "output_dir", d.OutputDir)
	r := newTextReporter(os.Stdout)
	res, err := d.Run(ctx, jobs.EnvFrom(cfg), r.report)
	r.endLine()
	return finish(res, err, "Downloads concluídos")
}

// finish traduz o resultado de um trabalho em mensagens e nos erros
// sentinela que decidem o código de saída.
func finish(res jobs.Result, err error, done string) error {
	switch {
	case errors.Is(err, context.Canceled):
		fmt.Fprintf(os.Stderr, "\n⏹️  Interrompido. Repete o mesmo comando para continuar de onde ficou.\n")
		return ErrInterrupted
	case errors.Is(err, jobs.ErrPartial):
		fmt.Fprintf(os.Stderr, "\n⚠️  %v\n", err)
		printFiles(res)
		return ErrPartial
	case err != nil:
		return err
	}
	fmt.Printf("\n✅ %s em %s\n", done, res.OutputDir)
	printFiles(res)
	return nil
}

func printFiles(res jobs.Result) {
	for _, f := range res.Files {
		rel, err := filepath.Rel(res.OutputDir, f)
		if err != nil || strings.HasPrefix(rel, "..") {
			rel = f
		}
		fmt.Printf("   %s\n", rel)
	}
}

// textReporter mostra os eventos de um trabalho em texto. Num terminal o
// progresso é uma só linha reescrita com "\r"; fora dele (logs, pipes) só
// aparecem as etapas e o log.
type textReporter struct {
	mu      sync.Mutex
	out     io.Writer
	tty     bool
	stage   jobs.Stage
	item    string
	pending bool // há uma linha de progresso por terminar
}

func newTextReporter(f *os.File) *textReporter {
	return &textReporter{out: f, tty: term.IsTerminal(f.Fd())}
}

func (r *textReporter) report(e jobs.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if e.Stage != "" && e.Stage != r.stage {
		r.stage = e.Stage
		r.line(fmt.Sprintf("%s %s", stageIcon(e.Stage), e.Stage))
	}
	if e.Item != "" && e.Item != r.item {
		r.item = e.Item
		prefix := "   "
		if e.Count > 1 {
			prefix = fmt.Sprintf("   [%d/%d] ", e.Index, e.Count)
		}
		r.line(prefix + e.Item)
	}
	if e.Log != "" {
		r.line(e.Log)
	}
	if e.Progress && e.Fraction >= 0 && r.tty {
		fmt.Fprintf(r.out, "\r\033[K   %s", ProgressText(e))
		r.pending = true
	}
}

// line escreve uma linha completa, terminando antes a de progresso.
func (r *textReporter) line(s string) {
	if r.pending {
		fmt.Fprintln(r.out)
		r.pending = false
	}
	fmt.Fprintln(r.out, s)
}

func (r *textReporter) endLine() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.pending {
		fmt.Fprintln(r.out)
		r.pending = false
	}
}

// ProgressText resume um evento de progresso: "45.3%  2.1 MB/s  ETA 0:12".
func ProgressText(e jobs.Event) string {
	parts := []string{fmt.Sprintf("%5.1f%%", e.Fraction*100)}
	if e.Speed > 0 {
		parts = append(parts, jobs.FormatBytes(e.Speed)+"/s")
	}
	if eta := jobs.FormatETA(e.ETA); eta != "" && e.Speed > 0 {
		parts = append(parts, "ETA "+eta)
	}
	return strings.Join(parts, "  ")
}

func stageIcon(s jobs.Stage) string {
	switch s {
	case jobs.StageDownload:
		return "⬇️ "
	case jobs.StageMerge, jobs.StageExport:
		return "📦"
	case jobs.StageExtract:
		return "🎵"
	case jobs.StageConvert:
		return "🔄"
	case jobs.StageSeparate:
		return "🎛️ "
	default:
		return "🔎"
	}
}
