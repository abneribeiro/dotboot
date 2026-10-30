package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"
	"strings"

	"github.com/abneribeiro/dotboot/internal/config"
	"github.com/abneribeiro/dotboot/internal/jobs"
)

const stemsUsageText = `Uso: dotboot stems [flags] URL|FICHEIRO...

Separa uma música nas suas pistas — vozes, bateria, baixo, guitarra… — com uma
pasta por música: <saída>/<título>/<pista>.<formato>

Flags (podem vir antes ou depois dos URLs/ficheiros):
  -o DIR          Pasta de saída (as mesmas regras do download)
  -modelo NOME    6s (default): vozes, bateria, baixo, guitarra, piano e outros
                  4s: vozes, bateria, baixo e outros (mais limpo, sem guitarra)
                  voz: vozes e instrumental, para karaoke (rápido)
                  voz-hq: o mesmo na melhor qualidade (muito lento no CPU)
  -formato FMT    mp3 (default, 320k), flac ou wav
  -so LISTA       Guarda só estas pistas (ex: -so guitarra,baixo)
  -sem LISTA      Gera também uma faixa sem estas pistas, para tocar por cima
                  (ex: -sem guitarra → sem-guitarra.mp3)
  -cookies FICH   Cookies para vídeos que pedem login (default: $DOTBOOT_COOKIES_FILE)
  -start TEMPO    Recorta antes de separar, MM:SS ou HH:MM:SS (exige -end e uma só música)
  -end TEMPO      Fim do recorte

Exemplos:
  dotboot stems https://youtube.com/watch?v=xxxx
  dotboot stems -sem guitarra -o praticar ~/Musica/song.mp3
  dotboot stems -so guitarra -sem guitarra https://youtube.com/watch?v=xxxx
  dotboot stems -modelo voz -so instrumental https://youtube.com/watch?v=xxxx
  dotboot stems -modelo 4s -formato flac -start 01:00 -end 02:30 URL

A separação corre no CPU e demora alguns minutos por música. Cada modelo é
descarregado na primeira utilização para $DOTBOOT_MODELS_DIR.
`

// SplitList interpreta "guitarra, baixo" como [guitarra baixo], sem
// repetições e em minúsculas.
func SplitList(s string) []string {
	var out []string
	for _, item := range strings.Split(s, ",") {
		if item = strings.ToLower(strings.TrimSpace(item)); item != "" && !slices.Contains(out, item) {
			out = append(out, item)
		}
	}
	return out
}

// parseStemsArgs interpreta as flags do subcomando stems e valida o pedido.
func parseStemsArgs(args []string, outputBase string) (*jobs.Stems, error) {
	var in jobs.StemsInput
	var only, without string

	fs := flag.NewFlagSet("stems", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&in.OutputDir, "o", "", "pasta de saída")
	fs.StringVar(&in.Model, "modelo", "", "modelo de separação")
	fs.StringVar(&in.Format, "formato", "", "formato de saída")
	fs.StringVar(&only, "so", "", "pistas a guardar")
	fs.StringVar(&without, "sem", "", "pistas a tirar da faixa de acompanhamento")
	fs.StringVar(&in.Cookies, "cookies", "", "ficheiro de cookies")
	fs.StringVar(&in.Start, "start", "", "início do recorte")
	fs.StringVar(&in.End, "end", "", "fim do recorte")

	inputs, err := parseInterspersed(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil, errHelp
		}
		return nil, err
	}
	in.Inputs = inputs
	in.Only, in.Without = SplitList(only), SplitList(without)
	return jobs.NewStems(in, outputBase)
}

// RunStems executa o subcomando stems.
func RunStems(ctx context.Context, args []string, cfg *config.Config, log *slog.Logger) error {
	s, err := parseStemsArgs(args, cfg.OutputDir)
	if errors.Is(err, errHelp) {
		fmt.Print(stemsUsageText)
		return nil
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n\n%s", err, stemsUsageText)
		return ErrUsage
	}

	log.Debug("a separar stems", "output_dir", s.OutputDir, "modelo", s.Preset.Name, "musicas", len(s.Inputs))
	r := newTextReporter(os.Stdout)
	res, err := s.Run(ctx, jobs.EnvFrom(cfg), r.report)
	r.endLine()
	return finish(res, err, "Pistas guardadas")
}
