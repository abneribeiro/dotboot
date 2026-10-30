// Package config carrega a configuração do dotboot a partir de variáveis de
// ambiente, com valores por omissão sensatos. Valores inválidos resultam em
// erro — nunca em fallback silencioso.
//
// As variáveis podem vir do ambiente, de um .env no diretório atual ou do
// ficheiro global ~/.config/dotboot/config.env (ver LoadFiles), por esta
// ordem de prioridade.
package config

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/joho/godotenv"
)

// Config agrega a configuração do CLI e da TUI.
type Config struct {
	// OutputDir é o caminho base de saída (DOTBOOT_OUTPUT_DIR): usado quando
	// -o não é passado, e raiz de um -o relativo. Um -o absoluto ignora-o.
	// Vazio = diretório atual.
	OutputDir string

	// TempDir guarda ficheiros temporários dos downloads (DOTBOOT_TEMP_DIR,
	// default os.TempDir()).
	TempDir string

	// LogLevel controla a verbosidade (DOTBOOT_LOG_LEVEL: debug|info|warn|error).
	LogLevel slog.Level

	// CookiesFile é um ficheiro de cookies em formato Netscape
	// (DOTBOOT_COOKIES_FILE), passado ao yt-dlp com --cookies. Resolve a
	// verificação "não és um robô" do YouTube, a restrição de idade e o
	// conteúdo que exige login. Tem de existir quando definido.
	CookiesFile string

	// JSRuntimes é passado ao yt-dlp como --js-runtimes (DOTBOOT_JS_RUNTIMES,
	// ex: "node"). Vazio usa o default do yt-dlp, o Deno; o main preenche-o
	// com "node" quando o Deno falta e o node existe.
	JSRuntimes string
}

// Chaves das variáveis de ambiente, partilhadas com a TUI (Definições).
const (
	KeyOutputDir   = "DOTBOOT_OUTPUT_DIR"
	KeyTempDir     = "DOTBOOT_TEMP_DIR"
	KeyLogLevel    = "DOTBOOT_LOG_LEVEL"
	KeyCookiesFile = "DOTBOOT_COOKIES_FILE"
	KeyJSRuntimes  = "DOTBOOT_JS_RUNTIMES"
)

// FilePath devolve o ficheiro de configuração global:
// $XDG_CONFIG_HOME/dotboot/config.env, ou ~/.config/dotboot/config.env.
func FilePath() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "dotboot", "config.env")
	}
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, "dotboot", "config.env")
	}
	return filepath.Join(os.TempDir(), "dotboot", "config.env")
}

// LoadFiles carrega para o ambiente o ./.env e depois o ficheiro global. O
// godotenv nunca sobrescreve uma variável já definida, por isso a
// prioridade é: ambiente > ./.env > config.env. Ambos são opcionais.
func LoadFiles() {
	_ = godotenv.Load(".env")     // opcional: só existe num projeto
	_ = godotenv.Load(FilePath()) // opcional: só existe depois de gravar Definições
}

// Load lê a configuração a partir do ambiente do processo.
func Load() (*Config, error) {
	return loadFrom(os.Getenv)
}

// loadFrom é a implementação testável de Load, com o ambiente injetado.
func loadFrom(getenv func(string) string) (*Config, error) {
	cfg := &Config{
		OutputDir:   getenv(KeyOutputDir),
		TempDir:     os.TempDir(),
		LogLevel:    slog.LevelInfo,
		JSRuntimes:  getenv(KeyJSRuntimes),
		CookiesFile: getenv(KeyCookiesFile),
	}

	if v := getenv(KeyTempDir); v != "" {
		cfg.TempDir = v
	}

	if v := getenv(KeyLogLevel); v != "" {
		switch strings.ToLower(v) {
		case "debug":
			cfg.LogLevel = slog.LevelDebug
		case "info":
			cfg.LogLevel = slog.LevelInfo
		case "warn", "warning":
			cfg.LogLevel = slog.LevelWarn
		case "error":
			cfg.LogLevel = slog.LevelError
		default:
			return nil, fmt.Errorf("%s inválido: %q (usa debug|info|warn|error)", KeyLogLevel, v)
		}
	}

	// O ~ só é expandido pela shell na linha de comandos; num .env chegava
	// aqui literal e criava uma pasta "~".
	for _, p := range []*string{&cfg.OutputDir, &cfg.TempDir, &cfg.CookiesFile} {
		expanded, err := ExpandHome(*p, getenv)
		if err != nil {
			return nil, err
		}
		*p = expanded
	}

	if cfg.CookiesFile != "" {
		if err := CheckCookiesFile(cfg.CookiesFile); err != nil {
			return nil, fmt.Errorf("%s inválido: %w", KeyCookiesFile, err)
		}
	}

	return cfg, nil
}

// CheckCookiesFile confirma que path é um ficheiro que existe.
func CheckCookiesFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s não é um ficheiro", path)
	}
	return nil
}

// ExpandHome expande um "~" inicial a partir de $HOME.
func ExpandHome(path string, getenv func(string) string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path, nil
	}
	home := getenv("HOME")
	if home == "" {
		var err error
		if home, err = os.UserHomeDir(); err != nil {
			return "", fmt.Errorf("não foi possível expandir %q: %w", path, err)
		}
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~")), nil
}

// Save grava values (chave → valor) em path, no formato do .env. Preserva as
// outras linhas e os comentários e substitui só as chaves pedidas; uma chave
// com valor vazio é removida. Cria a pasta e deixa o ficheiro com 0600 (pode
// apontar para cookies, que são credenciais).
func Save(path string, values map[string]string) error {
	var lines []string
	if f, err := os.Open(path); err == nil {
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			lines = append(lines, scanner.Text())
		}
		f.Close()
		if err := scanner.Err(); err != nil {
			return fmt.Errorf("erro ao ler %s: %w", path, err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("erro ao ler %s: %w", path, err)
	}

	done := make(map[string]bool, len(values))
	var out []string
	for _, line := range lines {
		key, _, ok := strings.Cut(strings.TrimSpace(line), "=")
		value, wanted := values[key]
		if !ok || !wanted {
			out = append(out, line)
			continue
		}
		done[key] = true
		if value != "" {
			out = append(out, key+"="+quote(value))
		}
	}

	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if !done[k] && values[k] != "" {
			out = append(out, k+"="+quote(values[k]))
		}
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("não foi possível criar %s: %w", filepath.Dir(path), err)
	}
	tmp := path + ".tmp"
	content := strings.Join(out, "\n")
	if content != "" {
		content += "\n"
	}
	if err := os.WriteFile(tmp, []byte(content), 0o600); err != nil {
		return fmt.Errorf("não foi possível gravar %s: %w", path, err)
	}
	return os.Rename(tmp, path)
}

// quote põe entre aspas um valor com espaços ou caracteres especiais, para o
// godotenv o ler tal e qual (ex: "/home/ana/Os Meus Vídeos").
func quote(v string) string {
	if strings.ContainsAny(v, " \t#\"'\\$") {
		return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(v) + `"`
	}
	return v
}
