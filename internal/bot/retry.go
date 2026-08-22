package bot

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

const (
	maxRetries     = 5
	baseRetryDelay = time.Second
	maxRetryDelay  = 60 * time.Second
	connectTimeout = 30 * time.Second

	// responseHeaderTimeout limita a espera pela resposta do Discord. Em Go
	// este relógio só arranca DEPOIS de o corpo do pedido estar escrito, por
	// isso não corta uploads grandes.
	responseHeaderTimeout = 60 * time.Second

	// clientTimeout cobre o pedido inteiro, incluindo a escrita do corpo —
	// é por isso que não pode ser 30s: um vídeo de 100MB numa ligação
	// modesta demora muito mais do que isso a subir. 15 minutos é a
	// validade do token da interação: nada sobrevive à sua utilidade.
	clientTimeout = 15 * time.Minute
)

// createCustomHTTPClient cria um cliente HTTP otimizado para ligações ao Discord.
func createCustomHTTPClient() *http.Client {
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   connectTimeout,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: responseHeaderTimeout,
		ExpectContinueTimeout: 1 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
	}

	return &http.Client{
		Transport: transport,
		Timeout:   clientTimeout,
	}
}

// isNetworkError verifica se um erro é de rede e potencialmente transitório.
func isNetworkError(err error) bool {
	if err == nil {
		return false
	}

	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "timeout") ||
		strings.Contains(errStr, "connection refused") ||
		strings.Contains(errStr, "connection reset") ||
		strings.Contains(errStr, "tls handshake timeout") ||
		strings.Contains(errStr, "dial") ||
		strings.Contains(errStr, "no such host")
}

// exponentialBackoff calcula o atraso com backoff exponencial limitado.
func exponentialBackoff(attempt int) time.Duration {
	if attempt >= maxRetries {
		return maxRetryDelay
	}

	delay := time.Duration(math.Pow(2, float64(attempt))) * baseRetryDelay
	if delay > maxRetryDelay {
		delay = maxRetryDelay
	}

	return delay
}

// sleepCtx espera d, ou devolve o erro do contexto se este for cancelado
// primeiro. Um time.Sleep simples faria um SIGTERM esperar até 60s.
func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// connectWithRetry tenta abrir a ligação ao Discord com backoff exponencial.
// Erros que não sejam de rede param as tentativas imediatamente.
func connectWithRetry(ctx context.Context, dg *discordgo.Session, log *slog.Logger) error {
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			delay := exponentialBackoff(attempt - 1)
			log.Info("a aguardar antes de nova tentativa de ligação", "attempt", attempt+1, "max", maxRetries, "delay", delay)
			if err := sleepCtx(ctx, delay); err != nil {
				return err
			}
		}

		log.Info("a tentar estabelecer ligação com o Discord", "attempt", attempt+1, "max", maxRetries)

		err := dg.Open()
		if err == nil {
			log.Info("ligação estabelecida com sucesso")
			return nil
		}

		lastErr = err
		log.Warn("erro ao ligar ao Discord", "attempt", attempt+1, "err", err)

		if !isNetworkError(err) {
			log.Error("erro não relacionado com a rede, a parar tentativas", "err", err)
			return err
		}
	}

	return fmt.Errorf("falhou após %d tentativas, último erro: %w", maxRetries, lastErr)
}

// registerCommandsWithRetry sincroniza os slash commands com backoff
// exponencial. Usa BulkOverwrite (e não Create) porque é idempotente: repor
// exatamente o mesmo conjunto em cada arranque não recria os comandos nem
// reinicia a propagação, ao contrário de apagar e voltar a criar.
// guildID vazio regista globalmente (propagação até 1 hora); com guildID a
// propagação é instantânea, o que é prático em desenvolvimento.
func registerCommandsWithRetry(ctx context.Context, dg *discordgo.Session, guildID string, log *slog.Logger) error {
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			delay := exponentialBackoff(attempt - 1)
			log.Info("a aguardar antes de nova tentativa de registo do comando", "delay", delay)
			if err := sleepCtx(ctx, delay); err != nil {
				return err
			}
		}

		registered, err := dg.ApplicationCommandBulkOverwrite(dg.State.User.ID, guildID, commands)
		if err == nil {
			scope := "global"
			if guildID != "" {
				scope = "guild:" + guildID
			}
			log.Info("comandos sincronizados", "count", len(registered), "scope", scope)
			return nil
		}

		lastErr = err
		log.Warn("erro ao registar comando", "attempt", attempt+1, "max", maxRetries, "err", err)

		if !isNetworkError(err) {
			return err
		}
	}

	return fmt.Errorf("falhou ao registar comando após %d tentativas: %w", maxRetries, lastErr)
}
