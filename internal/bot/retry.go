package bot

import (
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
	requestTimeout = 30 * time.Second
)

// createCustomHTTPClient cria um cliente HTTP otimizado para ligações ao Discord.
func createCustomHTTPClient() *http.Client {
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   connectTimeout,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: requestTimeout,
		ExpectContinueTimeout: 1 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
	}

	return &http.Client{
		Transport: transport,
		Timeout:   requestTimeout,
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

// connectWithRetry tenta abrir a ligação ao Discord com backoff exponencial.
// Erros que não sejam de rede param as tentativas imediatamente.
func connectWithRetry(dg *discordgo.Session, log *slog.Logger) error {
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			delay := exponentialBackoff(attempt - 1)
			log.Info("a aguardar antes de nova tentativa de ligação", "attempt", attempt+1, "max", maxRetries, "delay", delay)
			time.Sleep(delay)
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

// registerCommandWithRetry regista o slash command com backoff exponencial.
func registerCommandWithRetry(dg *discordgo.Session, log *slog.Logger) (*discordgo.ApplicationCommand, error) {
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			delay := exponentialBackoff(attempt - 1)
			log.Info("a aguardar antes de nova tentativa de registo do comando", "delay", delay)
			time.Sleep(delay)
		}

		registeredCmd, err := dg.ApplicationCommandCreate(dg.State.User.ID, "", command)
		if err == nil {
			log.Info("comando registado com sucesso", "command", registeredCmd.Name, "id", registeredCmd.ID)
			return registeredCmd, nil
		}

		lastErr = err
		log.Warn("erro ao registar comando", "attempt", attempt+1, "max", maxRetries, "err", err)

		if !isNetworkError(err) {
			return nil, err
		}
	}

	return nil, fmt.Errorf("falhou ao registar comando após %d tentativas: %w", maxRetries, lastErr)
}
