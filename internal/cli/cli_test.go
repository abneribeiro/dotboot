package cli

import (
	"os"
	"slices"
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

func TestParseArgsValidation(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{"sem URLs nem batch-file", []string{"-o", "/videos"}, true},
		{"batch-file sem URLs é aceite", []string{"-a", "urls.txt"}, false},
		{"vários URLs", []string{"https://youtu.be/a", "https://youtu.be/b"}, false},
		{"URL inválido", []string{"youtube.com/sem-esquema"}, true},
		{"recorte válido", []string{"-start", "01:00", "-end", "02:00", "https://youtu.be/a"}, false},
		{"start sem end", []string{"-start", "01:00", "https://youtu.be/a"}, true},
		{"end sem start", []string{"-end", "02:00", "https://youtu.be/a"}, true},
		{"recorte com dois URLs", []string{"-start", "01:00", "-end", "02:00", "https://youtu.be/a", "https://youtu.be/b"}, true},
		{"recorte com batch-file", []string{"-start", "01:00", "-end", "02:00", "-a", "urls.txt"}, true},
		{"tempo de recorte inválido", []string{"-start", "abc", "-end", "02:00", "https://youtu.be/a"}, true},
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

	// -q 720 traduz-se na ordenação por resolução preferida.
	o, err = parseArgs([]string{"-q", "720", "https://youtu.be/abc"}, "")
	if err != nil {
		t.Fatalf("parseArgs devolveu erro inesperado: %v", err)
	}
	if o.formatSort != "res:720" {
		t.Errorf("formatSort = %q, esperado res:720", o.formatSort)
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
