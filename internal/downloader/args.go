package downloader

import (
	"fmt"
	"io"
	"strconv"
)

// Section define um recorte de vídeo com tempos de início e fim nos formatos
// MM:SS ou HH:MM:SS, já validados pelo chamador (ver ValidateTime).
type Section struct {
	Start string
	End   string
}

// Options descreve uma invocação do yt-dlp. O bot e o modo CLI constroem
// combinações diferentes destas opções (ver BuildArgs e os testes).
type Options struct {
	// URLs são os links a descarregar, passados como últimos argumentos.
	URLs []string

	// BatchFile é um ficheiro com um URL por linha (--batch-file, modo CLI).
	BatchFile string

	// OutputTemplate é o valor completo de -o, construído pelo chamador.
	OutputTemplate string

	// Format é o valor de -f; vazio omite a flag (default do yt-dlp = melhor qualidade).
	Format string

	// FormatSort é o valor de -S/--format-sort (ver SortForQuality);
	// vazio omite a flag.
	FormatSort string

	// NoPlaylist impede o download de playlists inteiras quando o link
	// aponta para um vídeo dentro de uma playlist (sempre no bot;
	// no CLI apenas quando há recorte).
	NoPlaylist bool

	// Section adiciona --download-sections com --force-keyframes-at-cuts.
	Section *Section

	// MergeFormat força o contentor de saída ("mp4" no bot; vazio no CLI).
	MergeFormat string

	// MaxFileSize, quando > 0, adiciona --max-filesize: o yt-dlp recusa o
	// formato antes de descarregar quando já conhece o tamanho. É um travão
	// antecipado, não substitui a verificação pós-download.
	MaxFileSize int64

	// RestrictFilenames limita os nomes de saída a ASCII (bot), para que o
	// nome do anexo enviado ao Discord seja sempre seguro.
	RestrictFilenames bool

	// PrintToFile, quando definido, faz o yt-dlp escrever o caminho final do
	// ficheiro nesse ficheiro (--print-to-file after_move:filepath). Implica
	// --no-simulate: sem ele o --print-to-file cancelaria o download.
	PrintToFile string

	// ArchiveFile ativa --download-archive: vídeos já registados são
	// ignorados em execuções seguintes (semântica de "continuar" do CLI).
	ArchiveFile string

	// Continue retoma ficheiros .part deixados por execuções interrompidas.
	Continue bool

	// IgnoreErrors continua a playlist mesmo quando um vídeo falha.
	IgnoreErrors bool

	// Quiet ativa --no-warnings --quiet (bot). Quando falso, é adicionado
	// --newline para progresso legível em terminais e logs (CLI).
	Quiet bool

	// Stdout/Stderr, quando não nulos, recebem o output do yt-dlp em direto
	// (CLI). Quando nulos, o output é capturado para classificação de erros (bot).
	Stdout io.Writer
	Stderr io.Writer
}

// QualitySelectors traduz uma qualidade pedida no CLI nos valores de -f e -S
// do yt-dlp. "" ou "best" devolvem ambos vazios (o default do yt-dlp já é a
// melhor qualidade). Um inteiro (ex: "1080", "720") devolve um filtro que é um
// limite real de altura, mais a ordenação correspondente.
//
// O "?" em [height<=?N] é o que torna o limite seguro: sites que não expõem a
// altura dos formatos (ex: Instagram) mantêm-se elegíveis em vez de fazerem o
// filtro falhar. O -S resolve o desempate entre os que sobram, e a última
// alternativa (bv*+ba/b) garante que há sempre um formato quando nenhum
// cumpre o limite.
func QualitySelectors(q string) (format, sort string, err error) {
	if q == "" || q == "best" {
		return "", "", nil
	}
	n, err := strconv.Atoi(q)
	if err != nil || n < 1 {
		return "", "", fmt.Errorf("qualidade inválida: %q (usa \"best\" ou uma altura em pixels, ex: 1080, 720)", q)
	}
	format = fmt.Sprintf("bv*[height<=?%d]+ba/b[height<=?%d]/bv*+ba/b", n, n)
	return format, fmt.Sprintf("res:%d", n), nil
}

// BuildArgs constrói a lista de argumentos do yt-dlp a partir das opções.
// É uma função pura para ser testável sem executar o yt-dlp.
func BuildArgs(o Options) []string {
	var args []string

	if o.Format != "" {
		args = append(args, "-f", o.Format)
	}
	if o.FormatSort != "" {
		args = append(args, "-S", o.FormatSort)
	}
	if o.MergeFormat != "" {
		args = append(args, "--merge-output-format", o.MergeFormat)
	}
	if o.MaxFileSize > 0 {
		args = append(args, "--max-filesize", strconv.FormatInt(o.MaxFileSize, 10))
	}
	if o.RestrictFilenames {
		args = append(args, "--restrict-filenames")
	}
	if o.NoPlaylist {
		args = append(args, "--no-playlist")
	}
	if o.OutputTemplate != "" {
		args = append(args, "-o", o.OutputTemplate)
	}
	if o.ArchiveFile != "" {
		args = append(args, "--download-archive", o.ArchiveFile)
	}
	if o.Continue {
		args = append(args, "--continue")
	}
	if o.IgnoreErrors {
		args = append(args, "--ignore-errors")
	}
	if o.Quiet {
		args = append(args, "--no-warnings", "--quiet")
	} else {
		args = append(args, "--newline")
	}
	if o.BatchFile != "" {
		args = append(args, "--batch-file", o.BatchFile)
	}
	if o.Section != nil {
		section := fmt.Sprintf("*%s-%s", o.Section.Start, o.Section.End)
		args = append(args, "--download-sections", section, "--force-keyframes-at-cuts")
	}
	// --print-to-file implica --simulate; sem --no-simulate o yt-dlp
	// escreveria o caminho mas não descarregaria nada.
	if o.PrintToFile != "" {
		args = append(args, "--print-to-file", "after_move:filepath", o.PrintToFile, "--no-simulate")
	}

	args = append(args, o.URLs...)
	return args
}
