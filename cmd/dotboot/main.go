// dotboot — bot Discord e downloader de vídeos baseado em yt-dlp.
//
// Subcomandos:
//
//	dotboot bot         Arranca o bot Discord (/baixar)
//	dotboot download    Descarrega vídeos via terminal, sem limites do Discord
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	"github.com/joho/godotenv"

	"dotboot/internal/bot"
	"dotboot/internal/cli"
	"dotboot/internal/config"
)

const usageText = `dotboot — bot Discord e downloader de vídeos (yt-dlp)

Uso:
  dotboot bot                        Arranca o bot Discord
  dotboot download [flags] URL...    Descarrega vídeos via terminal
  dotboot help                       Mostra esta ajuda

Flags de download (têm de vir antes dos URLs):
  -o DIR        Diretório de saída; relativo é resolvido sob $DOTBOOT_OUTPUT_DIR
                quando definido (default: $DOTBOOT_OUTPUT_DIR ou o diretório atual)
  -a FICHEIRO   Ficheiro com um URL por linha
  -q QUALIDADE  Qualidade do vídeo: best (default) ou altura máxima em pixels
                (ex: 1080, 720, 480)
  -start TEMPO  Início do recorte, MM:SS ou HH:MM:SS (exige -end e um único URL)
  -end TEMPO    Fim do recorte

Exemplos:
  dotboot download https://youtube.com/watch?v=xxxx
  dotboot download -o ~/Videos https://youtube.com/playlist?list=yyyy
  dotboot download -a urls.txt -o ~/Videos
  dotboot download -q 720 -o aulas https://youtube.com/watch?v=xxxx

Re-executar "download" com o mesmo diretório salta os vídeos já descarregados
(registados em .dotboot-archive.txt) e retoma downloads interrompidos.
`

func main() {
	// Carrega variáveis de ambiente do ficheiro .env (se existir).
	// Não retorna erro se o ficheiro não existir, permitindo usar
	// variáveis de ambiente do sistema em produção.
	_ = godotenv.Load()

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "configuração inválida: %v\n", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(logger)

	// Os sinais são tratados apenas aqui; ambos os subcomandos recebem o
	// mesmo contexto e implementam o seu próprio encerramento gracioso.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usageText)
		os.Exit(2)
	}

	switch os.Args[1] {
	case "bot":
		err = runBot(ctx, cfg, logger)
	case "download":
		err = runDownload(ctx, cfg, logger, os.Args[2:])
	case "help", "-h", "--help":
		fmt.Print(usageText)
		return
	default:
		fmt.Fprintf(os.Stderr, "subcomando desconhecido: %q\n\n%s", os.Args[1], usageText)
		os.Exit(2)
	}

	switch {
	case err == nil:
	case errors.Is(err, cli.ErrUsage):
		// Já foi impresso, com a ajuda, no stderr.
		os.Exit(2)
	case errors.Is(err, cli.ErrPartial):
		// Já foi reportado; só o código de saída é que muda.
		os.Exit(1)
	default:
		logger.Error("erro fatal", "err", err)
		os.Exit(1)
	}
}

func runBot(ctx context.Context, cfg *config.Config, logger *slog.Logger) error {
	if err := checkBinaries(); err != nil {
		return err
	}

	if cfg.DiscordToken == "" {
		return fmt.Errorf("DISCORD_TOKEN não está definido; define a variável de ambiente antes de executar o bot")
	}
	// O prefixo "Bot " é adicionado pelo bot.New. Deixá-lo no token produziria
	// "Bot Bot <token>" e uma falha de autenticação, por isso remove-se aqui
	// em vez de apenas avisar.
	if strings.HasPrefix(cfg.DiscordToken, "Bot ") {
		logger.Warn("DISCORD_TOKEN começa com \"Bot \" — o prefixo é adicionado automaticamente e foi removido")
		cfg.DiscordToken = strings.TrimSpace(strings.TrimPrefix(cfg.DiscordToken, "Bot "))
	}

	b, err := bot.New(cfg, logger)
	if err != nil {
		return err
	}
	return b.Run(ctx)
}

func runDownload(ctx context.Context, cfg *config.Config, logger *slog.Logger, args []string) error {
	// O ffmpeg continua a ser necessário: o yt-dlp usa-o para juntar as
	// streams de vídeo e áudio dos formatos de melhor qualidade.
	if err := checkBinaries(); err != nil {
		return err
	}
	return cli.Run(ctx, args, cfg, logger)
}

// checkBinaries garante que as dependências externas estão no PATH.
// O ffprobe é usado para medir a duração e a resolução antes de comprimir.
func checkBinaries() error {
	for _, name := range []string{"yt-dlp", "ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(name); err != nil {
			return fmt.Errorf("%s não encontrado no PATH: %w", name, err)
		}
	}
	return nil
}
