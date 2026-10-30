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

	// FormatSort é o valor de -S/--format-sort (ver QualitySort e MP4Sort);
	// vazio omite a flag.
	FormatSort string

	// ExtractAudio, quando definido (ex: "mp3", "m4a", "wav"), guarda só o
	// áudio nesse formato (-x --audio-format). Sem vídeo, o MergeFormat e a
	// garantia de MP4 não se aplicam.
	ExtractAudio string

	// EmbedMetadata embute título, artista e capa no ficheiro (CLI em modo
	// áudio): é o que faz um MP3 aparecer com nome e capa no telemóvel.
	EmbedMetadata bool

	// CookiesFile passa --cookies: um ficheiro em formato Netscape com a
	// sessão de um browser, que resolve a verificação "não és um robô" do
	// YouTube, a restrição de idade e o conteúdo que exige login.
	CookiesFile string

	// JSRuntimes passa --js-runtimes (ex: "node"). Vazio deixa o default do
	// yt-dlp, o Deno — o runtime que resolve os desafios JavaScript com que o
	// YouTube protege os formatos.
	JSRuntimes string

	// NoPlaylist impede o download de playlists inteiras quando o link
	// aponta para um vídeo dentro de uma playlist (sempre no bot;
	// no CLI apenas quando há recorte).
	NoPlaylist bool

	// Section adiciona --download-sections com --force-keyframes-at-cuts.
	Section *Section

	// MergeFormat força o contentor de saída ("mp4" no bot; vazio no CLI).
	MergeFormat string

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

	// Progress, quando definido, recebe o progresso e as mudanças de fase do
	// yt-dlp (ver ParseLine); BuildArgs acrescenta o --progress-template que
	// as produz. As linhas de progresso não chegam ao Stdout.
	Progress func(Progress)

	// Stdout/Stderr recebem o output do yt-dlp em direto (podem ser nulos).
	Stdout io.Writer
	Stderr io.Writer
}

// QualitySort traduz uma qualidade pedida no CLI no -S do yt-dlp. "" ou
// "best" devolvem vazio (o default do yt-dlp já é a melhor qualidade). Um
// inteiro (ex: "1080", "720") devolve "res:N", que já é um limite com
// fallback: o yt-dlp escolhe a maior resolução que não passe de N e, só se
// não houver nenhuma, a mais pequena acima.
//
// A "res" do yt-dlp é o lado MENOR do vídeo, e é por isso que não há um
// filtro -f [height<=N]: num vídeo vertical (Shorts, TikTok, Reels) 720p é
// 720x1280, e filtrar pela altura deitava-o fora e entregava 360x640. Sites
// que não expõem a resolução (ex: Instagram) continuam a funcionar — sem o
// valor, o critério apenas não desempata.
func QualitySort(q string) (string, error) {
	if q == "" || q == "best" {
		return "", nil
	}
	n, err := strconv.Atoi(q)
	if err != nil || n < 1 {
		return "", fmt.Errorf("qualidade inválida: %q (usa \"best\" ou uma resolução, ex: 1080, 720)", q)
	}
	return fmt.Sprintf("res:%d", n), nil
}

// mp4CodecSort faz o yt-dlp preferir SDR e H.264 + AAC — o único par que
// iPhones e Androids reproduzem nativamente. O vcodec e o acodec são os
// campos do preset oficial "-t mp4" do yt-dlp. O hdr:sdr vem primeiro
// porque o yt-dlp, por omissão, prefere HDR (hdr:12): um VP9 HDR convertido
// para H.264 de 8 bits fica com as cores lavadas, sem a informação de HDR
// que o leitor precisava para as mostrar.
const mp4CodecSort = "hdr:sdr,vcodec:h264,acodec:aac"

// MP4Sort junta a preferência de codecs a uma ordenação de resolução.
// resSort vazio significa "a maior resolução disponível".
//
// A resolução vem SEMPRE primeiro: pedir -q 2160 tem de dar 2160p mesmo que
// essa altura só exista em AV1 (é o EnsureMP4 que converte depois). Com o
// vcodec à frente, um H.264 a 480p ganharia a um AV1 a 2160p e o -q seria
// silenciosamente ignorado.
//
// Como critério de desempate, porém, o vcodec vale ouro: entre dois formatos
// da mesma altura escolhe o H.264, que dispensa qualquer recodificação. É isso
// que faz o caminho comum (YouTube até 1080p, Instagram, TikTok, X) sair em
// MP4 sem custo nenhum de CPU.
func MP4Sort(resSort string) string {
	if resSort == "" {
		resSort = "res"
	}
	return resSort + "," + mp4CodecSort
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
	if o.ExtractAudio != "" {
		// --audio-quality 0 é o melhor VBR do codec; num WAV não tem efeito.
		args = append(args, "-x", "--audio-format", o.ExtractAudio, "--audio-quality", "0")
	}
	if o.EmbedMetadata {
		args = append(args, "--embed-metadata", "--embed-thumbnail")
	}
	if o.CookiesFile != "" {
		args = append(args, "--cookies", o.CookiesFile)
	}
	if o.JSRuntimes != "" {
		args = append(args, "--js-runtimes", o.JSRuntimes)
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
	// --newline: uma linha por atualização de progresso, em vez de "\r",
	// legível em logs e pelo ParseLine.
	args = append(args, "--newline")
	if o.Progress != nil {
		args = append(args, "--progress-template", progressTemplate)
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
