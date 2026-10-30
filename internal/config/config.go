// Package config carrega a configuração do dotboot a partir de variáveis de
// ambiente, com valores por omissão sensatos. Valores inválidos resultam em
// erro — nunca em fallback silencioso.
//
// As variáveis podem vir do ambiente, de um .env no diretório atual ou do
// ficheiro global ~/.config/dotboot/config.env (ver LoadFiles), por esta
// ordem de prioridade.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"
)

// Config agrega a configuração do servidor.
type Config struct {
	// DataDir guarda tudo o que o dotboot produz — separações, downloads,
	// uploads e cookies (DOTBOOT_DATA_DIR, default ~/.local/share/dotboot).
	// Nada sai daqui sem o utilizador o descarregar pelo browser.
	DataDir string

	// TempDir guarda o trabalho intermédio da separação de stems
	// (DOTBOOT_TEMP_DIR, default os.TempDir()).
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

	// ModelsDir guarda os modelos da separação de stems, descarregados na
	// primeira utilização de cada um (DOTBOOT_MODELS_DIR, default
	// ~/.cache/dotboot/models).
	ModelsDir string
}

// Chaves das variáveis de ambiente.
const (
	KeyDataDir     = "DOTBOOT_DATA_DIR"
	KeyTempDir     = "DOTBOOT_TEMP_DIR"
	KeyLogLevel    = "DOTBOOT_LOG_LEVEL"
	KeyCookiesFile = "DOTBOOT_COOKIES_FILE"
	KeyJSRuntimes  = "DOTBOOT_JS_RUNTIMES"
	KeyModelsDir   = "DOTBOOT_MODELS_DIR"
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
	_ = godotenv.Load(FilePath()) // opcional: o utilizador cria-o se quiser
}

// Load lê a configuração a partir do ambiente do processo.
func Load() (*Config, error) {
	return loadFrom(os.Getenv)
}

// loadFrom é a implementação testável de Load, com o ambiente injetado.
func loadFrom(getenv func(string) string) (*Config, error) {
	cfg := &Config{
		DataDir:     defaultDataDir(getenv),
		TempDir:     os.TempDir(),
		LogLevel:    slog.LevelInfo,
		JSRuntimes:  getenv(KeyJSRuntimes),
		ModelsDir:   defaultModelsDir(getenv),
		CookiesFile: getenv(KeyCookiesFile),
	}

	if v := getenv(KeyDataDir); v != "" {
		cfg.DataDir = v
	}
	if v := getenv(KeyTempDir); v != "" {
		cfg.TempDir = v
	}
	if v := getenv(KeyModelsDir); v != "" {
		cfg.ModelsDir = v
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
	for _, p := range []*string{&cfg.DataDir, &cfg.TempDir, &cfg.ModelsDir, &cfg.CookiesFile} {
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

// defaultDataDir segue a convenção XDG para dados de aplicações.
func defaultDataDir(getenv func(string) string) string {
	if dir := getenv("XDG_DATA_HOME"); dir != "" {
		return filepath.Join(dir, "dotboot")
	}
	if home := getenv("HOME"); home != "" {
		return filepath.Join(home, ".local", "share", "dotboot")
	}
	return filepath.Join(os.TempDir(), "dotboot")
}

// defaultModelsDir segue a convenção XDG para caches, como o resto das
// ferramentas que descarregam modelos.
func defaultModelsDir(getenv func(string) string) string {
	if dir := getenv("XDG_CACHE_HOME"); dir != "" {
		return filepath.Join(dir, "dotboot", "models")
	}
	if home := getenv("HOME"); home != "" {
		return filepath.Join(home, ".cache", "dotboot", "models")
	}
	if dir, err := os.UserCacheDir(); err == nil {
		return filepath.Join(dir, "dotboot", "models")
	}
	return filepath.Join(os.TempDir(), "dotboot-models")
}
