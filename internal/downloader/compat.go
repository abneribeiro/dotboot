package downloader

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/abneribeiro/dotboot/internal/procio"
)

// O par que qualquer iPhone, iPad, Android, Windows e macOS reproduz
// nativamente, sem apps extra. Tudo o que saia deste pacote passa por aqui
// antes de chegar ao utilizador.
const (
	mp4Ext        = ".mp4"
	mp4VideoCodec = "h264"
	mp4AudioCodec = "aac"

	// mp4PixFmt é 8 bits 4:2:0. Um AV1/VP9 a 10 bits recodificado sem
	// -pix_fmt daria H.264 High10, que o iPhone também não descodifica —
	// o ficheiro teria os codecs certos e continuaria a não abrir.
	mp4PixFmt = "yuv420p"

	// compatCRF é qualidade-alvo, não tamanho-alvo: não há orçamento de
	// bytes, só o dever de não estragar o vídeo ao converter.
	compatCRF = "20"

	// compatPreset privilegia a velocidade: a qualidade vem do CRF, e o
	// preset só decide quanto tempo se gasta a lá chegar.
	compatPreset = "veryfast"

	// compatProfile fixa o perfil High: o que qualquer iPhone, Android,
	// Windows ou macOS descodifica em hardware. Sem ele, o libx264 escolhe o
	// perfil a partir das opções, e uma combinação estranha podia sair
	// noutro.
	compatProfile = "high"

	// evenDimensions arredonda a largura e a altura para números pares: o
	// libx264 com yuv420p recusa dimensões ímpares, que aparecem em vídeos
	// cortados à mão (TikTok, Instagram), e a conversão falhava.
	evenDimensions = "scale=trunc(iw/2)*2:trunc(ih/2)*2"

	// compatAudioKbps é generoso de propósito: o áudio é uma fração do
	// ficheiro e recodificar Opus→AAC já custa uma geração de qualidade.
	compatAudioKbps = "192k"
)

// mp4Compatible reporta se um ficheiro com estes atributos já toca em qualquer
// telemóvel, sem conversão. Função pura: é aqui que vive a definição de
// "compatível" e é isto que os testes fixam.
//
// Um acodec vazio significa vídeo mudo, que é compatível — não há fluxo de
// áudio para ser incompatível.
func mp4Compatible(ext, vcodec, acodec, pixFmt string) bool {
	if strings.ToLower(ext) != mp4Ext {
		return false
	}
	if !videoCompatible(vcodec, pixFmt) {
		return false
	}
	return audioCompatible(acodec)
}

// videoCompatible aceita H.264 em 8 bits 4:2:0. O yuvj420p é a variante de
// gama completa do mesmo formato — descodifica na mesma, e recodificá-lo só
// perderia qualidade.
func videoCompatible(vcodec, pixFmt string) bool {
	return vcodec == mp4VideoCodec && (pixFmt == mp4PixFmt || pixFmt == "yuvj420p")
}

func audioCompatible(acodec string) bool {
	return acodec == "" || acodec == mp4AudioCodec
}

// compatArgs constrói os argumentos do ffmpeg que põem o ficheiro em conformidade
// copiando tudo o que já está bem: um .webm com H.264/AAC é remuxado em segundos
// em vez de ser recodificado durante minutos. Função pura, como o BuildArgs.
func compatArgs(inputPath, outputPath, vcodec, acodec, pixFmt string) []string {
	// O -map explícito descarta o que o contentor MP4 recusa (legendas WebVTT,
	// fluxos de dados), que de outra forma faria o ffmpeg abortar. O "?" no
	// áudio tolera vídeos mudos.
	args := []string{"-i", inputPath, "-map", "0:v:0", "-map", "0:a:0?"}

	if videoCompatible(vcodec, pixFmt) {
		args = append(args, "-c:v", "copy")
	} else {
		args = append(args,
			"-vf", evenDimensions,
			"-c:v", "libx264",
			"-crf", compatCRF,
			"-preset", compatPreset,
			"-profile:v", compatProfile,
			"-pix_fmt", mp4PixFmt,
		)
	}

	if audioCompatible(acodec) {
		args = append(args, "-c:a", "copy")
	} else {
		args = append(args, "-c:a", "aac", "-b:a", compatAudioKbps)
	}

	// +faststart move o índice para o início: é o que permite a um iPhone
	// começar a ver antes de ter o ficheiro todo.
	return append(args, "-movflags", "+faststart", "-y", outputPath)
}

// moovFirst reporta se o índice de um MP4 (a caixa moov) vem antes dos dados
// (mdat) — o que o +faststart garante e o que deixa o iPhone mostrar o vídeo
// sem o ler todo primeiro (ao pré-visualizar, partilhar ou ver na rede). Lê
// só os cabeçalhos das caixas de topo.
func moovFirst(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	return moovFirstIn(f)
}

// moovFirstIn é o moovFirst sobre um io.ReaderAt, para os testes.
func moovFirstIn(r io.ReaderAt) (bool, error) {
	var offset int64
	header := make([]byte, 16)
	for {
		if _, err := r.ReadAt(header[:8], offset); err != nil {
			return false, fmt.Errorf("MP4 sem moov nem mdat: %w", err)
		}
		size := int64(binary.BigEndian.Uint32(header[:4]))
		switch string(header[4:8]) {
		case "moov":
			return true, nil
		case "mdat":
			return false, nil
		}
		if size == 1 { // tamanho de 64 bits a seguir ao tipo
			if _, err := r.ReadAt(header[8:16], offset+8); err != nil {
				return false, err
			}
			size = int64(binary.BigEndian.Uint64(header[8:16]))
		}
		if size < 8 {
			return false, fmt.Errorf("caixa MP4 inválida em %d", offset)
		}
		offset += size
	}
}

// probeStreams devolve o codec de vídeo, o codec de áudio e o pixel format do
// ficheiro. Campos ausentes (vídeo mudo, ficheiro só de áudio) devolvem "",
// não erro — o ffprobe sai a 0 com output vazio quando o fluxo não existe.
func probeStreams(ctx context.Context, path string) (vcodec, acodec, pixFmt string, err error) {
	vcodec, err = probe(ctx, "-v", "error", "-select_streams", "v:0",
		"-show_entries", "stream=codec_name", "-of", "csv=p=0", path)
	if err != nil {
		return "", "", "", err
	}
	pixFmt, err = probe(ctx, "-v", "error", "-select_streams", "v:0",
		"-show_entries", "stream=pix_fmt", "-of", "csv=p=0", path)
	if err != nil {
		return "", "", "", err
	}
	acodec, err = probe(ctx, "-v", "error", "-select_streams", "a:0",
		"-show_entries", "stream=codec_name", "-of", "csv=p=0", path)
	if err != nil {
		return "", "", "", err
	}
	return vcodec, acodec, pixFmt, nil
}

// ConvertHooks deixa quem chama acompanhar uma conversão do EnsureMP4.
type ConvertHooks struct {
	// Start é chamado antes de converter. reencode é true quando o vídeo vai
	// ser recodificado (lento: minutos) e false num simples remux (segundos).
	Start func(reencode bool, vcodec string)
	// Progress recebe a fração já convertida, entre 0 e 1.
	Progress func(fraction float64)
}

// EnsureMP4 garante que path é um MP4 com vídeo H.264 (8 bits) e áudio AAC.
// Devolve o caminho a usar: o próprio path quando já está conforme, ou o do
// ficheiro convertido, que substitui o original.
//
// É a segunda das duas camadas que garantem o formato. A primeira é pedir
// H.264/AAC ao yt-dlp (ver MP4Sort), que resolve o caso comum sem custo; esta
// apanha o resto — o YouTube acima de 1080p só tem VP9/AV1, e nenhum -S o muda.
func EnsureMP4(ctx context.Context, path string, hooks ConvertHooks) (string, error) {
	vcodec, acodec, pixFmt, err := probeStreams(ctx, path)
	if err != nil {
		return path, fmt.Errorf("não foi possível analisar %s: %w", filepath.Base(path), err)
	}

	// Sem fluxo de vídeo não há nada a garantir, e converter produziria um MP4
	// vazio. Devolve-se o ficheiro tal como está.
	if vcodec == "" {
		slog.Debug("ficheiro sem fluxo de vídeo, deixado como está", "path", path)
		return path, nil
	}

	ext := filepath.Ext(path)
	if mp4Compatible(ext, vcodec, acodec, pixFmt) {
		// Os codecs estão certos, mas o índice pode estar no fim: acontece
		// com os recortes, que o próprio yt-dlp recodifica para H.264. Nesse
		// caso faz-se um remux só para o mover para o início.
		fast, err := moovFirst(path)
		if err != nil || fast {
			slog.Debug("ficheiro já é MP4 compatível", "path", path, "vcodec", vcodec, "acodec", acodec)
			return path, nil
		}
		slog.Debug("MP4 compatível com o índice no fim: remux com +faststart", "path", path)
	}

	reencoding := !videoCompatible(vcodec, pixFmt)
	slog.Debug("a converter para MP4 compatível",
		"path", path, "vcodec", vcodec, "acodec", acodec, "pix_fmt", pixFmt, "reencode", reencoding)
	if hooks.Start != nil {
		hooks.Start(reencoding, vcodec)
	}

	base := strings.TrimSuffix(path, ext)
	finalPath := base + mp4Ext
	// Escrever primeiro num nome temporário: uma interrupção a meio deixa o
	// ficheiro original intacto em vez de um MP4 truncado.
	tempPath := base + ".dotboot-compat" + mp4Ext

	args := compatArgs(path, tempPath, vcodec, acodec, pixFmt)
	var duration float64
	if hooks.Progress != nil {
		// Sem a duração não há fração: converte-se na mesma, só sem barra.
		if duration, err = ProbeDuration(ctx, path); err == nil {
			args = append([]string{"-progress", "pipe:1", "-nostats"}, args...)
		}
	}

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 10 * time.Second
	stderr := &procio.TailBuffer{Max: 4096}
	cmd.Stderr = stderr
	stdout := procio.NewLineWriter(func(line string) {
		if f, ok := FFmpegFraction(line, duration); ok {
			hooks.Progress(f)
		}
	})
	cmd.Stdout = stdout

	err = cmd.Run()
	stdout.Flush()
	if err != nil {
		os.Remove(tempPath)
		slog.Debug("conversão para MP4 falhou", "path", path, "err", err, "output", stderr.String())
		return path, fmt.Errorf("falhou ao converter %s para MP4: %w", filepath.Base(path), err)
	}

	// O rename sobre o destino é atómico: nunca há um instante em que não
	// exista um ficheiro completo.
	if err := os.Rename(tempPath, finalPath); err != nil {
		os.Remove(tempPath)
		return path, fmt.Errorf("falhou ao substituir %s: %w", filepath.Base(path), err)
	}
	// Quando a extensão mudou (.webm/.mkv → .mp4) o original ainda existe.
	if finalPath != path {
		os.Remove(path)
	}

	slog.Debug("conversão para MP4 concluída", "path", finalPath)
	return finalPath, nil
}
