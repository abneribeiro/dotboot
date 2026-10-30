package tui

import (
	"errors"
	"os"
	"strings"

	"charm.land/huh/v2"

	"github.com/abneribeiro/dotboot/internal/config"
	"github.com/abneribeiro/dotboot/internal/downloader"
	"github.com/abneribeiro/dotboot/internal/jobs"
)

// formKind identifica o formulário aberto.
type formKind int

const (
	formVideo formKind = iota
	formAudio
	formSettings
)

func (k formKind) title() string {
	switch k {
	case formAudio:
		return "🎵 Extrair áudio"
	case formSettings:
		return "⚙️  Definições"
	default:
		return "🎬 Descarregar vídeo"
	}
}

// Valores dos formulários. Vivem no modelo, e não no formulário, para que um
// erro de validação no fim volte a abrir o formulário já preenchido.
type videoValues struct {
	urls, quality, format, output string
	trim                          bool
	start, end                    string
}

type audioValues struct {
	urls, format, output string
}

type settingsValues struct {
	output, cookies string
}

// splitLines devolve as linhas não vazias de um campo de texto.
func splitLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}

func validateURLs(s string) error {
	lines := splitLines(s)
	if len(lines) == 0 {
		return errors.New("indica pelo menos um URL")
	}
	for _, l := range lines {
		if err := downloader.ValidateURL(l); err != nil {
			return err
		}
	}
	return nil
}

func validateOptionalTime(s string) error {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return downloader.ValidateTime(strings.TrimSpace(s))
}

// outputDescription explica onde vão parar os ficheiros com a pasta vazia.
func outputDescription(base string) string {
	if base == "" {
		wd, _ := os.Getwd()
		return "Vazio = pasta atual (" + wd + ")."
	}
	return "Vazio = " + base + ". Um nome relativo fica dentro dela."
}

func newVideoForm(v *videoValues, base string) *huh.Form {
	return huh.NewForm(
		huh.NewGroup(
			huh.NewText().Title("URLs").
				Description("Um por linha. Vídeos, playlists ou canais inteiros.").
				Lines(4).Value(&v.urls).Validate(validateURLs),
			huh.NewSelect[string]().Title("Qualidade").Options(
				huh.NewOption("Melhor disponível", "best"),
				huh.NewOption("2160p (4K) — converte para H.264, demora", "2160"),
				huh.NewOption("1440p — converte para H.264, demora", "1440"),
				huh.NewOption("1080p — a melhor sem conversão no YouTube", "1080"),
				huh.NewOption("720p", "720"),
				huh.NewOption("480p", "480"),
			).Value(&v.quality),
			huh.NewSelect[string]().Title("Formato").Options(
				huh.NewOption("MP4 — toca no iPhone, Android e Discord", jobs.FormatMP4),
				huh.NewOption("Original — tal como o site serve (.webm/.mkv)", jobs.FormatOriginal),
			).Value(&v.format),
			huh.NewInput().Title("Pasta de saída").Description(outputDescription(base)).Value(&v.output),
			huh.NewConfirm().Title("Fazer um recorte?").Affirmative("Sim").Negative("Não").Value(&v.trim),
		),
		huh.NewGroup(
			huh.NewInput().Title("Início").Placeholder("01:30").Description("MM:SS ou HH:MM:SS").
				Value(&v.start).Validate(validateOptionalTime),
			huh.NewInput().Title("Fim").Placeholder("02:00").Value(&v.end).Validate(validateOptionalTime),
		).Title("Recorte").WithHideFunc(func() bool { return !v.trim }),
	)
}

func newAudioForm(v *audioValues, base string) *huh.Form {
	return huh.NewForm(huh.NewGroup(
		huh.NewText().Title("URLs").Description("Um por linha. Músicas, playlists ou álbuns.").
			Lines(4).Value(&v.urls).Validate(validateURLs),
		huh.NewSelect[string]().Title("Formato").Options(
			huh.NewOption("MP3 — universal", jobs.FormatMP3),
			huh.NewOption("M4A (AAC) — melhor qualidade ao mesmo tamanho", jobs.FormatM4A),
		).Value(&v.format),
		huh.NewInput().Title("Pasta de saída").Description(outputDescription(base)).Value(&v.output),
	))
}

func newSettingsForm(v *settingsValues) *huh.Form {
	return huh.NewForm(huh.NewGroup(
		huh.NewNote().Title("Definições").Description("Gravadas em "+config.FilePath()+
			".\nAs variáveis de ambiente têm sempre prioridade."),
		huh.NewInput().Title("Pasta base dos downloads").Description("Ex: ~/Videos. Vazio = pasta atual.").
			Value(&v.output),
		huh.NewInput().Title("Ficheiro de cookies").
			Description("Formato Netscape, para vídeos que pedem login ou a verificação \"não és um robô\".").
			Value(&v.cookies).Validate(func(s string) error {
			s = strings.TrimSpace(s)
			if s == "" {
				return nil
			}
			path, err := config.ExpandHome(s, os.Getenv)
			if err != nil {
				return err
			}
			return config.CheckCookiesFile(path)
		}),
	))
}

// trimValues devolve o início e o fim, ou vazios quando não há recorte.
func trimValues(trim bool, start, end string) (string, string) {
	if !trim {
		return "", ""
	}
	return strings.TrimSpace(start), strings.TrimSpace(end)
}

// toDownload valida os valores do formulário de vídeo ou de áudio.
func (v videoValues) toDownload(base string) (*jobs.Download, error) {
	start, end := trimValues(v.trim, v.start, v.end)
	return jobs.NewDownload(jobs.DownloadInput{
		URLs: splitLines(v.urls), OutputDir: strings.TrimSpace(v.output),
		Quality: v.quality, Format: v.format, Start: start, End: end,
	}, base)
}

func (v audioValues) toDownload(base string) (*jobs.Download, error) {
	return jobs.NewDownload(jobs.DownloadInput{
		URLs: splitLines(v.urls), OutputDir: strings.TrimSpace(v.output), Format: v.format,
	}, base)
}
