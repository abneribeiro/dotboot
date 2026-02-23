package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/bwmarrin/discordgo"
	"github.com/joho/godotenv"
)

func main() {
	// Carrega variáveis de ambiente do ficheiro .env (se existir).
	// Não retorna erro se o ficheiro não existir, permitindo usar
	// variáveis de ambiente do sistema em produção.
	_ = godotenv.Load()

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

	// Regista o handler que será chamado sempre que uma mensagem for criada
	// num canal que o bot consiga ver.
	dg.AddHandler(MessageCreate)

	// Define os intents necessários. O bot só precisa de ler mensagens em guilds (servidores).
	dg.Identify.Intents = discordgo.IntentsGuildMessages

	// Abre a ligação WebSocket com o Discord e começa a escutar eventos.
	err = dg.Open()
	if err != nil {
		log.Fatalf("Erro ao abrir a ligação com o Discord: %v", err)
	}
	defer dg.Close()

	fmt.Println("Bot está a correr. Pressiona CTRL+C para terminar.")

	// Bloqueia a goroutine principal até receber um sinal de interrupção (CTRL+C)
	// ou de terminação (kill), garantindo que o bot continua a correr.
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM)
	<-sc

	fmt.Println("A encerrar o bot...")
}
