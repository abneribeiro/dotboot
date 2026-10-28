package bot

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGatewayHealthy(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		lastAck time.Time
		want    bool
	}{
		{"ACK de agora", now, true},
		{"ACK de há um intervalo do Discord", now.Add(-41 * time.Second), true},
		{"três ACKs falhados", now.Add(-gatewayStaleAfter), false},
		{"muito antigo", now.Add(-time.Hour), false},
		// Antes do primeiro Hello o discordgo ainda não escreveu nada.
		{"nunca recebeu ACK", time.Time{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := gatewayHealthy(tt.lastAck, now); got != tt.want {
				t.Errorf("gatewayHealthy(%v) = %v, esperado %v", tt.lastAck, got, tt.want)
			}
		})
	}
}

func TestCheckHeartbeat(t *testing.T) {
	dir := t.TempDir()
	path := HeartbeatPath(dir)
	now := time.Now()

	if err := CheckHeartbeat(path, HeartbeatMaxAge, now); err == nil {
		t.Error("sem ficheiro de heartbeat o bot não pode contar como vivo")
	}

	if err := os.WriteFile(path, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, now, now.Add(-30*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := CheckHeartbeat(path, HeartbeatMaxAge, now); err != nil {
		t.Errorf("heartbeat de há 30s devia passar: %v", err)
	}

	if err := os.Chtimes(path, now, now.Add(-2*HeartbeatMaxAge)); err != nil {
		t.Fatal(err)
	}
	if err := CheckHeartbeat(path, HeartbeatMaxAge, now); err == nil {
		t.Error("heartbeat parado devia falhar")
	}
}

// O healthcheck e o bot têm de olhar para o mesmo ficheiro a partir do mesmo
// TempDir — era o /tmp fixo no Dockerfile que os dessincronizava.
func TestHeartbeatPathSegueOTempDir(t *testing.T) {
	if got, want := HeartbeatPath("/scratch"), filepath.Join("/scratch", heartbeatFile); got != want {
		t.Errorf("HeartbeatPath = %q, esperado %q", got, want)
	}
}

// Depois de beginShutdown, track tem de recusar: é o que impede um wg.Add
// depois de o Run já estar à espera no wg.Wait.
func TestTrackRecusaDepoisDoEncerramento(t *testing.T) {
	b := &Bot{}
	if !b.track() {
		t.Fatal("track devia aceitar antes do encerramento")
	}
	b.wg.Done()

	b.beginShutdown()
	if !b.isClosing() {
		t.Error("isClosing devia ser true depois de beginShutdown")
	}
	if b.track() {
		t.Error("track devia recusar depois de beginShutdown")
	}
}
