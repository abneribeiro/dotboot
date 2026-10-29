package tui

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"charm.land/huh/v2"

	"github.com/abneribeiro/dotboot/internal/config"
	"github.com/abneribeiro/dotboot/internal/downloader"
	"github.com/abneribeiro/dotboot/internal/jobs"
	"github.com/abneribeiro/dotboot/internal/stems"
)

// formKind identifica o formulário aberto.
type formKind int

const (
	formVideo formKind = iota
	formAudio
	formStems
	formSettings
)

func (k formKind) title() string {
	switch k {
	case formAudio:
		return "🎵 Extrair áudio"
	case formStems:
		return "🎸 Separar música"
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

type stemsValues struct {
	source, url, file  string
	model, format      string
	only, without      []string
	output             string
	trim               bool
	start, end         string
	fileStartDirectory string
}

type settingsValues struct {
	output, cookies, models string
}

// Origens de um pedido de stems.
const (
	sourceURL  = "url"
	sourceFile = "file"
)

// audioExtensions são os ficheiros que o seletor de ficheiros mostra.
var audioExtensions = []string{".mp3", ".wav", ".flac", ".m4a", ".aac", ".ogg", ".opus",
	".wma", ".aiff", ".mp4", ".mkv", ".webm", ".mov"}

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

// stemOptions são as pistas do modelo escolhido, para os multiselect.
func stemOptions(model string) []huh.Option[string] {
	p, err := stems.LookupPreset(model)
	if err != nil {
		return nil
	}
	return huh.NewOptions(p.Stems...)
}

func newStemsForm(v *stemsValues, base string) *huh.Form {
	var modelOptions []huh.Option[string]
	for _, p := range stems.Presets() {
		modelOptions = append(modelOptions, huh.NewOption(fmt.Sprintf("%s — %s", p.Name, p.Description), p.Name))
	}

	return huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().Title("De onde vem a música?").Options(
				huh.NewOption("Um URL (YouTube, SoundCloud, …)", sourceURL),
				huh.NewOption("Um ficheiro do computador", sourceFile),
			).Value(&v.source),
		),
		huh.NewGroup(
			huh.NewInput().Title("URL da música").Value(&v.url).Validate(func(s string) error {
				return downloader.ValidateURL(strings.TrimSpace(s))
			}),
		).WithHideFunc(func() bool { return v.source != sourceURL }),
		huh.NewGroup(
			huh.NewFilePicker().Title("Ficheiro de música").
				Description("Enter abre pastas e escolhe o ficheiro; ← volta atrás.").
				CurrentDirectory(v.fileStartDirectory).AllowedTypes(audioExtensions).ShowPermissions(false).
				Height(12).Value(&v.file).Validate(func(s string) error {
				if s == "" {
					return errors.New("escolhe um ficheiro")
				}
				return nil
			}),
		).WithHideFunc(func() bool { return v.source != sourceFile }),
		huh.NewGroup(
			huh.NewSelect[string]().Title("Modelo").Options(modelOptions...).Value(&v.model),
			huh.NewMultiSelect[string]().Title("Pistas a guardar").
				Description("Nenhuma marcada = todas.").
				OptionsFunc(func() []huh.Option[string] { return stemOptions(v.model) }, &v.model).
				Value(&v.only),
			huh.NewMultiSelect[string]().Title("Gerar também uma faixa sem…").
				Description("Ex: sem guitarra, para tocar por cima. Opcional.").
				OptionsFunc(func() []huh.Option[string] { return stemOptions(v.model) }, &v.model).
				Value(&v.without),
			huh.NewSelect[string]().Title("Formato").Options(
				huh.NewOption("MP3 320k", "mp3"),
				huh.NewOption("FLAC (sem perdas)", "flac"),
				huh.NewOption("WAV", "wav"),
			).Value(&v.format),
			huh.NewInput().Title("Pasta de saída").Description(outputDescription(base)).Value(&v.output),
			huh.NewConfirm().Title("Separar só um pedaço?").Affirmative("Sim").Negative("Não").Value(&v.trim),
		),
		huh.NewGroup(
			huh.NewInput().Title("Início").Placeholder("00:30").Description("MM:SS ou HH:MM:SS").
				Value(&v.start).Validate(validateOptionalTime),
			huh.NewInput().Title("Fim").Placeholder("01:30").Value(&v.end).Validate(validateOptionalTime),
		).Title("Pedaço a separar").WithHideFunc(func() bool { return !v.trim }),
	)
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
		huh.NewInput().Title("Pasta dos modelos de stems").Value(&v.models),
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

// toStems valida o formulário de stems. As pistas marcadas que o modelo
// escolhido não tem (o utilizador marcou e depois mudou de modelo) são
// descartadas, em vez de recusarem o pedido.
func (v stemsValues) toStems(base string) (*jobs.Stems, error) {
	p, err := stems.LookupPreset(v.model)
	if err != nil {
		return nil, err
	}
	keep := func(list []string) []string {
		var out []string
		for _, s := range list {
			if slices.Contains(p.Stems, s) {
				out = append(out, s)
			}
		}
		return out
	}
	input := strings.TrimSpace(v.url)
	if v.source == sourceFile {
		input = v.file
	}
	start, end := trimValues(v.trim, v.start, v.end)
	return jobs.NewStems(jobs.StemsInput{
		Inputs: []string{input}, OutputDir: strings.TrimSpace(v.output), Model: v.model,
		Format: v.format, Only: keep(v.only), Without: keep(v.without), Start: start, End: end,
	}, base)
}
