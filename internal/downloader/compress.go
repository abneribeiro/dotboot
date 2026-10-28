package downloader

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	// muxOverheadFactor desconta o overhead do contentor MP4: o ffmpeg atinge
	// o bitrate pedido para os fluxos, não o tamanho final do ficheiro.
	muxOverheadFactor = 0.95

	// minVideoKbps é o ponto abaixo do qual o resultado deixa de ser vídeo
	// reconhecível — mais vale falhar depressa do que codificar em vão.
	minVideoKbps = 150

	// compressPreset privilegia a velocidade: com um bitrate-alvo explícito o
	// tamanho é atingido de qualquer forma, e o bot corre com CPU limitado.
	compressPreset = "veryfast"

	// minAttemptTimeout é o mínimo que se dá a uma tentativa de codificação,
	// mesmo quando o prazo restante é curto.
	minAttemptTimeout = 30 * time.Second
)

// audioLadder são os bitrates de áudio a experimentar, do melhor ao mais
// comprimido, quando o orçamento aperta.
var audioLadder = []int{128, 96, 64}

// resolutionLadder são as resoluções das tentativas sucessivas, medidas no
// lado menor do vídeo (ver scaleFilter). 0 = manter a resolução original.
// Descer a resolução com o mesmo bitrate-alvo dá menos pixels a codificar e,
// portanto, um resultado visivelmente melhor.
var resolutionLadder = []int{0, 720, 480}

// bitratesFor reparte um orçamento de bytes entre vídeo e áudio para um vídeo
// com durationSec segundos. Devolve ok=false quando nem com o áudio mais
// comprimido sobra bitrate de vídeo utilizável. Função pura — é aqui que vive
// a aritmética que vale a pena testar.
func bitratesFor(durationSec float64, budget int64) (videoKbps, audioKbps int, ok bool) {
	if durationSec <= 0 || budget <= 0 {
		return 0, 0, false
	}

	totalKbps := int(float64(budget) * 8 / 1000 / durationSec)
	for _, a := range audioLadder {
		if v := totalKbps - a; v >= minVideoKbps {
			return v, a, true
		}
	}
	return 0, 0, false
}

// probe corre o ffprobe e devolve a primeira linha do output.
func probe(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "ffprobe", args...)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("ffprobe falhou: %w", err)
	}
	return strings.TrimSpace(strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0]), nil
}

// probeDuration devolve a duração do ficheiro em segundos.
func probeDuration(ctx context.Context, path string) (float64, error) {
	out, err := probe(ctx, "-v", "error", "-show_entries", "format=duration", "-of", "csv=p=0", path)
	if err != nil {
		return 0, err
	}
	d, err := strconv.ParseFloat(out, 64)
	if err != nil {
		return 0, fmt.Errorf("duração ilegível do ffprobe: %q", out)
	}
	return d, nil
}

// probeDimensions devolve a largura e a altura do primeiro fluxo de vídeo, ou
// zeros quando desconhecidas.
func probeDimensions(ctx context.Context, path string) (width, height int) {
	out, err := probe(ctx, "-v", "error", "-select_streams", "v:0",
		"-show_entries", "stream=width,height", "-of", "csv=p=0:s=x", path)
	if err != nil {
		return 0, 0
	}
	return parseDimensions(out)
}

// parseDimensions interpreta o "1920x1080" que o ffprobe escreve com csv=s=x.
// Qualquer coisa ilegível dá zeros, que o scaleFilter trata como desconhecido.
func parseDimensions(s string) (width, height int) {
	w, h, ok := strings.Cut(s, "x")
	if !ok {
		return 0, 0
	}
	width, errW := strconv.Atoi(strings.TrimSpace(w))
	height, errH := strconv.Atoi(strings.TrimSpace(h))
	if errW != nil || errH != nil || width < 0 || height < 0 {
		return 0, 0
	}
	return width, height
}

// scaleFilter devolve o filtro que reduz o vídeo até o lado menor ter target
// pixels, ou "" quando não há nada a reduzir (target 0, ou já igual/menor).
//
// É o lado menor, e não a altura, que define a resolução: um vídeo vertical
// 1080x1920 (Shorts, TikTok, Reels) é 1080p, e reduzi-lo a 720 tem de dar
// 720x1280 — escalar a altura dava 405x720, pior do que o necessário. Com
// dimensões desconhecidas assume-se um vídeo horizontal.
func scaleFilter(width, height, target int) string {
	if target <= 0 {
		return ""
	}
	portrait := width > 0 && height > width
	short := height
	if portrait {
		short = width
	}
	if short > 0 && target >= short {
		return ""
	}
	// -2 mantém a proporção e garante uma dimensão par (exigência do x264).
	if portrait {
		return fmt.Sprintf("scale=%d:-2", target)
	}
	return fmt.Sprintf("scale=-2:%d", target)
}

// encodeArgs constrói os argumentos do ffmpeg de uma tentativa de compressão.
// Função pura, como o compatArgs: é aqui que se fixa que a saída é sempre
// H.264 8 bits + AAC em MP4 — o ensureMP4WithinBudget conta com isso para não
// converter o mesmo ficheiro duas vezes.
func encodeArgs(inputPath, outputPath, scale string, videoKbps, audioKbps int) []string {
	// O -map explícito tem a mesma razão de ser que no compatArgs: legendas ou
	// fluxos de dados fariam o ffmpeg abortar. O "?" tolera vídeos mudos.
	args := []string{"-i", inputPath, "-map", "0:v:0", "-map", "0:a:0?"}
	if scale != "" {
		args = append(args, "-vf", scale)
	}
	return append(args,
		"-c:v", "libx264",
		"-b:v", fmt.Sprintf("%dk", videoKbps),
		// maxrate/bufsize evitam picos que rebentem o orçamento.
		"-maxrate", fmt.Sprintf("%dk", videoKbps),
		"-bufsize", fmt.Sprintf("%dk", videoKbps*2),
		"-preset", compressPreset,
		// Sem -pix_fmt, uma fonte a 10 bits saía em H.264 High10, que o iPhone
		// não descodifica (ver mp4PixFmt).
		"-pix_fmt", mp4PixFmt,
		"-c:a", "aac",
		"-b:a", fmt.Sprintf("%dk", audioKbps),
		"-movflags", "+faststart",
		"-y",
		outputPath,
	)
}

// encode recodifica inputPath para o bitrate pedido, opcionalmente reduzindo a
// resolução (scale, ver scaleFilter). Devolve o caminho do ficheiro produzido;
// em caso de erro remove o que tiver sido escrito.
func encode(ctx context.Context, inputPath, workDir string, attempt int, scale string, videoKbps, audioKbps int) (string, error) {
	outputPath := filepath.Join(workDir, fmt.Sprintf("compressed_%d.mp4", attempt))
	args := encodeArgs(inputPath, outputPath, scale, videoKbps, audioKbps)

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 10 * time.Second

	output, err := cmd.CombinedOutput()
	if err != nil {
		// O ficheiro parcial não serve para nada e não pode ficar para trás.
		os.Remove(outputPath)
		slog.Error("compressão FFmpeg falhou", "err", err, "output", tail(string(output), 2048))
		return "", fmt.Errorf("falhou ao comprimir vídeo: %w", err)
	}

	return outputPath, nil
}

// attemptContext limita uma tentativa a metade do prazo restante, para que
// uma primeira tentativa lenta não consuma o orçamento das seguintes.
func attemptContext(ctx context.Context) (context.Context, context.CancelFunc) {
	deadline, ok := ctx.Deadline()
	if !ok {
		return context.WithCancel(ctx)
	}
	budget := time.Until(deadline) / 2
	if budget < minAttemptTimeout {
		budget = minAttemptTimeout
	}
	return context.WithTimeout(ctx, budget)
}

// validateAndProcessVideo verifica o tamanho do ficheiro e, se exceder
// maxSize, recodifica-o para um bitrate calculado a partir da duração e do
// orçamento, descendo a resolução enquanto o resultado não couber.
// Devolve o caminho do ficheiro a enviar. Todos os ficheiros ficam dentro de
// workDir, que o chamador remove por inteiro.
func validateAndProcessVideo(ctx context.Context, filePath string, maxSize int64, workDir string) (string, error) {
	info, err := os.Stat(filePath)
	if err != nil {
		return filePath, fmt.Errorf("erro ao verificar o tamanho do ficheiro: %w", err)
	}

	fileSize := info.Size()
	maxMB := roundMB(maxSize)
	slog.Info("ficheiro descarregado", "size_bytes", fileSize, "size_mb", fmt.Sprintf("%.2f", float64(fileSize)/mib))

	// Dentro do limite: usa o ficheiro diretamente.
	if fileSize <= maxSize {
		return filePath, nil
	}

	duration, err := probeDuration(ctx, filePath)
	if err != nil {
		return filePath, &DownloadError{
			Kind: ErrTooLarge,
			Msg:  fmt.Sprintf("o vídeo excede o limite de %dMB (tamanho: %dMB) e não foi possível analisá-lo para comprimir: %v", maxMB, fileSize/mib, err),
			Err:  err,
		}
	}
	width, height := probeDimensions(ctx, filePath)

	slog.Info("ficheiro excede o limite, a comprimir",
		"limit_mb", maxMB, "duration_s", fmt.Sprintf("%.1f", duration), "source_size", fmt.Sprintf("%dx%d", width, height))

	// factor corrige o orçamento entre tentativas: se uma tentativa
	// ultrapassar o limite, a seguinte pede proporcionalmente menos.
	factor := muxOverheadFactor
	attempt := 0

	for _, target := range resolutionLadder {
		scale := scaleFilter(width, height, target)
		// Não faz sentido "reduzir" para uma resolução igual ou maior do que a
		// original: seria repetir a tentativa anterior.
		if target > 0 && scale == "" {
			continue
		}

		videoKbps, audioKbps, ok := bitratesFor(duration, int64(float64(maxSize)*factor))
		if !ok {
			break
		}

		attempt++
		slog.Info("tentativa de compressão", "attempt", attempt,
			"scale", scale, "video_kbps", videoKbps, "audio_kbps", audioKbps)

		attemptCtx, cancel := attemptContext(ctx)
		compressedPath, err := encode(attemptCtx, filePath, workDir, attempt, scale, videoKbps, audioKbps)
		cancel()
		if err != nil {
			// Um contexto cancelado não é recuperável nas tentativas seguintes.
			if ctx.Err() != nil {
				return filePath, classify("", ctx.Err(), err)
			}
			continue
		}

		compressedInfo, err := os.Stat(compressedPath)
		if err != nil {
			os.Remove(compressedPath)
			continue
		}

		if compressedInfo.Size() <= maxSize {
			os.Remove(filePath)
			slog.Info("compressão bem-sucedida",
				"attempt", attempt, "before_mb", fileSize/mib, "after_mb", compressedInfo.Size()/mib)
			return compressedPath, nil
		}

		slog.Info("tentativa ainda excede o limite, a reduzir o alvo",
			"attempt", attempt, "size_mb", compressedInfo.Size()/mib)
		os.Remove(compressedPath)

		// 0.97 dá folga extra para não voltar a ultrapassar por pouco.
		factor *= float64(maxSize) / float64(compressedInfo.Size()) * 0.97
	}

	// Orçamento TOTAL disponível, comparado com o mínimo necessário. Não se
	// subtrai aqui o áudio: em orçamentos apertados o resultado seria
	// negativo e a mensagem falaria de "-41 kbps de vídeo".
	availableKbps := int(float64(maxSize) * muxOverheadFactor * 8 / 1000 / duration)
	if availableKbps < 0 {
		availableKbps = 0
	}
	requiredKbps := minVideoKbps + audioLadder[len(audioLadder)-1]

	// A mensagem não sugere como contornar o limite: isso depende de quem
	// chama (o bot fala do recorte com inicio/fim), e este pacote não conhece
	// o Discord.
	return filePath, &DownloadError{
		Kind: ErrTooLarge,
		Msg: fmt.Sprintf(
			"o vídeo excede o limite de %dMB: com %s de duração o orçamento só dá ~%d kbps no total, e são precisos pelo menos %d kbps.",
			maxMB, formatDuration(duration), availableKbps, requiredKbps),
	}
}

// roundMB converte bytes em MB para mensagens, arredondando ao mais próximo:
// o limite chega já sem a margem do envelope (20MB - 256KB), e truncá-lo
// anunciava "19MB" a quem sabe que o limite do Discord é 20MB.
func roundMB(bytes int64) int64 {
	return (bytes + mib/2) / mib
}

// formatDuration escreve uma duração para mensagens ao utilizador, evitando
// tanto os negativos como o "são 1 minutos".
func formatDuration(sec float64) string {
	d := time.Duration(sec * float64(time.Second)).Round(time.Second)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dmin", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh%02dmin", int(d.Hours()), int(d.Minutes())%60)
	}
}

// tail devolve os últimos n bytes de s, para logs de erro do ffmpeg sem
// despejar o output inteiro.
func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
