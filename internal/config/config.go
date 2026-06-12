// Package config carrega a configuração da aplicação a partir de variáveis
// de ambiente, com valores por omissão sensatos. Valores inválidos resultam
// em erro — nunca em fallback silencioso.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config agrega toda a configuração partilhada pelo bot e pelo modo CLI.
type Config struct {
	// DiscordToken é exigido apenas pelo subcomando bot (DISCORD_TOKEN).
	DiscordToken string

	// MaxFileSizeMB define o limite de upload do Discord em megabytes
	// (DOTBOOT_MAX_FILE_SIZE_MB, default 25). Não se aplica ao modo CLI.
	MaxFileSizeMB int

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
}

// MaxFileSizeBytes devolve o limite de upload em bytes.
func (c *Config) MaxFileSizeBytes() int64 {
	return int64(c.MaxFileSizeMB) * 1024 * 1024
}

// Load lê a configuração a partir do ambiente do processo.
func Load() (*Config, error) {
	return loadFrom(os.Getenv)
}

// loadFrom é a implementação testável de Load, com o ambiente injetado.
func loadFrom(getenv func(string) string) (*Config, error) {
	cfg := &Config{
		DiscordToken:    getenv("DISCORD_TOKEN"),
		MaxFileSizeMB:   25,
		DownloadTimeout: 10 * time.Minute,
		MaxConcurrent:   3,
		ShutdownGrace:   30 * time.Second,
		OutputDir:       getenv("DOTBOOT_OUTPUT_DIR"),
		TempDir:         os.TempDir(),
		LogLevel:        slog.LevelInfo,
	}

	if v := getenv("DOTBOOT_MAX_FILE_SIZE_MB"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return nil, fmt.Errorf("DOTBOOT_MAX_FILE_SIZE_MB inválido: %q (usa um inteiro >= 1)", v)
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

	return cfg, nil
}
