package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	// maxFileSize define o limite máximo de tamanho de ficheiro (25 MB) imposto pelo Discord
	// para uploads sem Nitro.
	maxFileSize = 25 * 1024 * 1024 // 25 MB em bytes

	// compressionThreshold define quando começar a tentar compressão (20 MB)
	// para dar margem antes de atingir o limite
	compressionThreshold = 20 * 1024 * 1024 // 20 MB em bytes

	// downloadTimeout define o tempo máximo de espera para um download via yt-dlp.
	// Downloads com recortes de vídeos longos ou lives podem demorar mais,
	// por isso usamos 10 minutos.
	downloadTimeout = 10 * time.Minute
)

// compressVideo attempts to compress a video file using FFmpeg
// Returns the path to the compressed file or an error
func compressVideo(inputPath string) (string, error) {
	// Generate a unique name for the compressed file
	uniqueID := fmt.Sprintf("%d", time.Now().UnixNano())
	outputPath := filepath.Join(os.TempDir(), fmt.Sprintf("dotboot_compressed_%s.mp4", uniqueID))

	// Create context with timeout for FFmpeg operation
	ctx, cancel := context.WithTimeout(context.Background(), downloadTimeout)
	defer cancel()

	// FFmpeg command to compress video:
	// -i input: input file
	// -c:v libx264: use H.264 codec for video
	// -crf 28: constant rate factor for compression (higher = more compression)
	// -preset fast: encoding speed vs compression efficiency tradeoff
	// -c:a aac: use AAC codec for audio
	// -b:a 128k: audio bitrate 128kbps
	// -movflags +faststart: optimize for web playback
	// -y: overwrite output file if it exists
	args := []string{
		"-i", inputPath,
		"-c:v", "libx264",
		"-crf", "28",
		"-preset", "fast",
		"-c:a", "aac",
		"-b:a", "128k",
		"-movflags", "+faststart",
		"-y", // overwrite if exists
		outputPath,
	}

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	output, err := cmd.CombinedOutput()

	if err != nil {
		log.Printf("FFmpeg compression failed: %v\nOutput: %s", err, string(output))
		return "", fmt.Errorf("falhou ao comprimir vídeo: %w", err)
	}

	// Verify the compressed file exists and check its size
	info, err := os.Stat(outputPath)
	if err != nil {
		return "", fmt.Errorf("erro ao verificar ficheiro comprimido: %w", err)
	}

	log.Printf("Vídeo comprimido com sucesso: %s (tamanho: %d bytes)", outputPath, info.Size())
	return outputPath, nil
}

// validateAndProcessVideo checks file size and compresses if necessary
func validateAndProcessVideo(filePath string) (string, error) {
	info, err := os.Stat(filePath)
	if err != nil {
		return filePath, fmt.Errorf("erro ao verificar o tamanho do ficheiro: %w", err)
	}

	fileSize := info.Size()
	log.Printf("Tamanho do ficheiro descarregado: %d bytes (%.2f MB)", fileSize, float64(fileSize)/(1024*1024))

	// Se o ficheiro está dentro do limite, usa-o directamente
	if fileSize <= maxFileSize {
		return filePath, nil
	}

	// Se excede o limite, tenta comprimir
	log.Printf("Ficheiro excede %d MB, a tentar compressão...", maxFileSize/(1024*1024))

	compressedPath, err := compressVideo(filePath)
	if err != nil {
		// Se a compressão falhar, retorna erro com o tamanho original
		return filePath, fmt.Errorf("o vídeo excede o limite de 25MB do Discord (tamanho: %d MB) e a compressão falhou: %v", fileSize/(1024*1024), err)
	}

	// Verifica se a compressão foi suficiente
	compressedInfo, err := os.Stat(compressedPath)
	if err != nil {
		return filePath, fmt.Errorf("erro ao verificar ficheiro comprimido: %w", err)
	}

	if compressedInfo.Size() > maxFileSize {
		// Remove o ficheiro comprimido se ainda for grande demais
		os.Remove(compressedPath)
		return filePath, fmt.Errorf("mesmo após compressão, o vídeo ainda excede 25MB (comprimido: %d MB)", compressedInfo.Size()/(1024*1024))
	}

	// Remove o ficheiro original já que agora temos a versão comprimida
	os.Remove(filePath)
	log.Printf("Compressão bem-sucedida: %d MB → %d MB", fileSize/(1024*1024), compressedInfo.Size()/(1024*1024))

	return compressedPath, nil
}
// DownloadVideo usa o yt-dlp para descarregar um vídeo a partir do URL fornecido.
// Se startTime e endTime forem fornecidos (não vazios), faz um recorte do vídeo
// usando --download-sections do yt-dlp.
// Inclui validação de tamanho e compressão automática para ficheiros grandes.
// Devolve o caminho completo do ficheiro descarregado ou um erro.
// O chamador é responsável por eliminar o ficheiro após a utilização.
func DownloadVideo(url string, startTime string, endTime string) (string, error) {
	log.Printf("Starting download for URL: %s (start: %s, end: %s)", url, startTime, endTime)

	// Gera um nome de ficheiro único usando um timestamp com nanossegundos
	// para evitar colisões entre goroutines concorrentes.
	uniqueID := fmt.Sprintf("%d", time.Now().UnixNano())
	outputTemplate := filepath.Join(os.TempDir(), fmt.Sprintf("dotboot_%s.%%(ext)s", uniqueID))

	// Cria um contexto com timeout para garantir que o processo yt-dlp
	// não fica a correr indefinidamente em caso de problemas de rede.
	ctx, cancel := context.WithTimeout(context.Background(), downloadTimeout)
	defer cancel()

	// Constrói os argumentos base do yt-dlp:
	// -f "best[filesize<25M]/best" — seleciona a melhor qualidade abaixo de 25 MB,
	//                                com fallback para a melhor disponível.
	// --merge-output-format mp4    — garante que o output é sempre MP4 para
	//                                compatibilidade com o Discord.
	// --no-playlist               — evita descarregar playlists inteiras quando
	//                                o link aponta para um vídeo dentro de uma playlist.
	// -o <template>               — define o caminho do ficheiro de saída com
	//                                nome único para evitar colisões.
	args := []string{
		"-f", "best[filesize<25M]/best",
		"--merge-output-format", "mp4",
		"--no-playlist",
		"-o", outputTemplate,
		"--no-warnings",  // Reduce noise in logs
		"--quiet",        // Only show errors
	}

	// Se startTime e endTime forem fornecidos, adiciona a flag de recorte.
	// A sintaxe do yt-dlp é: --download-sections "*inicio-fim"
	// --force-keyframes-at-cuts garante cortes precisos nos timestamps pedidos.
	if startTime != "" && endTime != "" {
		section := fmt.Sprintf("*%s-%s", startTime, endTime)
		args = append(args, "--download-sections", section, "--force-keyframes-at-cuts")
		log.Printf("Adding video section: %s", section)
	}

	// O URL é sempre o último argumento.
	args = append(args, url)

	cmd := exec.CommandContext(ctx, "yt-dlp", args...)

	// Captura a saída do yt-dlp para fins de debug e deteção de erros.
	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	if err != nil {
		log.Printf("yt-dlp failed with error: %v", err)
		if len(outputStr) > 0 {
			log.Printf("yt-dlp output:\n%s", outputStr)
		}

		// Enhanced error handling for common issues
		outputLower := strings.ToLower(outputStr)
		switch {
		case strings.Contains(outputLower, "does not allow dvr") ||
			 strings.Contains(outputLower, "is not a video") ||
			 strings.Contains(outputLower, "this live event will begin"):
			return "", fmt.Errorf("esta live não suporta DVR ou ainda não começou; não é possível fazer o recorte")
		case strings.Contains(outputLower, "private video"):
			return "", fmt.Errorf("vídeo privado ou indisponível")
		case strings.Contains(outputLower, "video unavailable"):
			return "", fmt.Errorf("vídeo indisponível ou removido")
		case strings.Contains(outputLower, "unsupported url"):
			return "", fmt.Errorf("URL não suportado pelo yt-dlp")
		case ctx.Err() == context.DeadlineExceeded:
			return "", fmt.Errorf("timeout durante o download (>%v)", downloadTimeout)
		default:
			return "", fmt.Errorf("yt-dlp falhou: %w", err)
		}
	}

	// Após a execução, resolve o caminho real do ficheiro.
	// O yt-dlp substitui %(ext)s pela extensão real (normalmente .mp4).
	// Usamos Glob para encontrar o ficheiro independentemente da extensão.
	expectedPath := filepath.Join(os.TempDir(), fmt.Sprintf("dotboot_%s.*", uniqueID))
	matches, err := filepath.Glob(expectedPath)
	if err != nil || len(matches) == 0 {
		return "", fmt.Errorf("ficheiro descarregado não encontrado em: %s", expectedPath)
	}

	// Usa o primeiro ficheiro encontrado (normalmente só existe um).
	downloadedFile := matches[0]

	// Processa o ficheiro (validação de tamanho + compressão se necessário)
	processedFile, err := validateAndProcessVideo(downloadedFile)
	if err != nil {
		return processedFile, err // retorna o caminho para limpeza mesmo com erro
	}

	log.Printf("Vídeo processado com sucesso: %s", processedFile)
	return processedFile, nil
}
