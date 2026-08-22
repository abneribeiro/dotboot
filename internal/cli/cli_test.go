package cli

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestParseArgsOutputDirPrecedence(t *testing.T) {
	wd, _ := os.Getwd()

	tests := []struct {
		name string
		args []string
		base string
		want string
	}{
		{"-o absoluto ignora a base", []string{"-o", "/da/flag", "https://youtu.be/abc"}, "/do/env", "/da/flag"},
		{"-o relativo é resolvido sob a base", []string{"-o", "musica", "https://youtu.be/abc"}, "/do/env", "/do/env/musica"},
		{"-o relativo sem base fica relativo ao diretório atual", []string{"-o", "musica", "https://youtu.be/abc"}, "", "musica"},
		{"sem -o usa a base", []string{"https://youtu.be/abc"}, "/do/env", "/do/env"},
		{"sem -o nem base cai para o diretório atual", []string{"https://youtu.be/abc"}, "", wd},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o, err := parseArgs(tt.args, tt.base)
			if err != nil {
				t.Fatalf("parseArgs devolveu erro inesperado: %v", err)
			}
			if o.outputDir != tt.want {
				t.Errorf("outputDir = %q, esperado %q", o.outputDir, tt.want)
			}
		})
	}
}

// batchFile cria um ficheiro de URLs temporário e devolve o seu caminho.
func batchFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "urls.txt")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("não foi possível criar o ficheiro de teste: %v", err)
	}
	return path
}

func TestParseArgsValidation(t *testing.T) {
	urls := batchFile(t, "https://youtu.be/a\nhttps://youtu.be/b\n")

	tests := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{"sem URLs nem batch-file", []string{"-o", "/videos"}, true},
		{"batch-file sem URLs é aceite", []string{"-a", urls}, false},
		{"vários URLs", []string{"https://youtu.be/a", "https://youtu.be/b"}, false},
		{"URL inválido", []string{"youtube.com/sem-esquema"}, true},
		{"recorte válido", []string{"-start", "01:00", "-end", "02:00", "https://youtu.be/a"}, false},
		{"start sem end", []string{"-start", "01:00", "https://youtu.be/a"}, true},
		{"end sem start", []string{"-end", "02:00", "https://youtu.be/a"}, true},
		{"recorte com dois URLs", []string{"-start", "01:00", "-end", "02:00", "https://youtu.be/a", "https://youtu.be/b"}, true},
		{"recorte com batch-file", []string{"-start", "01:00", "-end", "02:00", "-a", urls}, true},
		{"tempo de recorte inválido", []string{"-start", "abc", "-end", "02:00", "https://youtu.be/a"}, true},
		{"recorte invertido", []string{"-start", "02:00", "-end", "01:00", "https://youtu.be/a"}, true},
		{"recorte de duração nula", []string{"-start", "01:00", "-end", "01:00", "https://youtu.be/a"}, true},
		{"batch-file inexistente", []string{"-a", "/nao/existe/urls.txt"}, true},
		{"qualidade por altura é aceite", []string{"-q", "720", "https://youtu.be/a"}, false},
		{"qualidade best é aceite", []string{"-q", "best", "https://youtu.be/a"}, false},
		{"qualidade inválida", []string{"-q", "abc", "https://youtu.be/a"}, true},
		{"flag desconhecida", []string{"-formato", "mp4", "https://youtu.be/a"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseArgs(tt.args, "/videos")
			if (err != nil) != tt.wantErr {
				t.Errorf("parseArgs(%v) err = %v, wantErr = %v", tt.args, err, tt.wantErr)
			}
		})
	}
}

func TestParseArgsQualitySort(t *testing.T) {
	// Sem -q, a ordenação fica vazia (yt-dlp usa a melhor qualidade).
	o, err := parseArgs([]string{"https://youtu.be/abc"}, "")
	if err != nil {
		t.Fatalf("parseArgs devolveu erro inesperado: %v", err)
	}
	if o.formatSort != "" {
		t.Errorf("formatSort = %q, esperado vazio sem -q", o.formatSort)
	}
	if o.format != "" {
		t.Errorf("format = %q, esperado vazio sem -q", o.format)
	}

	// -q 720 traduz-se na ordenação por resolução preferida.
	o, err = parseArgs([]string{"-q", "720", "https://youtu.be/abc"}, "")
	if err != nil {
		t.Fatalf("parseArgs devolveu erro inesperado: %v", err)
	}
	if o.formatSort != "res:720" {
		t.Errorf("formatSort = %q, esperado res:720", o.formatSort)
	}
	// O -q é anunciado como altura MÁXIMA, por isso tem de produzir também
	// um filtro -f — só a ordenação seria apenas uma preferência.
	if !strings.Contains(o.format, "height<=?720") {
		t.Errorf("format = %q, esperado um limite de altura", o.format)
	}
}

// Pedir ajuda não é um erro: o -h tem de sair a 0, não com "erro fatal".
func TestParseArgsHelp(t *testing.T) {
	for _, flag := range []string{"-h", "-help"} {
		_, err := parseArgs([]string{flag}, "")
		if !errors.Is(err, errHelp) {
			t.Errorf("parseArgs(%q) = %v, esperado errHelp", flag, err)
		}
	}
}

func TestValidateBatchFile(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantErr bool
	}{
		{"URLs válidos", "https://youtu.be/a\nhttps://youtu.be/b\n", false},
		{"linhas vazias e comentários são ignorados", "# lista\n\nhttps://youtu.be/a\n", false},
		{"URL inválido é rejeitado", "https://youtu.be/a\nyoutube.com/sem-esquema\n", true},
		{"ficheiro sem URLs é rejeitado", "# só comentários\n\n", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateBatchFile(batchFile(t, tt.content))
			if (err != nil) != tt.wantErr {
				t.Errorf("validateBatchFile() = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}

// O flag stdlib para no primeiro argumento não-flag: flags depois dos URLs
// são tratadas como URLs. Este teste documenta esse comportamento.
func TestParseArgsFlagsAfterURLs(t *testing.T) {
	_, err := parseArgs([]string{"https://youtu.be/a", "-o", "/videos"}, "")
	if err == nil {
		t.Error("parseArgs devia rejeitar '-o' posicionado depois do URL (validado como URL inválido)")
	}

	o, err := parseArgs([]string{"-o", "/videos", "https://youtu.be/a", "https://youtu.be/b"}, "")
	if err != nil {
		t.Fatalf("parseArgs devolveu erro inesperado: %v", err)
	}
	if !slices.Equal(o.urls, []string{"https://youtu.be/a", "https://youtu.be/b"}) {
		t.Errorf("urls = %v", o.urls)
	}
}

func TestCountArchive(t *testing.T) {
	dir := t.TempDir()

	// Sem archive (primeira execução, ou recorte) conta zero.
	if got := countArchive(filepath.Join(dir, "nao-existe.txt")); got != 0 {
		t.Errorf("countArchive de ficheiro inexistente = %d, esperado 0", got)
	}
	if got := countArchive(""); got != 0 {
		t.Errorf("countArchive(\"\") = %d, esperado 0", got)
	}

	path := filepath.Join(dir, "archive.txt")
	if err := os.WriteFile(path, []byte("youtube abc\nyoutube def\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := countArchive(path); got != 2 {
		t.Errorf("countArchive = %d, esperado 2", got)
	}
}
