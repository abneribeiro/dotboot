package cli

import (
	"errors"
	"path/filepath"
	"slices"
	"testing"
)

func TestParseStemsArgsDefaults(t *testing.T) {
	song := batchFile(t, "não é música, mas existe")

	o, err := parseStemsArgs([]string{song}, "/base")
	if err != nil {
		t.Fatalf("parseStemsArgs devolveu erro inesperado: %v", err)
	}
	if o.preset.Name != "6s" || o.format != "mp3" {
		t.Errorf("preset = %q, formato = %q, esperado 6s e mp3", o.preset.Name, o.format)
	}
	// Por omissão guardam-se todas as pistas do modelo, sem mistura.
	if !slices.Equal(o.sel.Keep, o.preset.Stems) || o.sel.MixName != "" {
		t.Errorf("seleção por omissão inesperada: %+v", o.sel)
	}
	if o.outputDir != "/base" {
		t.Errorf("outputDir = %q, esperado /base", o.outputDir)
	}
}

func TestParseStemsArgs(t *testing.T) {
	song := batchFile(t, "x")
	dir := filepath.Dir(song)

	tests := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{"URL", []string{"https://youtu.be/abc"}, false},
		{"vários ficheiros e URLs", []string{song, "https://youtu.be/abc"}, false},
		{"flags depois da música", []string{song, "-sem", "guitarra", "-formato", "flac"}, false},
		{"-so com espaços e repetidos", []string{"-so", "guitarra, baixo,guitarra", song}, false},
		{"karaoke", []string{"-modelo", "voz", "-so", "instrumental", song}, false},
		{"recorte de um ficheiro", []string{"-start", "00:30", "-end", "01:00", song}, false},

		{"sem entradas", []string{"-o", "x"}, true},
		{"ficheiro inexistente", []string{"/nao/existe.mp3"}, true},
		{"pasta em vez de ficheiro", []string{dir}, true},
		{"URL sem esquema conta como ficheiro", []string{"youtube.com/watch?v=x"}, true},
		{"modelo desconhecido", []string{"-modelo", "12s", song}, true},
		{"formato desconhecido", []string{"-formato", "ogg", song}, true},
		{"pista que o modelo não tem", []string{"-modelo", "4s", "-sem", "guitarra", song}, true},
		{"recorte com duas músicas", []string{"-start", "00:30", "-end", "01:00", song, song}, true},
		{"recorte invertido", []string{"-start", "01:00", "-end", "00:30", song}, true},
		{"start sem end", []string{"-start", "00:30", song}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseStemsArgs(tt.args, "")
			if (err != nil) != tt.wantErr {
				t.Errorf("parseStemsArgs(%v) err = %v, wantErr = %v", tt.args, err, tt.wantErr)
			}
		})
	}
}

func TestParseStemsArgsSemGuitarra(t *testing.T) {
	o, err := parseStemsArgs([]string{"-so", "guitarra", "-sem", "guitarra", "https://youtu.be/abc"}, "")
	if err != nil {
		t.Fatalf("parseStemsArgs devolveu erro inesperado: %v", err)
	}
	// O par para praticar: a guitarra sozinha e a faixa sem ela.
	if !slices.Equal(o.sel.Keep, []string{"guitarra"}) || o.sel.MixName != "sem-guitarra" {
		t.Errorf("seleção = %+v", o.sel)
	}
}

func TestParseStemsArgsHelp(t *testing.T) {
	if _, err := parseStemsArgs([]string{"-h"}, ""); !errors.Is(err, errHelp) {
		t.Errorf("parseStemsArgs(-h) = %v, esperado errHelp", err)
	}
}

func TestSplitList(t *testing.T) {
	got := splitList(" Guitarra, baixo,,guitarra ")
	if !slices.Equal(got, []string{"guitarra", "baixo"}) {
		t.Errorf("splitList = %v", got)
	}
	if got := splitList(""); got != nil {
		t.Errorf("splitList(\"\") = %v, esperado nil", got)
	}
}
