package jobs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/abneribeiro/dotboot/internal/downloader"
	"github.com/abneribeiro/dotboot/internal/procio"
	"github.com/abneribeiro/dotboot/internal/stems"
)

// StemsInput é um pedido de separação tal como vem do utilizador.
type StemsInput struct {
	// Inputs são URLs (http/https) ou ficheiros locais.
	Inputs    []string
	OutputDir string
	// Model é o preset (6s, 4s, voz); vazio usa o default.
	Model string
	// Format: mp3 (default), flac ou wav.
	Format  string
	Only    []string
	Without []string
	Start   string
	End     string
	Cookies string
}

// Stems é um pedido de separação validado.
type Stems struct {
	Inputs    []string
	OutputDir string
	Preset    stems.Preset
	Format    string
	Selection stems.Selection
	Section   *downloader.Section
	Cookies   string
}

// IsURL distingue um URL de um caminho local. Um URL mal escrito
// ("youtube.com/…") conta como ficheiro e falha com "não existe", o que
// também é verdade.
func IsURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// NewStems valida o pedido. outputBase é o DOTBOOT_OUTPUT_DIR.
func NewStems(in StemsInput, outputBase string) (*Stems, error) {
	s := &Stems{Inputs: in.Inputs, Format: in.Format, Cookies: in.Cookies}

	model := in.Model
	if model == "" {
		model = stems.DefaultPreset
	}
	var err error
	if s.Preset, err = stems.LookupPreset(model); err != nil {
		return nil, err
	}
	if s.Format == "" {
		s.Format = "mp3"
	}
	if !slices.Contains(stems.Formats, s.Format) {
		return nil, fmt.Errorf("formato inválido: %q (usa %s)", s.Format, strings.Join(stems.Formats, ", "))
	}
	if s.Selection, err = stems.Select(s.Preset, in.Only, in.Without); err != nil {
		return nil, err
	}
	if s.OutputDir, err = ResolveOutputDir(in.OutputDir, outputBase); err != nil {
		return nil, err
	}

	if len(s.Inputs) == 0 {
		return nil, fmt.Errorf("indica pelo menos um URL ou ficheiro de música")
	}
	if (in.Start != "") != (in.End != "") {
		return nil, fmt.Errorf("para fazer um recorte indica o início e o fim")
	}
	if in.Start != "" {
		if len(s.Inputs) != 1 {
			return nil, fmt.Errorf("o recorte exige exatamente uma música")
		}
		if err := downloader.ValidateRange(in.Start, in.End); err != nil {
			return nil, fmt.Errorf("recorte inválido: %w", err)
		}
		s.Section = &downloader.Section{Start: in.Start, End: in.End}
	}

	for _, input := range s.Inputs {
		if IsURL(input) {
			if err := downloader.ValidateURL(input); err != nil {
				return nil, err
			}
			continue
		}
		info, err := os.Stat(input)
		if err != nil {
			return nil, fmt.Errorf("%s: não é um URL nem um ficheiro que exista", input)
		}
		if info.IsDir() {
			return nil, fmt.Errorf("%s é uma pasta; indica os ficheiros de música", input)
		}
	}
	if s.Cookies != "" {
		if _, err := os.Stat(s.Cookies); err != nil {
			return nil, fmt.Errorf("cookies: %w", err)
		}
	}
	return s, nil
}

// Run separa cada música e guarda as pistas pedidas, uma pasta por música.
// Uma música que falhe não impede as seguintes: no fim devolve ErrPartial
// com o que ficou feito, ou o erro quando nenhuma correu bem.
func (s *Stems) Run(ctx context.Context, env Env, report Reporter) (Result, error) {
	res := Result{OutputDir: s.OutputDir}
	sink := &reporterSink{report: report}

	if slices.ContainsFunc(s.Inputs, IsURL) {
		if _, err := exec.LookPath("yt-dlp"); err != nil {
			return res, fmt.Errorf("yt-dlp não encontrado no PATH (precisa dele para URLs): %w", err)
		}
	}
	if err := PrepareOutputDir(s.OutputDir); err != nil {
		return res, err
	}

	var errs []error
	for i, input := range s.Inputs {
		sink.send(Event{Index: i + 1, Count: len(s.Inputs), Item: displayName(input)})
		files, err := s.separateOne(ctx, input, env, sink)
		if ctx.Err() != nil {
			return res, context.Canceled
		}
		res.Files = append(res.Files, files...)
		if err != nil {
			sink.log(fmt.Sprintf("❌ %s: %v", displayName(input), err))
			errs = append(errs, err)
		}
	}

	switch {
	case len(errs) == 0:
		return res, nil
	case len(errs) < len(s.Inputs):
		return res, fmt.Errorf("%w: %d de %d música(s) falharam", ErrPartial, len(errs), len(s.Inputs))
	default:
		return res, errors.Join(errs...)
	}
}

// displayName encurta um caminho local para o nome do ficheiro.
func displayName(input string) string {
	if IsURL(input) {
		return input
	}
	return filepath.Base(input)
}

// separateOne trata de uma música. Tudo o que é intermédio (o WAV de
// entrada, as pistas em WAV) vive num diretório de trabalho sempre removido.
func (s *Stems) separateOne(ctx context.Context, input string, env Env, sink *reporterSink) ([]string, error) {
	work, err := os.MkdirTemp(env.TempDir, "dotboot-stems-")
	if err != nil {
		return nil, fmt.Errorf("não foi possível criar o diretório de trabalho: %w", err)
	}
	defer os.RemoveAll(work)

	cookies := s.Cookies
	if cookies == "" {
		cookies = env.CookiesFile
	}

	var audio, title string
	if IsURL(input) {
		sink.send(Event{Stage: StageDownload, Progress: true, Fraction: -1, ETA: -1})
		stderr := procio.NewLineWriter(sink.log)
		audio, err = downloader.DownloadAudio(ctx, input, s.Section, work, downloader.AudioOpts{
			CookiesFile: cookies,
			JSRuntimes:  env.JSRuntimes,
			Progress:    sink.progress,
			Stdout:      writerFunc(sink.log),
			Stderr:      stderr,
		})
		stderr.Flush()
		if err != nil {
			return nil, fmt.Errorf("download falhou: %w", err)
		}
		title = strings.TrimSuffix(filepath.Base(audio), filepath.Ext(audio))
	} else {
		sink.send(Event{Stage: StageExtract, Progress: true, Fraction: -1, ETA: -1})
		title = strings.TrimSuffix(filepath.Base(input), filepath.Ext(input))
		audio = filepath.Join(work, "entrada.wav")
		start, end := "", ""
		if s.Section != nil {
			start, end = s.Section.Start, s.Section.End
		}
		if err := stems.ToWAV(ctx, input, audio, start, end); err != nil {
			return nil, fmt.Errorf("não foi possível ler %s: %w", input, err)
		}
	}
	sink.send(Event{Item: title})

	sink.send(Event{Stage: StageSeparate, Progress: true, Fraction: 0, ETA: -1,
		Log: fmt.Sprintf("🎛️  Modelo %s (%s). No CPU demora alguns minutos.", s.Preset.Name, s.Preset.Description)})
	// Alguns modelos passam pela música mais de uma vez (o 6s duas, o 4s uma
	// por pista), cada passagem com a sua barra do tqdm: sem o número do
	// passo, a barra chegava aos 100% e voltava a zero sem explicação.
	pass, last := 1, 0.0
	tracks, err := stems.Separate(ctx, audio, filepath.Join(work, "pistas"), env.ModelsDir, s.Preset, sink.log,
		func(f float64) {
			if f < last-0.5 {
				pass++
				sink.send(Event{Stage: Stage(fmt.Sprintf("%s (passo %d)", StageSeparate, pass))})
			}
			last = f
			sink.send(Event{Progress: true, Fraction: f, ETA: -1})
		})
	if err != nil {
		return nil, err
	}

	sink.send(Event{Stage: StageExport, Progress: true, Fraction: -1, ETA: -1})
	dest := filepath.Join(s.OutputDir, title)
	files, err := stems.Export(ctx, tracks, s.Selection, dest, s.Format, title)
	if err != nil {
		return files, err
	}
	for _, f := range files {
		sink.log("✅ " + f)
	}
	return files, nil
}
