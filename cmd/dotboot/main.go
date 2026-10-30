// dotboot — descarrega vídeos e áudio (yt-dlp) e separa músicas em pistas
// (audio-separator), com uma página web local: um estúdio para ouvir as
// pistas em conjunto e descarregar as combinações que se quiser.
//
// Uso:
//
//	dotboot [-addr 127.0.0.1:8765] [-no-browser]   Arranca o servidor e abre a página
//	dotboot version                                Mostra a versão
//	dotboot health                                 Confirma que o servidor responde (HEALTHCHECK)
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"github.com/abneribeiro/dotboot/internal/config"
	"github.com/abneribeiro/dotboot/internal/doctor"
	"github.com/abneribeiro/dotboot/internal/store"
	"github.com/abneribeiro/dotboot/internal/web"
)

const usageText = `dotboot — vídeos, áudio e stems numa página web local

Uso:
  dotboot [flags]          Arranca o servidor e abre a página no browser
  dotboot version          Mostra a versão
  dotboot [-addr] health   Confirma que o servidor responde

Flags:
  -addr ENDEREÇO      Onde o servidor escuta (default 127.0.0.1:8765, ou $DOTBOOT_ADDR)
  -no-browser         Não abre o browser

As variáveis DOTBOOT_* (ver .env.example) podem ficar em
~/.config/dotboot/config.env.
`

// uploadMaxAge é quanto um ficheiro enviado e não usado fica guardado.
const uploadMaxAge = 24 * time.Hour

// Preenchidas com -ldflags "-X main.version=..." pelo Makefile, pelo
// GoReleaser e pelo Dockerfile.
var (
	version = "dev"
	commit  = ""
	date    = ""
)

func main() {
	// Antes das flags: o DOTBOOT_ADDR também pode vir do config.env.
	config.LoadFiles()

	fs := flag.NewFlagSet("dotboot", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() { fmt.Fprint(os.Stderr, usageText) }
	addr := fs.String("addr", envOr("DOTBOOT_ADDR", "127.0.0.1:8765"), "endereço do servidor")
	noBrowser := fs.Bool("no-browser", false, "não abrir o browser")
	showVersion := fs.Bool("version", false, "mostrar a versão")
	if err := fs.Parse(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		os.Exit(2)
	}

	// O subcomando vem depois das flags: a imagem Docker já traz -addr no
	// ENTRYPOINT, e "docker run dotboot:latest version" acrescenta-o no fim.
	var err error
	switch cmd := fs.Arg(0); {
	case *showVersion || cmd == "version":
		fmt.Println(versionString())
	case cmd == "help":
		fmt.Print(usageText)
	case cmd == "health":
		err = health(*addr)
	case cmd == "":
		err = run(*addr, !*noBrowser)
	default:
		fmt.Fprintf(os.Stderr, "comando desconhecido: %q\n\n%s", cmd, usageText)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ %v\n", err)
		os.Exit(1)
	}
}

// health pergunta ao servidor se está vivo (o HEALTHCHECK da imagem).
func health(addr string) error {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("http://" + browserHost(addr) + "/api/info")
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("o servidor respondeu %s", resp.Status)
	}
	return nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func run(addr string, openBrowser bool) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("configuração inválida: %w", err)
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(logger)
	configureJSRuntime(cfg, logger)

	st, err := store.Open(cfg.DataDir)
	if err != nil {
		return err
	}
	// Ao arrancar nenhum trabalho está a correr: o que ficou a meio de uma
	// execução anterior é lixo.
	st.Clean(uploadMaxAge)

	// Os sinais são tratados apenas aqui. Depois do primeiro, o seguinte
	// volta ao comportamento por omissão e termina o processo de imediato.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	context.AfterFunc(ctx, stop)

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("não foi possível escutar em %s (já há um dotboot a correr?): %w", addr, err)
	}
	srv := web.New(cfg, st, shortVersion(), logger)
	httpSrv := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}

	url := "http://" + browserHost(ln.Addr().String())
	fmt.Printf("dotboot %s a correr em %s (ctrl+c para sair)\n", shortVersion(), url)
	for _, c := range doctor.Collect(ctx, cfg) {
		if c.Level == doctor.LevelFail {
			logger.Warn("falta uma dependência: vê Definições → Diagnóstico", "verificação", c.Name, "detalhe", c.Detail)
		}
	}
	if openBrowser {
		go openURL(url, logger)
	}

	errCh := make(chan error, 1)
	go func() { errCh <- httpSrv.Serve(ln) }()
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	fmt.Println("\na encerrar: a cancelar os trabalhos em curso…")
	// Primeiro os trabalhos (espera pelos processos filhos) e os streams SSE,
	// que nunca acabariam sozinhos; depois o servidor HTTP.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	srv.Shutdown()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return nil
}

// browserHost troca o endereço de escuta por um que o browser abra:
// 0.0.0.0 e :: não são destinos.
func browserHost(listen string) string {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return listen
	}
	if ip := net.ParseIP(host); ip == nil || ip.IsUnspecified() || ip.IsLoopback() {
		host = "localhost"
	}
	return net.JoinHostPort(host, port)
}

// openURL abre a página no browser do sistema. No WSL o browser é o do
// Windows (wslview, ou o explorer.exe que abre o browser por omissão).
func openURL(url string, logger *slog.Logger) {
	var candidates [][]string
	switch runtime.GOOS {
	case "darwin":
		candidates = [][]string{{"open", url}}
	case "windows":
		candidates = [][]string{{"rundll32", "url.dll,FileProtocolHandler", url}}
	default:
		if isWSL() {
			candidates = append(candidates, []string{"wslview", url}, []string{"explorer.exe", url})
		}
		candidates = append(candidates, []string{"xdg-open", url})
	}
	for _, c := range candidates {
		if _, err := exec.LookPath(c[0]); err != nil {
			continue
		}
		// O explorer.exe devolve 1 mesmo quando abre: só conta conseguir lançá-lo.
		if err := exec.Command(c[0], c[1:]...).Start(); err == nil {
			return
		}
	}
	logger.Info("abre a página no browser", "url", url)
}

func isWSL() bool {
	data, err := os.ReadFile("/proc/version")
	return err == nil && strings.Contains(strings.ToLower(string(data)), "microsoft")
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

// shortVersion é a versão para a página: a tag, ou o commit num build de
// desenvolvimento.
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
