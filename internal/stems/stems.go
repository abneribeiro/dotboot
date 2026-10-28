// Package stems separa uma música nas suas pistas (vozes, bateria, baixo,
// guitarra, piano…) com o audio-separator, e mistura ou converte o resultado
// com o ffmpeg. Regra de fronteira, como no downloader: não importa discordgo
// nem o CLI — o /separar do bot há de usá-lo tal como está.
package stems

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Preset associa um nome curto (o -modelo do CLI) a um modelo do
// audio-separator e às pistas que ele produz.
type Preset struct {
	// Name é o valor de -modelo.
	Name string
	// Model é o ficheiro do modelo no catálogo do audio-separator.
	Model string
	// Stems são as pistas produzidas, já em português e pela ordem em que
	// são apresentadas e guardadas.
	Stems []string
	// Description resume o preset para a ajuda.
	Description string
}

// DefaultPreset é o -modelo por omissão: o único que separa a guitarra.
const DefaultPreset = "6s"

var presets = []Preset{
	{
		Name:        "6s",
		Model:       "htdemucs_6s.yaml",
		Stems:       []string{"vozes", "bateria", "baixo", "guitarra", "piano", "outros"},
		Description: "vozes, bateria, baixo, guitarra, piano e outros",
	},
	{
		// O htdemucs_ft afina um modelo por pista: mais limpo do que o 6s,
		// mas sem guitarra nem piano (ficam em "outros").
		Name:        "4s",
		Model:       "htdemucs_ft.yaml",
		Stems:       []string{"vozes", "bateria", "baixo", "outros"},
		Description: "vozes, bateria, baixo e outros (mais limpo, sem guitarra)",
	},
	{
		// O BS-RoFormer é o melhor a isolar a voz: ideal para karaoke ou
		// para ficar com a voz a capella.
		Name:        "voz",
		Model:       "model_bs_roformer_ep_317_sdr_12.9755.ckpt",
		Stems:       []string{"vozes", "instrumental"},
		Description: "vozes e instrumental (o melhor para karaoke)",
	},
}

// Presets devolve os presets disponíveis, pela ordem da ajuda.
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

// Selection é o que se guarda depois de separar.
type Selection struct {
	// Keep são as pistas a guardar, pela ordem do preset.
	Keep []string
	// MixName e MixStems descrevem a faixa "sem X" (vazios sem -sem): o nome
	// do ficheiro e as pistas que a compõem.
	MixName  string
	MixStems []string
}

// Select decide o que guardar: only (-so) restringe as pistas guardadas, e
// without (-sem) acrescenta a mistura de todas as outras — a faixa para tocar
// por cima. Recusa pistas que o preset não produz: "-sem guitarra" com o
// modelo de 4 pistas não teria nada para tirar.
func Select(p Preset, only, without []string) (Selection, error) {
	for _, name := range append(slices.Clone(only), without...) {
		if !slices.Contains(p.Stems, name) {
			return Selection{}, fmt.Errorf("o modelo %s não tem a pista %q (tem: %s)",
				p.Name, name, strings.Join(p.Stems, ", "))
		}
	}

	var sel Selection
	for _, s := range p.Stems {
		if len(only) == 0 || slices.Contains(only, s) {
			sel.Keep = append(sel.Keep, s)
		}
	}

	if len(without) == 0 {
		return sel, nil
	}
	var removed []string
	for _, s := range p.Stems {
		if slices.Contains(without, s) {
			removed = append(removed, s)
		} else {
			sel.MixStems = append(sel.MixStems, s)
		}
	}
	if len(sel.MixStems) == 0 {
		return Selection{}, fmt.Errorf("-sem tira todas as pistas: não sobra nada para misturar")
	}
	sel.MixName = "sem-" + strings.Join(removed, "-")
	return sel, nil
}

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
		// WAV à saída da separação: a conversão para o formato pedido é feita
		// depois, uma só vez, pelo ffmpeg — e a mistura "sem X" parte do WAV
		// sem perdas, em vez de somar MP3.
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

// EncodeArgs converte uma pista WAV para o formato final, com o título nos
// metadados — é o que aparece no leitor de música do telemóvel.
func EncodeArgs(input, output, format, title string) []string {
	args := []string{"-hide_banner", "-loglevel", "error", "-i", input}
	args = append(args, codecArgs(format)...)
	return append(args, "-metadata", "title="+title, "-y", output)
}

// MixArgs soma as pistas numa só faixa, já no formato final. O normalize=0 é
// o que mantém o volume original: as pistas somadas reconstroem a música, e
// o amix por omissão dividia cada uma pelo número de entradas.
func MixArgs(inputs []string, output, format, title string) []string {
	args := []string{"-hide_banner", "-loglevel", "error"}
	for _, in := range inputs {
		args = append(args, "-i", in)
	}
	args = append(args, "-filter_complex", fmt.Sprintf("amix=inputs=%d:normalize=0", len(inputs)))
	args = append(args, codecArgs(format)...)
	return append(args, "-metadata", "title="+title, "-y", output)
}

// Separate corre o audio-separator sobre input e devolve as pistas escritas em
// outDir (pista → WAV). O progresso vai para stdout/stderr quando não são
// nulos; o cancelamento do contexto envia SIGINT, como no downloader.
func Separate(ctx context.Context, input, outDir, modelDir string, p Preset, stdout, stderr io.Writer) (map[string]string, error) {
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
	// Sem destino para o output (uso não interativo), guarda-se o fim para a
	// mensagem de erro.
	var captured bytes.Buffer
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if stdout == nil {
		cmd.Stdout = &captured
	}
	if stderr == nil {
		cmd.Stderr = &captured
	}

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if out := strings.TrimSpace(captured.String()); out != "" {
			return nil, fmt.Errorf("audio-separator falhou: %w\n%s", err, tail(out, 1024))
		}
		return nil, fmt.Errorf("audio-separator falhou: %w", err)
	}
	return Collect(outDir)
}

// Export guarda o resultado em destDir: as pistas de sel.Keep e, se pedida, a
// mistura "sem X", tudo no formato final. title (o nome da música) vai para
// os metadados. Devolve os ficheiros criados.
func Export(ctx context.Context, tracks map[string]string, sel Selection, destDir, format, title string) ([]string, error) {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return nil, fmt.Errorf("não foi possível criar %s: %w", destDir, err)
	}

	var files []string
	for _, stem := range sel.Keep {
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

	if sel.MixName != "" {
		inputs := make([]string, 0, len(sel.MixStems))
		for _, stem := range sel.MixStems {
			src, ok := tracks[stem]
			if !ok {
				return files, fmt.Errorf("o modelo não produziu a pista %q", stem)
			}
			inputs = append(inputs, src)
		}
		dst := filepath.Join(destDir, sel.MixName+"."+format)
		if err := runFFmpeg(ctx, MixArgs(inputs, dst, format, title+" ("+sel.MixName+")")); err != nil {
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
		return fmt.Errorf("ffmpeg falhou: %w\n%s", err, tail(strings.TrimSpace(string(out)), 1024))
	}
	return nil
}

// tail devolve os últimos n bytes de s.
func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
