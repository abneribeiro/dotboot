package downloader

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// compressVideo comprime um vídeo com FFmpeg e devolve o caminho do ficheiro
// comprimido. O contexto do chamador limita a duração da operação.
func compressVideo(ctx context.Context, inputPath, tempDir string) (string, error) {
	uniqueID := fmt.Sprintf("%d", time.Now().UnixNano())
	outputPath := filepath.Join(tempDir, fmt.Sprintf("dotboot_compressed_%s.mp4", uniqueID))

	// -crf 28: fator de compressão (maior = mais compressão)
	// -preset fast: equilíbrio entre velocidade e eficiência
	// -movflags +faststart: otimiza para reprodução em streaming
	args := []string{
		"-i", inputPath,
		"-c:v", "libx264",
		"-crf", "28",
		"-preset", "fast",
		"-c:a", "aac",
		"-b:a", "128k",
		"-movflags", "+faststart",
		"-y", // overwrite se já existir
		outputPath,
	}

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 10 * time.Second

	output, err := cmd.CombinedOutput()
	if err != nil {
		slog.Error("compressão FFmpeg falhou", "err", err, "output", string(output))
		return "", fmt.Errorf("falhou ao comprimir vídeo: %w", err)
	}

	info, err := os.Stat(outputPath)
	if err != nil {
		return "", fmt.Errorf("erro ao verificar ficheiro comprimido: %w", err)
	}

	slog.Info("vídeo comprimido com sucesso", "path", outputPath, "size_bytes", info.Size())
	return outputPath, nil
}

// validateAndProcessVideo verifica o tamanho do ficheiro e comprime-o se
// exceder maxSize. Devolve o caminho do ficheiro final (original ou comprimido).
// Mesmo em caso de erro, devolve um caminho não vazio para o chamador limpar.
func validateAndProcessVideo(ctx context.Context, filePath string, maxSize int64, tempDir string) (string, error) {
	info, err := os.Stat(filePath)
	if err != nil {
		return filePath, fmt.Errorf("erro ao verificar o tamanho do ficheiro: %w", err)
	}

	fileSize := info.Size()
	maxMB := maxSize / (1024 * 1024)
	slog.Info("ficheiro descarregado", "size_bytes", fileSize, "size_mb", fmt.Sprintf("%.2f", float64(fileSize)/(1024*1024)))

	// Dentro do limite: usa o ficheiro diretamente.
	if fileSize <= maxSize {
		return filePath, nil
	}

	slog.Info("ficheiro excede o limite, a tentar compressão", "limit_mb", maxMB)

	compressedPath, err := compressVideo(ctx, filePath, tempDir)
	if err != nil {
		return filePath, &DownloadError{
			Kind: ErrTooLarge,
			Msg:  fmt.Sprintf("o vídeo excede o limite de %dMB (tamanho: %dMB) e a compressão falhou: %v", maxMB, fileSize/(1024*1024), err),
			Err:  err,
		}
	}

	compressedInfo, err := os.Stat(compressedPath)
	if err != nil {
		return filePath, fmt.Errorf("erro ao verificar ficheiro comprimido: %w", err)
	}

	if compressedInfo.Size() > maxSize {
		// Remove o comprimido; o original fica para o chamador limpar.
		os.Remove(compressedPath)
		return filePath, &DownloadError{
			Kind: ErrTooLarge,
			Msg:  fmt.Sprintf("mesmo após compressão, o vídeo ainda excede %dMB (comprimido: %dMB)", maxMB, compressedInfo.Size()/(1024*1024)),
		}
	}

	// Remove o original; a partir daqui o ficheiro útil é o comprimido.
	os.Remove(filePath)
	slog.Info("compressão bem-sucedida", "before_mb", fileSize/(1024*1024), "after_mb", compressedInfo.Size()/(1024*1024))

	return compressedPath, nil
}
