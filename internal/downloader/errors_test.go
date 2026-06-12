package downloader

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestClassify(t *testing.T) {
	runErr := fmt.Errorf("exit status 1")

	tests := []struct {
		name   string
		output string
		ctxErr error
		want   ErrorKind
	}{
		{"live sem DVR", "ERROR: this video does not allow DVR", nil, ErrLiveNoDVR},
		{"não é vídeo", "ERROR: yt-dlp: this URL is not a video", nil, ErrLiveNoDVR},
		{"live ainda não começou", "ERROR: This live event will begin in 2 hours", nil, ErrLiveNoDVR},
		{"vídeo privado", "ERROR: Private video. Sign in if you've been granted access", nil, ErrPrivate},
		{"vídeo indisponível", "ERROR: Video unavailable", nil, ErrUnavailable},
		{"URL não suportado", "ERROR: Unsupported URL: https://example.com", nil, ErrUnsupportedURL},
		{"timeout do contexto", "partial output", context.DeadlineExceeded, ErrTimeout},
		{"erro desconhecido", "ERROR: something completely different", nil, ErrUnknown},
		{"output vazio", "", nil, ErrUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classify(tt.output, tt.ctxErr, runErr)
			if got.Kind != tt.want {
				t.Errorf("classify(%q).Kind = %v, esperado %v", tt.output, got.Kind, tt.want)
			}
			if !errors.Is(got, runErr) {
				t.Error("classify devia preservar o erro original via Unwrap")
			}
		})
	}
}

func TestDownloadErrorMessage(t *testing.T) {
	// Msg personalizado tem precedência sobre a mensagem do Kind.
	withMsg := &DownloadError{Kind: ErrTooLarge, Msg: "excede 25MB (tamanho: 40MB)"}
	if withMsg.Error() != "excede 25MB (tamanho: 40MB)" {
		t.Errorf("Error() = %q, esperado o Msg personalizado", withMsg.Error())
	}

	// Sem Msg, cada Kind tem uma mensagem própria não vazia.
	kinds := []ErrorKind{ErrLiveNoDVR, ErrPrivate, ErrUnavailable, ErrUnsupportedURL, ErrTimeout, ErrTooLarge, ErrUnknown}
	seen := make(map[string]ErrorKind)
	for _, k := range kinds {
		msg := (&DownloadError{Kind: k}).Error()
		if msg == "" {
			t.Errorf("DownloadError{Kind: %v}.Error() vazio", k)
		}
		if prev, dup := seen[msg]; dup {
			t.Errorf("Kinds %v e %v partilham a mensagem %q", prev, k, msg)
		}
		seen[msg] = k
	}
}
