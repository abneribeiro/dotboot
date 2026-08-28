package cli

import (
	"context"
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
		{"flag desconhecida", []string{"-legendas", "pt", "https://youtu.be/a"}, true},
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

func TestParseArgsFormato(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantMP4 bool
		wantErr bool
	}{
		{"omissão garante mp4", []string{"https://youtu.be/x"}, true, false},
		{"mp4 explícito", []string{"-formato", "mp4", "https://youtu.be/x"}, true, false},
		{"original desliga a conversão", []string{"-formato", "original", "https://youtu.be/x"}, false, false},
		{"valor inválido", []string{"-formato", "webm", "https://youtu.be/x"}, false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o, err := parseArgs(tt.args, "")
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseArgs(%v) err = %v, wantErr = %v", tt.args, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if o.mp4 != tt.wantMP4 {
				t.Errorf("mp4 = %v, esperado %v", o.mp4, tt.wantMP4)
			}
		})
	}
}

// A altura só é guardada para avisar que acima de 1080p a conversão é certa.
func TestParseArgsGuardaAltura(t *testing.T) {
	tests := []struct {
		q    string
		want int
	}{
		{"", 0},
		{"best", 0},
		{"720", 720},
		{"2160", 2160},
	}
	for _, tt := range tests {
		t.Run(tt.q, func(t *testing.T) {
			args := []string{"https://youtu.be/x"}
			if tt.q != "" {
				args = append([]string{"-q", tt.q}, args...)
			}
			o, err := parseArgs(args, "")
			if err != nil {
				t.Fatalf("parseArgs falhou: %v", err)
			}
			if o.height != tt.want {
				t.Errorf("height = %d, esperado %d", o.height, tt.want)
			}
		})
	}
}

func TestPrintedPaths(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "video.mp4")
	if err := os.WriteFile(existing, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	listFile := filepath.Join(dir, "paths.txt")
	// Linhas vazias e um ficheiro que já não existe (apagado entretanto, ou
	// resto de uma execução anterior) não podem chegar ao ffmpeg.
	content := existing + "\n\n" + filepath.Join(dir, "apagado.mkv") + "\n" + existing + "\n"
	if err := os.WriteFile(listFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	got := printedPaths(listFile)
	want := []string{existing, existing}
	if len(got) != len(want) {
		t.Fatalf("printedPaths = %v, esperado %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("printedPaths[%d] = %q, esperado %q", i, got[i], want[i])
		}
	}
}

// Um ficheiro de caminhos inexistente acontece sempre que o archive salta tudo:
// tem de ser uma lista vazia, não um erro.
func TestPrintedPathsFicheiroInexistente(t *testing.T) {
	if got := printedPaths(filepath.Join(t.TempDir(), "nao-existe.txt")); got != nil {
		t.Errorf("printedPaths = %v, esperado nil", got)
	}
}

// -formato original não usa --print-to-file, por isso o ensurePlayable recebe
// um caminho vazio e não pode fazer nada (nem falhar).
func TestEnsurePlayableSemPathFile(t *testing.T) {
	if err := ensurePlayable(context.Background(), ""); err != nil {
		t.Errorf("ensurePlayable(\"\") = %v, esperado nil", err)
	}
}
