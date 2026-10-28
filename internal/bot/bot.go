// Package bot implementa o bot Discord: sessão, comando /baixar e o ciclo
// de vida com encerramento gracioso em duas fases.
package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/abneribeiro/dotboot/internal/config"
)

// commands é o conjunto completo de Slash Commands do bot. O
// BulkOverwrite repõe exatamente esta lista em cada arranque.
var commands = []*discordgo.ApplicationCommand{{
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
}}

// Bot agrega a sessão Discord e o estado de controlo de downloads.
type Bot struct {
	cfg     *config.Config
	log     *slog.Logger
	session *discordgo.Session

	// sem limita downloads simultâneos (cap = cfg.MaxConcurrent).
	sem chan struct{}
	// users limita os downloads de cada utilizador (cfg.MaxPerUser).
	users *userLimiter
	// wg conta downloads em curso (incluindo os que esperam no semáforo).
	wg sync.WaitGroup
	// dlCtx é o pai de todos os downloads. Deriva de context.Background()
	// — nunca do contexto de sinal — para que um SIGTERM não mate os
	// downloads em curso antes do período de graça (shutdown em 2 fases).
	dlCtx  context.Context
	dlStop context.CancelFunc

	// mu protege closing e serializa-o com o wg.Add (ver track).
	mu sync.Mutex
	// closing faz o handler rejeitar novos pedidos durante o encerramento.
	closing bool
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

	// Mantém o estado (o nível de boost vem de lá — ver limits.go), mas sem
	// o tracking que este bot não usa.
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
		users:   newUserLimiter(cfg.MaxPerUser),
		dlCtx:   dlCtx,
		dlStop:  dlStop,
	}
	dg.AddHandler(b.handleInteraction)

	return b, nil
}

// errGatewayDead é a causa do encerramento pedido pelo watchdog do heartbeat.
var errGatewayDead = errors.New("ligação ao Discord perdida")

// Run liga ao Discord, regista o comando e bloqueia até o contexto (de sinal)
// ser cancelado. O encerramento é em duas fases: primeiro espera ShutdownGrace
// pelos downloads em curso; só depois os cancela à força.
//
// Devolve erro também quando o gateway fica em baixo mais do que
// gatewayDeadAfter: sair com erro é o que leva a restart policy do Docker a
// reiniciar o bot, coisa que um container "unhealthy" por si só não faz.
func (b *Bot) Run(ctx context.Context) error {
	defer b.dlStop()

	if err := connectWithRetry(ctx, b.session, b.log); err != nil {
		return fmt.Errorf("erro ao abrir a ligação com o Discord: %w", err)
	}
	defer b.session.Close()

	// Comandos globais podem demorar até 1 hora a propagar — daí o registo
	// ser idempotente e o comando NUNCA ser apagado no encerramento: um
	// reinício deixaria o /baixar invisível durante essa hora.
	if err := registerCommandsWithRetry(ctx, b.session, b.cfg.GuildID, b.log); err != nil {
		return fmt.Errorf("erro ao registar o comando /baixar: %w", err)
	}

	// runCtx junta o sinal do processo e o watchdog do gateway: qualquer um
	// dos dois inicia o mesmo encerramento em duas fases.
	runCtx, cancelRun := context.WithCancelCause(ctx)
	defer cancelRun(nil)

	// Sinal de vida para o healthcheck do container, que só sabe ver o
	// ficheiro (não tem acesso ao estado interno do bot).
	stopHeartbeat := b.startHeartbeat(cancelRun)
	defer stopHeartbeat()

	b.log.Info("bot está a correr; pressiona CTRL+C para terminar")
	<-runCtx.Done()

	b.log.Info("a encerrar o bot...")
	b.beginShutdown()

	// Fase 1: dá tempo aos downloads em curso para terminarem.
	if !waitWithTimeout(&b.wg, b.cfg.ShutdownGrace) {
		// Fase 2: cancela os downloads (SIGINT ao yt-dlp/ffmpeg) e espera
		// um pouco mais pela limpeza dos ficheiros temporários.
		b.log.Warn("downloads em curso não terminaram no período de graça, a cancelar", "grace", b.cfg.ShutdownGrace)
		b.dlStop()
		waitWithTimeout(&b.wg, 5*time.Second)
	}

	// Um SIGTERM é um encerramento pedido, não um erro; o watchdog é.
	if cause := context.Cause(runCtx); errors.Is(cause, errGatewayDead) {
		return cause
	}
	return nil
}

// track regista um download no WaitGroup, a não ser que o encerramento já
// tenha começado. O mutex fecha a janela em que um pedido validado antes do
// SIGTERM chegava ao wg.Add depois de o Run já estar no wg.Wait — um uso que
// o WaitGroup não permite e que deixava o download fora da contagem.
func (b *Bot) track() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closing {
		return false
	}
	b.wg.Add(1)
	return true
}

func (b *Bot) isClosing() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.closing
}

func (b *Bot) beginShutdown() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closing = true
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

// heartbeatFile é o ficheiro que o healthcheck do container observa.
const heartbeatFile = "dotboot.alive"

// heartbeatInterval tem de ser bem mais curto do que HeartbeatMaxAge, para
// uma escrita atrasada não dar um falso negativo.
const heartbeatInterval = 30 * time.Second

// HeartbeatMaxAge é a idade máxima do heartbeat para o bot contar como vivo:
// três intervalos.
const HeartbeatMaxAge = 3 * heartbeatInterval

// gatewayStaleAfter é a idade máxima do último ACK do gateway para o bot se
// considerar ligado. O Discord pede um heartbeat a cada ~41s, por isso 2
// minutos são três falhas seguidas.
const gatewayStaleAfter = 2 * time.Minute

// gatewayDeadAfter é quanto tempo o bot tolera sem gateway antes de desistir.
// O discordgo volta a ligar sozinho na grande maioria dos casos; isto é a rede
// de segurança para quando não consegue.
const gatewayDeadAfter = 10 * time.Minute

// HeartbeatPath é o ficheiro que o bot atualiza enquanto está ligado ao
// Discord. O subcomando healthcheck usa-o com a mesma configuração, por isso
// um DOTBOOT_TEMP_DIR diferente de /tmp já não dessincroniza os dois.
func HeartbeatPath(tempDir string) string {
	return filepath.Join(tempDir, heartbeatFile)
}

// CheckHeartbeat devolve erro se o heartbeat não existir ou for mais antigo
// do que maxAge.
func CheckHeartbeat(path string, maxAge time.Duration, now time.Time) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("sem heartbeat: %w", err)
	}
	if age := now.Sub(info.ModTime()); age > maxAge {
		return fmt.Errorf("heartbeat parado há %s", age.Round(time.Second))
	}
	return nil
}

// gatewayHealthy reporta se o último ACK do gateway é recente.
func gatewayHealthy(lastAck, now time.Time) bool {
	return !lastAck.IsZero() && now.Sub(lastAck) < gatewayStaleAfter
}

// lastHeartbeatAck lê o instante do último ACK do gateway, que o discordgo
// escreve sob o lock da sessão.
func (b *Bot) lastHeartbeatAck() time.Time {
	b.session.RLock()
	defer b.session.RUnlock()
	return b.session.LastHeartbeatAck
}

// startHeartbeat atualiza periodicamente a data do ficheiro de heartbeat
// enquanto o gateway responde — e só então, para o healthcheck não dar como
// vivo um bot desligado. Se o gateway ficar em baixo mais do que
// gatewayDeadAfter, chama fail. Devolve a função que para o ciclo.
func (b *Bot) startHeartbeat(fail context.CancelCauseFunc) func() {
	path := HeartbeatPath(b.cfg.TempDir)
	// Só a goroutine do ciclo (e a primeira chamada, antes dela) lê e escreve
	// unhealthySince, por isso não precisa de lock.
	var unhealthySince time.Time

	tick := func(now time.Time) {
		if gatewayHealthy(b.lastHeartbeatAck(), now) {
			if !unhealthySince.IsZero() {
				b.log.Info("gateway do Discord recuperado")
				unhealthySince = time.Time{}
			}
			if err := os.WriteFile(path, []byte(now.UTC().Format(time.RFC3339)+"\n"), 0o644); err != nil {
				b.log.Debug("não foi possível escrever o heartbeat", "path", path, "err", err)
			}
			return
		}

		if unhealthySince.IsZero() {
			unhealthySince = now
			b.log.Warn("o gateway do Discord não responde; o heartbeat do container fica parado")
			return
		}
		if down := now.Sub(unhealthySince); down >= gatewayDeadAfter {
			b.log.Error("sem ligação ao Discord, a encerrar para o bot ser reiniciado", "down", down.Round(time.Second))
			fail(fmt.Errorf("%w há %s", errGatewayDead, down.Round(time.Second)))
		}
	}
	tick(time.Now())

	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(heartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case now := <-ticker.C:
				tick(now)
			case <-done:
				os.Remove(path)
				return
			}
		}
	}()

	return func() { close(done) }
}
