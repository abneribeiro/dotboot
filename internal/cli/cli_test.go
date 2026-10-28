package cli

import (
	"context"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/abneribeiro/dotboot/internal/downloader"
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

	// -q 720 traduz-se em res:720, que o yt-dlp trata como limite com
	// fallback e mede no lado menor — 720x1280 num vídeo vertical.
	o, err = parseArgs([]string{"-q", "720", "https://youtu.be/abc"}, "")
	if err != nil {
		t.Fatalf("parseArgs devolveu erro inesperado: %v", err)
	}
	if o.formatSort != "res:720" {
		t.Errorf("formatSort = %q, esperado res:720", o.formatSort)
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

// O flag da stdlib para no primeiro argumento que não é flag, e um "-o"
// depois do URL era validado como URL. As flags valem em qualquer posição.
func TestParseArgsFlagsAfterURLs(t *testing.T) {
	o, err := parseArgs([]string{"https://youtu.be/a", "-o", "/videos", "https://youtu.be/b", "-q", "720"}, "")
	if err != nil {
		t.Fatalf("parseArgs devolveu erro inesperado: %v", err)
	}
	if o.outputDir != "/videos" {
		t.Errorf("outputDir = %q, esperado /videos", o.outputDir)
	}
	if o.formatSort != "res:720" {
		t.Errorf("formatSort = %q, esperado res:720", o.formatSort)
	}
	if !slices.Equal(o.urls, []string{"https://youtu.be/a", "https://youtu.be/b"}) {
		t.Errorf("urls = %v", o.urls)
	}
}

func TestParseInterspersed(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantPos  []string
		wantFlag string
	}{
		{"só posicionais", []string{"a", "b"}, []string{"a", "b"}, ""},
		{"flag antes", []string{"-x", "1", "a"}, []string{"a"}, "1"},
		{"flag no meio", []string{"a", "-x", "1", "b"}, []string{"a", "b"}, "1"},
		{"flag no fim", []string{"a", "b", "-x", "1"}, []string{"a", "b"}, "1"},
		// Depois de "--" nada é flag, nem o que começa por "-".
		{"depois de -- tudo é posicional", []string{"a", "--", "-x", "1"}, []string{"a", "-x", "1"}, ""},
		{"-- no início", []string{"--", "-x"}, []string{"-x"}, ""},
		{"vazio", nil, nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := flag.NewFlagSet("t", flag.ContinueOnError)
			fs.SetOutput(io.Discard)
			x := fs.String("x", "", "")

			pos, err := parseInterspersed(fs, tt.args)
			if err != nil {
				t.Fatalf("parseInterspersed(%v) devolveu erro: %v", tt.args, err)
			}
			if !slices.Equal(pos, tt.wantPos) {
				t.Errorf("posicionais = %v, esperado %v", pos, tt.wantPos)
			}
			if *x != tt.wantFlag {
				t.Errorf("-x = %q, esperado %q", *x, tt.wantFlag)
			}
		})
	}
}

func TestOutputTemplate(t *testing.T) {
	full := outputTemplate("/videos", nil)
	if full != "/videos/%(title)s [%(id)s].%(ext)s" {
		t.Errorf("outputTemplate sem recorte = %q", full)
	}

	// Com o mesmo nome do vídeo completo, o yt-dlp dizia "already
	// downloaded" e o archive ficava com o recorte no lugar do vídeo.
	trimmed := outputTemplate("/videos", &downloader.Section{Start: "01:30", End: "00:02:00"})
	if trimmed == full {
		t.Fatal("o recorte não pode ter o mesmo nome do vídeo completo")
	}
	if want := "/videos/%(title)s [%(id)s] (01.30-00.02.00).%(ext)s"; trimmed != want {
		t.Errorf("outputTemplate com recorte = %q, esperado %q", trimmed, want)
	}
	if strings.Contains(filepath.Base(trimmed), ":") {
		t.Errorf("o nome não pode ter ':' (inválido no Windows/macOS): %q", trimmed)
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
		name      string
		args      []string
		wantMode  string
		wantAudio bool
		wantErr   bool
	}{
		{"omissão garante mp4", []string{"https://youtu.be/x"}, "mp4", false, false},
		{"mp4 explícito", []string{"-formato", "mp4", "https://youtu.be/x"}, "mp4", false, false},
		{"original desliga a conversão", []string{"-formato", "original", "https://youtu.be/x"}, "original", false, false},
		{"mp3 guarda só o áudio", []string{"-formato", "mp3", "https://youtu.be/x"}, "mp3", true, false},
		{"m4a guarda só o áudio", []string{"-formato", "m4a", "https://youtu.be/x"}, "m4a", true, false},
		{"valor inválido", []string{"-formato", "webm", "https://youtu.be/x"}, "", false, true},
		// A resolução não tem significado num ficheiro só de áudio.
		{"-q com áudio é recusado", []string{"-formato", "mp3", "-q", "720", "https://youtu.be/x"}, "", false, true},
		{"-q best com áudio é aceite", []string{"-formato", "mp3", "-q", "best", "https://youtu.be/x"}, "mp3", true, false},
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
			if o.mode != tt.wantMode {
				t.Errorf("mode = %q, esperado %q", o.mode, tt.wantMode)
			}
			if o.audio() != tt.wantAudio {
				t.Errorf("audio() = %v, esperado %v", o.audio(), tt.wantAudio)
			}
		})
	}
}

func TestParseArgsCookies(t *testing.T) {
	cookies := batchFile(t, "# Netscape HTTP Cookie File\n")

	o, err := parseArgs([]string{"-cookies", cookies, "https://youtu.be/x"}, "")
	if err != nil {
		t.Fatalf("parseArgs devolveu erro inesperado: %v", err)
	}
	if o.cookies != cookies {
		t.Errorf("cookies = %q, esperado %q", o.cookies, cookies)
	}

	if _, err := parseArgs([]string{"-cookies", "/nao/existe.txt", "https://youtu.be/x"}, ""); err == nil {
		t.Error("parseArgs devia rejeitar um ficheiro de cookies inexistente")
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

	// A lista acumula entre execuções: o mesmo caminho aparece uma só vez.
	got := printedPaths(listFile)
	want := []string{existing}
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

// Sem nada pendente (tudo já convertido, ou só ficheiros que entretanto
// desapareceram), a lista de pendentes é apagada: não pode crescer para
// sempre nem obrigar a reprocessar o mesmo em cada execução.
func TestEnsurePlayableApagaPendentesResolvidos(t *testing.T) {
	dir := t.TempDir()
	pending := filepath.Join(dir, pendingFileName)
	if err := os.WriteFile(pending, []byte(filepath.Join(dir, "convertido-e-apagado.webm")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := ensurePlayable(context.Background(), pending); err != nil {
		t.Fatalf("ensurePlayable = %v, esperado nil", err)
	}
	if _, err := os.Stat(pending); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a lista de pendentes devia ter sido apagada (stat: %v)", err)
	}
}

// Um CTRL+C antes de chegar a vez de um ficheiro mantém a lista, para a
// execução seguinte o converter.
func TestEnsurePlayableInterrompidoMantemPendentes(t *testing.T) {
	dir := t.TempDir()
	video := filepath.Join(dir, "video.webm")
	if err := os.WriteFile(video, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	pending := filepath.Join(dir, pendingFileName)
	if err := os.WriteFile(pending, []byte(video+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := ensurePlayable(ctx, pending); !errors.Is(err, ErrPartial) {
		t.Errorf("ensurePlayable = %v, esperado ErrPartial", err)
	}
	if _, err := os.Stat(pending); err != nil {
		t.Errorf("a lista de pendentes tinha de ficar para a próxima execução: %v", err)
	}
}
