// Package downloader encapsula a invocação do yt-dlp e do ffmpeg. Não conhece
// a interface (CLI ou TUI): o progresso sai por callbacks e o output por
// io.Writer, e quem chama decide como os mostrar.
package downloader

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/abneribeiro/dotboot/internal/procio"
)

// Run executa o yt-dlp com as opções fornecidas. O cancelamento do contexto
// envia SIGINT (não SIGKILL) para o yt-dlp finalizar os ficheiros .part —
// é isso que torna o resume (--continue) fiável após interrupções.
//
// O stdout é lido linha a linha: com o.Progress definido, as linhas de
// progresso e as mudanças de fase (ver ParseLine) vão para o callback; as
// restantes seguem para o.Stdout. O stderr vai para o.Stderr e para um tail
// que serve para classificar o erro.
func Run(ctx context.Context, o Options) error {
	args := BuildArgs(o)
	slog.Debug("a executar yt-dlp", "args", args)

	cmd := exec.CommandContext(ctx, "yt-dlp", args...)
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 10 * time.Second

	tail := &procio.TailBuffer{Max: 16 * 1024}
	cmd.Stderr = tail
	if o.Stderr != nil {
		cmd.Stderr = io.MultiWriter(o.Stderr, tail)
	}
	stdout := procio.NewLineWriter(func(line string) {
		if o.Progress != nil {
			if p, ok := ParseLine(line); ok {
				o.Progress(p)
				// As linhas do template são só para o callback.
				if strings.HasPrefix(line, progressMarker) {
					return
				}
			}
		}
		if o.Stdout != nil {
			fmt.Fprintln(o.Stdout, line)
		}
	})
	cmd.Stdout = stdout

	err := cmd.Run()
	stdout.Flush()
	if err != nil {
		dlErr := classify(tail.String(), ctx.Err(), err)
		if dlErr.Kind != ErrCanceled {
			slog.Debug("yt-dlp falhou", "err", err, "output", procio.Tail(tail.String(), 2048))
		}
		return dlErr
	}
	return nil
}
