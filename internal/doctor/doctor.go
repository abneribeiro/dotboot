// Package doctor verifica se o ambiente tem o que o dotboot precisa e diz
// como resolver o que falta. É o último passo do `make setup`, e o primeiro
// a correr quando um download falha sem razão aparente.
package doctor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/abneribeiro/dotboot/internal/config"
)

// ErrProblems sinaliza que falta algo essencial. O relatório já foi escrito.
var ErrProblems = errors.New("o ambiente tem problemas")

type level int

const (
	levelOK level = iota
	levelWarn
	levelFail
)

// check é uma linha do relatório.
type check struct {
	name   string
	level  level
	detail string
	// fix diz como resolver; vazio quando está tudo bem.
	fix string
}

const (
	// ytdlpMaxAge é a idade a partir da qual o yt-dlp provavelmente já não
	// acompanha as mudanças do YouTube, que partem os downloads.
	ytdlpMaxAge = 60 * 24 * time.Hour

	// commandTimeout limita cada "--version": o yt-dlp standalone demora a
	// arrancar na primeira execução, enquanto se descompacta.
	commandTimeout = 30 * time.Second

	// tokenPlaceholder é o valor do .env.example.
	tokenPlaceholder = "your_discord_bot_token_here"
)

// system abstrai o sistema para os testes não dependerem do PATH real.
type system struct {
	lookPath func(string) (string, error)
	output   func(ctx context.Context, name string, args ...string) (string, error)
	now      time.Time
}

// Run verifica o ambiente e escreve o relatório em w. Devolve ErrProblems
// quando falta algo sem o qual o dotboot não consegue descarregar.
func Run(ctx context.Context, cfg *config.Config, w io.Writer) error {
	sys := system{lookPath: exec.LookPath, output: commandOutput, now: time.Now()}
	checks := collect(ctx, cfg, sys)
	render(w, checks)
	for _, c := range checks {
		if c.level == levelFail {
			return ErrProblems
		}
	}
	return nil
}

func commandOutput(ctx context.Context, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	return string(out), err
}

func collect(ctx context.Context, cfg *config.Config, sys system) []check {
	checks := []check{
		checkYtdlp(ctx, sys),
		checkTool(ctx, sys, "ffmpeg", "ffmpeg version "),
		checkTool(ctx, sys, "ffprobe", "ffprobe version "),
		checkJSRuntime(ctx, cfg, sys),
		checkStems(cfg, sys),
		checkToken(cfg),
		checkOutputDir(cfg),
		checkTempDir(cfg),
	}
	if cfg.CookiesFile != "" {
		// O config.Load já garantiu que existe.
		checks = append(checks, check{name: "cookies", level: levelOK, detail: cfg.CookiesFile})
	}
	return checks
}

func checkYtdlp(ctx context.Context, sys system) check {
	c := check{name: "yt-dlp"}
	if _, err := sys.lookPath("yt-dlp"); err != nil {
		c.level, c.detail, c.fix = levelFail, "não encontrado", "make deps (instala em ~/.local/bin)"
		return c
	}
	out, err := sys.output(ctx, "yt-dlp", "--version")
	if err != nil {
		c.level, c.detail, c.fix = levelFail, "não arranca: "+firstLine(out), "make deps"
		return c
	}

	version := firstLine(out)
	released, ok := ytdlpReleaseDate(version)
	if !ok {
		c.level, c.detail = levelOK, version
		return c
	}
	age := sys.now.Sub(released)
	c.detail = fmt.Sprintf("%s (%s)", version, describeAge(age))
	if age > ytdlpMaxAge {
		// O YouTube muda com frequência e o yt-dlp acompanha: uma versão
		// antiga é a causa mais comum de downloads que deixam de funcionar.
		c.level, c.fix = levelWarn, "yt-dlp -U (ou make deps); na imagem Docker: make update-ytdlp"
	}
	return c
}

// checkTool verifica um binário obrigatório e mostra a versão que ele anuncia
// na primeira linha de "-version" (depois de prefix).
func checkTool(ctx context.Context, sys system, name, prefix string) check {
	c := check{name: name}
	if _, err := sys.lookPath(name); err != nil {
		c.level, c.detail, c.fix = levelFail, "não encontrado", "make deps (ou sudo apt install ffmpeg)"
		return c
	}
	out, err := sys.output(ctx, name, "-version")
	if err != nil {
		c.level, c.detail, c.fix = levelFail, "não arranca: "+firstLine(out), "reinstala o ffmpeg"
		return c
	}
	c.detail = versionAfter(out, prefix)
	if c.detail == "" {
		c.detail = "encontrado"
	}
	return c
}

func checkJSRuntime(ctx context.Context, cfg *config.Config, sys system) check {
	c := check{name: "JavaScript"}
	if cfg.JSRuntimes != "" {
		c.detail = cfg.JSRuntimes + " (DOTBOOT_JS_RUNTIMES)"
		return c
	}
	if _, err := sys.lookPath("deno"); err == nil {
		out, _ := sys.output(ctx, "deno", "--version")
		c.detail = "deno " + versionAfter(out, "deno ")
		return c
	}
	if _, err := sys.lookPath("node"); err == nil {
		out, _ := sys.output(ctx, "node", "--version")
		c.detail = "node " + firstLine(out) + " (ativado automaticamente; o deno é o recomendado)"
		return c
	}
	c.level = levelWarn
	c.detail = "nenhum runtime (deno ou node): o YouTube pode falhar ou ter menos formatos"
	c.fix = "make deps (instala o deno)"
	return c
}

func checkStems(cfg *config.Config, sys system) check {
	c := check{name: "stems"}
	if _, err := sys.lookPath("audio-separator"); err != nil {
		c.level = levelWarn
		c.detail = "audio-separator não encontrado (só para `dotboot stems`)"
		c.fix = "make deps-stems, ou a imagem Docker: make stems-build"
		return c
	}
	c.detail = "audio-separator; modelos em " + cfg.ModelsDir
	return c
}

func checkToken(cfg *config.Config) check {
	c := check{name: "token"}
	switch cfg.DiscordToken {
	case "":
		c.level, c.detail, c.fix = levelWarn, "DISCORD_TOKEN não definido (só é preciso para o bot)", "make env"
	case tokenPlaceholder:
		c.level, c.detail, c.fix = levelWarn, "DISCORD_TOKEN ainda tem o valor do .env.example", "make env"
	default:
		// Nunca se mostra o token, nem parte dele.
		c.detail = "definido"
	}
	return c
}

func checkOutputDir(cfg *config.Config) check {
	c := check{name: "saída"}
	dir := cfg.OutputDir
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			c.level, c.detail = levelWarn, "não foi possível determinar o diretório atual"
			return c
		}
		dir = wd
	}
	c.detail = dir

	info, err := os.Stat(dir)
	switch {
	case errors.Is(err, os.ErrNotExist):
		c.detail += " (é criada no primeiro download)"
	case err != nil:
		c.level, c.fix = levelWarn, err.Error()
	case !info.IsDir():
		c.level, c.fix = levelFail, "DOTBOOT_OUTPUT_DIR aponta para um ficheiro"
	case !writable(dir):
		// Só o modo CLI escreve aqui, e -o pode apontar para outro sítio.
		c.level, c.fix = levelWarn, "sem permissão de escrita: define DOTBOOT_OUTPUT_DIR ou usa -o"
	}
	return c
}

func checkTempDir(cfg *config.Config) check {
	c := check{name: "temporários", detail: cfg.TempDir}
	info, err := os.Stat(cfg.TempDir)
	switch {
	case err != nil:
		c.level, c.fix = levelFail, "DOTBOOT_TEMP_DIR não existe: "+err.Error()
	case !info.IsDir():
		c.level, c.fix = levelFail, "DOTBOOT_TEMP_DIR aponta para um ficheiro"
	case !writable(cfg.TempDir):
		c.level, c.fix = levelFail, "sem permissão de escrita em DOTBOOT_TEMP_DIR"
	}
	return c
}

// writable confirma a escrita criando e apagando um ficheiro: as permissões
// por si só enganam (ACLs, sistemas de ficheiros só de leitura).
func writable(dir string) bool {
	f, err := os.CreateTemp(dir, ".dotboot-doctor-*")
	if err != nil {
		return false
	}
	name := f.Name()
	f.Close()
	os.Remove(name)
	return true
}

// ytdlpReleaseDate interpreta a versão do yt-dlp, que é a data da release
// ("2026.10.26", ou "2026.10.26.234255" nas nightly).
func ytdlpReleaseDate(version string) (time.Time, bool) {
	parts := strings.Split(strings.TrimSpace(version), ".")
	if len(parts) < 3 {
		return time.Time{}, false
	}
	var ymd [3]int
	for i := range ymd {
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			return time.Time{}, false
		}
		ymd[i] = n
	}
	if ymd[1] < 1 || ymd[1] > 12 || ymd[2] < 1 || ymd[2] > 31 {
		return time.Time{}, false
	}
	return time.Date(ymd[0], time.Month(ymd[1]), ymd[2], 0, 0, 0, 0, time.UTC), true
}

// describeAge escreve a idade de uma versão para humanos.
func describeAge(age time.Duration) string {
	days := int(age.Hours() / 24)
	switch {
	case days <= 0:
		return "de hoje"
	case days == 1:
		return "de ontem"
	default:
		return fmt.Sprintf("há %d dias", days)
	}
}

// versionAfter devolve a palavra que se segue a prefix na primeira linha do
// output ("ffmpeg version 7.1.5-0+deb13u1 Copyright…" → "7.1.5-0+deb13u1").
func versionAfter(out, prefix string) string {
	rest, ok := strings.CutPrefix(firstLine(out), prefix)
	if !ok {
		return ""
	}
	if i := strings.IndexByte(rest, ' '); i >= 0 {
		rest = rest[:i]
	}
	return rest
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(line)
}

// render escreve o relatório: uma linha por verificação, com a solução por
// baixo quando há uma, e um resumo no fim.
func render(w io.Writer, checks []check) {
	fmt.Fprintln(w, "dotboot doctor")
	fmt.Fprintln(w)

	var warnings, failures int
	for _, c := range checks {
		icon := "✅"
		switch c.level {
		case levelWarn:
			icon = "⚠️ "
			warnings++
		case levelFail:
			icon = "❌"
			failures++
		}
		fmt.Fprintf(w, "  %s %-12s %s\n", icon, c.name, c.detail)
		if c.fix != "" {
			fmt.Fprintf(w, "     %-12s → %s\n", "", c.fix)
		}
	}

	fmt.Fprintln(w)
	switch {
	case failures > 0:
		fmt.Fprintf(w, "%d problema(s) por resolver.\n", failures)
	case warnings > 0:
		fmt.Fprintf(w, "Pronto a usar (%d aviso(s)).\n", warnings)
	default:
		fmt.Fprintln(w, "Tudo pronto.")
	}
}
