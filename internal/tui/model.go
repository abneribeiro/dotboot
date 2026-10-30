// Package tui é a interface de terminal do dotboot (Bubble Tea v2, Lip Gloss
// v2, Bubbles e Huh). Usa o mesmo motor do CLI (internal/jobs): os
// formulários produzem os mesmos pedidos validados, e os eventos de
// progresso chegam aqui por um canal.
package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"charm.land/bubbles/v2/progress"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/abneribeiro/dotboot/internal/config"
	"github.com/abneribeiro/dotboot/internal/doctor"
	"github.com/abneribeiro/dotboot/internal/jobs"
)

type screen int

const (
	screenHome screen = iota
	screenForm
	screenJobs
	screenDetail
	screenDoctor
)

// menuItem é uma entrada do menu inicial.
type menuItem struct {
	icon, title, desc string
}

var menu = []menuItem{
	{"🎬", "Descarregar vídeo", "MP4 que toca em todo o lado · playlists · recortes"},
	{"🎵", "Extrair áudio", "MP3 ou M4A com título e capa"},
	{"📋", "Trabalhos", "progresso, ficheiros e logs"},
	{"🩺", "Diagnóstico", "o que está instalado e o que falta"},
	{"⚙️ ", "Definições", "pasta de saída e cookies"},
}

// doctorMsg traz o resultado das verificações do diagnóstico.
type doctorMsg struct{ checks []doctor.Check }

// Model é o estado da TUI.
type Model struct {
	cfg     *config.Config
	version string
	theme   theme

	width, height int
	screen        screen
	menuCursor    int

	form     *huh.Form
	formKind formKind
	formErr  string
	video    videoValues
	audio    audioValues
	settings settingsValues

	queue     *queue
	jobCursor int
	detailID  int
	logView   viewport.Model
	spinner   spinner.Model
	bar       progress.Model

	checks   []doctor.Check
	checking bool

	flash     string
	quitArmed bool

	// collect faz o diagnóstico; nos testes é substituído.
	collect func(context.Context, *config.Config) []doctor.Check
}

// New cria a TUI. Com fundo escuro por omissão, até o terminal responder.
func New(cfg *config.Config, version string) *Model {
	m := &Model{
		cfg:     cfg,
		version: version,
		theme:   newTheme(true),
		width:   80,
		height:  24,
		queue:   newQueue(),
		spinner: spinner.New(spinner.WithSpinner(spinner.Dot)),
		bar:     progress.New(progress.WithoutPercentage()),
		logView: viewport.New(),
		collect: doctor.Collect,
	}
	m.video = videoValues{quality: "best", format: jobs.FormatMP4}
	m.audio = audioValues{format: jobs.FormatMP3}
	m.applyTheme()
	return m
}

func (m *Model) applyTheme() {
	m.spinner.Style = lipgloss.NewStyle().Foreground(m.theme.cAccent)
	m.bar = progress.New(progress.WithoutPercentage(), progress.WithFillCharacters('█', '░'),
		progress.WithColors(m.theme.cAccent, m.theme.cAccent2))
}

// Init pede a cor de fundo (para o tema) e começa a ouvir os trabalhos.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(tea.RequestBackgroundColor, m.queue.listen, m.spinner.Tick)
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resizeLog()
		if m.form != nil {
			return m, m.updateForm(m.formSize())
		}
		return m, nil

	case tea.BackgroundColorMsg:
		m.theme = newTheme(msg.IsDark())
		m.applyTheme()
		if m.form != nil {
			return m, m.updateForm(msg)
		}
		return m, nil

	case jobEventMsg:
		if j := m.queue.byID(msg.id); j != nil {
			j.apply(msg.event)
			if m.screen == screenDetail && m.detailID == j.id && msg.event.Log != "" {
				m.refreshLog(j)
			}
		}
		return m, m.queue.listen

	case jobDoneMsg:
		m.queue.finish(msg)
		if j := m.queue.byID(msg.id); j != nil {
			m.flash = doneFlash(j)
			if m.screen == screenDetail && m.detailID == j.id {
				m.refreshLog(j)
			}
		}
		return m, m.queue.listen

	case doctorMsg:
		m.checks, m.checking = msg.checks, false
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			return m, m.quit()
		}
		m.quitArmed = false
		if m.screen != screenForm {
			m.flash = ""
		}
		switch m.screen {
		case screenHome:
			return m, m.updateHome(msg)
		case screenForm:
			if msg.String() == "esc" {
				m.closeForm()
				return m, nil
			}
			return m, m.updateForm(msg)
		case screenJobs:
			return m, m.updateJobs(msg)
		case screenDetail:
			return m, m.updateDetail(msg)
		case screenDoctor:
			return m, m.updateDoctor(msg)
		}
	}

	// Mensagens internas dos formulários (mudança de campo, leitura de
	// pastas do seletor de ficheiros…) têm de chegar ao formulário.
	if m.form != nil {
		return m, m.updateForm(msg)
	}
	return m, nil
}

// quit sai da TUI. Com trabalhos a correr, o primeiro ctrl+c só avisa: sair
// cancela-os.
func (m *Model) quit() tea.Cmd {
	if running, queued := m.queue.counts(); running+queued > 0 && !m.quitArmed {
		m.quitArmed = true
		m.flash = "⚠️  Há trabalhos por terminar. Carrega outra vez para os cancelar e sair."
		return nil
	}
	return tea.Quit
}

func (m *Model) updateHome(msg tea.KeyPressMsg) tea.Cmd {
	switch key := msg.String(); key {
	case "up", "k":
		m.menuCursor = (m.menuCursor + len(menu) - 1) % len(menu)
	case "down", "j":
		m.menuCursor = (m.menuCursor + 1) % len(menu)
	case "enter", "space":
		return m.openMenu(m.menuCursor)
	case "tab":
		m.screen = screenJobs
	case "q":
		return m.quit()
	default:
		if len(key) == 1 && key[0] >= '1' && key[0] <= byte('0'+len(menu)) {
			m.menuCursor = int(key[0] - '1')
			return m.openMenu(m.menuCursor)
		}
	}
	return nil
}

func (m *Model) openMenu(i int) tea.Cmd {
	switch i {
	case 0:
		return m.openForm(formVideo)
	case 1:
		return m.openForm(formAudio)
	case 2:
		m.screen = screenJobs
	case 3:
		return m.openDoctor()
	case 4:
		m.settings = settingsValues{output: m.cfg.OutputDir, cookies: m.cfg.CookiesFile}
		return m.openForm(formSettings)
	}
	return nil
}

// formWidth é a largura útil dos formulários: o ecrã todo é demasiado em
// terminais largos.
func (m *Model) formWidth() int {
	return max(min(m.width-6, 90), 30)
}

// chromeHeight é o espaço do cabeçalho e do rodapé.
const chromeHeight = 7

func (m *Model) formSize() tea.WindowSizeMsg {
	return tea.WindowSizeMsg{Width: m.formWidth(), Height: max(m.height-chromeHeight-2, 8)}
}

func (m *Model) openForm(kind formKind) tea.Cmd {
	base := m.cfg.OutputDir
	switch kind {
	case formVideo:
		m.form = newVideoForm(&m.video, base)
	case formAudio:
		m.form = newAudioForm(&m.audio, base)
	case formSettings:
		m.form = newSettingsForm(&m.settings)
	}
	m.form.WithTheme(huh.ThemeFunc(huh.ThemeCharm)).WithShowHelp(true).WithWidth(m.formWidth())
	m.formKind = kind
	m.screen = screenForm
	size := m.formSize()
	return tea.Batch(m.form.Init(), func() tea.Msg { return size })
}

func (m *Model) closeForm() {
	m.form, m.formErr = nil, ""
	m.screen = screenHome
}

// updateForm entrega msg ao formulário e trata do envio.
func (m *Model) updateForm(msg tea.Msg) tea.Cmd {
	if m.form == nil {
		return nil
	}
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		msg = tea.WindowSizeMsg{Width: m.formWidth(), Height: max(size.Height, 8)}
	}
	updated, cmd := m.form.Update(msg)
	if f, ok := updated.(*huh.Form); ok {
		m.form = f
	}
	switch m.form.State {
	case huh.StateCompleted:
		return m.submit()
	case huh.StateAborted:
		m.closeForm()
		return nil
	}
	return cmd
}

// submit valida o formulário enviado e põe o trabalho na fila. Um erro de
// validação reabre o formulário com os valores e o erro por cima.
func (m *Model) submit() tea.Cmd {
	env := jobs.EnvFrom(m.cfg)
	base := m.cfg.OutputDir

	var err error
	switch m.formKind {
	case formVideo:
		var d *jobs.Download
		if d, err = m.video.toDownload(base); err == nil {
			m.enqueue(kindVideo, titleOf(d.URLs), func(ctx context.Context, r jobs.Reporter) (jobs.Result, error) {
				return d.Run(ctx, env, r)
			})
			m.video.urls = ""
		}
	case formAudio:
		var d *jobs.Download
		if d, err = m.audio.toDownload(base); err == nil {
			m.enqueue(kindAudio, titleOf(d.URLs), func(ctx context.Context, r jobs.Reporter) (jobs.Result, error) {
				return d.Run(ctx, env, r)
			})
			m.audio.urls = ""
		}
	case formSettings:
		if err = m.saveSettings(); err == nil {
			m.closeForm()
			m.flash = "✅ Definições gravadas em " + config.FilePath()
			return nil
		}
	}

	if err != nil {
		kind := m.formKind
		cmd := m.openForm(kind)
		m.formErr = err.Error()
		return cmd
	}
	m.form, m.formErr = nil, ""
	m.screen = screenJobs
	m.jobCursor = len(m.queue.jobs) - 1
	return nil
}

func (m *Model) enqueue(kind jobKind, title string, run runFunc) {
	m.queue.add(kind, title, run)
	m.flash = "➕ Trabalho adicionado à fila."
}

// titleOf resume uma lista de entradas: "url" ou "url (+2)".
func titleOf(inputs []string) string {
	switch len(inputs) {
	case 0:
		return "lista de URLs"
	case 1:
		return inputs[0]
	default:
		return fmt.Sprintf("%s (+%d)", inputs[0], len(inputs)-1)
	}
}

// saveSettings grava as definições e aplica-as já à configuração em memória.
func (m *Model) saveSettings() error {
	v := m.settings
	values := map[string]string{
		config.KeyOutputDir:   strings.TrimSpace(v.output),
		config.KeyCookiesFile: strings.TrimSpace(v.cookies),
	}
	if err := config.Save(config.FilePath(), values); err != nil {
		return err
	}
	expand := func(p string) string {
		e, err := config.ExpandHome(strings.TrimSpace(p), os.Getenv)
		if err != nil {
			return p
		}
		return e
	}
	m.cfg.OutputDir, m.cfg.CookiesFile = expand(v.output), expand(v.cookies)
	return nil
}

func (m *Model) selectedJob() *job {
	if len(m.queue.jobs) == 0 {
		return nil
	}
	m.jobCursor = min(max(m.jobCursor, 0), len(m.queue.jobs)-1)
	return m.queue.jobs[m.jobCursor]
}

func (m *Model) updateJobs(msg tea.KeyPressMsg) tea.Cmd {
	j := m.selectedJob()
	switch msg.String() {
	case "esc", "backspace":
		m.screen = screenHome
	case "q":
		return m.quit()
	case "up", "k":
		m.jobCursor--
	case "down", "j":
		m.jobCursor++
	case "n":
		m.screen = screenHome
	case "enter":
		if j != nil {
			m.detailID = j.id
			m.screen = screenDetail
			m.resizeLog()
			m.refreshLog(j)
		}
	case "c":
		if j != nil && j.active() {
			m.queue.cancel(j)
			m.flash = "⏹  A cancelar…"
		}
	case "r":
		if j != nil && (j.status == statusFailed || j.status == statusCanceled) {
			m.queue.retry(j)
			m.jobCursor = len(m.queue.jobs) - 1
		}
	case "o":
		if j != nil {
			m.openFolder(j)
		}
	case "x":
		m.clearFinished()
	}
	m.selectedJob()
	return nil
}

// clearFinished tira da lista os trabalhos terminados.
func (m *Model) clearFinished() {
	var keep []*job
	for _, j := range m.queue.jobs {
		if j.active() {
			keep = append(keep, j)
		}
	}
	m.queue.jobs = keep
	m.jobCursor = 0
}

func (m *Model) updateDetail(msg tea.KeyPressMsg) tea.Cmd {
	j := m.queue.byID(m.detailID)
	switch msg.String() {
	case "esc", "backspace", "q":
		m.screen = screenJobs
		return nil
	case "c":
		if j != nil && j.active() {
			m.queue.cancel(j)
			m.flash = "⏹  A cancelar…"
		}
		return nil
	case "r":
		if j != nil && (j.status == statusFailed || j.status == statusCanceled) {
			nj := m.queue.retry(j)
			m.detailID = nj.id
			m.jobCursor = len(m.queue.jobs) - 1
			m.refreshLog(nj)
		}
		return nil
	case "o":
		if j != nil {
			m.openFolder(j)
		}
		return nil
	}
	var cmd tea.Cmd
	m.logView, cmd = m.logView.Update(msg)
	return cmd
}

func (m *Model) openDoctor() tea.Cmd {
	m.screen = screenDoctor
	m.checking = true
	cfg, collect := m.cfg, m.collect
	return func() tea.Msg { return doctorMsg{checks: collect(context.Background(), cfg)} }
}

func (m *Model) updateDoctor(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "backspace":
		m.screen = screenHome
	case "q":
		return m.quit()
	case "r":
		return m.openDoctor()
	}
	return nil
}

// openFolder abre a pasta do resultado no gestor de ficheiros. Dentro do
// Docker não há gestor: mostra-se o caminho.
func (m *Model) openFolder(j *job) {
	dir := j.result.OutputDir
	if dir == "" {
		m.flash = "Este trabalho ainda não tem pasta."
		return
	}
	opener := "xdg-open"
	switch runtime.GOOS {
	case "darwin":
		opener = "open"
	case "windows":
		opener = "explorer"
	}
	if err := exec.Command(opener, dir).Start(); err != nil {
		m.flash = "📁 " + dir
		return
	}
	m.flash = "📁 A abrir " + dir
}

func (m *Model) resizeLog() {
	m.logView.SetWidth(max(m.width-6, 20))
	// Cabeçalho, rodapé, cartão do trabalho e lista de ficheiros.
	m.logView.SetHeight(max(m.height-chromeHeight-12, 4))
}

func (m *Model) refreshLog(j *job) {
	lines := make([]string, len(j.logs))
	for i, l := range j.logs {
		lines[i] = ansi.Truncate(l, m.logView.Width(), "…")
	}
	if len(lines) == 0 {
		lines = []string{m.theme.muted.Render("(ainda sem output)")}
	}
	m.logView.SetContentLines(lines)
	m.logView.GotoBottom()
}

// doneFlash é a mensagem do rodapé quando um trabalho termina.
func doneFlash(j *job) string {
	switch j.status {
	case statusDone:
		if j.err != nil {
			return "⚠️  Terminado com falhas: " + j.errSummary()
		}
		return fmt.Sprintf("✅ Concluído: %d ficheiro(s) em %s", len(j.result.Files), j.result.OutputDir)
	case statusCanceled:
		return "⏹  Cancelado."
	default:
		return "❌ Falhou: " + j.errSummary()
	}
}
