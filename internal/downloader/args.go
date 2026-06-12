package downloader

import (
	"fmt"
	"io"
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

	// NoPlaylist impede o download de playlists inteiras quando o link
	// aponta para um vídeo dentro de uma playlist (sempre no bot;
	// no CLI apenas quando há recorte).
	NoPlaylist bool

	// Section adiciona --download-sections com --force-keyframes-at-cuts.
	Section *Section

	// MergeFormat força o contentor de saída ("mp4" no bot; vazio no CLI).
	MergeFormat string

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

// BuildArgs constrói a lista de argumentos do yt-dlp a partir das opções.
// É uma função pura para ser testável sem executar o yt-dlp.
func BuildArgs(o Options) []string {
	var args []string

	if o.Format != "" {
		args = append(args, "-f", o.Format)
	}
	if o.MergeFormat != "" {
		args = append(args, "--merge-output-format", o.MergeFormat)
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

	args = append(args, o.URLs...)
	return args
}
