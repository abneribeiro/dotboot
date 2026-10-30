package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/abneribeiro/dotboot/internal/downloader"
	"github.com/abneribeiro/dotboot/internal/procio"
	"github.com/abneribeiro/dotboot/internal/stems"
)

const (
	// SessionFormat é o formato das pistas guardadas para o estúdio: sem
	// perdas (a exportação parte dele), metade do WAV e toca em todos os
	// browsers atuais.
	SessionFormat = "flac"

	// PeaksFile guarda a forma de onda de cada pista, ao lado das pistas.
	PeaksFile = "peaks.json"

	// peaksPoints é a resolução da forma de onda: mais do que os píxeis de
	// largura de um ecrã grande.
	peaksPoints = 2000
)

// StemsInput é um pedido de separação tal como vem da página.
type StemsInput struct {
	// Input é um URL (http/https) ou um ficheiro local (um upload).
	Input string
	// Name é o nome original de um ficheiro enviado, que dá o título à
	// música (o upload em disco tem um nome aleatório).
	Name string
	// OutputDir é a pasta (absoluta) da sessão.
	OutputDir string
	// Model é o preset (6s, 4s, voz, voz-hq); vazio usa o default.
	Model string
	Start string
	End   string
}

// Stems é um pedido de separação validado.
type Stems struct {
	Input     string
	Name      string
	OutputDir string
	Preset    stems.Preset
	Section   *downloader.Section
}

// IsURL distingue um URL de um caminho local.
func IsURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// NewStems valida o pedido.
func NewStems(in StemsInput) (*Stems, error) {
	s := &Stems{Input: strings.TrimSpace(in.Input), Name: in.Name, OutputDir: in.OutputDir}

	model := in.Model
	if model == "" {
		model = stems.DefaultPreset
	}
	var err error
	if s.Preset, err = stems.LookupPreset(model); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(s.OutputDir) {
		return nil, fmt.Errorf("pasta da sessão inválida: %q", s.OutputDir)
	}

	if s.Input == "" {
		return nil, fmt.Errorf("indica um URL ou um ficheiro de música")
	}
	if (in.Start != "") != (in.End != "") {
		return nil, fmt.Errorf("para fazer um recorte indica o início e o fim")
	}
	if in.Start != "" {
		if err := downloader.ValidateRange(in.Start, in.End); err != nil {
			return nil, fmt.Errorf("recorte inválido: %w", err)
		}
		s.Section = &downloader.Section{Start: in.Start, End: in.End}
	}

	if IsURL(s.Input) {
		if err := downloader.ValidateURL(s.Input); err != nil {
			return nil, err
		}
		return s, nil
	}
	info, err := os.Stat(s.Input)
	if err != nil {
		return nil, fmt.Errorf("o ficheiro de música já não existe: %w", err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("%s é uma pasta, não um ficheiro de música", s.Input)
	}
	return s, nil
}

// Run separa a música e deixa na pasta da sessão uma pista FLAC por stem do
// modelo, mais a forma de onda de cada uma (PeaksFile). O intermédio (o WAV
// de entrada, as pistas em WAV) vive num diretório de trabalho sempre
// removido.
func (s *Stems) Run(ctx context.Context, env Env, report Reporter) (Result, error) {
	res := Result{OutputDir: s.OutputDir}
	sink := &reporterSink{report: report}

	if IsURL(s.Input) {
		if _, err := exec.LookPath("yt-dlp"); err != nil {
			return res, fmt.Errorf("yt-dlp não encontrado no PATH (precisa dele para URLs): %w", err)
		}
	}
	if err := PrepareOutputDir(s.OutputDir); err != nil {
		return res, err
	}

	work, err := os.MkdirTemp(env.TempDir, "dotboot-stems-")
	if err != nil {
		return res, fmt.Errorf("não foi possível criar o diretório de trabalho: %w", err)
	}
	defer os.RemoveAll(work)

	audio, title, err := s.prepareInput(ctx, work, env, sink)
	if err != nil {
		if ctx.Err() != nil {
			return res, context.Canceled
		}
		return res, err
	}
	res.Title = title
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
		if ctx.Err() != nil {
			return res, context.Canceled
		}
		return res, err
	}

	sink.send(Event{Stage: StageExport, Progress: true, Fraction: 0, ETA: -1})
	if res.Files, err = s.store(ctx, tracks, title, sink); err != nil {
		if ctx.Err() != nil {
			return res, context.Canceled
		}
		return res, err
	}
	sink.log("✅ Pistas prontas: " + strings.Join(s.Preset.Stems, ", "))
	return res, nil
}

// prepareInput deixa a música num WAV pronto a separar e devolve-o com o
// título: de um URL descarrega só o áudio; de um ficheiro converte-o.
func (s *Stems) prepareInput(ctx context.Context, work string, env Env, sink *reporterSink) (string, string, error) {
	if IsURL(s.Input) {
		sink.send(Event{Stage: StageDownload, Progress: true, Fraction: -1, ETA: -1, Item: s.Input})
		stderr := procio.NewLineWriter(sink.log)
		audio, err := downloader.DownloadAudio(ctx, s.Input, s.Section, work, downloader.AudioOpts{
			CookiesFile: env.CookiesFile,
			JSRuntimes:  env.JSRuntimes,
			Progress:    sink.progress,
			Stdout:      writerFunc(sink.log),
			Stderr:      stderr,
		})
		stderr.Flush()
		if err != nil {
			return "", "", fmt.Errorf("download falhou: %w", err)
		}
		return audio, strings.TrimSuffix(filepath.Base(audio), filepath.Ext(audio)), nil
	}

	name := s.Name
	if name == "" {
		name = filepath.Base(s.Input)
	}
	title := strings.TrimSuffix(name, filepath.Ext(name))
	sink.send(Event{Stage: StageExtract, Progress: true, Fraction: -1, ETA: -1, Item: title})
	audio := filepath.Join(work, "entrada.wav")
	start, end := "", ""
	if s.Section != nil {
		start, end = s.Section.Start, s.Section.End
	}
	if err := stems.ToWAV(ctx, s.Input, audio, start, end); err != nil {
		return "", "", fmt.Errorf("não foi possível ler %s: %w", name, err)
	}
	return audio, title, nil
}

// store converte as pistas para a sessão e mede a forma de onda de cada uma.
func (s *Stems) store(ctx context.Context, tracks map[string]string, title string, sink *reporterSink) ([]string, error) {
	files, err := stems.Export(ctx, tracks, s.Preset.Stems, s.OutputDir, SessionFormat, title)
	if err != nil {
		return files, err
	}
	peaks := make(map[string]stems.Peaks, len(files))
	for i, stem := range s.Preset.Stems {
		p, err := stems.MeasurePeaks(ctx, files[i], peaksPoints)
		if err != nil {
			return files, err
		}
		peaks[stem] = p
		sink.send(Event{Progress: true, Fraction: float64(i+1) / float64(len(files)), ETA: -1})
	}
	data, err := json.Marshal(peaks)
	if err != nil {
		return files, err
	}
	if err := os.WriteFile(filepath.Join(s.OutputDir, PeaksFile), data, 0o644); err != nil {
		return files, fmt.Errorf("não foi possível gravar a forma de onda: %w", err)
	}
	return files, nil
}
