// dotboot — descarrega vídeos e música (yt-dlp), pelo terminal: uma TUI e
// subcomandos para scripts.
//
// Subcomandos:
//
//	dotboot              Abre a TUI (num terminal)
//	dotboot tui          Abre a TUI
//	dotboot download     Descarrega vídeos ou áudio
//	dotboot doctor       Verifica dependências e configuração
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
	"syscall"

	"github.com/charmbracelet/x/term"

	"github.com/abneribeiro/dotboot/internal/cli"
	"github.com/abneribeiro/dotboot/internal/config"
	"github.com/abneribeiro/dotboot/internal/doctor"
	"github.com/abneribeiro/dotboot/internal/tui"
)

const usageText = `dotboot — vídeos e música no terminal (yt-dlp e ffmpeg)

Uso:
  dotboot                            Abre a interface (TUI)
  dotboot download [flags] URL...    Descarrega vídeos ou áudio
  dotboot doctor                     Verifica dependências e configuração
  dotboot version                    Mostra a versão
  dotboot help                       Mostra esta ajuda

Exemplos:
  dotboot download -q 720 -o aulas "https://youtube.com/watch?v=xxxx"
  dotboot download -formato mp3 -o musica "https://youtube.com/watch?v=xxxx"

A ajuda completa do download: dotboot download -h.
As definições ficam em ~/.config/dotboot/config.env (editáveis na TUI).
`

// errMissingDeps marca a falta de um binário externo. É um problema de
// instalação, não um "erro fatal": o main mostra-o sem o formato do log.
var errMissingDeps = errors.New("dependência em falta")

// Preenchidas com -ldflags "-X main.version=..." pelo Makefile, pelo
// GoReleaser e pelo Dockerfile.
var (
	version = "dev"
	commit  = ""
	date    = ""
)

func main() {
	cmd := "tui"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}

	// help e version não dependem da configuração: têm de funcionar mesmo
	// com uma config inválida, que é precisamente quando se vai à procura.
	switch cmd {
	case "help", "-h", "--help":
		fmt.Print(usageText)
		return
	case "version", "-v", "--version":
		fmt.Println(versionString())
		return
	}

	// Sem argumentos e sem terminal (um pipe, um cron), não há TUI para abrir.
	if len(os.Args) < 2 && !(term.IsTerminal(os.Stdin.Fd()) && term.IsTerminal(os.Stdout.Fd())) {
		fmt.Fprint(os.Stderr, usageText)
		os.Exit(2)
	}

	config.LoadFiles()
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "configuração inválida: %v\n", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(logger)

	// Os sinais são tratados apenas aqui; todos os modos recebem o mesmo
	// contexto e cancelam os processos externos com SIGINT.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// Depois do primeiro sinal, o seguinte volta ao comportamento por omissão
	// e termina o processo de imediato.
	context.AfterFunc(ctx, stop)

	args := os.Args[min(2, len(os.Args)):]
	switch cmd {
	case "tui":
		configureJSRuntime(cfg, logger)
		err = tui.Run(ctx, cfg, shortVersion())
	case "download":
		err = runDownload(ctx, cfg, logger, args)
	case "doctor":
		err = doctor.Run(ctx, cfg, os.Stdout)
	default:
		fmt.Fprintf(os.Stderr, "subcomando desconhecido: %q\n\n%s", cmd, usageText)
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
	case errors.Is(err, errMissingDeps):
		fmt.Fprintf(os.Stderr, "❌ %v\n", err)
		os.Exit(1)
	case errors.Is(err, cli.ErrPartial), errors.Is(err, doctor.ErrProblems):
		// Já foi reportado; só o código de saída é que muda.
		os.Exit(1)
	default:
		fmt.Fprintf(os.Stderr, "❌ %v\n", err)
		os.Exit(1)
	}
}

func runDownload(ctx context.Context, cfg *config.Config, logger *slog.Logger, args []string) error {
	// O ffmpeg continua a ser necessário: o yt-dlp usa-o para juntar as
	// streams de vídeo e áudio dos formatos de melhor qualidade, e o ffprobe
	// decide se o resultado já é MP4 compatível.
	if err := checkBinaries(args, "yt-dlp", "ffmpeg", "ffprobe"); err != nil {
		return err
	}
	configureJSRuntime(cfg, logger)
	return cli.Run(ctx, args, cfg, logger)
}

// checkBinaries garante que as dependências externas estão no PATH. Um
// pedido de ajuda (-h) passa sempre: ler a ajuda não pode exigir o ffmpeg.
func checkBinaries(args []string, names ...string) error {
	if wantsHelp(args) {
		return nil
	}
	for _, name := range names {
		if _, err := exec.LookPath(name); err != nil {
			return fmt.Errorf("%w: %s não está no PATH (instala com `make deps`, ou vê o que falta com `dotboot doctor`)",
				errMissingDeps, name)
		}
	}
	return nil
}

// wantsHelp reporta se os argumentos de um subcomando pedem a ajuda.
func wantsHelp(args []string) bool {
	for _, a := range args {
		switch a {
		case "-h", "-help", "--help":
			return true
		case "--":
			return false
		}
	}
	return false
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

// versionInfo devolve a versão, o commit e a data do binário. Sem -ldflags
// (go install, go run) usa a informação que o Go embute: a versão do módulo
// num go install @vX.Y.Z, ou a revisão do git num build local.
func versionInfo() (v, c, d string) {
	v, c, d = version, commit, date
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
	return v, c, d
}

// shortVersion é a versão para o cabeçalho da TUI: a tag, ou o commit num
// build de desenvolvimento.
func shortVersion() string {
	v, c, _ := versionInfo()
	if v == "dev" && c != "" {
		return "dev " + c
	}
	return v
}

// versionString descreve o binário por extenso (dotboot version).
func versionString() string {
	v, c, d := versionInfo()
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
