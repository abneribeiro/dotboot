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

	"github.com/abneribeiro/dotboot/internal/config"
	"github.com/abneribeiro/dotboot/internal/downloader"
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

	// ErrInterrupted sinaliza um CTRL+C. Não é uma falha: o que ficou a meio
	// retoma-se ao repetir o comando. O main sai com 130, como as shells.
	ErrInterrupted = errors.New("interrompido")
)

const (
	// archiveFileName regista os vídeos já descarregados no diretório de
	// saída; re-executar o mesmo comando salta-os e continua dos que faltam.
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
)

// audioFormats são os valores de -formato que guardam só o áudio.
var audioFormats = []string{"mp3", "m4a"}

const usageText = `Uso: dotboot download [flags] URL...

Flags (podem vir antes ou depois dos URLs):
  -o DIR          Diretório de saída; relativo é resolvido sob $DOTBOOT_OUTPUT_DIR
                  quando definido (default: $DOTBOOT_OUTPUT_DIR ou o diretório atual)
  -a FICHEIRO     Ficheiro com um URL por linha
  -q QUALIDADE    best (default) ou a resolução máxima, ex: 1080, 720, 480.
                  Conta o lado menor: -q 720 num vídeo vertical dá 720x1280
  -formato FMT    mp4 (default) garante MP4 com H.264/AAC, que o iPhone e o
                  Android leem sem conversão; original guarda o ficheiro tal
                  como o site o serve (pode ser .webm ou .mkv); mp3 ou m4a
                  guardam só o áudio, com título e capa
  -cookies FICH   Cookies (formato Netscape) para vídeos que pedem login ou a
                  verificação "não és um robô" (default: $DOTBOOT_COOKIES_FILE)
  -start TEMPO    Início do recorte, MM:SS ou HH:MM:SS (exige -end e um único URL)
  -end TEMPO      Fim do recorte

Exemplos:
  dotboot download https://youtube.com/watch?v=xxxx
  dotboot download -o ~/Videos https://youtube.com/playlist?list=yyyy
  dotboot download -a urls.txt -o ~/Videos
  dotboot download -q 720 -o aulas https://youtube.com/watch?v=xxxx
  dotboot download -formato mp3 -o musica https://youtube.com/watch?v=xxxx
  dotboot download -formato original https://youtube.com/watch?v=xxxx
  dotboot download -start 01:30 -end 02:00 https://x.com/user/status/123

Re-executar o mesmo comando salta o que já foi descarregado e retoma o que
ficou a meio, incluindo conversões para MP4 interrompidas.
`

// options é o resultado validado do parsing dos argumentos.
type options struct {
	outputDir string
	batchFile string
	// formatSort é o -S do yt-dlp traduzido a partir de -q; vazio = melhor
	// qualidade disponível.
	formatSort string
	// mode é o -formato: "mp4" (default) garante MP4/H.264/AAC, "original"
	// devolve o comportamento cru do yt-dlp, e um formato de áudio (ver
	// audioFormats) guarda só o áudio.
	mode string
	// height é a resolução pedida em -q, 0 para "best". Só serve para avisar
	// que acima de 1080p uma recodificação é praticamente certa.
	height  int
	cookies string
	start   string
	end     string
	urls    []string
}

// audio reporta se o -formato pede só o áudio.
func (o *options) audio() bool {
	for _, f := range audioFormats {
		if o.mode == f {
			return true
		}
	}
	return false
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
	fs.StringVar(&quality, "q", "", "qualidade do vídeo: best ou resolução")
	fs.StringVar(&format, "formato", "", "formato de saída: mp4 (default), original, mp3 ou m4a")
	fs.StringVar(&o.cookies, "cookies", "", "ficheiro de cookies")
	fs.StringVar(&o.start, "start", "", "tempo de início do recorte")
	fs.StringVar(&o.end, "end", "", "tempo de fim do recorte")

	urls, err := parseInterspersed(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil, errHelp
		}
		return nil, err
	}
	o.urls = urls

	switch format {
	case "", "mp4":
		o.mode = "mp4"
	case "original", "mp3", "m4a":
		o.mode = format
	default:
		return nil, fmt.Errorf("-formato inválido: %q (usa mp4, original, mp3 ou m4a)", format)
	}

	if o.formatSort, err = downloader.QualitySort(quality); err != nil {
		return nil, fmt.Errorf("-q: %w", err)
	}
	if o.formatSort != "" {
		if o.audio() {
			return nil, fmt.Errorf("-q escolhe a resolução do vídeo e não se aplica a -formato %s", o.mode)
		}
		// Já validado por QualitySort, que rejeita tudo o que não seja um
		// inteiro positivo.
		o.height, _ = strconv.Atoi(quality)
	}

	if o.outputDir, err = resolveOutputDir(o.outputDir, defaultOutputDir); err != nil {
		return nil, err
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

	if o.cookies != "" {
		if _, err := os.Stat(o.cookies); err != nil {
			return nil, fmt.Errorf("-cookies: %w", err)
		}
	}

	return o, nil
}

// parseInterspersed faz o parsing de flags em qualquer posição. O flag da
// stdlib para no primeiro argumento que não é flag, por isso "dotboot
// download URL -o pasta" tratava o "-o" como um URL. Depois de "--", tudo é
// posicional, como manda a convenção.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return positional, nil
		}
		// O Parse consome o "--" sem o devolver: se foi o último argumento
		// consumido, o resto é todo posicional.
		if consumed := len(args) - len(rest); consumed > 0 && args[consumed-1] == "--" {
			return append(positional, rest...), nil
		}
		positional = append(positional, rest[0])
		args = rest[1:]
	}
}

// resolveOutputDir aplica as regras do caminho base (DOTBOOT_OUTPUT_DIR) ao
// valor de -o: vazio usa a base, relativo fica dentro dela, absoluto ignora-a.
// Sem base, um -o relativo é relativo ao diretório atual e a ausência de -o
// cai para o próprio diretório atual.
func resolveOutputDir(flagValue, base string) (string, error) {
	dir := flagValue
	if dir == "" {
		dir = base
	} else if base != "" && !filepath.IsAbs(dir) {
		dir = filepath.Join(base, dir)
	}
	if dir != "" {
		return dir, nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("não foi possível determinar o diretório atual: %w", err)
	}
	return wd, nil
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
	if o.cookies == "" {
		o.cookies = cfg.CookiesFile
	}

	if err := os.MkdirAll(o.outputDir, 0o755); err != nil {
		return fmt.Errorf("não foi possível criar o diretório de saída %s: %w", o.outputDir, err)
	}

	var sec *downloader.Section
	if o.start != "" {
		sec = &downloader.Section{Start: o.start, End: o.end}
	}

	opts := downloader.Options{
		URLs:           o.urls,
		BatchFile:      o.batchFile,
		FormatSort:     o.formatSort,
		OutputTemplate: outputTemplate(o.outputDir, sec),
		Continue:       true,
		IgnoreErrors:   true,
		CookiesFile:    o.cookies,
		JSRuntimes:     cfg.JSRuntimes,
		Stdout:         os.Stdout,
		Stderr:         os.Stderr,
	}

	archive := archiveFileName
	switch {
	case o.audio():
		opts.Format = "ba/b"
		opts.ExtractAudio = o.mode
		opts.EmbedMetadata = true
		archive = audioArchiveFileName

	case o.mode == "mp4":
		// Primeira camada da garantia: pedir ao yt-dlp formatos que já são
		// H.264/AAC num contentor MP4. Resolve o caso comum sem recodificar.
		opts.MergeFormat = "mp4"
		opts.FormatSort = downloader.MP4Sort(o.formatSort)

		// O yt-dlp acrescenta aqui uma linha por vídeo concluído. É o que
		// permite converter exatamente os ficheiros produzidos — incluindo os
		// de uma playlist — sem adivinhar nomes a partir do diretório.
		opts.PrintToFile = filepath.Join(o.outputDir, pendingFileName)

		if o.height > maxH264Height {
			fmt.Printf("ℹ️  Acima de %dp a maioria dos sites só serve AV1/VP9, "+
				"por isso o vídeo vai ter de ser convertido para H.264 (demora).\n"+
				"   Usa -q %d para um download direto, ou -formato original para não converter.\n\n",
				maxH264Height, maxH264Height)
		}
	}

	if sec != nil {
		// Recorte: aplica-se a um único vídeo (nunca a playlists inteiras)
		// e não usa o archive — um recorte não pode marcar o vídeo completo
		// como já descarregado.
		opts.Section = sec
		opts.NoPlaylist = true
	} else {
		opts.ArchiveFile = filepath.Join(o.outputDir, archive)
	}

	log.Info("a iniciar downloads", "output_dir", o.outputDir)

	// O archive ganha uma linha por vídeo descarregado com sucesso: comparar
	// antes e depois distingue "a playlist toda falhou" de "um vídeo privado
	// pelo meio", que com --ignore-errors dão o mesmo código de saída.
	before := countArchive(opts.ArchiveFile)

	runErr := downloader.Run(ctx, opts)

	// A conversão corre mesmo quando o yt-dlp falhou: com --ignore-errors uma
	// playlist pode ter descarregado quase tudo antes de tropeçar num vídeo
	// privado, e esses ficheiros também têm de sair em MP4. Corre também o
	// que tenha ficado pendente de uma execução interrompida.
	convErr := ensurePlayable(ctx, opts.PrintToFile)

	if ctx.Err() != nil {
		fmt.Fprintf(os.Stderr, "\n⏹️  Interrompido. Repete o mesmo comando para continuar de onde ficou.\n")
		return ErrInterrupted
	}
	if runErr != nil {
		if done := countArchive(opts.ArchiveFile) - before; done > 0 {
			fmt.Printf("\n⚠️  %d ficheiro(s) descarregado(s) em %s; outros falharam (vê os erros acima).\n", done, o.outputDir)
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

// ensurePlayable converte para MP4/H.264/AAC os ficheiros listados em
// pathFile. É a segunda camada da garantia: o -S pede os codecs certos, isto
// assegura-os quando o site não os tinha. Quando tudo fica convertido, apaga
// a lista; com falhas ou uma interrupção mantém-na, e a execução seguinte
// recomeça por aí — o EnsureMP4 não toca no que já está conforme.
//
// pathFile vazio significa -formato original ou áudio: não há nada a fazer.
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
			fmt.Fprintf(os.Stderr, "\n⚠️  Conversão interrompida; %s e os seguintes são convertidos na próxima execução.\n",
				filepath.Base(path))
			return ErrPartial
		}
		if _, err := downloader.EnsureMP4(ctx, path, os.Stdout); err != nil {
			if ctx.Err() != nil {
				fmt.Fprintf(os.Stderr, "\n⚠️  Conversão interrompida; %s e os seguintes são convertidos na próxima execução.\n",
					filepath.Base(path))
				return ErrPartial
			}
			fmt.Fprintf(os.Stderr, "⚠️  %v\n", err)
			failed++
		}
	}

	if failed > 0 {
		// O download em si correu bem: os ficheiros existem, só não estão
		// garantidamente em MP4. Não é uma falha total.
		fmt.Fprintf(os.Stderr, "\n⚠️  %d de %d ficheiro(s) não puderam ser convertidos para MP4 e ficaram no formato original.\n"+
			"   Ficam em %s e são tentados outra vez na próxima execução.\n",
			failed, len(paths), pendingFileName)
		return ErrPartial
	}

	if err := os.Remove(pathFile); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("não foi possível limpar %s: %w", pathFile, err)
	}
	return nil
}

// printedPaths lê os caminhos que o yt-dlp escreveu com --print-to-file, um por
// linha e sem repetições (a lista acumula entre execuções). Um ficheiro
// inexistente ou vazio (nada descarregado, tudo já no archive) devolve uma
// lista vazia, não um erro.
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
		// O ficheiro pode ter sobrevivido a uma execução anterior, ou o vídeo
		// pode ter sido removido ou convertido entretanto (.webm → .mp4):
		// confirmar antes de invocar o ffmpeg.
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
