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
	"path/filepath"
	"regexp"
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

// AudioOpts parametriza um DownloadAudio.
type AudioOpts struct {
	CookiesFile string // ver Options.CookiesFile; copiado para o workDir
	JSRuntimes  string // ver Options.JSRuntimes

	// Progress, Stdout e Stderr: ver Options.
	Progress func(Progress)
	Stdout   io.Writer
	Stderr   io.Writer
}

// DownloadAudio descarrega só o áudio de url para workDir, em WAV — sem
// perdas, que é o que a separação de stems precisa à entrada — e devolve o
// caminho do ficheiro. O workDir é do chamador, que o cria e remove.
func DownloadAudio(ctx context.Context, url string, sec *Section, workDir string, o AudioOpts) (string, error) {
	pathFile := filepath.Join(workDir, "final.path")

	cookies, err := copyCookies(o.CookiesFile, workDir)
	if err != nil {
		return "", err
	}

	opts := Options{
		URLs:           []string{url},
		OutputTemplate: filepath.Join(workDir, "%(title).150B.%(ext)s"),
		Format:         "ba/b",
		ExtractAudio:   "wav",
		NoPlaylist:     true,
		PrintToFile:    pathFile,
		Section:        sec,
		CookiesFile:    cookies,
		JSRuntimes:     o.JSRuntimes,
		Progress:       o.Progress,
		Stdout:         o.Stdout,
		Stderr:         o.Stderr,
	}
	if err := Run(ctx, opts); err != nil {
		return "", err
	}
	return finalFile(workDir, pathFile)
}

// cookiesFileName é o nome da cópia dos cookies dentro do diretório de
// trabalho; o finalFile ignora-a.
const cookiesFileName = "cookies.txt"

// copyCookies copia o ficheiro de cookies para workDir e devolve o caminho da
// cópia ("" quando não há cookies). O yt-dlp reescreve o ficheiro no fim de
// cada execução, e o original do utilizador não deve ser tocado — e o 0600 é
// porque são credenciais.
func copyCookies(src, workDir string) (string, error) {
	if src == "" {
		return "", nil
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return "", fmt.Errorf("não foi possível ler o ficheiro de cookies: %w", err)
	}
	dst := filepath.Join(workDir, cookiesFileName)
	if err := os.WriteFile(dst, data, 0o600); err != nil {
		return "", fmt.Errorf("não foi possível copiar o ficheiro de cookies: %w", err)
	}
	return dst, nil
}

// fragmentPattern identifica os ficheiros por-formato que o yt-dlp deixa
// quando um merge é interrompido (ex: "Video.f299.mp4").
var fragmentPattern = regexp.MustCompile(`\.f\d+\.[^.]+$`)

// finalFile determina o ficheiro descarregado. A fonte de verdade é o
// caminho que o yt-dlp escreveu com --print-to-file; a varredura do
// diretório é apenas uma rede de segurança para o caso de o yt-dlp não o
// ter escrito (ex: versão antiga).
func finalFile(workDir, pathFile string) (string, error) {
	if data, err := os.ReadFile(pathFile); err == nil {
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		candidate := strings.TrimSpace(lines[len(lines)-1])
		if candidate != "" {
			if _, err := os.Stat(candidate); err == nil {
				return candidate, nil
			}
			slog.Warn("caminho indicado pelo yt-dlp não existe, a procurar no diretório", "path", candidate)
		}
	}

	entries, err := os.ReadDir(workDir)
	if err != nil {
		return "", fmt.Errorf("erro ao listar o diretório de trabalho: %w", err)
	}

	var best string
	var bestSize int64 = -1
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || name == filepath.Base(pathFile) || name == cookiesFileName {
			continue
		}
		// Descarta trabalho intermédio do yt-dlp.
		if strings.HasSuffix(name, ".part") || strings.HasSuffix(name, ".ytdl") ||
			strings.HasSuffix(name, ".temp") || fragmentPattern.MatchString(name) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.Size() > bestSize {
			best, bestSize = filepath.Join(workDir, name), info.Size()
		}
	}

	if best == "" {
		return "", fmt.Errorf("ficheiro descarregado não encontrado em %s", workDir)
	}
	return best, nil
}
