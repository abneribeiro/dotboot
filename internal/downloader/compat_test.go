package downloader

import (
	"bytes"
	"encoding/binary"
	"slices"
	"strings"
	"testing"
)

func TestMP4Compatible(t *testing.T) {
	tests := []struct {
		name   string
		ext    string
		vcodec string
		acodec string
		pixFmt string
		want   bool
	}{
		{"o alvo: mp4 h264 aac 8 bits", ".mp4", "h264", "aac", "yuv420p", true},
		{"gama completa também toca", ".mp4", "h264", "aac", "yuvj420p", true},
		{"vídeo mudo é compatível", ".mp4", "h264", "", "yuv420p", true},
		{"extensão em maiúsculas", ".MP4", "h264", "aac", "yuv420p", true},

		{"webm com codecs certos: só o contentor está mal", ".webm", "h264", "aac", "yuv420p", false},
		{"mkv do merge incompatível", ".mkv", "h264", "aac", "yuv420p", false},
		{"o caso do YouTube: av1 + opus dentro de mp4", ".mp4", "av1", "opus", "yuv420p", false},
		{"vp9 dentro de mp4", ".mp4", "vp9", "aac", "yuv420p", false},
		{"h264 mas áudio opus", ".mp4", "h264", "opus", "yuv420p", false},
		{"h264 a 10 bits: o iPhone não descodifica High10", ".mp4", "h264", "aac", "yuv420p10le", false},
		{"hevc não é universal", ".mp4", "hevc", "aac", "yuv420p", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mp4Compatible(tt.ext, tt.vcodec, tt.acodec, tt.pixFmt)
			if got != tt.want {
				t.Errorf("mp4Compatible(%q, %q, %q, %q) = %v, esperado %v",
					tt.ext, tt.vcodec, tt.acodec, tt.pixFmt, got, tt.want)
			}
		})
	}
}

// Um remux (tudo copiado) é a diferença entre segundos e minutos: é o caminho
// que apanha um .webm que já traz H.264/AAC.
func TestCompatArgsRemuxaSemRecodificar(t *testing.T) {
	got := compatArgs("/tmp/v.webm", "/tmp/v.mp4", "h264", "aac", "yuv420p")

	want := []string{
		"-i", "/tmp/v.webm",
		"-map", "0:v:0", "-map", "0:a:0?",
		"-c:v", "copy",
		"-c:a", "copy",
		"-movflags", "+faststart",
		"-y", "/tmp/v.mp4",
	}
	if !slices.Equal(got, want) {
		t.Errorf("compatArgs (remux) =\n%v\nesperado\n%v", got, want)
	}
}

func TestCompatArgsRecodificaSoOFluxoErrado(t *testing.T) {
	tests := []struct {
		name       string
		vcodec     string
		acodec     string
		pixFmt     string
		wantVideo  string
		wantAudio  string
		wantPixFmt bool
	}{
		{"só o áudio", "h264", "opus", "yuv420p", "copy", "aac", false},
		{"só o vídeo", "av1", "aac", "yuv420p", "libx264", "copy", true},
		{"os dois", "vp9", "opus", "yuv420p", "libx264", "aac", true},
		{"h264 a 10 bits obriga a recodificar", "h264", "aac", "yuv420p10le", "libx264", "copy", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := compatArgs("/tmp/in", "/tmp/out.mp4", tt.vcodec, tt.acodec, tt.pixFmt)

			if v := argValue(got, "-c:v"); v != tt.wantVideo {
				t.Errorf("-c:v = %q, esperado %q", v, tt.wantVideo)
			}
			if a := argValue(got, "-c:a"); a != tt.wantAudio {
				t.Errorf("-c:a = %q, esperado %q", a, tt.wantAudio)
			}
			// Sem -pix_fmt, uma fonte a 10 bits produziria H.264 High10 —
			// codecs certos e mesmo assim ilegível no iPhone.
			if hasPixFmt := slices.Contains(got, "-pix_fmt"); hasPixFmt != tt.wantPixFmt {
				t.Errorf("-pix_fmt presente = %v, esperado %v: %v", hasPixFmt, tt.wantPixFmt, got)
			}
			if tt.wantPixFmt && argValue(got, "-pix_fmt") != mp4PixFmt {
				t.Errorf("-pix_fmt = %q, esperado %q", argValue(got, "-pix_fmt"), mp4PixFmt)
			}
			// Ao recodificar, as dimensões ficam pares (o libx264 recusa as
			// ímpares em yuv420p) e o perfil é o High, que todos os leitores
			// descodificam. Num remux nada disto se aplica.
			reencode := tt.wantVideo == "libx264"
			if (argValue(got, "-vf") == evenDimensions) != reencode {
				t.Errorf("-vf = %q, recodifica = %v: %v", argValue(got, "-vf"), reencode, got)
			}
			if (argValue(got, "-profile:v") == compatProfile) != reencode {
				t.Errorf("-profile:v = %q, recodifica = %v", argValue(got, "-profile:v"), reencode)
			}
		})
	}
}

// O +faststart é o que deixa o vídeo começar a tocar antes de estar todo
// transferido — no iPhone e no Safari é a diferença entre ver e esperar.
func TestCompatArgsIncluiSempreFaststart(t *testing.T) {
	for _, vcodec := range []string{"h264", "av1", "vp9"} {
		got := compatArgs("/tmp/in", "/tmp/out.mp4", vcodec, "aac", "yuv420p")
		if argValue(got, "-movflags") != "+faststart" {
			t.Errorf("vcodec %q: faltou +faststart em %v", vcodec, got)
		}
		if got[len(got)-1] != "/tmp/out.mp4" {
			t.Errorf("vcodec %q: o output tem de ser o último argumento: %v", vcodec, got)
		}
	}
}

// argValue devolve o valor que se segue a uma flag, ou "" se não existir.
func argValue(args []string, flag string) string {
	i := slices.Index(args, flag)
	if i < 0 || i+1 >= len(args) {
		return ""
	}
	return args[i+1]
}

// Um ficheiro só de áudio não tem nada a garantir: converter produziria um MP4
// sem vídeo. Documenta-se aqui porque a decisão vive no EnsureMP4.
func TestAudioCompatibleAceitaAusenciaDeAudio(t *testing.T) {
	if !audioCompatible("") {
		t.Error("um vídeo mudo não devia obrigar a recodificar")
	}
	if audioCompatible("opus") {
		t.Error("opus não é compatível com MP4 em iOS")
	}
}

// A extensão é comparada sem distinguir maiúsculas; garantir que não é por
// acaso de um caso de teste.
func TestMP4CompatibleIgnoraCaixaDaExtensao(t *testing.T) {
	for _, ext := range []string{".mp4", ".Mp4", ".MP4"} {
		if !mp4Compatible(ext, "h264", "aac", "yuv420p") {
			t.Errorf("extensão %q devia ser aceite", ext)
		}
	}
	if mp4Compatible(strings.ToUpper(".webm"), "h264", "aac", "yuv420p") {
		t.Error(".WEBM não é mp4")
	}
}

// box escreve uma caixa MP4 com o tipo e o conteúdo dados.
func box(typ string, payload int) []byte {
	b := make([]byte, 8+payload)
	binary.BigEndian.PutUint32(b, uint32(8+payload))
	copy(b[4:8], typ)
	return b
}

func TestMoovFirst(t *testing.T) {
	large := make([]byte, 16) // mdat com tamanho de 64 bits
	binary.BigEndian.PutUint32(large, 1)
	copy(large[4:8], "mdat")
	binary.BigEndian.PutUint64(large[8:], 16)

	tests := []struct {
		name    string
		file    [][]byte
		want    bool
		wantErr bool
	}{
		{"faststart", [][]byte{box("ftyp", 24), box("moov", 100), box("mdat", 500)}, true, false},
		{"índice no fim", [][]byte{box("ftyp", 24), box("free", 0), box("mdat", 500), box("moov", 100)}, false, false},
		{"mdat de 64 bits", [][]byte{box("ftyp", 24), large, box("moov", 10)}, false, false},
		{"sem moov nem mdat", [][]byte{box("ftyp", 24)}, false, true},
		{"caixa inválida", [][]byte{box("ftyp", 24), {0, 0, 0, 2, 'x', 'x', 'x', 'x'}}, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := moovFirstIn(bytes.NewReader(bytes.Join(tt.file, nil)))
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Errorf("moovFirstIn = %v, %v; quero %v (erro: %v)", got, err, tt.want, tt.wantErr)
			}
		})
	}
}
