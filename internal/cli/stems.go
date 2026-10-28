package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/abneribeiro/dotboot/internal/config"
	"github.com/abneribeiro/dotboot/internal/downloader"
	"github.com/abneribeiro/dotboot/internal/stems"
)

const stemsUsageText = `Uso: dotboot stems [flags] URL|FICHEIRO...

Separa uma música nas suas pistas — vozes, bateria, baixo, guitarra… — com uma
pasta por música: <saída>/<título>/<pista>.<formato>

Flags (podem vir antes ou depois dos URLs/ficheiros):
  -o DIR          Diretório de saída (as mesmas regras do download)
  -modelo NOME    6s (default): vozes, bateria, baixo, guitarra, piano e outros
                  4s: vozes, bateria, baixo e outros (mais limpo, sem guitarra)
                  voz: vozes e instrumental (o melhor para karaoke)
  -formato FMT    mp3 (default, 320k), flac ou wav
  -so LISTA       Guarda só estas pistas (ex: -so guitarra,baixo)
  -sem LISTA      Gera também uma faixa sem estas pistas, para tocar por cima
                  (ex: -sem guitarra → sem-guitarra.mp3)
  -cookies FICH   Cookies para vídeos que pedem login (default: $DOTBOOT_COOKIES_FILE)
  -start TEMPO    Recorta antes de separar, MM:SS ou HH:MM:SS (exige -end e uma só música)
  -end TEMPO      Fim do recorte

Exemplos:
  dotboot stems https://youtube.com/watch?v=xxxx
  dotboot stems -sem guitarra -o praticar ~/Musica/song.mp3
  dotboot stems -so guitarra -sem guitarra https://youtube.com/watch?v=xxxx
  dotboot stems -modelo voz -so instrumental https://youtube.com/watch?v=xxxx
  dotboot stems -modelo 4s -formato flac -start 01:00 -end 02:30 URL

A separação corre no CPU e demora alguns minutos por música. Cada modelo é
descarregado na primeira utilização para $DOTBOOT_MODELS_DIR.
`

// stemsOptions é o resultado validado do parsing de "dotboot stems".
type stemsOptions struct {
	outputDir string
	preset    stems.Preset
	format    string
	sel       stems.Selection
	cookies   string
	start     string
	end       string
	inputs    []string
}

// isURL distingue um URL de um caminho local. Um URL mal escrito ("youtube.com/…")
// conta como ficheiro e falha com "não existe", o que também é verdade.
func isURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// splitList interpreta "guitarra, baixo" como [guitarra baixo].
func splitList(s string) []string {
	var out []string
	for _, item := range strings.Split(s, ",") {
		if item = strings.ToLower(strings.TrimSpace(item)); item != "" && !slices.Contains(out, item) {
			out = append(out, item)
		}
	}
	return out
}

// parseStemsArgs interpreta e valida os argumentos do subcomando stems. O
// defaultOutputDir tem as mesmas regras do download (ver resolveOutputDir).
func parseStemsArgs(args []string, defaultOutputDir string) (*stemsOptions, error) {
	o := &stemsOptions{}

	fs := flag.NewFlagSet("stems", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var model, only, without string
	fs.StringVar(&o.outputDir, "o", "", "diretório de saída")
	fs.StringVar(&model, "modelo", stems.DefaultPreset, "modelo de separação")
	fs.StringVar(&o.format, "formato", "mp3", "formato de saída")
	fs.StringVar(&only, "so", "", "pistas a guardar")
	fs.StringVar(&without, "sem", "", "pistas a tirar da faixa de acompanhamento")
	fs.StringVar(&o.cookies, "cookies", "", "ficheiro de cookies")
	fs.StringVar(&o.start, "start", "", "tempo de início do recorte")
	fs.StringVar(&o.end, "end", "", "tempo de fim do recorte")

	inputs, err := parseInterspersed(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil, errHelp
		}
		return nil, err
	}
	o.inputs = inputs

	if o.preset, err = stems.LookupPreset(model); err != nil {
		return nil, fmt.Errorf("-modelo: %w", err)
	}
	if !slices.Contains(stems.Formats, o.format) {
		return nil, fmt.Errorf("-formato inválido: %q (usa %s)", o.format, strings.Join(stems.Formats, ", "))
	}
	if o.sel, err = stems.Select(o.preset, splitList(only), splitList(without)); err != nil {
		return nil, err
	}

	if o.outputDir, err = resolveOutputDir(o.outputDir, defaultOutputDir); err != nil {
		return nil, err
	}

	if len(o.inputs) == 0 {
		return nil, fmt.Errorf("indica pelo menos um URL ou ficheiro de música")
	}
	if (o.start != "") != (o.end != "") {
		return nil, fmt.Errorf("para fazer um recorte fornece ambos -start e -end")
	}
	if o.start != "" {
		if len(o.inputs) != 1 {
			return nil, fmt.Errorf("o recorte (-start/-end) exige exatamente uma música")
		}
		if err := downloader.ValidateRange(o.start, o.end); err != nil {
			return nil, fmt.Errorf("recorte inválido: %w", err)
		}
	}

	for _, in := range o.inputs {
		if isURL(in) {
			if err := downloader.ValidateURL(in); err != nil {
				return nil, err
			}
			continue
		}
		info, err := os.Stat(in)
		if err != nil {
			return nil, fmt.Errorf("%s: não é um URL nem um ficheiro que exista", in)
		}
		if info.IsDir() {
			return nil, fmt.Errorf("%s é uma pasta; indica os ficheiros de música", in)
		}
	}

	if o.cookies != "" {
		if _, err := os.Stat(o.cookies); err != nil {
			return nil, fmt.Errorf("-cookies: %w", err)
		}
	}
	return o, nil
}

// RunStems executa o subcomando stems: para cada música, obtém o áudio
// (descarregado ou local), separa-o e guarda as pistas pedidas.
func RunStems(ctx context.Context, args []string, cfg *config.Config, log *slog.Logger) error {
	o, err := parseStemsArgs(args, cfg.OutputDir)
	if errors.Is(err, errHelp) {
		fmt.Print(stemsUsageText)
		return nil
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n\n%s", err, stemsUsageText)
		return ErrUsage
	}
	if o.cookies == "" {
		o.cookies = cfg.CookiesFile
	}

	// O yt-dlp só faz falta para URLs: separar ficheiros locais não precisa dele.
	if slices.ContainsFunc(o.inputs, isURL) {
		if _, err := exec.LookPath("yt-dlp"); err != nil {
			return fmt.Errorf("yt-dlp não encontrado no PATH (precisa dele para URLs; instala com `make deps`): %w", err)
		}
	}

	if err := os.MkdirAll(o.outputDir, 0o755); err != nil {
		return fmt.Errorf("não foi possível criar o diretório de saída %s: %w", o.outputDir, err)
	}
	log.Info("a separar stems", "output_dir", o.outputDir, "modelo", o.preset.Name, "musicas", len(o.inputs))

	var failed int
	for i, in := range o.inputs {
		fmt.Printf("\n🎵 [%d/%d] %s\n", i+1, len(o.inputs), in)
		dir, files, err := separateOne(ctx, in, o, cfg)
		if ctx.Err() != nil {
			fmt.Fprintf(os.Stderr, "\n⏹️  Interrompido.\n")
			return ErrInterrupted
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			failed++
			continue
		}
		fmt.Printf("✅ %d ficheiro(s) em %s\n", len(files), dir)
		for _, f := range files {
			fmt.Printf("   %s\n", filepath.Base(f))
		}
	}

	switch {
	case failed == 0:
		return nil
	case failed < len(o.inputs):
		fmt.Fprintf(os.Stderr, "\n⚠️  %d de %d música(s) não foram separadas (vê os erros acima).\n", failed, len(o.inputs))
		return ErrPartial
	default:
		return fmt.Errorf("nenhuma música foi separada")
	}
}

// separateOne trata de uma música e devolve a pasta e os ficheiros criados.
// Tudo o que é intermédio (o WAV de entrada, as pistas em WAV) vive num
// diretório de trabalho que é sempre removido.
func separateOne(ctx context.Context, in string, o *stemsOptions, cfg *config.Config) (string, []string, error) {
	work, err := os.MkdirTemp(cfg.TempDir, "dotboot-stems-")
	if err != nil {
		return "", nil, fmt.Errorf("não foi possível criar o diretório de trabalho: %w", err)
	}
	defer os.RemoveAll(work)

	var audio, title string
	if isURL(in) {
		fmt.Println("⬇️  A descarregar o áudio…")
		var sec *downloader.Section
		if o.start != "" {
			sec = &downloader.Section{Start: o.start, End: o.end}
		}
		audio, err = downloader.DownloadAudio(ctx, in, sec, work, downloader.AudioOpts{
			CookiesFile: o.cookies,
			JSRuntimes:  cfg.JSRuntimes,
			Stdout:      os.Stdout,
			Stderr:      os.Stderr,
		})
		if err != nil {
			return "", nil, fmt.Errorf("download falhou: %w", err)
		}
		title = strings.TrimSuffix(filepath.Base(audio), filepath.Ext(audio))
	} else {
		title = strings.TrimSuffix(filepath.Base(in), filepath.Ext(in))
		audio = filepath.Join(work, "entrada.wav")
		if err := stems.ToWAV(ctx, in, audio, o.start, o.end); err != nil {
			return "", nil, fmt.Errorf("não foi possível ler %s: %w", in, err)
		}
	}

	fmt.Printf("🎛️  A separar com o modelo %s (%s). No CPU demora alguns minutos…\n",
		o.preset.Name, o.preset.Description)
	tracks, err := stems.Separate(ctx, audio, filepath.Join(work, "pistas"), cfg.ModelsDir, o.preset, os.Stdout, os.Stderr)
	if err != nil {
		return "", nil, err
	}

	dest := filepath.Join(o.outputDir, title)
	files, err := stems.Export(ctx, tracks, o.sel, dest, o.format, title)
	if err != nil {
		return "", nil, err
	}
	return dest, files, nil
}
