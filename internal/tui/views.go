package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/abneribeiro/dotboot/internal/doctor"
	"github.com/abneribeiro/dotboot/internal/jobs"
)

// View desenha o ecrã inteiro: cabeçalho, conteúdo e rodapé.
func (m *Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.WindowTitle = "dotboot"
	return v
}

func (m *Model) render() string {
	header := m.renderHeader()
	footer := m.renderFooter()
	bodyHeight := max(m.height-lipgloss.Height(header)-lipgloss.Height(footer), 1)

	var body string
	switch m.screen {
	case screenForm:
		body = m.renderForm()
	case screenJobs:
		body = m.renderJobs(bodyHeight)
	case screenDetail:
		body = m.renderDetail()
	case screenDoctor:
		body = m.renderDoctor()
	default:
		body = m.renderHome()
	}
	body = lipgloss.NewStyle().Padding(0, 2).Height(bodyHeight).MaxHeight(bodyHeight).Render(body)
	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
}

func (m *Model) renderHeader() string {
	t := m.theme
	left := t.logo.Render("▶ dotboot") + " " + t.version.Render(m.version)
	sub := t.subtitle.Render("vídeos, áudio e stems no terminal")

	var status string
	if running, queued := m.queue.counts(); running+queued > 0 {
		parts := []string{}
		if running > 0 {
			parts = append(parts, m.spinner.View()+" a correr")
		}
		if queued > 0 {
			parts = append(parts, fmt.Sprintf("%d na fila", queued))
		}
		status = t.badge.Render(strings.Join(parts, " · "))
	}

	width := max(m.width-4, 20)
	// O estado (a correr, na fila) tem prioridade sobre a versão.
	left = ansi.Truncate(left, max(width-lipgloss.Width(status)-1, 10), "…")
	gap := max(width-lipgloss.Width(left)-lipgloss.Width(status), 1)
	line := left + strings.Repeat(" ", gap) + status
	rule := t.muted.Render(strings.Repeat("─", width))
	return t.header.Render(lipgloss.JoinVertical(lipgloss.Left, line, sub, rule))
}

// hint é um par tecla/descrição do rodapé.
type hint struct{ key, desc string }

func (m *Model) hints() []hint {
	switch m.screen {
	case screenForm:
		return []hint{{"enter", "seguinte"}, {"shift+tab", "anterior"}, {"esc", "cancelar"}}
	case screenJobs:
		return []hint{{"↑/↓", "escolher"}, {"enter", "detalhes"}, {"c", "cancelar"}, {"r", "repetir"},
			{"o", "abrir pasta"}, {"x", "limpar terminados"}, {"esc", "menu"}}
	case screenDetail:
		return []hint{{"↑/↓", "log"}, {"c", "cancelar"}, {"r", "repetir"}, {"o", "abrir pasta"}, {"esc", "voltar"}}
	case screenDoctor:
		return []hint{{"r", "verificar de novo"}, {"esc", "menu"}}
	default:
		return []hint{{"↑/↓", "escolher"}, {"enter", "abrir"}, {"1-6", "atalho"}, {"tab", "trabalhos"}, {"q", "sair"}}
	}
}

func (m *Model) renderFooter() string {
	t := m.theme
	var parts []string
	for _, h := range m.hints() {
		parts = append(parts, t.keyHint.Render(h.key)+" "+t.keyDesc.Render(h.desc))
	}
	keys := ansi.Truncate(strings.Join(parts, t.muted.Render(" • ")), max(m.width-4, 10), "…")

	flash := " "
	if m.flash != "" {
		style := t.flash
		if strings.HasPrefix(m.flash, "❌") || strings.HasPrefix(m.flash, "⚠️") {
			style = t.warning
		}
		flash = style.Render(ansi.Truncate(m.flash, max(m.width-4, 10), "…"))
	}
	return lipgloss.NewStyle().Padding(0, 2).Render(lipgloss.JoinVertical(lipgloss.Left, flash, keys))
}

func (m *Model) renderHome() string {
	t := m.theme
	var b strings.Builder
	b.WriteString(t.title.Render("O que queres fazer?") + "\n\n")
	for i, item := range menu {
		cursor, title := "  ", t.text.Render(item.title)
		if i == m.menuCursor {
			cursor, title = t.selected.Render("▸ "), t.selected.Render(item.title)
		}
		fmt.Fprintf(&b, "%s%s  %s %s\n", cursor, t.muted.Render(fmt.Sprint(i+1)), item.icon, title)
		fmt.Fprintf(&b, "        %s\n", t.muted.Render(ansi.Truncate(item.desc, max(m.width-12, 10), "…")))
	}

	// O trabalho em curso continua visível no menu.
	if j := m.queue.running(); j != nil {
		b.WriteString("\n" + t.title.Render("A correr") + "\n")
		b.WriteString(m.renderCard(j, false))
	}
	return b.String()
}

func (m *Model) renderForm() string {
	t := m.theme
	var b strings.Builder
	b.WriteString(t.title.Render(m.formKind.title()) + "\n\n")
	if m.formErr != "" {
		b.WriteString(t.errorBox.Width(m.formWidth()).Render(m.formErr) + "\n\n")
	}
	if m.form != nil {
		b.WriteString(m.form.View())
	}
	return b.String()
}

// cardWidth é a largura dos cartões dos trabalhos.
func (m *Model) cardWidth() int {
	return max(min(m.width-6, 110), 30)
}

func (m *Model) renderJobs(height int) string {
	t := m.theme
	if len(m.queue.jobs) == 0 {
		return t.title.Render("Trabalhos") + "\n\n" +
			t.muted.Render("Ainda não há trabalhos. Escolhe uma opção no menu (esc) para começar.")
	}

	// Cada cartão ocupa 5 linhas; mostra-se uma janela à volta do cursor.
	const cardHeight = 5
	visible := max((height-2)/cardHeight, 1)
	first := max(min(m.jobCursor-visible/2, len(m.queue.jobs)-visible), 0)
	last := min(first+visible, len(m.queue.jobs))

	var b strings.Builder
	title := fmt.Sprintf("Trabalhos (%d)", len(m.queue.jobs))
	if first > 0 || last < len(m.queue.jobs) {
		title += t.muted.Render(fmt.Sprintf("  %d–%d", first+1, last))
	}
	b.WriteString(t.title.Render(title) + "\n")
	for i := first; i < last; i++ {
		b.WriteString(m.renderCard(m.queue.jobs[i], i == m.jobCursor) + "\n")
	}
	return b.String()
}

// statusLabel é o estado de um trabalho, colorido.
func (m *Model) statusLabel(j *job) string {
	t := m.theme
	switch j.status {
	case statusQueued:
		return t.muted.Render("⏳ na fila")
	case statusRunning:
		return t.selected.Render(m.spinner.View() + " " + string(j.stage))
	case statusDone:
		if j.err != nil {
			return t.warning.Render("⚠️  concluído com falhas")
		}
		return t.success.Render("✅ concluído")
	case statusCanceled:
		return t.muted.Render("⏹  cancelado")
	default:
		return t.danger.Render("❌ falhou")
	}
}

// renderCard desenha um trabalho: tipo e título, ficheiro em curso, e a
// barra de progresso ou o resultado.
func (m *Model) renderCard(j *job, focused bool) string {
	t := m.theme
	width := m.cardWidth()
	inner := width - 4

	status := m.statusLabel(j)
	head := t.text.Bold(true).Render(j.kind.label()) + t.muted.Render(" · ")
	titleWidth := max(inner-lipgloss.Width(head)-lipgloss.Width(status)-2, 8)
	head += t.text.Render(ansi.Truncate(j.title, titleWidth, "…"))
	head += strings.Repeat(" ", max(inner-lipgloss.Width(head)-lipgloss.Width(status), 1)) + status

	item := j.item
	if j.count > 1 {
		item = fmt.Sprintf("[%d/%d] %s", j.index, j.count, item)
	}
	if item == "" {
		item = " "
	}
	item = t.muted.Render(ansi.Truncate(item, inner, "…"))

	var last string
	switch j.status {
	case statusRunning:
		last = m.progressLine(j, inner)
	case statusDone:
		last = t.muted.Render(ansi.Truncate(fmt.Sprintf("%d ficheiro(s) em %s · %s",
			len(j.result.Files), j.result.OutputDir, elapsed(j)), inner, "…"))
		if j.err != nil {
			last = t.warning.Render(ansi.Truncate(j.errSummary(), inner, "…"))
		}
	case statusFailed:
		last = t.danger.Render(ansi.Truncate(j.errSummary(), inner, "…"))
	case statusCanceled:
		last = t.muted.Render("O que ficou a meio retoma-se ao repetir (r).")
	default:
		last = t.muted.Render("À espera que o trabalho anterior termine.")
	}

	style := t.card
	if focused {
		style = t.cardFocus
	}
	return style.Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, head, item, last))
}

// progressLine é a barra do trabalho em curso, ou um spinner quando a etapa
// não tem percentagem (juntar vídeo e áudio, obter informação).
func (m *Model) progressLine(j *job, width int) string {
	t := m.theme
	if j.fraction < 0 {
		return t.muted.Render(m.spinner.View() + " " + string(j.stage) + "…")
	}
	info := fmt.Sprintf(" %3.0f%%", j.fraction*100)
	if j.speed > 0 {
		info += " · " + jobs.FormatBytes(j.speed) + "/s"
	}
	if eta := jobs.FormatETA(j.eta); eta != "" && j.speed > 0 {
		info += " · " + eta
	}
	bar := m.bar
	bar.SetWidth(max(width-lipgloss.Width(info), 10))
	return bar.ViewAs(j.fraction) + t.text.Render(info)
}

func elapsed(j *job) string {
	if j.started.IsZero() || j.finished.IsZero() {
		return ""
	}
	return j.finished.Sub(j.started).Round(time.Second).String()
}

func (m *Model) renderDetail() string {
	t := m.theme
	j := m.queue.byID(m.detailID)
	if j == nil {
		return t.muted.Render("Trabalho não encontrado.")
	}
	var b strings.Builder
	b.WriteString(m.renderCard(j, true) + "\n")

	if len(j.result.Files) > 0 {
		b.WriteString(t.title.Render("Ficheiros") + "\n")
		const maxFiles = 6
		for i, f := range j.result.Files {
			if i == maxFiles {
				b.WriteString(t.muted.Render(fmt.Sprintf("  … e mais %d", len(j.result.Files)-maxFiles)) + "\n")
				break
			}
			rel, err := filepath.Rel(j.result.OutputDir, f)
			if err != nil || strings.HasPrefix(rel, "..") {
				rel = f
			}
			b.WriteString("  " + t.success.Render("•") + " " + ansi.Truncate(rel, m.cardWidth()-4, "…") + "\n")
		}
	}
	if j.err != nil {
		b.WriteString(t.errorBox.Width(m.cardWidth()).Render(j.err.Error()) + "\n")
	}
	b.WriteString(t.title.Render("Log") + "\n")
	b.WriteString(m.logView.View())
	return b.String()
}

func (m *Model) renderDoctor() string {
	t := m.theme
	var b strings.Builder
	b.WriteString(t.title.Render("Diagnóstico") + "\n\n")
	if m.checking {
		b.WriteString(m.spinner.View() + " a verificar o yt-dlp, o ffmpeg, o deno e o audio-separator…")
		return b.String()
	}
	for _, c := range m.checks {
		icon := t.success.Render("✔")
		switch c.Level {
		case doctor.LevelWarn:
			icon = t.warning.Render("!")
		case doctor.LevelFail:
			icon = t.danger.Render("✘")
		}
		name := t.text.Bold(true).Render(fmt.Sprintf("%-12s", c.Name))
		b.WriteString(fmt.Sprintf("  %s %s %s\n", icon, name, ansi.Truncate(c.Detail, max(m.width-24, 20), "…")))
		if c.Fix != "" {
			b.WriteString(t.muted.Render("                 → "+c.Fix) + "\n")
		}
	}
	b.WriteString("\n")
	if doctor.HasFailures(m.checks) {
		b.WriteString(t.danger.Render("Falta instalar algo: vê as indicações acima (make deps, ou a imagem Docker)."))
	} else {
		b.WriteString(t.success.Render("Tudo pronto."))
	}
	return b.String()
}
