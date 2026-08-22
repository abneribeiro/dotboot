//go:build integration

// Testes que executam mesmo o yt-dlp e o ffmpeg, contra a rede. Ficam fora do
// "go test ./..." por precisarem dos binários e de ligação à internet:
//
//	go test -tags integration -v -timeout 15m ./internal/downloader
package downloader

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testURL é um vídeo Creative Commons da Blender Foundation.
const testURL = "https://www.youtube.com/watch?v=aqz-KE-bpKQ"

// TestDownloadForDiscordCabeNoLimite verifica o caminho feliz: o vídeo já cabe
// no limite, por isso não há recompressão.
func TestDownloadForDiscordCabeNoLimite(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	tempDir := t.TempDir()
	sec := &Section{Start: "00:10", End: "00:20"}

	res, err := DownloadForDiscord(ctx, testURL, sec, DiscordOpts{
		MaxFileSize: 20 * mib,
		TempDir:     tempDir,
	})
	if res.WorkDir != "" {
		defer os.RemoveAll(res.WorkDir)
	}
	if err != nil {
		t.Fatalf("DownloadForDiscord falhou: %v", err)
	}

	info, err := os.Stat(res.Path)
	if err != nil {
		t.Fatalf("ficheiro final inexistente: %v", err)
	}
	if info.Size() > 20*mib {
		t.Errorf("ficheiro tem %d bytes, acima do limite de %d", info.Size(), 20*mib)
	}
	if res.Name != filepath.Base(res.Path) {
		t.Errorf("Name = %q, esperado %q", res.Name, filepath.Base(res.Path))
	}
	// O bot força mp4 com --merge-output-format; um .webm rotulado .mp4 era
	// exatamente o bug que partia a reprodução inline no Discord.
	if !strings.HasSuffix(res.Name, ".mp4") {
		t.Errorf("Name = %q, esperado terminar em .mp4", res.Name)
	}
	t.Logf("ficheiro: %s (%d bytes)", res.Name, info.Size())
}

// TestDownloadForDiscordComprime força a compressão com um limite apertado.
func TestDownloadForDiscordComprime(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	tempDir := t.TempDir()
	sec := &Section{Start: "00:10", End: "00:40"}
	const limit = 1 * mib

	res, err := DownloadForDiscord(ctx, testURL, sec, DiscordOpts{
		MaxFileSize: limit,
		TempDir:     tempDir,
	})
	if res.WorkDir != "" {
		defer os.RemoveAll(res.WorkDir)
	}
	if err != nil {
		t.Fatalf("DownloadForDiscord falhou: %v", err)
	}

	info, err := os.Stat(res.Path)
	if err != nil {
		t.Fatalf("ficheiro final inexistente: %v", err)
	}
	if info.Size() > limit {
		t.Errorf("ficheiro comprimido tem %d bytes, acima do limite de %d", info.Size(), limit)
	}
	t.Logf("comprimido para %d bytes (limite %d)", info.Size(), limit)
}

// TestDownloadForDiscordLimpaTudo garante que o WorkDir devolvido remove todo
// o trabalho intermédio — a fuga de ficheiros temporários que existia antes.
func TestDownloadForDiscordLimpaTudo(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	tempDir := t.TempDir()
	sec := &Section{Start: "00:10", End: "00:40"}

	res, err := DownloadForDiscord(ctx, testURL, sec, DiscordOpts{
		MaxFileSize: 1 * mib,
		TempDir:     tempDir,
	})
	if err != nil {
		t.Fatalf("DownloadForDiscord falhou: %v", err)
	}
	if err := os.RemoveAll(res.WorkDir); err != nil {
		t.Fatalf("RemoveAll falhou: %v", err)
	}

	entries, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("sobraram ficheiros em TempDir depois da limpeza: %v", names)
	}
}

// TestDownloadForDiscordVideoImpossivel verifica que um vídeo longo demais
// falha depressa, com uma mensagem útil, em vez de codificar em vão.
func TestDownloadForDiscordVideoImpossivel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	tempDir := t.TempDir()
	// Um clip curto com um orçamento minúsculo exercita exatamente o mesmo
	// caminho que um vídeo de duas horas em 20MB, sem descarregar 100MB.
	sec := &Section{Start: "00:10", End: "01:10"} // 1 minuto
	const limit = 16 * 1024                       // ~2 kbps: impossível

	res, err := DownloadForDiscord(ctx, testURL, sec, DiscordOpts{
		MaxFileSize: limit,
		TempDir:     tempDir,
	})
	if res.WorkDir != "" {
		defer os.RemoveAll(res.WorkDir)
	}
	if err == nil {
		t.Fatal("esperava ErrTooLarge para 1 minuto em 16KB")
	}

	var dlErr *DownloadError
	if !errors.As(err, &dlErr) || dlErr.Kind != ErrTooLarge {
		t.Fatalf("erro = %v, esperado ErrTooLarge", err)
	}
	// A mensagem tem de dizer ao utilizador o que fazer a seguir.
	if !strings.Contains(dlErr.Error(), "kbps") {
		t.Errorf("mensagem não explica o bitrate necessário: %q", dlErr.Error())
	}
	t.Logf("erro esperado: %v", dlErr)
}
