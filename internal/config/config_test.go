package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joho/godotenv"
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
	if cfg.OutputDir != "" || cfg.CookiesFile != "" || cfg.JSRuntimes != "" {
		t.Errorf("saída/cookies/js deviam vir vazios: %+v", cfg)
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

func TestLoadFromOverrides(t *testing.T) {
	cookies := filepath.Join(t.TempDir(), "cookies.txt")
	if err := os.WriteFile(cookies, []byte("# Netscape HTTP Cookie File\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadFrom(envMap(map[string]string{
		KeyOutputDir:   "/videos",
		KeyTempDir:     "/scratch",
		KeyLogLevel:    "debug",
		KeyCookiesFile: cookies,
		KeyJSRuntimes:  "node",
		KeyModelsDir:   "/modelos",
	}))
	if err != nil {
		t.Fatalf("loadFrom devolveu erro inesperado: %v", err)
	}
	if cfg.OutputDir != "/videos" || cfg.TempDir != "/scratch" || cfg.ModelsDir != "/modelos" {
		t.Errorf("caminhos = %q %q %q", cfg.OutputDir, cfg.TempDir, cfg.ModelsDir)
	}
	if cfg.LogLevel != slog.LevelDebug || cfg.CookiesFile != cookies || cfg.JSRuntimes != "node" {
		t.Errorf("config = %+v", cfg)
	}
}

// Num .env o ~ chega literal: sem expansão, o CLI criava uma pasta chamada "~".
func TestLoadFromExpandeTil(t *testing.T) {
	cfg, err := loadFrom(envMap(map[string]string{
		"HOME":       "/home/ana",
		KeyOutputDir: "~/Videos",
		KeyTempDir:   "~",
	}))
	if err != nil {
		t.Fatalf("loadFrom devolveu erro inesperado: %v", err)
	}
	if cfg.OutputDir != "/home/ana/Videos" || cfg.TempDir != "/home/ana" {
		t.Errorf("OutputDir/TempDir = %q/%q", cfg.OutputDir, cfg.TempDir)
	}

	// Só o ~ inicial é especial.
	cfg, err = loadFrom(envMap(map[string]string{"HOME": "/home/ana", KeyOutputDir: "/dados/~backup"}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OutputDir != "/dados/~backup" {
		t.Errorf("OutputDir = %q, esperado inalterado", cfg.OutputDir)
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

func TestSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.env")

	// Ficheiro novo: cria a pasta, escreve ordenado e com 0600.
	if err := Save(path, map[string]string{KeyOutputDir: "/home/ana/Os Meus Vídeos", KeyModelsDir: "/m"}); err != nil {
		t.Fatalf("Save falhou: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("permissões = %o, esperado 600", info.Mode().Perm())
	}

	// Segunda gravação: preserva o resto, substitui e remove (valor vazio).
	if err := os.WriteFile(path, []byte("# comentário\nOUTRA=1\n"+KeyOutputDir+"=/antigo\n"+KeyModelsDir+"=/m\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Save(path, map[string]string{KeyOutputDir: "/novo", KeyModelsDir: ""}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	got := string(data)
	for _, want := range []string{"# comentário", "OUTRA=1", KeyOutputDir + "=/novo"} {
		if !strings.Contains(got, want) {
			t.Errorf("falta %q em:\n%s", want, got)
		}
	}
	if strings.Contains(got, KeyModelsDir) || strings.Contains(got, "/antigo") {
		t.Errorf("a chave vazia devia sair e a antiga ser substituída:\n%s", got)
	}

	// Um valor com espaços tem de voltar igual pelo godotenv.
	if err := Save(path, map[string]string{KeyOutputDir: `/home/ana/Os "Meus" Vídeos`}); err != nil {
		t.Fatal(err)
	}
	env, err := godotenv.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if env[KeyOutputDir] != `/home/ana/Os "Meus" Vídeos` {
		t.Errorf("valor lido = %q", env[KeyOutputDir])
	}
}

func TestFilePathSegueXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/xdg")
	if got := FilePath(); got != "/xdg/dotboot/config.env" {
		t.Errorf("FilePath = %q", got)
	}
}
