package main

import (
	"errors"
	"strings"
	"testing"
)

// fakeLookPath simula um PATH com apenas os binários indicados.
func fakeLookPath(present ...string) func(string) (string, error) {
	return func(name string) (string, error) {
		for _, p := range present {
			if p == name {
				return "/usr/bin/" + name, nil
			}
		}
		return "", errors.New("não encontrado")
	}
}

func TestPickJSRuntime(t *testing.T) {
	tests := []struct {
		name      string
		present   []string
		wantValue string
		wantFound bool
	}{
		// O Deno é o default do yt-dlp: não precisa de --js-runtimes.
		{"deno dispensa a flag", []string{"deno", "node"}, "", true},
		{"só node ativa o node", []string{"node"}, "node", true},
		{"nenhum runtime", nil, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, found := pickJSRuntime(fakeLookPath(tt.present...))
			if got != tt.wantValue || found != tt.wantFound {
				t.Errorf("pickJSRuntime = (%q, %v), esperado (%q, %v)", got, found, tt.wantValue, tt.wantFound)
			}
		})
	}
}

func TestVersionStringIncluiAVersao(t *testing.T) {
	old := version
	defer func() { version = old }()

	version = "v1.2.3"
	if got := versionString(); !strings.HasPrefix(got, "dotboot v1.2.3") {
		t.Errorf("versionString = %q, esperado começar por \"dotboot v1.2.3\"", got)
	}
}
