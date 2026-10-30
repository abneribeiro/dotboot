package jobs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/abneribeiro/dotboot/internal/downloader"
	"github.com/abneribeiro/dotboot/internal/procio"
)

const (
	// archiveFileName regista os vídeos já descarregados no diretório de
	// saída; repetir o mesmo pedido salta-os e continua dos que faltam.
	archiveFileName = ".dotboot-archive.txt"

	// audioArchiveFileName é o archive dos downloads só de áudio. Tem de ser
	// outro ficheiro: o archive regista o ID do vídeo e não o formato, e com
	// um só ficheiro um vídeo já guardado em MP4 nunca chegava a sair em MP3.
	audioArchiveFileName = ".dotboot-archive-audio.txt"

	// pendingFileName lista os ficheiros que o yt-dlp concluiu e que ainda
	// não passaram pela conversão para MP4. Vive no diretório de saída, e não
	// num temporário, porque o archive regista o vídeo ANTES da conversão: um
	// CTRL+C a meio deixava o .webm para sempre, já que a execução seguinte
	// saltava o vídeo. Só é apagado quando tudo o que lista foi convertido.
	pendingFileName = ".dotboot-pendentes.txt"

	// maxH264Height é a resolução acima da qual os grandes sites (o YouTube
	// em particular) deixam de oferecer H.264 — a partir daqui garantir MP4
	// implica recodificar.
	maxH264Height = 1080
)

// Formatos aceites num Download.
const (
	FormatMP4      = "mp4"
	FormatOriginal = "original"
	FormatMP3      = "mp3"
	FormatM4A      = "m4a"
)

// VideoFormats e AudioFormats são as opções de formato, pela ordem da ajuda.
var (
	VideoFormats = []string{FormatMP4, FormatOriginal}
	AudioFormats = []string{FormatMP3, FormatM4A}
)

// DownloadInput é um pedido de download tal como vem da página, antes de
// validado.
type DownloadInput struct {
	URLs []string
	// OutputDir é a pasta (absoluta) onde o servidor quer os ficheiros.
	OutputDir string
	// Quality é "best" (ou vazio) ou a resolução máxima, ex: "720".
	Quality string
	// Format: mp4 (default), original, mp3 ou m4a.
	Format string
	Start  string
	End    string
}

// Download é um pedido validado, pronto a correr.
type Download struct {
	URLs       []string
	OutputDir  string
	Format     string
	FormatSort string
	// Height é a resolução pedida, 0 para "best": serve para avisar que
	// acima de 1080p uma recodificação é praticamente certa.
	Height  int
	Section *downloader.Section
}

// Audio reporta se o pedido guarda só o áudio.
func (d *Download) Audio() bool {
	return slices.Contains(AudioFormats, d.Format)
}

// NewDownload valida o pedido.
func NewDownload(in DownloadInput) (*Download, error) {
	d := &Download{URLs: in.URLs, OutputDir: in.OutputDir}
	if !filepath.IsAbs(d.OutputDir) {
		return nil, fmt.Errorf("pasta de saída inválida: %q", d.OutputDir)
	}

	switch in.Format {
	case "", FormatMP4:
		d.Format = FormatMP4
	case FormatOriginal, FormatMP3, FormatM4A:
		d.Format = in.Format
	default:
		return nil, fmt.Errorf("formato inválido: %q (usa mp4, original, mp3 ou m4a)", in.Format)
	}

	var err error
	if d.FormatSort, err = downloader.QualitySort(in.Quality); err != nil {
		return nil, err
	}
	if d.FormatSort != "" {
		if d.Audio() {
			return nil, fmt.Errorf("a qualidade escolhe a resolução do vídeo e não se aplica a %s", d.Format)
		}
		// Já validado por QualitySort, que rejeita tudo o que não seja um
		// inteiro positivo.
		d.Height, _ = strconv.Atoi(in.Quality)
	}

	if len(d.URLs) == 0 {
		return nil, fmt.Errorf("indica pelo menos um URL")
	}

	if (in.Start != "") != (in.End != "") {
		return nil, fmt.Errorf("para fazer um recorte indica o início e o fim")
	}
	if in.Start != "" {
		if len(d.URLs) != 1 {
			return nil, fmt.Errorf("o recorte exige exatamente um URL")
		}
		if err := downloader.ValidateRange(in.Start, in.End); err != nil {
			return nil, fmt.Errorf("recorte inválido: %w", err)
		}
		d.Section = &downloader.Section{Start: in.Start, End: in.End}
	}

	for _, u := range d.URLs {
		if err := downloader.ValidateURL(u); err != nil {
			return nil, err
		}
	}
	return d, nil
}

// outputTemplate devolve o -o do yt-dlp. Um recorte leva o intervalo no nome:
// com o mesmo nome do vídeo completo, o yt-dlp encontrava o ficheiro já
// existente e respondia "already downloaded" — num sentido não recortava
// nada, no outro registava no archive o vídeo completo com o conteúdo do
// recorte, e esse vídeo nunca mais era descarregado.
func outputTemplate(dir string, sec *downloader.Section) string {
	name := "%(title)s [%(id)s]"
	if sec != nil {
		name += fmt.Sprintf(" (%s-%s)", clockForFilename(sec.Start), clockForFilename(sec.End))
	}
	return filepath.Join(dir, name+".%(ext)s")
}

// clockForFilename troca ":" por ".": o Windows e o Finder do macOS não
// aceitam dois pontos em nomes de ficheiros.
func clockForFilename(t string) string {
	return strings.ReplaceAll(t, ":", ".")
}

// reporterSink adapta um Reporter aos callbacks do downloader, num só sítio.
type reporterSink struct {
	mu     sync.Mutex
	report Reporter
}

func (r *reporterSink) send(e Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.report(e)
}

func (r *reporterSink) log(line string) { r.send(Event{Log: line}) }

// progress traduz um downloader.Progress num Event.
func (r *reporterSink) progress(p downloader.Progress) {
	e := Event{Index: p.Index, Count: p.Count}
	if p.File != "" {
		e.Item = filepath.Base(p.File)
	}
	switch p.Phase {
	case downloader.PhaseInfo:
		e.Stage = StageInfo
	case downloader.PhaseDownload:
		e.Stage = StageDownload
		// Uma linha "Destination" só anuncia o ficheiro; o progresso vem nas
		// linhas do template, que têm sempre Downloaded ou Total.
		if p.File == "" {
			e.Progress, e.Fraction, e.Speed, e.ETA = true, p.Fraction(), p.Speed, p.ETA
		}
	case downloader.PhaseMerge:
		e.Stage, e.Progress, e.Fraction = StageMerge, true, -1
	case downloader.PhaseExtract:
		e.Stage, e.Progress, e.Fraction = StageExtract, true, -1
	}
	r.send(e)
}

// Run executa o download. Não impõe timeout — playlists grandes podem
// demorar horas; cancelar o contexto envia SIGINT ao yt-dlp e os .part ficam
// prontos para retomar no pedido seguinte. Um contexto cancelado devolve
// context.Canceled; parte dos itens falhada devolve ErrPartial com o Result
// do que foi feito.
func (d *Download) Run(ctx context.Context, env Env, report Reporter) (Result, error) {
	res := Result{OutputDir: d.OutputDir}
	sink := &reporterSink{report: report}

	if err := PrepareOutputDir(d.OutputDir); err != nil {
		return res, err
	}
	stderr := procio.NewLineWriter(sink.log)
	defer stderr.Flush()
	opts := downloader.Options{
		URLs:           d.URLs,
		FormatSort:     d.FormatSort,
		OutputTemplate: outputTemplate(d.OutputDir, d.Section),
		Continue:       true,
		IgnoreErrors:   true,
		CookiesFile:    env.CookiesFile,
		JSRuntimes:     env.JSRuntimes,
		Progress:       sink.progress,
		Stdout:         writerFunc(sink.log),
		Stderr:         stderr,
	}

	archive := archiveFileName
	switch {
	case d.Audio():
		opts.Format = "ba/b"
		opts.ExtractAudio = d.Format
		opts.EmbedMetadata = true
		archive = audioArchiveFileName

	case d.Format == FormatMP4:
		// Primeira camada da garantia: pedir ao yt-dlp formatos que já são
		// H.264/AAC num contentor MP4. Resolve o caso comum sem recodificar.
		opts.MergeFormat = "mp4"
		opts.FormatSort = downloader.MP4Sort(d.FormatSort)
		if d.Height > maxH264Height {
			sink.log(fmt.Sprintf("ℹ️  Acima de %dp a maioria dos sites só serve AV1/VP9: "+
				"o vídeo vai ser convertido para H.264, o que demora. "+
				"%dp descarrega direto; o formato original não converte.", maxH264Height, maxH264Height))
		}
	}

	// O yt-dlp acrescenta aqui uma linha por ficheiro concluído: é por aí que
	// se sabe exatamente o que foi produzido — incluindo numa playlist. No
	// modo MP4 é a lista persistente de pendentes; nos outros, um temporário.
	if d.Format == FormatMP4 {
		opts.PrintToFile = filepath.Join(d.OutputDir, pendingFileName)
	} else {
		tmp, err := os.CreateTemp(env.TempDir, "dotboot-ficheiros-*.txt")
		if err != nil {
			return res, fmt.Errorf("não foi possível criar a lista de ficheiros: %w", err)
		}
		tmp.Close()
		defer os.Remove(tmp.Name())
		opts.PrintToFile = tmp.Name()
	}

	if d.Section != nil {
		// Recorte: aplica-se a um único vídeo (nunca a playlists inteiras)
		// e não usa o archive — um recorte não pode marcar o vídeo completo
		// como já descarregado.
		opts.Section = d.Section
		opts.NoPlaylist = true
	} else {
		opts.ArchiveFile = filepath.Join(d.OutputDir, archive)
	}

	sink.send(Event{Stage: StageInfo, Progress: true, Fraction: -1})

	// O archive ganha uma linha por vídeo descarregado com sucesso: comparar
	// antes e depois distingue "a playlist toda falhou" de "um vídeo privado
	// pelo meio", que com --ignore-errors dão o mesmo código de saída.
	before := countArchive(opts.ArchiveFile)
	runErr := downloader.Run(ctx, opts)

	var convErr error
	if d.Format == FormatMP4 {
		// A conversão corre mesmo quando o yt-dlp falhou: com --ignore-errors
		// uma playlist pode ter descarregado quase tudo antes de tropeçar num
		// vídeo privado, e esses ficheiros também têm de sair em MP4. Corre
		// também o que tenha ficado pendente de uma execução interrompida.
		res.Files, convErr = ensurePlayable(ctx, opts.PrintToFile, sink)
	} else {
		res.Files = printedPaths(opts.PrintToFile)
	}

	if ctx.Err() != nil {
		return res, context.Canceled
	}
	if runErr != nil {
		if countArchive(opts.ArchiveFile)-before > 0 || len(res.Files) > 0 {
			return res, fmt.Errorf("%w: %w", ErrPartial, runErr)
		}
		return res, runErr
	}
	return res, convErr
}

// writerFunc adapta uma função de linha a io.Writer (o downloader já entrega
// o stdout linha a linha).
type writerFunc func(string)

func (f writerFunc) Write(p []byte) (int, error) {
	f(strings.TrimRight(string(p), "\n"))
	return len(p), nil
}

// ensurePlayable converte para MP4/H.264/AAC os ficheiros listados em
// pathFile e devolve os caminhos finais. É a segunda camada da garantia: o
// -S pede os codecs certos, isto assegura-os quando o site não os tinha.
// Quando tudo fica convertido, apaga a lista; com falhas ou uma interrupção
// mantém-na, e a execução seguinte recomeça por aí — o EnsureMP4 não toca no
// que já está conforme.
func ensurePlayable(ctx context.Context, pathFile string, sink *reporterSink) ([]string, error) {
	paths := printedPaths(pathFile)
	var files []string
	var failed int
	for i, path := range paths {
		// Um contexto já cancelado não vai conseguir converter nada; mais
		// vale deixar os ficheiros intactos do que tentar e falhar N vezes.
		if ctx.Err() != nil {
			sink.log(fmt.Sprintf("⚠️  Conversão interrompida: %s e os seguintes são convertidos no próximo pedido.",
				filepath.Base(path)))
			return files, context.Canceled
		}
		final, err := downloader.EnsureMP4(ctx, path, downloader.ConvertHooks{
			Start: func(reencode bool, vcodec string) {
				msg := "remux para MP4, sem perdas"
				if reencode {
					msg = fmt.Sprintf("%s → H.264: pode demorar vários minutos", vcodec)
				}
				sink.send(Event{Stage: StageConvert, Item: filepath.Base(path), Index: i + 1, Count: len(paths),
					Progress: true, Fraction: 0, ETA: -1, Log: "🔄 " + filepath.Base(path) + ": " + msg})
			},
			Progress: func(f float64) {
				sink.send(Event{Progress: true, Fraction: f, ETA: -1})
			},
		})
		if err != nil {
			if ctx.Err() != nil {
				return files, context.Canceled
			}
			sink.log("⚠️  " + err.Error())
			failed++
			files = append(files, path)
			continue
		}
		files = append(files, final)
	}

	if failed > 0 {
		// O download em si correu bem: os ficheiros existem, só não estão
		// garantidamente em MP4.
		return files, fmt.Errorf("%w: %d de %d ficheiro(s) não puderam ser convertidos para MP4 "+
			"(ficam em %s e são tentados no próximo pedido)", ErrPartial, failed, len(paths), pendingFileName)
	}
	if err := os.Remove(pathFile); err != nil && !errors.Is(err, os.ErrNotExist) {
		return files, fmt.Errorf("não foi possível limpar %s: %w", pathFile, err)
	}
	return files, nil
}

// printedPaths lê os caminhos que o yt-dlp escreveu com --print-to-file, um
// por linha e sem repetições (a lista de pendentes acumula entre execuções).
// Um ficheiro inexistente ou vazio (nada descarregado, tudo já no archive)
// devolve uma lista vazia, não um erro.
func printedPaths(pathFile string) []string {
	data, err := os.ReadFile(pathFile)
	if err != nil {
		return nil
	}

	var paths []string
	seen := make(map[string]bool)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || seen[line] {
			continue
		}
		seen[line] = true
		// O vídeo pode ter sido removido ou convertido entretanto
		// (.webm → .mp4): confirmar antes de invocar o ffmpeg.
		if _, err := os.Stat(line); err != nil {
			continue
		}
		paths = append(paths, line)
	}
	return paths
}

// countArchive conta as entradas do ficheiro de archive. Devolve 0 quando o
// ficheiro não existe (primeira execução) ou não está a ser usado (recorte).
func countArchive(path string) int {
	if path == "" {
		return 0
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	return bytes.Count(data, []byte("\n"))
}
