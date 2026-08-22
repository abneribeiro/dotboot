package config

import (
	"log/slog"
	"testing"
	"time"
)

// envMap devolve um getenv baseado num mapa, para testes determinísticos.
func envMap(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadFromDefaults(t *testing.T) {
	cfg, err := loadFrom(envMap(nil))
	if err != nil {
		t.Fatalf("loadFrom devolveu erro inesperado: %v", err)
	}

	// Zero = sem teto; o limite real vem do nível de boost do servidor.
	if cfg.MaxFileSizeMB != 0 {
		t.Errorf("MaxFileSizeMB = %d, esperado 0 (sem teto)", cfg.MaxFileSizeMB)
	}
	if cfg.GuildID != "" {
		t.Errorf("GuildID = %q, esperado vazio", cfg.GuildID)
	}
	if cfg.DownloadTimeout != 10*time.Minute {
		t.Errorf("DownloadTimeout = %v, esperado 10m", cfg.DownloadTimeout)
	}
	if cfg.MaxConcurrent != 3 {
		t.Errorf("MaxConcurrent = %d, esperado 3", cfg.MaxConcurrent)
	}
	if cfg.ShutdownGrace != 30*time.Second {
		t.Errorf("ShutdownGrace = %v, esperado 30s", cfg.ShutdownGrace)
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Errorf("LogLevel = %v, esperado info", cfg.LogLevel)
	}
}

func TestLoadFromOverrides(t *testing.T) {
	cfg, err := loadFrom(envMap(map[string]string{
		"DISCORD_TOKEN":            "token123",
		"DOTBOOT_GUILD_ID":         "123456789",
		"DOTBOOT_MAX_FILE_SIZE_MB": "50",
		"DOTBOOT_DOWNLOAD_TIMEOUT": "30m",
		"DOTBOOT_MAX_CONCURRENT":   "5",
		"DOTBOOT_SHUTDOWN_GRACE":   "1m",
		"DOTBOOT_OUTPUT_DIR":       "/videos",
		"DOTBOOT_TEMP_DIR":         "/scratch",
		"DOTBOOT_LOG_LEVEL":        "debug",
	}))
	if err != nil {
		t.Fatalf("loadFrom devolveu erro inesperado: %v", err)
	}

	if cfg.DiscordToken != "token123" {
		t.Errorf("DiscordToken = %q, esperado token123", cfg.DiscordToken)
	}
	if cfg.GuildID != "123456789" {
		t.Errorf("GuildID = %q, esperado 123456789", cfg.GuildID)
	}
	if cfg.MaxFileSizeMB != 50 {
		t.Errorf("MaxFileSizeMB = %d, esperado 50", cfg.MaxFileSizeMB)
	}
	if cfg.DownloadTimeout != 30*time.Minute {
		t.Errorf("DownloadTimeout = %v, esperado 30m", cfg.DownloadTimeout)
	}
	if cfg.MaxConcurrent != 5 {
		t.Errorf("MaxConcurrent = %d, esperado 5", cfg.MaxConcurrent)
	}
	if cfg.ShutdownGrace != time.Minute {
		t.Errorf("ShutdownGrace = %v, esperado 1m", cfg.ShutdownGrace)
	}
	if cfg.OutputDir != "/videos" {
		t.Errorf("OutputDir = %q, esperado /videos", cfg.OutputDir)
	}
	if cfg.TempDir != "/scratch" {
		t.Errorf("TempDir = %q, esperado /scratch", cfg.TempDir)
	}
	if cfg.LogLevel != slog.LevelDebug {
		t.Errorf("LogLevel = %v, esperado debug", cfg.LogLevel)
	}
}

func TestLoadFromInvalidValues(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
	}{
		{"tamanho não numérico", map[string]string{"DOTBOOT_MAX_FILE_SIZE_MB": "abc"}},
		{"tamanho zero", map[string]string{"DOTBOOT_MAX_FILE_SIZE_MB": "0"}},
		{"tamanho negativo", map[string]string{"DOTBOOT_MAX_FILE_SIZE_MB": "-5"}},
		{"timeout inválido", map[string]string{"DOTBOOT_DOWNLOAD_TIMEOUT": "dez minutos"}},
		{"timeout zero", map[string]string{"DOTBOOT_DOWNLOAD_TIMEOUT": "0s"}},
		{"concorrência não numérica", map[string]string{"DOTBOOT_MAX_CONCURRENT": "muitos"}},
		{"concorrência zero", map[string]string{"DOTBOOT_MAX_CONCURRENT": "0"}},
		{"grace inválido", map[string]string{"DOTBOOT_SHUTDOWN_GRACE": "depois"}},
		{"nível de log desconhecido", map[string]string{"DOTBOOT_LOG_LEVEL": "verboso"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := loadFrom(envMap(tt.env)); err == nil {
				t.Errorf("loadFrom(%v) devia devolver erro", tt.env)
			}
		})
	}
}
