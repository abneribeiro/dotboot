package downloader

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
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

	// ErrLoginRequired: o site exige uma sessão — a verificação "não és um
	// robô" do YouTube, a restrição de idade, conteúdo que pede login.
	// Resolve-se com cookies (ver Options.CookiesFile).
	ErrLoginRequired

	// ErrRateLimited: o site respondeu 429, demasiados pedidos deste IP.
	ErrRateLimited

	// ErrCanceled: o download foi cancelado (encerramento do bot, CTRL+C).
	// Não é uma falha do download.
	ErrCanceled
)

// maxDetailLen limita a linha de erro do yt-dlp mostrada ao utilizador.
const maxDetailLen = 300

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
	case ErrLoginRequired:
		return "o site pediu login ou verificação (são precisos cookies)"
	case ErrRateLimited:
		return "o site está a limitar os pedidos (HTTP 429)"
	case ErrCanceled:
		return "download cancelado"
	}
	if e.Err != nil {
		return fmt.Sprintf("yt-dlp falhou: %v", e.Err)
	}
	return "erro desconhecido no download"
}

func (e *DownloadError) Unwrap() error { return e.Err }

// Detail devolve a última linha "ERROR:" do yt-dlp, sem o prefixo e limitada a
// maxDetailLen caracteres. É o motivo real de uma falha não classificada — o
// que o utilizador precisa de ler em vez de "exit status 1". Vazio quando o
// output não tem nenhuma.
func (e *DownloadError) Detail() string {
	lines := strings.Split(e.Output, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(lines[i]), "ERROR:"); ok {
			return truncateRunes(strings.TrimSpace(rest), maxDetailLen)
		}
	}
	return ""
}

// truncateRunes corta s a n caracteres (não bytes, para não partir um
// carácter UTF-8 a meio), terminando em "…" quando corta.
func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	runes := []rune(s)
	return string(runes[:n-1]) + "…"
}

// classify converte o output do yt-dlp e os erros de execução/contexto
// num DownloadError com o Kind apropriado.
func classify(output string, ctxErr, runErr error) *DownloadError {
	lower := strings.ToLower(output)
	kind := ErrUnknown

	switch {
	case errors.Is(ctxErr, context.DeadlineExceeded):
		kind = ErrTimeout
	case errors.Is(ctxErr, context.Canceled):
		kind = ErrCanceled
	case strings.Contains(lower, "does not allow dvr"),
		strings.Contains(lower, "is not a video"),
		strings.Contains(lower, "this live event will begin"):
		kind = ErrLiveNoDVR
	// Antes das verificações de login: a mensagem de vídeo privado do YouTube
	// também diz "Sign in if you've been granted access".
	case strings.Contains(lower, "private video"):
		kind = ErrPrivate
	case strings.Contains(lower, "sign in to confirm"),
		strings.Contains(lower, "login required"),
		strings.Contains(lower, "cookies for the authentication"):
		kind = ErrLoginRequired
	case strings.Contains(lower, "http error 429"),
		strings.Contains(lower, "too many requests"):
		kind = ErrRateLimited
	case strings.Contains(lower, "video unavailable"):
		kind = ErrUnavailable
	case strings.Contains(lower, "unsupported url"):
		kind = ErrUnsupportedURL
	}

	return &DownloadError{Kind: kind, Output: output, Err: runErr}
}
