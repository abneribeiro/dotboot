// Package bot implementa o bot Discord: sessão, comando /baixar e o ciclo
// de vida com encerramento gracioso em duas fases.
package bot

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bwmarrin/discordgo"

	"dotboot/internal/config"
)

// command define o Slash Command /baixar com os seus parâmetros.
var command = &discordgo.ApplicationCommand{
	Name:        "baixar",
	Description: "Descarrega um vídeo de YouTube, X.com ou Instagram e envia-o para o canal.",
	Options: []*discordgo.ApplicationCommandOption{
		{
			Type:        discordgo.ApplicationCommandOptionString,
			Name:        "url",
			Description: "Link do vídeo (YouTube, X.com, Instagram).",
			Required:    true,
		},
		{
			Type:        discordgo.ApplicationCommandOptionString,
			Name:        "inicio",
			Description: "Tempo de início do recorte (ex: 01:30 ou 00:01:30). Opcional.",
			Required:    false,
		},
		{
			Type:        discordgo.ApplicationCommandOptionString,
			Name:        "fim",
			Description: "Tempo de fim do recorte (ex: 02:00 ou 00:02:00). Opcional.",
			Required:    false,
		},
	},
}

// Bot agrega a sessão Discord e o estado de controlo de downloads.
type Bot struct {
	cfg     *config.Config
	log     *slog.Logger
	session *discordgo.Session

	// sem limita downloads simultâneos (cap = cfg.MaxConcurrent).
	sem chan struct{}
	// wg conta downloads em curso (incluindo os que esperam no semáforo).
	wg sync.WaitGroup
	// dlCtx é o pai de todos os downloads. Deriva de context.Background()
	// — nunca do contexto de sinal — para que um SIGTERM não mate os
	// downloads em curso antes do período de graça (shutdown em 2 fases).
	dlCtx  context.Context
	dlStop context.CancelFunc
	// closing faz o handler rejeitar novos pedidos durante o encerramento.
	closing atomic.Bool
}

// New cria a sessão Discord e o Bot, sem abrir a ligação.
func New(cfg *config.Config, log *slog.Logger) (*Bot, error) {
	// O prefixo "Bot " é obrigatório para autenticação com tokens de bot.
	dg, err := discordgo.New("Bot " + cfg.DiscordToken)
	if err != nil {
		return nil, fmt.Errorf("erro ao criar a sessão do Discord: %w", err)
	}

	// Cliente HTTP customizado para melhor estabilidade de rede.
	dg.Client = createCustomHTTPClient()

	// Desativa tracking de estado desnecessário para este bot.
	dg.StateEnabled = true
	dg.State.TrackVoice = false
	dg.State.TrackPresences = false
	dg.State.TrackEmojis = false

	// Para Slash Commands basta IntentsGuilds.
	dg.Identify.Intents = discordgo.IntentsGuilds

	dlCtx, dlStop := context.WithCancel(context.Background())
	b := &Bot{
		cfg:     cfg,
		log:     log,
		session: dg,
		sem:     make(chan struct{}, cfg.MaxConcurrent),
		dlCtx:   dlCtx,
		dlStop:  dlStop,
	}
	dg.AddHandler(b.handleInteraction)

	return b, nil
}

// Run liga ao Discord, regista o comando e bloqueia até o contexto (de sinal)
// ser cancelado. O encerramento é em duas fases: primeiro espera ShutdownGrace
// pelos downloads em curso; só depois os cancela à força.
func (b *Bot) Run(ctx context.Context) error {
	defer b.dlStop()

	if err := connectWithRetry(b.session, b.log); err != nil {
		return fmt.Errorf("erro ao abrir a ligação com o Discord: %w", err)
	}
	defer b.session.Close()

	// Comandos globais podem demorar até 1 hora a propagar.
	registeredCmd, err := registerCommandWithRetry(b.session, b.log)
	if err != nil {
		return fmt.Errorf("erro ao registar o comando /baixar: %w", err)
	}

	b.log.Info("bot está a correr; pressiona CTRL+C para terminar")
	<-ctx.Done()

	b.log.Info("a encerrar o bot...")
	b.closing.Store(true)

	// Fase 1: dá tempo aos downloads em curso para terminarem.
	if !waitWithTimeout(&b.wg, b.cfg.ShutdownGrace) {
		// Fase 2: cancela os downloads (SIGINT ao yt-dlp/ffmpeg) e espera
		// um pouco mais pela limpeza dos ficheiros temporários.
		b.log.Warn("downloads em curso não terminaram no período de graça, a cancelar", "grace", b.cfg.ShutdownGrace)
		b.dlStop()
		waitWithTimeout(&b.wg, 5*time.Second)
	}

	// Remove o Slash Command ao encerrar para manter o Discord limpo.
	b.deleteCommand(registeredCmd)
	return nil
}

// deleteCommand remove o slash command com um timeout para não bloquear
// o encerramento indefinidamente.
func (b *Bot) deleteCommand(cmd *discordgo.ApplicationCommand) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- b.session.ApplicationCommandDelete(b.session.State.User.ID, "", cmd.ID)
	}()

	select {
	case err := <-done:
		if err != nil {
			b.log.Warn("erro ao remover o comando /baixar", "err", err)
		} else {
			b.log.Info("comando /baixar removido com sucesso")
		}
	case <-ctx.Done():
		b.log.Warn("timeout ao tentar remover o comando /baixar")
	}
}

// waitWithTimeout espera pelo WaitGroup até d. Devolve true se terminou a tempo.
func waitWithTimeout(wg *sync.WaitGroup, d time.Duration) bool {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return true
	case <-time.After(d):
		return false
	}
}
