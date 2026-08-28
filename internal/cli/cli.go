// Package cli implementa o modo terminal: downloads sem os limites do
// Discord (sem cap de tamanho, sem compressão, melhor qualidade), com
// suporte a playlists/canais, vários URLs e ficheiros de URLs, e com
// semântica de "continuar" via --download-archive e --continue.
package cli

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"dotboot/internal/config"
	"dotboot/internal/downloader"
)

var (
	// errHelp sinaliza que o utilizador pediu a ajuda. Pedir ajuda não é um
	// erro: o Run imprime o texto e devolve nil, para o processo sair a 0.
	errHelp = errors.New("ajuda pedida")

	// ErrUsage sinaliza argumentos inválidos já reportados ao utilizador.
	// O main sai com código 2 sem voltar a imprimir a mensagem.
	ErrUsage = errors.New("argumentos inválidos")

	// ErrPartial sinaliza que parte dos downloads terminou e parte falhou —
	// o normal numa playlist com um vídeo privado pelo meio. Já foi
	// reportado ao utilizador; o main apenas ajusta o código de saída.
	ErrPartial = errors.New("alguns downloads falharam")
)

// archiveFileName regista os vídeos já descarregados no diretório de saída;
// re-executar o mesmo comando salta-os e continua dos que faltam.
const archiveFileName = ".dotboot-archive.txt"

const usageText = `Uso: dotboot download [flags] URL...

As flags têm de vir antes dos URLs.

Flags:
  -o DIR        Diretório de saída; relativo é resolvido sob $DOTBOOT_OUTPUT_DIR
                quando definido (default: $DOTBOOT_OUTPUT_DIR ou o diretório atual)
  -a FICHEIRO   Ficheiro com um URL por linha
  -q QUALIDADE  Qualidade do vídeo: best (default) ou altura máxima em pixels
                (ex: 1080, 720, 480)
  -formato FMT  mp4 (default) garante MP4 com H.264/AAC, que o iPhone e o
                Android leem sem conversão; original guarda o ficheiro tal
                como o site o serve (pode ser .webm ou .mkv)
  -start TEMPO  Início do recorte, MM:SS ou HH:MM:SS (exige -end e um único URL)
  -end TEMPO    Fim do recorte

Exemplos:
  dotboot download https://youtube.com/watch?v=xxxx
  dotboot download -o ~/Videos https://youtube.com/playlist?list=yyyy
  dotboot download -a urls.txt -o ~/Videos
  dotboot download -q 720 -o aulas https://youtube.com/watch?v=xxxx
  dotboot download -formato original https://youtube.com/watch?v=xxxx
  dotboot download -start 01:30 -end 02:00 https://x.com/user/status/123
`

// options é o resultado validado do parsing dos argumentos.
type options struct {
	outputDir string
	batchFile string
	// format e formatSort são os valores de -f e -S do yt-dlp já traduzidos
	// a partir de -q; ambos vazios = melhor qualidade disponível.
	format     string
	formatSort string
	// mp4 pede a garantia de MP4/H.264/AAC (-formato mp4, o default).
	// Falso (-formato original) devolve o comportamento cru do yt-dlp.
	mp4 bool
	// height é a altura pedida em -q, 0 para "best". Só serve para avisar
	// que acima de 1080p uma recodificação é praticamente certa.
	height int
	start  string
	end    string
	urls   []string
}

// parseArgs interpreta e valida os argumentos do subcomando download.
// defaultOutputDir (tipicamente cfg.OutputDir, vindo de DOTBOOT_OUTPUT_DIR)
// é o caminho base: usado quando -o não é passado, e raiz de um -o relativo.
// Um -o absoluto ignora a base; sem base, -o relativo refere o diretório
// atual e a ausência de -o cai para o diretório atual.
func parseArgs(args []string, defaultOutputDir string) (*options, error) {
	o := &options{}

	fs := flag.NewFlagSet("download", flag.ContinueOnError)
	// O FlagSet não imprime nada: quem reporta erros e ajuda é o Run, para
	// não duplicar mensagens nem escrever a ajuda no stderr.
	fs.SetOutput(io.Discard)
	var quality, format string
	fs.StringVar(&o.outputDir, "o", "", "diretório de saída")
	fs.StringVar(&o.batchFile, "a", "", "ficheiro com um URL por linha")
	fs.StringVar(&quality, "q", "", "qualidade do vídeo: best ou altura em pixels")
	fs.StringVar(&format, "formato", "", "formato de saída: mp4 (default) ou original")
	fs.StringVar(&o.start, "start", "", "tempo de início do recorte")
	fs.StringVar(&o.end, "end", "", "tempo de fim do recorte")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil, errHelp
		}
		return nil, err
	}
	o.urls = fs.Args()

	sel, sort, err := downloader.QualitySelectors(quality)
	if err != nil {
		return nil, fmt.Errorf("-q: %w", err)
	}
	o.format, o.formatSort = sel, sort
	if quality != "" && quality != "best" {
		// Já validado por QualitySelectors, que rejeita tudo o que não seja
		// um inteiro positivo.
		o.height, _ = strconv.Atoi(quality)
	}

	switch format {
	case "", "mp4":
		o.mp4 = true
	case "original":
		o.mp4 = false
	default:
		return nil, fmt.Errorf("-formato inválido: %q (usa \"mp4\" ou \"original\")", format)
	}

	if o.outputDir == "" {
		o.outputDir = defaultOutputDir
	} else if defaultOutputDir != "" && !filepath.IsAbs(o.outputDir) {
		o.outputDir = filepath.Join(defaultOutputDir, o.outputDir)
	}
	if o.outputDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("não foi possível determinar o diretório atual: %w", err)
		}
		o.outputDir = wd
	}

	if len(o.urls) == 0 && o.batchFile == "" {
		return nil, fmt.Errorf("indica pelo menos um URL ou um ficheiro de URLs com -a (vê: dotboot help)")
	}

	if (o.start != "") != (o.end != "") {
		return nil, fmt.Errorf("para fazer um recorte fornece ambos -start e -end")
	}
	if o.start != "" {
		if o.batchFile != "" {
			return nil, fmt.Errorf("o recorte (-start/-end) não é compatível com -a")
		}
		if len(o.urls) != 1 {
			return nil, fmt.Errorf("o recorte (-start/-end) exige exatamente um URL")
		}
		if err := downloader.ValidateRange(o.start, o.end); err != nil {
			return nil, fmt.Errorf("recorte inválido: %w", err)
		}
	}

	for _, u := range o.urls {
		if err := downloader.ValidateURL(u); err != nil {
			return nil, err
		}
	}

	if o.batchFile != "" {
		if err := validateBatchFile(o.batchFile); err != nil {
			return nil, err
		}
	}

	return o, nil
}

// validateBatchFile garante que o ficheiro de -a existe e que cada linha útil
// é um URL válido — apanhar isto aqui poupa uma execução inteira do yt-dlp.
// Linhas vazias e comentários (#) são ignorados, como no yt-dlp.
func validateBatchFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("ficheiro de URLs (-a): %w", err)
	}
	defer f.Close()

	urls := 0
	scanner := bufio.NewScanner(f)
	for line := 1; scanner.Scan(); line++ {
		text := strings.TrimSpace(scanner.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		if err := downloader.ValidateURL(text); err != nil {
			return fmt.Errorf("%s, linha %d: %w", path, line, err)
		}
		urls++
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("erro ao ler %s: %w", path, err)
	}
	if urls == 0 {
		return fmt.Errorf("%s não contém nenhum URL", path)
	}
	return nil
}

// Run executa o subcomando download. Não impõe timeout — playlists grandes
// podem demorar horas; CTRL+C cancela o contexto, o yt-dlp recebe SIGINT e
// os ficheiros .part ficam prontos para retomar na execução seguinte.
func Run(ctx context.Context, args []string, cfg *config.Config, log *slog.Logger) error {
	o, err := parseArgs(args, cfg.OutputDir)
	if errors.Is(err, errHelp) {
		fmt.Print(usageText)
		return nil
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n\n%s", err, usageText)
		return ErrUsage
	}

	if err := os.MkdirAll(o.outputDir, 0o755); err != nil {
		return fmt.Errorf("não foi possível criar o diretório de saída %s: %w", o.outputDir, err)
	}

	opts := downloader.Options{
		URLs:           o.urls,
		BatchFile:      o.batchFile,
		Format:         o.format,
		FormatSort:     o.formatSort,
		OutputTemplate: filepath.Join(o.outputDir, "%(title)s [%(id)s].%(ext)s"),
		Continue:       true,
		IgnoreErrors:   true,
		Stdout:         os.Stdout,
		Stderr:         os.Stderr,
	}

	if o.mp4 {
		// Primeira camada da garantia: pedir ao yt-dlp formatos que já são
		// H.264/AAC num contentor MP4. Resolve o caso comum sem recodificar.
		opts.MergeFormat = "mp4"
		opts.FormatSort = downloader.MP4Sort(o.formatSort)

		// O yt-dlp escreve aqui uma linha por vídeo concluído. É o que permite
		// converter exatamente os ficheiros produzidos — incluindo os de uma
		// playlist — sem adivinhar nomes a partir do diretório.
		pathFile, err := os.CreateTemp("", "dotboot-paths-*.txt")
		if err != nil {
			return fmt.Errorf("não foi possível criar o ficheiro de caminhos: %w", err)
		}
		pathFile.Close()
		defer os.Remove(pathFile.Name())
		opts.PrintToFile = pathFile.Name()

		if o.height > maxH264Height {
			fmt.Printf("ℹ️  Acima de %dp a maioria dos sites só serve AV1/VP9, "+
				"por isso o vídeo vai ter de ser convertido para H.264 (demora).\n"+
				"   Usa -q %d para um download direto, ou -formato original para não converter.\n\n",
				maxH264Height, maxH264Height)
		}
	}

	if o.start != "" {
		// Recorte: aplica-se a um único vídeo (nunca a playlists inteiras)
		// e não usa o archive — um recorte não pode marcar o vídeo completo
		// como já descarregado.
		opts.Section = &downloader.Section{Start: o.start, End: o.end}
		opts.NoPlaylist = true
	} else {
		opts.ArchiveFile = filepath.Join(o.outputDir, archiveFileName)
	}

	log.Info("a iniciar downloads", "output_dir", o.outputDir)

	// O archive ganha uma linha por vídeo descarregado com sucesso: comparar
	// antes e depois distingue "a playlist toda falhou" de "um vídeo privado
	// pelo meio", que com --ignore-errors dão o mesmo código de saída.
	before := countArchive(opts.ArchiveFile)

	runErr := downloader.Run(ctx, opts)

	// A conversão corre mesmo quando o yt-dlp falhou: com --ignore-errors uma
	// playlist pode ter descarregado quase tudo antes de tropeçar num vídeo
	// privado, e esses ficheiros também têm de sair em MP4.
	convErr := ensurePlayable(ctx, opts.PrintToFile)

	if runErr != nil {
		if done := countArchive(opts.ArchiveFile) - before; done > 0 {
			fmt.Printf("\n⚠️  %d vídeo(s) descarregado(s) em %s; outros falharam (vê os erros acima).\n", done, o.outputDir)
			return ErrPartial
		}
		return fmt.Errorf("download falhou: %w", runErr)
	}
	if convErr != nil {
		return convErr
	}

	fmt.Printf("\n✅ Downloads concluídos em %s\n", o.outputDir)
	return nil
}

// maxH264Height é a altura acima da qual os grandes sites (o YouTube em
// particular) deixam de oferecer H.264 e só servem VP9/AV1 — a partir daqui
// garantir MP4 implica recodificar.
const maxH264Height = 1080

// ensurePlayable converte para MP4/H.264/AAC os ficheiros que o yt-dlp
// registou em pathFile. É a segunda camada da garantia: o -S pede os codecs
// certos, isto assegura-os quando o site não os tinha.
//
// pathFile vazio significa -formato original: não há nada a fazer.
func ensurePlayable(ctx context.Context, pathFile string) error {
	if pathFile == "" {
		return nil
	}

	paths := printedPaths(pathFile)
	var failed int
	for _, path := range paths {
		// Um contexto já cancelado (CTRL+C) não vai conseguir converter nada;
		// mais vale deixar os ficheiros intactos do que tentar e falhar N vezes.
		if ctx.Err() != nil {
			fmt.Fprintf(os.Stderr, "\n⚠️  Conversão interrompida; %s e os seguintes ficam no formato original.\n",
				filepath.Base(path))
			return ErrPartial
		}
		if _, err := downloader.EnsureMP4(ctx, path, os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "⚠️  %v\n", err)
			failed++
		}
	}

	if failed > 0 {
		// O download em si correu bem: os ficheiros existem, só não estão
		// garantidamente em MP4. Não é uma falha total.
		fmt.Fprintf(os.Stderr, "\n⚠️  %d de %d ficheiro(s) não puderam ser convertidos para MP4 e ficaram no formato original.\n",
			failed, len(paths))
		return ErrPartial
	}
	return nil
}

// printedPaths lê os caminhos que o yt-dlp escreveu com --print-to-file, um por
// linha. Um ficheiro inexistente ou vazio (nada descarregado, tudo já no
// archive) devolve uma lista vazia, não um erro.
func printedPaths(pathFile string) []string {
	data, err := os.ReadFile(pathFile)
	if err != nil {
		return nil
	}

	var paths []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// O ficheiro pode ter sobrevivido a uma execução anterior, ou o vídeo
		// pode ter sido removido entretanto: confirmar antes de invocar o ffmpeg.
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
