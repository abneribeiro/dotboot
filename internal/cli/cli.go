// Package cli implementa o modo terminal: downloads sem os limites do
// Discord (sem cap de tamanho, sem compressão, melhor qualidade), com
// suporte a playlists/canais, vários URLs e ficheiros de URLs, e com
// semântica de "continuar" via --download-archive e --continue.
package cli

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"dotboot/internal/config"
	"dotboot/internal/downloader"
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
  -start TEMPO  Início do recorte, MM:SS ou HH:MM:SS (exige -end e um único URL)
  -end TEMPO    Fim do recorte

Exemplos:
  dotboot download https://youtube.com/watch?v=xxxx
  dotboot download -o ~/Videos https://youtube.com/playlist?list=yyyy
  dotboot download -a urls.txt -o ~/Videos
  dotboot download -start 01:30 -end 02:00 https://x.com/user/status/123
`

// options é o resultado validado do parsing dos argumentos.
type options struct {
	outputDir string
	batchFile string
	start     string
	end       string
	urls      []string
}

// parseArgs interpreta e valida os argumentos do subcomando download.
// defaultOutputDir (tipicamente cfg.OutputDir, vindo de DOTBOOT_OUTPUT_DIR)
// é o caminho base: usado quando -o não é passado, e raiz de um -o relativo.
// Um -o absoluto ignora a base; sem base, -o relativo refere o diretório
// atual e a ausência de -o cai para o diretório atual.
func parseArgs(args []string, defaultOutputDir string) (*options, error) {
	o := &options{}

	fs := flag.NewFlagSet("download", flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprint(fs.Output(), usageText) }
	fs.StringVar(&o.outputDir, "o", "", "diretório de saída")
	fs.StringVar(&o.batchFile, "a", "", "ficheiro com um URL por linha")
	fs.StringVar(&o.start, "start", "", "tempo de início do recorte")
	fs.StringVar(&o.end, "end", "", "tempo de fim do recorte")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	o.urls = fs.Args()

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
		if err := downloader.ValidateTime(o.start); err != nil {
			return nil, fmt.Errorf("-start: %w", err)
		}
		if err := downloader.ValidateTime(o.end); err != nil {
			return nil, fmt.Errorf("-end: %w", err)
		}
	}

	for _, u := range o.urls {
		if err := downloader.ValidateURL(u); err != nil {
			return nil, err
		}
	}

	return o, nil
}

// Run executa o subcomando download. Não impõe timeout — playlists grandes
// podem demorar horas; CTRL+C cancela o contexto, o yt-dlp recebe SIGINT e
// os ficheiros .part ficam prontos para retomar na execução seguinte.
func Run(ctx context.Context, args []string, cfg *config.Config, log *slog.Logger) error {
	o, err := parseArgs(args, cfg.OutputDir)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(o.outputDir, 0o755); err != nil {
		return fmt.Errorf("não foi possível criar o diretório de saída %s: %w", o.outputDir, err)
	}

	opts := downloader.Options{
		URLs:           o.urls,
		BatchFile:      o.batchFile,
		OutputTemplate: filepath.Join(o.outputDir, "%(title)s [%(id)s].%(ext)s"),
		Continue:       true,
		IgnoreErrors:   true,
		Stdout:         os.Stdout,
		Stderr:         os.Stderr,
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

	if err := downloader.Run(ctx, opts); err != nil {
		return fmt.Errorf("download falhou: %w", err)
	}

	fmt.Printf("\n✅ Downloads concluídos em %s\n", o.outputDir)
	return nil
}
