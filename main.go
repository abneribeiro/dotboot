package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/joho/godotenv"
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

const (
	maxRetries      = 5
	baseRetryDelay  = time.Second
	maxRetryDelay   = 60 * time.Second
	connectTimeout  = 30 * time.Second
	requestTimeout  = 30 * time.Second
)

// createCustomHTTPClient creates an HTTP client optimized for Discord connections
func createCustomHTTPClient() *http.Client {
	transport := &http.Transport{
		Dial: (&net.Dialer{
			Timeout:   connectTimeout,
			KeepAlive: 30 * time.Second,
		}).Dial,
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

// isNetworkError checks if an error is network-related and potentially temporary
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

// exponentialBackoff calculates delay with exponential backoff and jitter
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

// connectWithRetry attempts to establish Discord connection with exponential backoff
func connectWithRetry(dg *discordgo.Session) error {
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			delay := exponentialBackoff(attempt - 1)
			log.Printf("Tentativa %d de %d após %v...", attempt+1, maxRetries, delay)
			time.Sleep(delay)
		}

		log.Printf("A tentar estabelecer ligação com o Discord (tentativa %d/%d)...", attempt+1, maxRetries)

		err := dg.Open()
		if err == nil {
			log.Println("Ligação estabelecida com sucesso!")
			return nil
		}

		lastErr = err
		log.Printf("Erro na tentativa %d: %v", attempt+1, err)

		// Se não for um erro de rede, não vale a pena tentar novamente
		if !isNetworkError(err) {
			log.Printf("Erro não relacionado com a rede detectado, a parar tentativas: %v", err)
			return err
		}
	}

	return fmt.Errorf("falhou após %d tentativas, último erro: %v", maxRetries, lastErr)
}

// registerCommandWithRetry attempts to register slash command with retry logic
func registerCommandWithRetry(dg *discordgo.Session) (*discordgo.ApplicationCommand, error) {
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			delay := exponentialBackoff(attempt - 1)
			log.Printf("A tentar registar comando após %v...", delay)
			time.Sleep(delay)
		}

		registeredCmd, err := dg.ApplicationCommandCreate(dg.State.User.ID, "", command)
		if err == nil {
			log.Printf("Comando /%s registado com sucesso (ID: %s)", registeredCmd.Name, registeredCmd.ID)
			return registeredCmd, nil
		}

		lastErr = err
		log.Printf("Erro ao registar comando (tentativa %d/%d): %v", attempt+1, maxRetries, err)

		if !isNetworkError(err) {
			return nil, err
		}
	}

	return nil, fmt.Errorf("falhou ao registar comando após %d tentativas: %v", maxRetries, lastErr)
}

func validateEnvironment() error {
	// Check required environment variables
	token := os.Getenv("DISCORD_TOKEN")
	if token == "" {
		return fmt.Errorf("DISCORD_TOKEN environment variable is not set")
	}

	// Validate token format (Discord bot tokens start with specific prefixes)
	if !strings.HasPrefix(token, "Bot ") && !strings.HasPrefix(token, "MTQ") && !strings.HasPrefix(token, "MTE") && !strings.HasPrefix(token, "MTI") {
		log.Println("Warning: Discord token format may be invalid")
	}

	// Check if yt-dlp is available
	if _, err := exec.LookPath("yt-dlp"); err != nil {
		return fmt.Errorf("yt-dlp not found in PATH: %w", err)
	}

	// Check if ffmpeg is available
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return fmt.Errorf("ffmpeg not found in PATH: %w", err)
	}

	log.Println("Environment validation successful")
	return nil
}

func main() {
	// Carrega variáveis de ambiente do ficheiro .env (se existir).
	// Não retorna erro se o ficheiro não existir, permitindo usar
	// variáveis de ambiente do sistema em produção.
	_ = godotenv.Load()

	// Validate environment variables and dependencies
	if err := validateEnvironment(); err != nil {
		log.Fatalf("Environment validation failed: %v", err)
	}

	// Carrega o token do bot a partir da variável de ambiente DISCORD_TOKEN.
	token := os.Getenv("DISCORD_TOKEN")
	if token == "" {
		log.Fatal("DISCORD_TOKEN não está definido. Define a variável de ambiente antes de executar o bot.")
	}

	// Cria uma nova sessão do Discord usando o token do bot.
	// O prefixo "Bot " é obrigatório para autenticação com tokens de bot.
	dg, err := discordgo.New("Bot " + token)
	if err != nil {
		log.Fatalf("Erro ao criar a sessão do Discord: %v", err)
	}

	// Configura cliente HTTP customizado para melhor estabilidade de rede
	dg.Client = createCustomHTTPClient()

	// Configura opções da sessão para melhor estabilidade
	dg.StateEnabled = true
	dg.State.TrackVoice = false
	dg.State.TrackPresences = false
	dg.State.TrackEmojis = false

	// Regista o handler de interações (Slash Commands).
	dg.AddHandler(InteractionCreate)

	// Define os intents necessários. Para Slash Commands basta IntentsGuilds.
	dg.Identify.Intents = discordgo.IntentsGuilds

	// Abre a ligação WebSocket com o Discord com retry logic.
	err = connectWithRetry(dg)
	if err != nil {
		log.Fatalf("Erro ao abrir a ligação com o Discord após múltiplas tentativas: %v", err)
	}
	defer dg.Close()

	// Regista o Slash Command /baixar globalmente com retry logic.
	// Comandos globais podem demorar até 1 hora a propagar para todos os servidores.
	registeredCmd, err := registerCommandWithRetry(dg)
	if err != nil {
		log.Fatalf("Erro ao registar o comando /baixar após múltiplas tentativas: %v", err)
	}

	fmt.Println("Bot está a correr com estabilidade de rede melhorada. Pressiona CTRL+C para terminar.")

	// Bloqueia a goroutine principal até receber um sinal de interrupção (CTRL+C)
	// ou de terminação (kill), garantindo que o bot continua a correr.
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM)
	<-sc

	fmt.Println("A encerrar o bot...")

	// Remove o Slash Command ao encerrar para manter o Discord limpo.
	// Usa context com timeout para evitar bloquear indefinidamente
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- dg.ApplicationCommandDelete(dg.State.User.ID, "", registeredCmd.ID)
	}()

	select {
	case err := <-done:
		if err != nil {
			log.Printf("Aviso: erro ao remover o comando /baixar: %v", err)
		} else {
			log.Println("Comando /baixar removido com sucesso.")
		}
	case <-ctx.Done():
		log.Println("Timeout ao tentar remover o comando /baixar")
	}
}
