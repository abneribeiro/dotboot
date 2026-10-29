package tui

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// theme reúne os estilos da TUI. É reconstruído quando o terminal diz se o
// fundo é claro ou escuro (tea.BackgroundColorMsg), para as cores terem
// sempre contraste.
type theme struct {
	isDark bool

	cAccent, cAccent2, cSuccess, cDanger, cWarning, cMuted, cText color.Color

	logo      lipgloss.Style
	version   lipgloss.Style
	subtitle  lipgloss.Style
	header    lipgloss.Style
	title     lipgloss.Style
	muted     lipgloss.Style
	text      lipgloss.Style
	selected  lipgloss.Style
	card      lipgloss.Style
	cardFocus lipgloss.Style
	badge     lipgloss.Style
	success   lipgloss.Style
	danger    lipgloss.Style
	warning   lipgloss.Style
	keyHint   lipgloss.Style
	keyDesc   lipgloss.Style
	flash     lipgloss.Style
	errorBox  lipgloss.Style
}

func newTheme(isDark bool) theme {
	ld := lipgloss.LightDark(isDark)
	t := theme{
		isDark:   isDark,
		cAccent:  ld(lipgloss.Color("#5A3FC0"), lipgloss.Color("#9B7BFF")),
		cAccent2: ld(lipgloss.Color("#0E7C86"), lipgloss.Color("#4FD1C5")),
		cSuccess: ld(lipgloss.Color("#1A7F37"), lipgloss.Color("#3FD07F")),
		cDanger:  ld(lipgloss.Color("#C62828"), lipgloss.Color("#FF6B81")),
		cWarning: ld(lipgloss.Color("#9A6700"), lipgloss.Color("#F2C94C")),
		cMuted:   ld(lipgloss.Color("#6E6E80"), lipgloss.Color("#8A8AA0")),
		cText:    ld(lipgloss.Color("#1F1F28"), lipgloss.Color("#E6E6F0")),
	}

	t.logo = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).
		Background(t.cAccent).Padding(0, 1)
	t.version = lipgloss.NewStyle().Foreground(t.cMuted)
	t.subtitle = lipgloss.NewStyle().Foreground(t.cMuted).Italic(true)
	t.header = lipgloss.NewStyle().Padding(1, 2, 0, 2)
	t.title = lipgloss.NewStyle().Bold(true).Foreground(t.cAccent)
	t.muted = lipgloss.NewStyle().Foreground(t.cMuted)
	t.text = lipgloss.NewStyle().Foreground(t.cText)
	t.selected = lipgloss.NewStyle().Bold(true).Foreground(t.cAccent)
	t.card = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).
		BorderForeground(ld(lipgloss.Color("#D0D0DC"), lipgloss.Color("#3A3A4A"))).Padding(0, 1)
	t.cardFocus = t.card.BorderForeground(t.cAccent)
	t.badge = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")).Background(t.cAccent2).Padding(0, 1)
	t.success = lipgloss.NewStyle().Foreground(t.cSuccess)
	t.danger = lipgloss.NewStyle().Foreground(t.cDanger)
	t.warning = lipgloss.NewStyle().Foreground(t.cWarning)
	t.keyHint = lipgloss.NewStyle().Foreground(t.cAccent).Bold(true)
	t.keyDesc = lipgloss.NewStyle().Foreground(t.cMuted)
	t.flash = lipgloss.NewStyle().Foreground(t.cSuccess).Bold(true)
	t.errorBox = lipgloss.NewStyle().Foreground(t.cDanger).Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(t.cDanger).PaddingLeft(1)
	return t
}
