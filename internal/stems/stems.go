// Package stems separa uma música nas suas pistas (vozes, bateria, baixo,
// guitarra, piano…) com o audio-separator, e mistura, converte e mede o
// resultado com o ffmpeg. Não conhece a interface web.
package stems

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/abneribeiro/dotboot/internal/procio"
)

// Preset associa um nome curto a um modelo do audio-separator e às pistas
// que ele produz.
type Preset struct {
	// Name é o identificador usado pela API e pela página.
	Name string
	// Model é o ficheiro do modelo no catálogo do audio-separator.
	Model string
	// Stems são as pistas produzidas, já em português e pela ordem em que
	// são apresentadas e guardadas.
	Stems []string
	// Description resume o preset para a página.
	Description string
	// Speed é o tempo de separação no CPU em múltiplos da duração da música
	// (medido num portátil), para a página estimar a espera.
	Speed float64
}

// DefaultPreset é o modelo por omissão: o único que separa a guitarra.
const DefaultPreset = "6s"

var presets = []Preset{
	{
		Name:        "6s",
		Model:       "htdemucs_6s.yaml",
		Stems:       []string{"vozes", "bateria", "baixo", "guitarra", "piano", "outros"},
		Description: "vozes, bateria, baixo, guitarra, piano e outros",
		Speed:       1.2,
	},
	{
		// O htdemucs_ft afina um modelo por pista: mais limpo do que o 6s,
		// mas sem guitarra nem piano (ficam em "outros").
		Name:        "4s",
		Model:       "htdemucs_ft.yaml",
		Stems:       []string{"vozes", "bateria", "baixo", "outros"},
		Description: "vozes, bateria, baixo e outros (mais limpo, sem guitarra)",
		Speed:       5.3,
	},
	{
		// O MDX-Net (Kim Vocal 2) isola bem a voz e corre depressa no CPU,
		// mais ou menos à velocidade da música: é o modelo de karaoke por
		// omissão.
		Name:        "voz",
		Model:       "Kim_Vocal_2.onnx",
		Stems:       []string{"vozes", "instrumental"},
		Description: "vozes e instrumental, para karaoke (rápido)",
		Speed:       1.4,
	},
	{
		// O BS-RoFormer é o melhor a isolar a voz, mas é um transformer
		// grande: no CPU demora cerca de 20 vezes a duração da música.
		Name:        "voz-hq",
		Model:       "model_bs_roformer_ep_317_sdr_12.9755.ckpt",
		Stems:       []string{"vozes", "instrumental"},
		Description: "vozes e instrumental na melhor qualidade (muito lento no CPU)",
		Speed:       23,
	},
}

// Presets devolve os presets disponíveis, pela ordem da página.
func Presets() []Preset {
	return slices.Clone(presets)
}

// LookupPreset devolve o preset com este nome.
func LookupPreset(name string) (Preset, error) {
	for _, p := range presets {
		if p.Name == name {
			return p, nil
		}
	}
	names := make([]string, len(presets))
	for i, p := range presets {
		names[i] = p.Name
	}
	return Preset{}, fmt.Errorf("modelo desconhecido: %q (usa %s)", name, strings.Join(names, ", "))
}

// stemNames traduz os nomes que o audio-separator dá às pistas.
var stemNames = map[string]string{
	"vocals":       "vozes",
	"drums":        "bateria",
	"bass":         "baixo",
	"guitar":       "guitarra",
	"piano":        "piano",
	"other":        "outros",
	"instrumental": "instrumental",
}

// Formats são os formatos de saída aceites.
var Formats = []string{"mp3", "flac", "wav"}

// customNamesJSON é o valor de --custom_output_names: põe cada pista com o
// nome final em português (vozes.wav…) em vez do formato por omissão do
// audio-separator, "<entrada>_(Vocals)_<modelo>.wav".
func customNamesJSON() string {
	names := make(map[string]string, len(stemNames))
	for en, pt := range stemNames {
		names[strings.ToUpper(en[:1])+en[1:]] = pt
	}
	// Um map[string]string sai sempre com as chaves ordenadas: o JSON é
	// determinístico e os testes podem fixá-lo.
	data, _ := json.Marshal(names)
	return string(data)
}

// SeparatorArgs constrói os argumentos do audio-separator. Função pura, como
// o BuildArgs do downloader.
func SeparatorArgs(input, outDir, modelDir string, p Preset) []string {
	return []string{
		input,
		"--model_filename", p.Model,
		"--model_file_dir", modelDir,
		"--output_dir", outDir,
		// WAV à saída da separação: a conversão (FLAC para o estúdio) é
		// feita depois, uma só vez, pelo ffmpeg.
		"--output_format", "WAV",
		"--custom_output_names", customNamesJSON(),
	}
}

// defaultNamePattern apanha a pista no nome por omissão do audio-separator:
// "música_(Vocals)_htdemucs_6s.wav".
var defaultNamePattern = regexp.MustCompile(`_\(([A-Za-z]+)\)`)

// stemFromFilename devolve a pista (em português) de um ficheiro de saída do
// audio-separator, aceitando tanto o nome final como o formato por omissão
// (a rede de segurança para uma versão que ignore --custom_output_names).
func stemFromFilename(name string) (string, bool) {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	for _, pt := range stemNames {
		if base == pt {
			return pt, true
		}
	}
	// A última ocorrência: o título da música pode ter parênteses seus.
	matches := defaultNamePattern.FindAllStringSubmatch(base, -1)
	for i := len(matches) - 1; i >= 0; i-- {
		if pt, ok := stemNames[strings.ToLower(matches[i][1])]; ok {
			return pt, true
		}
	}
	return "", false
}

// Collect encontra as pistas que o audio-separator escreveu em dir e devolve
// pista → caminho do WAV.
func Collect(dir string) (map[string]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("erro ao ler as pistas separadas: %w", err)
	}
	tracks := make(map[string]string)
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".wav") {
			continue
		}
		if stem, ok := stemFromFilename(e.Name()); ok {
			tracks[stem] = filepath.Join(dir, e.Name())
		}
	}
	if len(tracks) == 0 {
		return nil, fmt.Errorf("o audio-separator não produziu nenhuma pista em %s", dir)
	}
	return tracks, nil
}

// ToWAVArgs converte qualquer áudio (ou vídeo) num WAV estéreo a 44.1kHz, o
// que os modelos esperam à entrada, aplicando um recorte opcional.
func ToWAVArgs(input, output, start, end string) []string {
	args := []string{"-hide_banner", "-loglevel", "error", "-i", input}
	if start != "" {
		args = append(args, "-ss", start)
	}
	if end != "" {
		args = append(args, "-to", end)
	}
	return append(args, "-vn", "-ac", "2", "-ar", "44100", "-c:a", "pcm_s16le", "-y", output)
}

// codecArgs são os argumentos do ffmpeg para cada formato de saída.
func codecArgs(format string) []string {
	switch format {
	case "flac":
		return []string{"-c:a", "flac"}
	case "wav":
		return []string{"-c:a", "pcm_s16le"}
	default:
		return []string{"-c:a", "libmp3lame", "-b:a", "320k"}
	}
}

// EncodeArgs converte uma pista para o formato final, com o título nos
// metadados — é o que aparece no leitor de música do telemóvel.
func EncodeArgs(input, output, format, title string) []string {
	args := []string{"-hide_banner", "-loglevel", "error", "-i", input}
	args = append(args, codecArgs(format)...)
	return append(args, "-metadata", "title="+title, "-y", output)
}

// Track é uma pista a misturar, com o seu ganho (1 = volume original).
type Track struct {
	Path string
	Gain float64
}

// gainFilter escreve um ganho para o filtro volume do ffmpeg.
func gainFilter(g float64) string {
	return "volume=" + strconv.FormatFloat(g, 'f', 3, 64)
}

// MixArgs soma as pistas numa só faixa, já no formato final, aplicando o
// ganho de cada uma (o volume que o utilizador deixou no estúdio). O
// normalize=0 é o que mantém o volume original: as pistas somadas
// reconstroem a música, e o amix por omissão dividia cada uma pelo número de
// entradas. Uma só pista não passa pelo amix.
func MixArgs(tracks []Track, output, format, title string) []string {
	args := []string{"-hide_banner", "-loglevel", "error"}
	for _, t := range tracks {
		args = append(args, "-i", t.Path)
	}
	if len(tracks) == 1 {
		if tracks[0].Gain != 1 {
			args = append(args, "-af", gainFilter(tracks[0].Gain))
		}
	} else {
		var graph, labels strings.Builder
		for i, t := range tracks {
			if t.Gain == 1 {
				fmt.Fprintf(&labels, "[%d:a]", i)
				continue
			}
			fmt.Fprintf(&graph, "[%d:a]%s[g%d];", i, gainFilter(t.Gain), i)
			fmt.Fprintf(&labels, "[g%d]", i)
		}
		graph.WriteString(labels.String())
		fmt.Fprintf(&graph, "amix=inputs=%d:normalize=0", len(tracks))
		args = append(args, "-filter_complex", graph.String())
	}
	args = append(args, codecArgs(format)...)
	return append(args, "-metadata", "title="+title, "-y", output)
}

// Render mistura as pistas num ficheiro (ver MixArgs).
func Render(ctx context.Context, tracks []Track, output, format, title string) error {
	if len(tracks) == 0 {
		return fmt.Errorf("escolhe pelo menos uma pista")
	}
	if !slices.Contains(Formats, format) {
		return fmt.Errorf("formato inválido: %q (usa %s)", format, strings.Join(Formats, ", "))
	}
	return runFFmpeg(ctx, MixArgs(tracks, output, format, title))
}

// peaksRate é a frequência a que o ffmpeg reamostra a pista para medir a
// forma de onda: chega para o desenho e mantém 5 minutos em ~5MB.
const peaksRate = 8000

// PeaksArgs pede ao ffmpeg a pista em PCM mono de 16 bits, no stdout.
func PeaksArgs(input string) []string {
	return []string{"-hide_banner", "-loglevel", "error", "-i", input,
		"-ac", "1", "-ar", strconv.Itoa(peaksRate), "-f", "s16le", "-"}
}

// Peaks é a forma de onda de uma pista: o pico de cada intervalo, entre 0 e
// 1, e a duração em segundos.
type Peaks struct {
	Duration float64   `json:"duration"`
	Values   []float64 `json:"values"`
}

// ComputePeaks reduz PCM mono s16le a n picos. Função pura.
func ComputePeaks(pcm []byte, rate, n int) Peaks {
	samples := len(pcm) / 2
	p := Peaks{Duration: float64(samples) / float64(rate)}
	if samples == 0 || n <= 0 {
		return p
	}
	n = min(n, samples)
	p.Values = make([]float64, n)
	for i := range n {
		from, to := i*samples/n, (i+1)*samples/n
		var peak int
		for j := from; j < to; j++ {
			v := int(int16(binary.LittleEndian.Uint16(pcm[2*j:])))
			peak = max(peak, v, -v)
		}
		p.Values[i] = math.Round(float64(peak)/32768*1000) / 1000
	}
	return p
}

// MeasurePeaks mede a forma de onda de uma pista com n pontos.
func MeasurePeaks(ctx context.Context, input string, n int) (Peaks, error) {
	cmd := exec.CommandContext(ctx, "ffmpeg", PeaksArgs(input)...)
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 10 * time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return Peaks{}, ctx.Err()
		}
		return Peaks{}, fmt.Errorf("ffmpeg falhou a medir %s: %w\n%s", filepath.Base(input), err,
			procio.Tail(strings.TrimSpace(stderr.String()), 1024))
	}
	return ComputePeaks(stdout.Bytes(), peaksRate, n), nil
}

// tqdmPercent apanha a percentagem das barras do tqdm que o audio-separator
// escreve no stderr ("  45%|████▌     | 9/20 [00:10<00:12, …]").
var tqdmPercent = regexp.MustCompile(`(\d{1,3})%\|`)

// ParseTQDM devolve a fração de uma linha de progresso do tqdm.
func ParseTQDM(line string) (float64, bool) {
	m := tqdmPercent.FindStringSubmatch(line)
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil || n > 100 {
		return 0, false
	}
	return float64(n) / 100, true
}

// Separate corre o audio-separator sobre input e devolve as pistas escritas em
// outDir (pista → WAV). O output vai para log, linha a linha, quando não é
// nulo; progress recebe a fração da barra do modelo. O cancelamento do
// contexto envia SIGINT, como no downloader.
func Separate(ctx context.Context, input, outDir, modelDir string, p Preset, log func(string), progress func(float64)) (map[string]string, error) {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, fmt.Errorf("não foi possível criar %s: %w", outDir, err)
	}
	// Os modelos descarregam-se na primeira utilização de cada um.
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		return nil, fmt.Errorf("não foi possível criar a pasta dos modelos %s: %w", modelDir, err)
	}

	cmd := exec.CommandContext(ctx, "audio-separator", SeparatorArgs(input, outDir, modelDir, p)...)
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 10 * time.Second

	captured := &procio.TailBuffer{Max: 4096}
	lines := procio.NewLineWriter(func(line string) {
		if f, ok := ParseTQDM(line); ok {
			if progress != nil {
				progress(f)
			}
			return
		}
		fmt.Fprintln(captured, line)
		if log != nil {
			log(line)
		}
	})
	cmd.Stdout, cmd.Stderr = lines, lines

	err := cmd.Run()
	lines.Flush()
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if out := strings.TrimSpace(captured.String()); out != "" {
			return nil, fmt.Errorf("audio-separator falhou: %w\n%s", err, procio.Tail(out, 1024))
		}
		return nil, fmt.Errorf("audio-separator falhou: %w", err)
	}
	return Collect(outDir)
}

// Export converte as pistas pedidas (pela ordem dada) para destDir, uma por
// ficheiro <pista>.<formato>. title (o nome da música) vai para os
// metadados. Devolve os ficheiros criados.
func Export(ctx context.Context, tracks map[string]string, names []string, destDir, format, title string) ([]string, error) {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return nil, fmt.Errorf("não foi possível criar %s: %w", destDir, err)
	}

	var files []string
	for _, stem := range names {
		src, ok := tracks[stem]
		if !ok {
			return files, fmt.Errorf("o modelo não produziu a pista %q", stem)
		}
		dst := filepath.Join(destDir, stem+"."+format)
		if err := runFFmpeg(ctx, EncodeArgs(src, dst, format, title+" ("+stem+")")); err != nil {
			return files, err
		}
		files = append(files, dst)
	}
	return files, nil
}

// ToWAV converte input num WAV pronto a separar (ver ToWAVArgs).
func ToWAV(ctx context.Context, input, output, start, end string) error {
	return runFFmpeg(ctx, ToWAVArgs(input, output, start, end))
}

func runFFmpeg(ctx context.Context, args []string) error {
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 10 * time.Second
	if out, err := cmd.CombinedOutput(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("ffmpeg falhou: %w\n%s", err, procio.Tail(strings.TrimSpace(string(out)), 1024))
	}
	return nil
}
