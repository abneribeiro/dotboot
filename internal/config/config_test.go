package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"slices"
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

func TestLoadFromDefaultsNovos(t *testing.T) {
	cfg, err := loadFrom(envMap(map[string]string{"HOME": "/home/ana"}))
	if err != nil {
		t.Fatalf("loadFrom devolveu erro inesperado: %v", err)
	}
	if cfg.MaxPerUser != 2 {
		t.Errorf("MaxPerUser = %d, esperado 2", cfg.MaxPerUser)
	}
	if cfg.ModelsDir != "/home/ana/.cache/dotboot/models" {
		t.Errorf("ModelsDir = %q, esperado a cache do utilizador", cfg.ModelsDir)
	}
	if cfg.CookiesFile != "" || cfg.JSRuntimes != "" || cfg.AllowedDomains != nil {
		t.Errorf("cookies/js/domínios deviam vir vazios: %q %q %v", cfg.CookiesFile, cfg.JSRuntimes, cfg.AllowedDomains)
	}

	// O XDG_CACHE_HOME tem prioridade, como nas outras ferramentas.
	cfg, err = loadFrom(envMap(map[string]string{"HOME": "/home/ana", "XDG_CACHE_HOME": "/cache"}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ModelsDir != "/cache/dotboot/models" {
		t.Errorf("ModelsDir = %q, esperado /cache/dotboot/models", cfg.ModelsDir)
	}
}

func TestLoadFromOverridesNovos(t *testing.T) {
	cookies := filepath.Join(t.TempDir(), "cookies.txt")
	if err := os.WriteFile(cookies, []byte("# Netscape HTTP Cookie File\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadFrom(envMap(map[string]string{
		"DOTBOOT_COOKIES_FILE":    cookies,
		"DOTBOOT_JS_RUNTIMES":     "node",
		"DOTBOOT_ALLOWED_DOMAINS": " YouTube.com, *.x.com ,youtu.be.,",
		"DOTBOOT_MAX_PER_USER":    "1",
		"DOTBOOT_MODELS_DIR":      "/modelos",
	}))
	if err != nil {
		t.Fatalf("loadFrom devolveu erro inesperado: %v", err)
	}
	if cfg.CookiesFile != cookies {
		t.Errorf("CookiesFile = %q, esperado %q", cfg.CookiesFile, cookies)
	}
	if cfg.JSRuntimes != "node" {
		t.Errorf("JSRuntimes = %q, esperado node", cfg.JSRuntimes)
	}
	if want := []string{"youtube.com", "x.com", "youtu.be"}; !slices.Equal(cfg.AllowedDomains, want) {
		t.Errorf("AllowedDomains = %v, esperado %v", cfg.AllowedDomains, want)
	}
	if cfg.MaxPerUser != 1 {
		t.Errorf("MaxPerUser = %d, esperado 1", cfg.MaxPerUser)
	}
	if cfg.ModelsDir != "/modelos" {
		t.Errorf("ModelsDir = %q, esperado /modelos", cfg.ModelsDir)
	}
}

// Num .env o ~ chega literal: sem expansão, o CLI criava uma pasta chamada "~".
func TestLoadFromExpandeTil(t *testing.T) {
	cfg, err := loadFrom(envMap(map[string]string{
		"HOME":               "/home/ana",
		"DOTBOOT_OUTPUT_DIR": "~/Videos",
		"DOTBOOT_TEMP_DIR":   "~",
		"DOTBOOT_MODELS_DIR": "~/modelos",
	}))
	if err != nil {
		t.Fatalf("loadFrom devolveu erro inesperado: %v", err)
	}
	if cfg.OutputDir != "/home/ana/Videos" {
		t.Errorf("OutputDir = %q, esperado /home/ana/Videos", cfg.OutputDir)
	}
	if cfg.TempDir != "/home/ana" {
		t.Errorf("TempDir = %q, esperado /home/ana", cfg.TempDir)
	}
	if cfg.ModelsDir != "/home/ana/modelos" {
		t.Errorf("ModelsDir = %q, esperado /home/ana/modelos", cfg.ModelsDir)
	}

	// Só o ~ inicial é especial: "~ana" ou um ~ a meio ficam como estão.
	cfg, err = loadFrom(envMap(map[string]string{"HOME": "/home/ana", "DOTBOOT_OUTPUT_DIR": "/dados/~backup"}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OutputDir != "/dados/~backup" {
		t.Errorf("OutputDir = %q, esperado inalterado", cfg.OutputDir)
	}
}

func TestLoadFromInvalidValuesNovos(t *testing.T) {
	dir := t.TempDir()

	tests := []struct {
		name string
		env  map[string]string
	}{
		{"por utilizador zero", map[string]string{"DOTBOOT_MAX_PER_USER": "0"}},
		{"por utilizador não numérico", map[string]string{"DOTBOOT_MAX_PER_USER": "dois"}},
		{"URL em vez de domínio", map[string]string{"DOTBOOT_ALLOWED_DOMAINS": "https://youtube.com"}},
		{"lista de domínios vazia", map[string]string{"DOTBOOT_ALLOWED_DOMAINS": " , "}},
		{"cookies inexistentes", map[string]string{"DOTBOOT_COOKIES_FILE": filepath.Join(dir, "nao-existe.txt")}},
		{"cookies é uma pasta", map[string]string{"DOTBOOT_COOKIES_FILE": dir}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := loadFrom(envMap(tt.env)); err == nil {
				t.Errorf("loadFrom(%v) devia devolver erro", tt.env)
			}
		})
	}
}
