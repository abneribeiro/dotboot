// Package downloader encapsula a invocação do yt-dlp e do ffmpeg.
// Regra de fronteira: este pacote NUNCA importa discordgo — é partilhado
// pelo bot e pelo modo CLI.
package downloader

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// tailBuffer guarda apenas os últimos max bytes escritos. Permite classificar
// as linhas finais de erro do yt-dlp sem acumular o progresso de uma playlist
// inteira em memória.
type tailBuffer struct {
	buf []byte
	max int
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.buf = append(t.buf, p...)
	if len(t.buf) > t.max {
		t.buf = t.buf[len(t.buf)-t.max:]
	}
	return len(p), nil
}

// Run executa o yt-dlp com as opções fornecidas. O cancelamento do contexto
// envia SIGINT (não SIGKILL) para o yt-dlp finalizar os ficheiros .part —
// é isso que torna o resume (--continue) fiável após interrupções.
func Run(ctx context.Context, o Options) error {
	args := BuildArgs(o)
	slog.Debug("a executar yt-dlp", "args", args)

	cmd := exec.CommandContext(ctx, "yt-dlp", args...)
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 10 * time.Second

	// Modo capturado (bot): todo o output vai para classificação de erros.
	if o.Stdout == nil && o.Stderr == nil {
		output, err := cmd.CombinedOutput()
		if err != nil {
			slog.Debug("yt-dlp falhou", "err", err, "output", string(output))
			return classify(string(output), ctx.Err(), err)
		}
		return nil
	}

	// Modo streaming (CLI): output em direto, com um tail do stderr para
	// classificação caso falhe.
	tail := &tailBuffer{max: 8 * 1024}
	cmd.Stdout = o.Stdout
	if o.Stderr != nil {
		cmd.Stderr = io.MultiWriter(o.Stderr, tail)
	} else {
		cmd.Stderr = tail
	}

	if err := cmd.Run(); err != nil {
		return classify(string(tail.buf), ctx.Err(), err)
	}
	return nil
}

// DiscordOpts parametriza um download destinado a upload no Discord.
type DiscordOpts struct {
	MaxFileSize int64 // limite de upload em bytes
	TempDir     string
}

// DownloadForDiscord descarrega um único vídeo para um ficheiro temporário
// único, garante que cabe em MaxFileSize (recodificando com ffmpeg se preciso)
// e devolve o caminho final. O chamador é responsável por eliminar o ficheiro;
// o caminho pode ser não vazio mesmo em caso de erro, para permitir a limpeza.
// O timeout vem do contexto do chamador.
func DownloadForDiscord(ctx context.Context, url string, sec *Section, o DiscordOpts) (string, error) {
	slog.Info("a iniciar download para Discord", "url", url)

	// Nome único por nanossegundos para evitar colisões entre goroutines.
	uniqueID := fmt.Sprintf("%d", time.Now().UnixNano())
	outputTemplate := filepath.Join(o.TempDir, fmt.Sprintf("dotboot_%s.%%(ext)s", uniqueID))

	opts := Options{
		URLs:           []string{url},
		OutputTemplate: outputTemplate,
		Format:         fmt.Sprintf("best[filesize<%dM]/best", o.MaxFileSize/(1024*1024)),
		NoPlaylist:     true,
		MergeFormat:    "mp4",
		Quiet:          true,
		Section:        sec,
	}

	if err := Run(ctx, opts); err != nil {
		return "", err
	}

	// O yt-dlp substitui %(ext)s pela extensão real; o Glob encontra o
	// ficheiro independentemente dela. Seguro apenas aqui: --no-playlist
	// garante um único ficheiro de saída.
	pattern := filepath.Join(o.TempDir, fmt.Sprintf("dotboot_%s.*", uniqueID))
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) == 0 {
		return "", fmt.Errorf("ficheiro descarregado não encontrado em: %s", pattern)
	}
	downloadedFile := matches[0]

	processedFile, err := validateAndProcessVideo(ctx, downloadedFile, o.MaxFileSize, o.TempDir)
	if err != nil {
		return processedFile, err // devolve o caminho para limpeza mesmo com erro
	}

	slog.Info("vídeo processado com sucesso", "path", processedFile)
	return processedFile, nil
}
