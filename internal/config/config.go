// Package config carrega a configuração da aplicação a partir de variáveis
// de ambiente, com valores por omissão sensatos. Valores inválidos resultam
// em erro — nunca em fallback silencioso.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Config agrega toda a configuração partilhada pelo bot e pelo modo CLI.
type Config struct {
	// DiscordToken é exigido apenas pelo subcomando bot (DISCORD_TOKEN).
	DiscordToken string

	// MaxFileSizeMB é um teto opcional para o upload do bot, em megabytes
	// (DOTBOOT_MAX_FILE_SIZE_MB). Zero — o default — significa "sem teto":
	// o limite é o que o Discord permite no servidor onde o comando corre
	// (ver internal/bot/limits.go). Um valor definido nunca aumenta esse
	// limite, apenas o baixa. Não se aplica ao modo CLI.
	MaxFileSizeMB int

	// GuildID, quando definido (DOTBOOT_GUILD_ID), regista o comando apenas
	// nesse servidor, com propagação instantânea — útil em desenvolvimento.
	// Vazio regista globalmente (propagação até 1 hora).
	GuildID string

	// DownloadTimeout limita cada download do bot (DOTBOOT_DOWNLOAD_TIMEOUT,
	// default 10m, sintaxe de time.ParseDuration). Não se aplica ao modo CLI.
	DownloadTimeout time.Duration

	// MaxConcurrent limita downloads simultâneos no bot
	// (DOTBOOT_MAX_CONCURRENT, default 3).
	MaxConcurrent int

	// ShutdownGrace é o tempo que o bot espera por downloads em curso
	// antes de os cancelar no encerramento (DOTBOOT_SHUTDOWN_GRACE, default 30s).
	ShutdownGrace time.Duration

	// OutputDir é o caminho base de saída do modo CLI (DOTBOOT_OUTPUT_DIR):
	// usado quando -o não é passado, e raiz de um -o relativo. Um -o absoluto
	// ignora-o. Vazio = diretório atual. Não se aplica ao bot, que usa TempDir.
	OutputDir string

	// TempDir é onde o bot guarda ficheiros temporários
	// (DOTBOOT_TEMP_DIR, default os.TempDir()).
	TempDir string

	// LogLevel controla a verbosidade (DOTBOOT_LOG_LEVEL: debug|info|warn|error).
	LogLevel slog.Level

	// CookiesFile é um ficheiro de cookies em formato Netscape
	// (DOTBOOT_COOKIES_FILE), passado ao yt-dlp com --cookies em ambos os
	// modos. Resolve a verificação "não és um robô" do YouTube, a restrição
	// de idade e o conteúdo que exige login. Tem de existir quando definido.
	CookiesFile string

	// JSRuntimes é passado ao yt-dlp como --js-runtimes (DOTBOOT_JS_RUNTIMES,
	// ex: "node"). Vazio usa o default do yt-dlp, o Deno; o main preenche-o
	// com "node" quando o Deno falta e o node existe.
	JSRuntimes string

	// AllowedDomains, quando não vazio (DOTBOOT_ALLOWED_DOMAINS, separados
	// por vírgulas), limita o bot a esses domínios e aos seus subdomínios.
	// Não se aplica ao modo CLI.
	AllowedDomains []string

	// MaxPerUser limita os downloads de cada utilizador do bot, em curso ou
	// em fila (DOTBOOT_MAX_PER_USER, default 2): sem ele, uma só pessoa
	// enchia a fila de todos.
	MaxPerUser int

	// ModelsDir guarda os modelos da separação de stems, descarregados na
	// primeira utilização de cada um (DOTBOOT_MODELS_DIR, default
	// ~/.cache/dotboot/models).
	ModelsDir string
}

// Load lê a configuração a partir do ambiente do processo.
func Load() (*Config, error) {
	return loadFrom(os.Getenv)
}

// loadFrom é a implementação testável de Load, com o ambiente injetado.
func loadFrom(getenv func(string) string) (*Config, error) {
	cfg := &Config{
		DiscordToken:    getenv("DISCORD_TOKEN"),
		GuildID:         getenv("DOTBOOT_GUILD_ID"),
		MaxFileSizeMB:   0,
		DownloadTimeout: 10 * time.Minute,
		MaxConcurrent:   3,
		ShutdownGrace:   30 * time.Second,
		OutputDir:       getenv("DOTBOOT_OUTPUT_DIR"),
		TempDir:         os.TempDir(),
		LogLevel:        slog.LevelInfo,
		JSRuntimes:      getenv("DOTBOOT_JS_RUNTIMES"),
		MaxPerUser:      2,
		ModelsDir:       defaultModelsDir(getenv),
	}

	if v := getenv("DOTBOOT_MAX_FILE_SIZE_MB"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return nil, fmt.Errorf("DOTBOOT_MAX_FILE_SIZE_MB inválido: %q (usa um inteiro >= 1, ou remove a variável para usar o limite do servidor)", v)
		}
		cfg.MaxFileSizeMB = n
	}

	if v := getenv("DOTBOOT_DOWNLOAD_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			return nil, fmt.Errorf("DOTBOOT_DOWNLOAD_TIMEOUT inválido: %q (usa ex: 10m, 1h30m)", v)
		}
		cfg.DownloadTimeout = d
	}

	if v := getenv("DOTBOOT_MAX_CONCURRENT"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return nil, fmt.Errorf("DOTBOOT_MAX_CONCURRENT inválido: %q (usa um inteiro >= 1)", v)
		}
		cfg.MaxConcurrent = n
	}

	if v := getenv("DOTBOOT_SHUTDOWN_GRACE"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d < 0 {
			return nil, fmt.Errorf("DOTBOOT_SHUTDOWN_GRACE inválido: %q (usa ex: 30s, 1m)", v)
		}
		cfg.ShutdownGrace = d
	}

	if v := getenv("DOTBOOT_TEMP_DIR"); v != "" {
		cfg.TempDir = v
	}

	if v := getenv("DOTBOOT_LOG_LEVEL"); v != "" {
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
			return nil, fmt.Errorf("DOTBOOT_LOG_LEVEL inválido: %q (usa debug|info|warn|error)", v)
		}
	}

	if v := getenv("DOTBOOT_MAX_PER_USER"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return nil, fmt.Errorf("DOTBOOT_MAX_PER_USER inválido: %q (usa um inteiro >= 1)", v)
		}
		cfg.MaxPerUser = n
	}

	if v := getenv("DOTBOOT_ALLOWED_DOMAINS"); v != "" {
		domains, err := parseDomains(v)
		if err != nil {
			return nil, err
		}
		cfg.AllowedDomains = domains
	}

	if v := getenv("DOTBOOT_MODELS_DIR"); v != "" {
		cfg.ModelsDir = v
	}
	cfg.CookiesFile = getenv("DOTBOOT_COOKIES_FILE")

	// O ~ só é expandido pela shell na linha de comandos; num .env ou num
	// "environment:" do compose chegava aqui literal e criava uma pasta "~".
	for _, p := range []*string{&cfg.OutputDir, &cfg.TempDir, &cfg.ModelsDir, &cfg.CookiesFile} {
		expanded, err := expandHome(*p, getenv)
		if err != nil {
			return nil, err
		}
		*p = expanded
	}

	if cfg.CookiesFile != "" {
		info, err := os.Stat(cfg.CookiesFile)
		if err != nil {
			return nil, fmt.Errorf("DOTBOOT_COOKIES_FILE inválido: %w", err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("DOTBOOT_COOKIES_FILE inválido: %s não é um ficheiro", cfg.CookiesFile)
		}
	}

	return cfg, nil
}

// parseDomains interpreta DOTBOOT_ALLOWED_DOMAINS: domínios separados por
// vírgulas, normalizados em minúsculas e sem "*." nem pontos nas pontas. Um
// URL colado por engano (com esquema ou caminho) é rejeitado em vez de nunca
// corresponder a nada.
func parseDomains(v string) ([]string, error) {
	var domains []string
	for _, d := range strings.Split(v, ",") {
		d = strings.ToLower(strings.TrimSpace(d))
		d = strings.Trim(strings.TrimPrefix(d, "*."), ".")
		if d == "" {
			continue
		}
		if strings.ContainsAny(d, "/: ") {
			return nil, fmt.Errorf("DOTBOOT_ALLOWED_DOMAINS inválido: %q (usa só domínios, ex: youtube.com,x.com)", d)
		}
		domains = append(domains, d)
	}
	if len(domains) == 0 {
		return nil, fmt.Errorf("DOTBOOT_ALLOWED_DOMAINS inválido: %q não tem nenhum domínio", v)
	}
	return domains, nil
}

// expandHome expande um "~" inicial a partir de $HOME.
func expandHome(path string, getenv func(string) string) (string, error) {
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
