// dotboot — bot Discord e downloader de vídeos baseado em yt-dlp.
//
// Subcomandos:
//
//	dotboot bot          Arranca o bot Discord (/baixar)
//	dotboot download     Descarrega vídeos via terminal, sem limites do Discord
//	dotboot doctor       Verifica dependências e configuração
//	dotboot healthcheck  Verifica o heartbeat do bot (HEALTHCHECK da imagem)
//	dotboot version      Mostra a versão
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"github.com/abneribeiro/dotboot/internal/bot"
	"github.com/abneribeiro/dotboot/internal/cli"
	"github.com/abneribeiro/dotboot/internal/config"
	"github.com/abneribeiro/dotboot/internal/doctor"
)

const usageText = `dotboot — bot Discord e downloader de vídeos (yt-dlp)

Uso:
  dotboot bot                        Arranca o bot Discord
  dotboot download [flags] URL...    Descarrega vídeos via terminal
  dotboot doctor                     Verifica dependências e configuração
  dotboot healthcheck                Verifica se o bot está ligado (Docker)
  dotboot version                    Mostra a versão
  dotboot help                       Mostra esta ajuda

Flags de download (podem vir antes ou depois dos URLs):
  -o DIR          Diretório de saída; relativo é resolvido sob $DOTBOOT_OUTPUT_DIR
                  quando definido (default: $DOTBOOT_OUTPUT_DIR ou o diretório atual)
  -a FICHEIRO     Ficheiro com um URL por linha
  -q QUALIDADE    best (default) ou a resolução máxima (ex: 1080, 720, 480)
  -formato FMT    mp4 (default, H.264/AAC que o iPhone e o Android leem),
                  original (tal como o site serve) ou mp3/m4a (só o áudio)
  -cookies FICH   Cookies para vídeos que pedem login ou verificação
  -start TEMPO    Início do recorte, MM:SS ou HH:MM:SS (exige -end e um único URL)
  -end TEMPO      Fim do recorte

Exemplos:
  dotboot download https://youtube.com/watch?v=xxxx
  dotboot download -o ~/Videos https://youtube.com/playlist?list=yyyy
  dotboot download -a urls.txt -o ~/Videos
  dotboot download -q 720 -o aulas https://youtube.com/watch?v=xxxx
  dotboot download -formato mp3 -o musica https://youtube.com/watch?v=xxxx

Re-executar "download" com o mesmo diretório salta os vídeos já descarregados
(registados em .dotboot-archive.txt) e retoma downloads interrompidos.
`

// Preenchidas com -ldflags "-X main.version=..." pelo Makefile, pelo
// GoReleaser e pelo Dockerfile.
var (
	version = "dev"
	commit  = ""
	date    = ""
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usageText)
		os.Exit(2)
	}

	// help e version não dependem da configuração: têm de funcionar mesmo
	// com um .env inválido, que é precisamente quando se vai à procura deles.
	switch os.Args[1] {
	case "help", "-h", "--help":
		fmt.Print(usageText)
		return
	case "version", "-v", "--version":
		fmt.Println(versionString())
		return
	}

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
	// Depois do primeiro sinal, o seguinte volta ao comportamento por omissão
	// e termina o processo de imediato. Sem isto, um segundo CTRL+C durante
	// os 30s de graça do bot era simplesmente ignorado.
	context.AfterFunc(ctx, stop)

	switch os.Args[1] {
	case "bot":
		err = runBot(ctx, cfg, logger)
	case "download":
		err = runDownload(ctx, cfg, logger, os.Args[2:])
	case "doctor":
		err = doctor.Run(ctx, cfg, os.Stdout)
	case "healthcheck":
		if err := bot.CheckHeartbeat(bot.HeartbeatPath(cfg.TempDir), bot.HeartbeatMaxAge, time.Now()); err != nil {
			fmt.Fprintf(os.Stderr, "unhealthy: %v\n", err)
			os.Exit(1)
		}
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
	case errors.Is(err, cli.ErrInterrupted):
		// 128 + SIGINT, como as shells: um CTRL+C não é um "erro fatal".
		os.Exit(130)
	case errors.Is(err, cli.ErrPartial), errors.Is(err, doctor.ErrProblems):
		// Já foi reportado; só o código de saída é que muda.
		os.Exit(1)
	default:
		logger.Error("erro fatal", "err", err)
		os.Exit(1)
	}
}

func runBot(ctx context.Context, cfg *config.Config, logger *slog.Logger) error {
	if err := checkBinaries("yt-dlp", "ffmpeg", "ffprobe"); err != nil {
		return err
	}
	configureJSRuntime(cfg, logger)

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
	// streams de vídeo e áudio dos formatos de melhor qualidade, e o ffprobe
	// decide se o resultado já é MP4 compatível.
	if err := checkBinaries("yt-dlp", "ffmpeg", "ffprobe"); err != nil {
		return err
	}
	configureJSRuntime(cfg, logger)
	return cli.Run(ctx, args, cfg, logger)
}

// checkBinaries garante que as dependências externas estão no PATH.
func checkBinaries(names ...string) error {
	for _, name := range names {
		if _, err := exec.LookPath(name); err != nil {
			return fmt.Errorf("%s não encontrado no PATH (instala com `make deps` ou vê `dotboot doctor`): %w", name, err)
		}
	}
	return nil
}

// configureJSRuntime escolhe o runtime JavaScript com que o yt-dlp resolve os
// desafios do YouTube, quando DOTBOOT_JS_RUNTIMES não o fixa. Sem nenhum, o
// YouTube continua a responder, mas com menos formatos ou erros.
func configureJSRuntime(cfg *config.Config, logger *slog.Logger) {
	if cfg.JSRuntimes != "" {
		return
	}
	jsRuntime, found := pickJSRuntime(exec.LookPath)
	if !found {
		logger.Warn("nenhum runtime JavaScript (deno ou node) no PATH: o YouTube pode falhar ou ter menos formatos; instala o deno com `make deps`")
		return
	}
	cfg.JSRuntimes = jsRuntime
}

// pickJSRuntime devolve o valor de --js-runtimes a usar. O Deno é o default do
// yt-dlp e dispensa a flag (""); o node só é usado quando pedido, por isso
// ativa-se quando é o único disponível.
func pickJSRuntime(lookPath func(string) (string, error)) (jsRuntime string, found bool) {
	if _, err := lookPath("deno"); err == nil {
		return "", true
	}
	if _, err := lookPath("node"); err == nil {
		return "node", true
	}
	return "", false
}

// versionString descreve o binário. Sem -ldflags (go install, go run) usa a
// informação que o Go embute: a versão do módulo num go install @vX.Y.Z, ou a
// revisão do git num build local.
func versionString() string {
	v, c, d := version, commit, date
	if info, ok := debug.ReadBuildInfo(); ok {
		if v == "dev" && info.Main.Version != "" && info.Main.Version != "(devel)" {
			v = info.Main.Version
		}
		for _, s := range info.Settings {
			switch {
			case s.Key == "vcs.revision" && c == "":
				c = s.Value[:min(len(s.Value), 7)]
			case s.Key == "vcs.time" && d == "":
				d = s.Value
			}
		}
	}

	out := "dotboot " + v
	if c != "" {
		out += " (" + c
		if d != "" {
			out += ", " + d
		}
		out += ")"
	}
	return fmt.Sprintf("%s %s %s/%s", out, runtime.Version(), runtime.GOOS, runtime.GOARCH)
}
