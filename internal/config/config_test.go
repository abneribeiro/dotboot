package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

// envMap devolve um getenv baseado num mapa, para testes determinísticos.
func envMap(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadFromDefaults(t *testing.T) {
	cfg, err := loadFrom(envMap(map[string]string{"HOME": "/home/ana"}))
	if err != nil {
		t.Fatalf("loadFrom devolveu erro inesperado: %v", err)
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Errorf("LogLevel = %v, esperado info", cfg.LogLevel)
	}
	if cfg.ModelsDir != "/home/ana/.cache/dotboot/models" {
		t.Errorf("ModelsDir = %q, esperado a cache do utilizador", cfg.ModelsDir)
	}
	if cfg.DataDir != "/home/ana/.local/share/dotboot" {
		t.Errorf("DataDir = %q, esperado ~/.local/share/dotboot", cfg.DataDir)
	}
	if cfg.CookiesFile != "" || cfg.JSRuntimes != "" {
		t.Errorf("cookies/js deviam vir vazios: %+v", cfg)
	}

	// O XDG_CACHE_HOME tem prioridade, como nas outras ferramentas.
	cfg, err = loadFrom(envMap(map[string]string{"HOME": "/home/ana", "XDG_CACHE_HOME": "/cache", "XDG_DATA_HOME": "/dados"}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ModelsDir != "/cache/dotboot/models" || cfg.DataDir != "/dados/dotboot" {
		t.Errorf("ModelsDir/DataDir = %q/%q, esperado o XDG", cfg.ModelsDir, cfg.DataDir)
	}
}

func TestLoadFromOverrides(t *testing.T) {
	cookies := filepath.Join(t.TempDir(), "cookies.txt")
	if err := os.WriteFile(cookies, []byte("# Netscape HTTP Cookie File\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadFrom(envMap(map[string]string{
		KeyDataDir:     "/dados",
		KeyTempDir:     "/scratch",
		KeyLogLevel:    "debug",
		KeyCookiesFile: cookies,
		KeyJSRuntimes:  "node",
		KeyModelsDir:   "/modelos",
	}))
	if err != nil {
		t.Fatalf("loadFrom devolveu erro inesperado: %v", err)
	}
	if cfg.DataDir != "/dados" || cfg.TempDir != "/scratch" || cfg.ModelsDir != "/modelos" {
		t.Errorf("caminhos = %q %q %q", cfg.DataDir, cfg.TempDir, cfg.ModelsDir)
	}
	if cfg.LogLevel != slog.LevelDebug || cfg.CookiesFile != cookies || cfg.JSRuntimes != "node" {
		t.Errorf("config = %+v", cfg)
	}
}

// Num .env o ~ chega literal: sem expansão, criava-se uma pasta chamada "~".
func TestLoadFromExpandeTil(t *testing.T) {
	cfg, err := loadFrom(envMap(map[string]string{
		"HOME":     "/home/ana",
		KeyDataDir: "~/dotboot",
		KeyTempDir: "~",
	}))
	if err != nil {
		t.Fatalf("loadFrom devolveu erro inesperado: %v", err)
	}
	if cfg.DataDir != "/home/ana/dotboot" || cfg.TempDir != "/home/ana" {
		t.Errorf("DataDir/TempDir = %q/%q", cfg.DataDir, cfg.TempDir)
	}

	// Só o ~ inicial é especial.
	cfg, err = loadFrom(envMap(map[string]string{"HOME": "/home/ana", KeyDataDir: "/dados/~backup"}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DataDir != "/dados/~backup" {
		t.Errorf("DataDir = %q, esperado inalterado", cfg.DataDir)
	}
}

func TestLoadFromInvalidValues(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		name string
		env  map[string]string
	}{
		{"nível de log desconhecido", map[string]string{KeyLogLevel: "verboso"}},
		{"cookies inexistentes", map[string]string{KeyCookiesFile: filepath.Join(dir, "nao-existe.txt")}},
		{"cookies é uma pasta", map[string]string{KeyCookiesFile: dir}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := loadFrom(envMap(tt.env)); err == nil {
				t.Errorf("loadFrom(%v) devia devolver erro", tt.env)
			}
		})
	}
}

func TestFilePathSegueXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/xdg")
	if got := FilePath(); got != "/xdg/dotboot/config.env" {
		t.Errorf("FilePath = %q", got)
	}
}
