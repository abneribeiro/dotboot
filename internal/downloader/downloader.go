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
	"regexp"
	"strings"
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
	MaxFileSize int64 // limite de upload em bytes, já com margens aplicadas
	TempDir     string
}

// Result descreve o ficheiro pronto a enviar para o Discord.
type Result struct {
	// Path é o ficheiro final (já dentro do limite de tamanho).
	Path string

	// WorkDir é o diretório privado deste download. O chamador tem SEMPRE de
	// o remover com os.RemoveAll, mesmo quando DownloadForDiscord devolve
	// erro — é o que garante que fragmentos, .part e comprimidos parciais
	// não se acumulam em TempDir.
	WorkDir string

	// Name é o nome do anexo a enviar, com a extensão real do contentor.
	Name string
}

const mib = 1024 * 1024

// sortForBudget escolhe a resolução preferida em função do orçamento de
// upload: não vale a pena descarregar 4K para depois recomprimir até 20MB,
// mas com 100MB disponíveis 1080p passa muitas vezes sem recompressão.
func sortForBudget(maxSize int64) string {
	if maxSize <= 24*mib {
		return "res:720"
	}
	return "res:1080"
}

// downloadCap é um travão de sanidade para o --max-filesize: impede
// descarregar ficheiros absurdos que nunca caberiam nem depois de comprimir,
// mas fica muito acima do limite de upload para não roubar à compressão o
// material com que trabalhar.
func downloadCap(maxSize int64) int64 {
	const floor = 500 * mib
	if cap := maxSize * 25; cap > floor {
		return cap
	}
	return floor
}

// DownloadForDiscord descarrega um único vídeo para um diretório temporário
// exclusivo, garante que cabe em MaxFileSize (recodificando com ffmpeg se
// preciso) e devolve o resultado. O Result.WorkDir devolvido tem de ser
// removido pelo chamador em todos os casos, incluindo os de erro.
// O timeout vem do contexto do chamador.
func DownloadForDiscord(ctx context.Context, url string, sec *Section, o DiscordOpts) (Result, error) {
	slog.Info("a iniciar download para Discord", "url", url)

	// Um diretório por download: isola goroutines concorrentes e reduz a
	// limpeza a um único os.RemoveAll.
	workDir, err := os.MkdirTemp(o.TempDir, "dotboot-")
	if err != nil {
		return Result{}, fmt.Errorf("não foi possível criar o diretório temporário: %w", err)
	}
	res := Result{WorkDir: workDir}

	pathFile := filepath.Join(workDir, "final.path")

	opts := Options{
		URLs: []string{url},
		// %(title).80B trunca a 80 bytes (não runes), o que é seguro com
		// títulos unicode; --restrict-filenames mantém o nome em ASCII.
		OutputTemplate:    filepath.Join(workDir, "%(title).80B.%(ext)s"),
		Format:            "bv*+ba/b",
		FormatSort:        sortForBudget(o.MaxFileSize),
		MaxFileSize:       downloadCap(o.MaxFileSize),
		RestrictFilenames: true,
		PrintToFile:       pathFile,
		NoPlaylist:        true,
		MergeFormat:       "mp4",
		Quiet:             true,
		Section:           sec,
	}

	if err := Run(ctx, opts); err != nil {
		return res, err
	}

	downloadedFile, err := finalFile(workDir, pathFile)
	if err != nil {
		return res, err
	}

	processedFile, err := validateAndProcessVideo(ctx, downloadedFile, o.MaxFileSize, workDir)
	res.Path = processedFile
	res.Name = filepath.Base(processedFile)
	if err != nil {
		return res, err
	}

	slog.Info("vídeo processado com sucesso", "path", processedFile, "name", res.Name)
	return res, nil
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
		if e.IsDir() || name == filepath.Base(pathFile) {
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
