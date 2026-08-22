package bot

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestIsNetworkError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil não é erro de rede", nil, false},
		{"timeout", errors.New("i/o timeout"), true},
		{"ligação recusada", errors.New("dial tcp: connection refused"), true},
		{"ligação reposta", errors.New("read: connection reset by peer"), true},
		{"handshake TLS", errors.New("net/http: TLS handshake timeout"), true},
		{"host inexistente", errors.New("lookup discord.com: no such host"), true},
		{"token inválido não é de rede", errors.New("HTTP 401 Unauthorized"), false},
		{"permissões não são de rede", errors.New("HTTP 403 Forbidden"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isNetworkError(tt.err); got != tt.want {
				t.Errorf("isNetworkError(%v) = %v, esperado %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestExponentialBackoff(t *testing.T) {
	// Cresce a dobrar a partir de baseRetryDelay.
	for attempt, want := range map[int]time.Duration{
		0: baseRetryDelay,
		1: 2 * baseRetryDelay,
		2: 4 * baseRetryDelay,
		3: 8 * baseRetryDelay,
	} {
		if got := exponentialBackoff(attempt); got != want {
			t.Errorf("exponentialBackoff(%d) = %v, esperado %v", attempt, got, want)
		}
	}

	// E nunca ultrapassa o tecto, por muito alta que seja a tentativa.
	for _, attempt := range []int{maxRetries, maxRetries + 10, 100} {
		if got := exponentialBackoff(attempt); got != maxRetryDelay {
			t.Errorf("exponentialBackoff(%d) = %v, esperado o tecto %v", attempt, got, maxRetryDelay)
		}
	}
}

// Um SIGTERM durante o backoff tem de ser observado de imediato, não ao fim
// dos 60 segundos de espera.
func TestSleepCtxRespeitaCancelamento(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	err := sleepCtx(ctx, time.Minute)
	elapsed := time.Since(start)

	if !errors.Is(err, context.Canceled) {
		t.Errorf("sleepCtx devolveu %v, esperado context.Canceled", err)
	}
	if elapsed > time.Second {
		t.Errorf("sleepCtx demorou %v a notar o cancelamento", elapsed)
	}
}

func TestSleepCtxEsperaOTempoPedido(t *testing.T) {
	start := time.Now()
	if err := sleepCtx(context.Background(), 20*time.Millisecond); err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 20*time.Millisecond {
		t.Errorf("sleepCtx voltou ao fim de %v, esperado >= 20ms", elapsed)
	}
}
