package tui

import (
	"context"
	"io"
	"log/slog"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/abneribeiro/dotboot/internal/config"
)

// shutdownTimeout é quanto se espera, ao sair, que os trabalhos cancelados
// terminem — o yt-dlp e o ffmpeg recebem SIGINT e têm até 10s (WaitDelay)
// para fechar os ficheiros.
const shutdownTimeout = 15 * time.Second

// Run abre a TUI e só volta quando o utilizador sai (ou ctx é cancelado).
// Os trabalhos ainda a correr são cancelados à saída, e espera-se por eles:
// sair antes deixava processos órfãos.
func Run(ctx context.Context, cfg *config.Config, version string) error {
	// Os logs do slog iriam parar ao meio do ecrã: na TUI, tudo o que
	// interessa ao utilizador chega pelos eventos dos trabalhos.
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer slog.SetDefault(prev)

	m := New(cfg, version)
	_, err := tea.NewProgram(m, tea.WithContext(ctx)).Run()
	m.queue.shutdown(shutdownTimeout)
	if err != nil && ctx.Err() != nil {
		// Um SIGTERM fecha a TUI pelo contexto: não é um erro.
		return nil
	}
	return err
}
