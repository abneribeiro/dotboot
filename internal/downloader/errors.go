package downloader

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ErrorKind classifica as falhas conhecidas do yt-dlp/ffmpeg para que os
// chamadores (bot, CLI) escolham mensagens próprias sem fazer grep de strings.
type ErrorKind int

const (
	ErrUnknown ErrorKind = iota
	ErrLiveNoDVR
	ErrPrivate
	ErrUnavailable
	ErrUnsupportedURL
	ErrTimeout
	ErrTooLarge
)

// DownloadError envolve uma falha de download com a sua classificação.
type DownloadError struct {
	Kind ErrorKind
	// Msg, quando preenchido, substitui a mensagem por omissão do Kind
	// (usado por ErrTooLarge para incluir os tamanhos).
	Msg    string
	Output string
	Err    error
}

func (e *DownloadError) Error() string {
	if e.Msg != "" {
		return e.Msg
	}
	switch e.Kind {
	case ErrLiveNoDVR:
		return "esta live não suporta DVR ou ainda não começou; não é possível fazer o recorte"
	case ErrPrivate:
		return "vídeo privado ou indisponível"
	case ErrUnavailable:
		return "vídeo indisponível ou removido"
	case ErrUnsupportedURL:
		return "URL não suportado pelo yt-dlp"
	case ErrTimeout:
		return "timeout durante o download"
	case ErrTooLarge:
		return "o vídeo excede o tamanho máximo permitido"
	}
	if e.Err != nil {
		return fmt.Sprintf("yt-dlp falhou: %v", e.Err)
	}
	return "erro desconhecido no download"
}

func (e *DownloadError) Unwrap() error { return e.Err }

// classify converte o output do yt-dlp e os erros de execução/contexto
// num DownloadError com o Kind apropriado.
func classify(output string, ctxErr, runErr error) *DownloadError {
	lower := strings.ToLower(output)
	kind := ErrUnknown

	switch {
	case errors.Is(ctxErr, context.DeadlineExceeded):
		kind = ErrTimeout
	case strings.Contains(lower, "does not allow dvr"),
		strings.Contains(lower, "is not a video"),
		strings.Contains(lower, "this live event will begin"):
		kind = ErrLiveNoDVR
	case strings.Contains(lower, "private video"):
		kind = ErrPrivate
	case strings.Contains(lower, "video unavailable"):
		kind = ErrUnavailable
	case strings.Contains(lower, "unsupported url"):
		kind = ErrUnsupportedURL
	}

	return &DownloadError{Kind: kind, Output: output, Err: runErr}
}
