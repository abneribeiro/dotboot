package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

const (
	// maxFileSize define o limite máximo de tamanho de ficheiro (25 MB) imposto pelo Discord
	// para uploads sem Nitro.
	maxFileSize = 25 * 1024 * 1024 // 25 MB em bytes

	// downloadTimeout define o tempo máximo de espera para um download via yt-dlp.
	// Downloads que excedam este limite serão cancelados automaticamente.
	downloadTimeout = 5 * time.Minute
)

// DownloadVideo usa o yt-dlp para descarregar um vídeo a partir do URL fornecido.
// Devolve o caminho completo do ficheiro descarregado ou um erro.
// O chamador é responsável por eliminar o ficheiro após a utilização.
func DownloadVideo(url string) (string, error) {
	// Gera um nome de ficheiro único usando um timestamp com nanossegundos
	// para evitar colisões entre goroutines concorrentes.
	uniqueID := fmt.Sprintf("%d", time.Now().UnixNano())
	outputTemplate := filepath.Join(os.TempDir(), fmt.Sprintf("dotboot_%s.%%(ext)s", uniqueID))

	// Cria um contexto com timeout para garantir que o processo yt-dlp
	// não fica a correr indefinidamente em caso de problemas de rede.
	ctx, cancel := context.WithTimeout(context.Background(), downloadTimeout)
	defer cancel()

	// Constrói o comando yt-dlp com os argumentos necessários:
	// -f "best[filesize<25M]/best" — seleciona a melhor qualidade abaixo de 25 MB,
	//                                  com fallback para a melhor disponível.
	// --merge-output-format mp4    — garante que o output é sempre MP4 para
	//                                  compatibilidade com o Discord.
	// --no-playlist               — evita descarregar playlists inteiras quando
	//                                  o link aponta para um vídeo dentro de uma playlist.
	// -o <template>               — define o caminho do ficheiro de saída com
	//                                  nome único para evitar colisões.
	cmd := exec.CommandContext(ctx, "yt-dlp",
		"-f", "best[filesize<25M]/best",
		"--merge-output-format", "mp4",
		"--no-playlist",
		"-o", outputTemplate,
		url,
	)

	// Captura a saída do yt-dlp para fins de debug.
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("yt-dlp output:\n%s", string(output))
		return "", fmt.Errorf("yt-dlp falhou: %w", err)
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

	// Verifica se o ficheiro respeita o limite de tamanho do Discord (25 MB).
	info, err := os.Stat(downloadedFile)
	if err != nil {
		return downloadedFile, fmt.Errorf("erro ao verificar o tamanho do ficheiro: %w", err)
	}

	if info.Size() > maxFileSize {
		// O ficheiro excede o limite. É devolvido o caminho para permitir a limpeza
		// pelo chamador (via defer os.Remove), mas com um erro explícito.
		return downloadedFile, fmt.Errorf("o vídeo excede o limite de 25MB do Discord (tamanho: %d MB)", info.Size()/(1024*1024))
	}

	log.Printf("Vídeo descarregado com sucesso: %s (tamanho: %d bytes)", downloadedFile, info.Size())
	return downloadedFile, nil
}
