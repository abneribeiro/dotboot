package downloader

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// O par que qualquer iPhone, iPad ou Android reproduz nativamente, e o único
// que o Discord consegue mostrar inline em todas as plataformas. Tudo o que
// saia deste pacote passa por aqui antes de chegar ao utilizador.
const (
	mp4Ext        = ".mp4"
	mp4VideoCodec = "h264"
	mp4AudioCodec = "aac"

	// mp4PixFmt é 8 bits 4:2:0. Um AV1/VP9 a 10 bits recodificado sem
	// -pix_fmt daria H.264 High10, que o iPhone também não descodifica —
	// o ficheiro teria os codecs certos e continuaria a não abrir.
	mp4PixFmt = "yuv420p"

	// compatCRF é qualidade-alvo, não tamanho-alvo: ao contrário da
	// compressão para o Discord (compress.go), aqui não há orçamento de
	// bytes, só o dever de não estragar o vídeo ao converter.
	compatCRF = "20"

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
			"-c:v", "libx264",
			"-crf", compatCRF,
			"-preset", compressPreset,
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

// EnsureMP4 garante que path é um MP4 com vídeo H.264 (8 bits) e áudio AAC.
// Devolve o caminho a usar: o próprio path quando já está conforme, ou o do
// ficheiro convertido, que substitui o original.
//
// É a segunda das duas camadas que garantem o formato. A primeira é pedir
// H.264/AAC ao yt-dlp (ver MP4Sort), que resolve o caso comum sem custo; esta
// apanha o resto — o YouTube acima de 1080p só tem VP9/AV1, e nenhum -S o muda.
//
// progress, quando não nulo, recebe um aviso antes de uma recodificação, que ao
// contrário do remux pode demorar muitos minutos.
func EnsureMP4(ctx context.Context, path string, progress io.Writer) (string, error) {
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
		slog.Debug("ficheiro já é MP4 compatível", "path", path, "vcodec", vcodec, "acodec", acodec)
		return path, nil
	}

	reencoding := !videoCompatible(vcodec, pixFmt)
	slog.Info("a converter para MP4 compatível",
		"path", path, "vcodec", vcodec, "acodec", acodec, "pix_fmt", pixFmt, "reencode", reencoding)

	if progress != nil {
		if reencoding {
			fmt.Fprintf(progress, "🔄 %s está em %s; a converter para H.264 (pode demorar vários minutos).\n"+
				"   Usa -formato original para guardar o ficheiro tal como vem.\n",
				filepath.Base(path), vcodec)
		} else {
			fmt.Fprintf(progress, "🔄 %s: a converter para MP4 (sem perda de qualidade).\n", filepath.Base(path))
		}
	}

	base := strings.TrimSuffix(path, ext)
	finalPath := base + mp4Ext
	// Escrever primeiro num nome temporário: uma interrupção a meio deixa o
	// ficheiro original intacto em vez de um MP4 truncado.
	tempPath := base + ".dotboot-compat" + mp4Ext

	args := compatArgs(path, tempPath, vcodec, acodec, pixFmt)
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 10 * time.Second

	if output, err := cmd.CombinedOutput(); err != nil {
		os.Remove(tempPath)
		slog.Error("conversão para MP4 falhou", "path", path, "err", err, "output", tail(string(output), 2048))
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

	slog.Info("conversão para MP4 concluída", "path", finalPath)
	return finalPath, nil
}
