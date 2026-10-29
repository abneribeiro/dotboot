package downloader

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Phase é a etapa em que o yt-dlp está.
type Phase int

const (
	// PhaseInfo: a obter a informação do vídeo, antes de descarregar.
	PhaseInfo Phase = iota
	// PhaseDownload: a descarregar um ficheiro (vídeo, áudio ou formato).
	PhaseDownload
	// PhaseMerge: a juntar vídeo e áudio com o ffmpeg.
	PhaseMerge
	// PhaseExtract: a extrair/converter o áudio (modo áudio, stems).
	PhaseExtract
)

// Progress é um evento do yt-dlp: uma linha de progresso de download ou uma
// mudança de fase.
type Progress struct {
	Phase Phase
	// Downloaded e Total em bytes; Total 0 quando o site não o diz.
	Downloaded int64
	Total      int64
	// Speed em bytes/s (0 = desconhecida) e ETA (-1 = desconhecido).
	Speed float64
	ETA   time.Duration
	// File é o ficheiro em que o yt-dlp está a trabalhar, quando a linha o diz.
	File string
	// Index e Count: posição numa playlist (0 fora de playlists).
	Index int
	Count int
}

// Fraction devolve o progresso entre 0 e 1, ou -1 quando o total é desconhecido.
func (p Progress) Fraction() float64 {
	if p.Total <= 0 {
		return -1
	}
	return min(float64(p.Downloaded)/float64(p.Total), 1)
}

// progressMarker identifica as linhas do --progress-template, para não as
// confundir com o resto do output.
const progressMarker = "dotboot-progress"

// progressTemplate faz o yt-dlp escrever uma linha fácil de interpretar a cada
// atualização: bytes, total, total estimado, velocidade e ETA ("NA" quando
// não sabe).
const progressTemplate = "download:" + progressMarker +
	" %(progress.downloaded_bytes)s %(progress.total_bytes)s %(progress.total_bytes_estimate)s" +
	" %(progress.speed)s %(progress.eta)s"

var (
	destinationLine = regexp.MustCompile(`^\[download\] Destination: (.+)$`)
	alreadyLine     = regexp.MustCompile(`^\[download\] (.+) has already been (?:downloaded|recorded in the archive)`)
	itemLine        = regexp.MustCompile(`^\[download\] Downloading item (\d+) of (\d+)`)
	mergeLine       = regexp.MustCompile(`^\[Merger\] Merging formats into "(.+)"$`)
	extractLine     = regexp.MustCompile(`^\[ExtractAudio\] Destination: (.+)$`)
)

// ParseLine interpreta uma linha do stdout do yt-dlp. Devolve ok=false para
// linhas que não são progresso nem mudança de fase.
func ParseLine(line string) (Progress, bool) {
	line = strings.TrimSpace(line)

	if rest, ok := strings.CutPrefix(line, progressMarker+" "); ok {
		f := strings.Fields(rest)
		if len(f) != 5 {
			return Progress{}, false
		}
		p := Progress{Phase: PhaseDownload, ETA: -1}
		p.Downloaded = int64(parseNum(f[0]))
		p.Total = int64(parseNum(f[1]))
		if p.Total <= 0 {
			// Os sites com streams fragmentados (HLS/DASH) só dão estimativa.
			p.Total = int64(parseNum(f[2]))
		}
		p.Speed = parseNum(f[3])
		if eta := parseNum(f[4]); eta >= 0 && f[4] != "NA" {
			p.ETA = time.Duration(eta * float64(time.Second))
		}
		return p, true
	}

	if m := destinationLine.FindStringSubmatch(line); m != nil {
		return Progress{Phase: PhaseDownload, File: m[1], ETA: -1}, true
	}
	if m := alreadyLine.FindStringSubmatch(line); m != nil {
		return Progress{Phase: PhaseDownload, File: m[1], ETA: -1}, true
	}
	if m := itemLine.FindStringSubmatch(line); m != nil {
		index, _ := strconv.Atoi(m[1])
		count, _ := strconv.Atoi(m[2])
		return Progress{Phase: PhaseInfo, Index: index, Count: count, ETA: -1}, true
	}
	if m := mergeLine.FindStringSubmatch(line); m != nil {
		return Progress{Phase: PhaseMerge, File: m[1], ETA: -1}, true
	}
	if m := extractLine.FindStringSubmatch(line); m != nil {
		return Progress{Phase: PhaseExtract, File: m[1], ETA: -1}, true
	}
	return Progress{}, false
}

// parseNum lê um número do template; "NA" ou lixo dá 0.
func parseNum(s string) float64 {
	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return n
}
