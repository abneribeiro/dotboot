package doctor

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/abneribeiro/dotboot/internal/config"
)

// fakeSystem simula um PATH com os binários de outputs (nome → output de
// "--version"/"-version").
func fakeSystem(now time.Time, outputs map[string]string) system {
	return system{
		lookPath: func(name string) (string, error) {
			if _, ok := outputs[name]; ok {
				return "/usr/bin/" + name, nil
			}
			return "", errors.New("não encontrado")
		},
		output: func(_ context.Context, name string, _ ...string) (string, error) {
			return outputs[name], nil
		},
		now: now,
	}
}

func byName(checks []check, name string) check {
	for _, c := range checks {
		if c.name == name {
			return c
		}
	}
	return check{name: "em falta: " + name, level: levelFail}
}

func TestCollect(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	cfg := &config.Config{TempDir: t.TempDir(), OutputDir: t.TempDir(), DiscordToken: "segredo"}

	complete := map[string]string{
		"yt-dlp":          "2026.10.26\n",
		"ffmpeg":          "ffmpeg version 7.1.5-0+deb13u1 Copyright (c) 2000-2026\n",
		"ffprobe":         "ffprobe version 7.1.5-0+deb13u1 Copyright (c) 2000-2026\n",
		"deno":            "deno 2.9.8 (stable, release, x86_64-unknown-linux-gnu)\n",
		"audio-separator": "",
	}
	checks := collect(context.Background(), cfg, fakeSystem(now, complete))
	for _, c := range checks {
		if c.level != levelOK {
			t.Errorf("%s: nível %d (%s), esperado OK num ambiente completo", c.name, c.level, c.detail)
		}
	}
	if got := byName(checks, "ffmpeg").detail; got != "7.1.5-0+deb13u1" {
		t.Errorf("versão do ffmpeg = %q", got)
	}
	if got := byName(checks, "JavaScript").detail; got != "deno 2.9.8" {
		t.Errorf("runtime JS = %q", got)
	}
	// O token nunca aparece no relatório.
	if got := byName(checks, "token").detail; strings.Contains(got, "segredo") {
		t.Errorf("o relatório expôs o token: %q", got)
	}
}

func TestCollectAmbienteIncompleto(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	cfg := &config.Config{TempDir: t.TempDir(), OutputDir: t.TempDir(), DiscordToken: tokenPlaceholder}

	checks := collect(context.Background(), cfg, fakeSystem(now, map[string]string{
		"yt-dlp": "2026.01.10\n", // ~9 meses
		"node":   "v26.3.1\n",
	}))

	tests := []struct {
		name string
		want level
	}{
		{"yt-dlp", levelWarn},   // antigo, mas funciona
		{"ffmpeg", levelFail},   // sem ele não há merge nem conversão
		{"ffprobe", levelFail},  // sem ele não há verificação de formato
		{"JavaScript", levelOK}, // o node serve
		{"stems", levelWarn},    // opcional
		{"token", levelWarn},    // só o bot precisa
		{"temporários", levelOK},
		{"saída", levelOK},
	}
	for _, tt := range tests {
		c := byName(checks, tt.name)
		if c.level != tt.want {
			t.Errorf("%s: nível %d (%s), esperado %d", tt.name, c.level, c.detail, tt.want)
		}
		if c.level != levelOK && c.fix == "" {
			t.Errorf("%s: um problema tem de dizer como se resolve", tt.name)
		}
	}

	// Sem DOTBOOT_COOKIES_FILE, a linha dos cookies nem aparece.
	if c := byName(checks, "cookies"); !strings.HasPrefix(c.name, "em falta") {
		t.Errorf("verificação de cookies inesperada: %+v", c)
	}
}

func TestYtdlpReleaseDate(t *testing.T) {
	tests := []struct {
		in   string
		want time.Time
		ok   bool
	}{
		{"2026.10.26", time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC), true},
		{"2026.10.26.234255", time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC), true}, // nightly
		{" 2025.11.12\n", time.Date(2025, 11, 12, 0, 0, 0, 0, time.UTC), true},
		{"2026.13.01", time.Time{}, false},
		{"stable@2026.10.26", time.Time{}, false},
		{"", time.Time{}, false},
	}
	for _, tt := range tests {
		got, ok := ytdlpReleaseDate(tt.in)
		if ok != tt.ok || !got.Equal(tt.want) {
			t.Errorf("ytdlpReleaseDate(%q) = (%v, %v), esperado (%v, %v)", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}

func TestVersionAfter(t *testing.T) {
	tests := []struct{ out, prefix, want string }{
		{"ffmpeg version 7.1.5-0+deb13u1 Copyright (c)\nbuilt with gcc", "ffmpeg version ", "7.1.5-0+deb13u1"},
		{"deno 2.9.8 (stable, release)\nv8 14.0", "deno ", "2.9.8"},
		{"algo inesperado", "ffmpeg version ", ""},
		{"", "deno ", ""},
	}
	for _, tt := range tests {
		if got := versionAfter(tt.out, tt.prefix); got != tt.want {
			t.Errorf("versionAfter(%q, %q) = %q, esperado %q", tt.out, tt.prefix, got, tt.want)
		}
	}
}

func TestRender(t *testing.T) {
	var buf bytes.Buffer
	render(&buf, []check{{name: "a", level: levelWarn, detail: "x", fix: "y"}})
	if !strings.Contains(buf.String(), "1 aviso") || !strings.Contains(buf.String(), "→ y") {
		t.Errorf("relatório inesperado:\n%s", buf.String())
	}

	buf.Reset()
	render(&buf, []check{{name: "b", level: levelFail, detail: "x", fix: "z"}})
	if !strings.Contains(buf.String(), "1 problema") {
		t.Errorf("relatório inesperado:\n%s", buf.String())
	}
}
