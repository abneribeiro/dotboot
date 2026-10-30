package cli

import (
	"bytes"
	"errors"
	"flag"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/abneribeiro/dotboot/internal/jobs"
)

func TestParseArgsMapeiaAsFlags(t *testing.T) {
	d, err := parseArgs([]string{"-o", "aulas", "-q", "720", "-formato", "original", "https://youtu.be/a"}, "/base")
	if err != nil {
		t.Fatalf("parseArgs devolveu erro inesperado: %v", err)
	}
	if d.OutputDir != "/base/aulas" || d.FormatSort != "res:720" || d.Format != jobs.FormatOriginal {
		t.Errorf("Download = %+v", d)
	}
	if !slices.Equal(d.URLs, []string{"https://youtu.be/a"}) {
		t.Errorf("URLs = %v", d.URLs)
	}
}

// O flag da stdlib para no primeiro argumento que não é flag, e um "-o"
// depois do URL era validado como URL. As flags valem em qualquer posição.
func TestParseArgsFlagsDepoisDosURLs(t *testing.T) {
	d, err := parseArgs([]string{"https://youtu.be/a", "-o", "/videos", "https://youtu.be/b", "-formato", "mp3"}, "")
	if err != nil {
		t.Fatalf("parseArgs devolveu erro inesperado: %v", err)
	}
	if d.OutputDir != "/videos" || d.Format != jobs.FormatMP3 {
		t.Errorf("Download = %+v", d)
	}
	if !slices.Equal(d.URLs, []string{"https://youtu.be/a", "https://youtu.be/b"}) {
		t.Errorf("URLs = %v", d.URLs)
	}
}

func TestParseArgsErros(t *testing.T) {
	for _, args := range [][]string{
		{"-o", "x"},                         // sem URLs
		{"-legendas", "pt", "https://a.pt"}, // flag desconhecida
		{"-q", "hd", "https://a.pt"},        // validação do jobs
	} {
		if _, err := parseArgs(args, ""); err == nil {
			t.Errorf("parseArgs(%v) devia falhar", args)
		}
	}
	for _, f := range []string{"-h", "-help"} {
		if _, err := parseArgs([]string{f}, ""); !errors.Is(err, errHelp) {
			t.Errorf("parseArgs(%q) = %v, esperado errHelp", f, err)
		}
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
			if !slices.Equal(pos, tt.wantPos) || *x != tt.wantFlag {
				t.Errorf("posicionais = %v, -x = %q; esperado %v, %q", pos, *x, tt.wantPos, tt.wantFlag)
			}
		})
	}
}

func TestTextReporter(t *testing.T) {
	var buf bytes.Buffer
	r := &textReporter{out: &buf, tty: true}
	r.report(jobs.Event{Stage: jobs.StageDownload, Item: "Vídeo [abc].mp4"})
	r.report(jobs.Event{Progress: true, Fraction: 0.5, Speed: 2 * 1024 * 1024, ETA: 3 * time.Second})
	r.report(jobs.Event{Log: "[Merger] a juntar"})
	r.endLine()

	out := buf.String()
	for _, want := range []string{"a descarregar", "Vídeo [abc].mp4", " 50.0%  2.0 MB/s  ETA 0:03", "[Merger] a juntar\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("falta %q em:\n%q", want, out)
		}
	}
	// Uma linha de log nunca pode ficar colada à linha de progresso.
	if strings.Contains(out, "ETA 0:03[Merger]") {
		t.Errorf("o log colou-se ao progresso:\n%q", out)
	}

	// Fora de um terminal não há "\r": só etapas e log.
	buf.Reset()
	r = &textReporter{out: &buf}
	r.report(jobs.Event{Progress: true, Fraction: 0.5})
	if buf.Len() != 0 {
		t.Errorf("sem terminal o progresso não devia aparecer: %q", buf.String())
	}
}
