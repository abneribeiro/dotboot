package downloader

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
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
		{"cancelamento não é uma falha", "ERROR: Interrupted by user", context.Canceled, ErrCanceled},
		{"bot-check do YouTube", "ERROR: [youtube] abc: Sign in to confirm you’re not a bot. Use --cookies-from-browser or --cookies for the authentication.", nil, ErrLoginRequired},
		{"restrição de idade", "ERROR: [youtube] abc: Sign in to confirm your age. This video may be inappropriate for some users.", nil, ErrLoginRequired},
		{"login do Instagram", "ERROR: [Instagram] abc: Requested content is not available, rate-limit reached or login required.", nil, ErrLoginRequired},
		// A mensagem de vídeo privado também diz "Sign in": tem de continuar privado.
		{"privado com sign in", "ERROR: [youtube] abc: Private video. Sign in if you've been granted access to this video", nil, ErrPrivate},
		{"limite de pedidos", "ERROR: unable to download webpage: HTTP Error 429: Too Many Requests", nil, ErrRateLimited},
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
	kinds := []ErrorKind{ErrLiveNoDVR, ErrPrivate, ErrUnavailable, ErrUnsupportedURL, ErrTimeout, ErrTooLarge,
		ErrLoginRequired, ErrRateLimited, ErrCanceled, ErrUnknown}
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

func TestDownloadErrorDetail(t *testing.T) {
	long := "ERROR: " + strings.Repeat("x", 400)

	tests := []struct {
		name   string
		output string
		want   string
	}{
		{"sem output", "", ""},
		{"sem linha de erro", "[download] 100%\n", ""},
		{
			"última linha ERROR, sem o prefixo",
			"WARNING: aviso\nERROR: primeiro\n[info] algo\nERROR: [generic] Unable to download webpage: HTTP Error 403: Forbidden\n",
			"[generic] Unable to download webpage: HTTP Error 403: Forbidden",
		},
		{"linha comprida é cortada", long, strings.Repeat("x", maxDetailLen-1) + "…"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := (&DownloadError{Output: tt.output}).Detail()
			if got != tt.want {
				t.Errorf("Detail() = %q, esperado %q", got, tt.want)
			}
		})
	}
}

// Cortar por bytes podia partir um carácter acentuado a meio e mostrar UTF-8
// inválido no terminal.
func TestTruncateRunesNaoPartePalavrasAcentuadas(t *testing.T) {
	got := truncateRunes(strings.Repeat("ã", 10), 5)
	if got != "ãããã…" {
		t.Errorf("truncateRunes = %q, esperado %q", got, "ãããã…")
	}
	if !utf8.ValidString(got) {
		t.Errorf("truncateRunes produziu UTF-8 inválido: %q", got)
	}
}
