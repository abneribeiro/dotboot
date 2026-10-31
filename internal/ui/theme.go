// Package ui is dotboot's look: a red Lip Gloss theme, the Bubble Tea
// progress view of a download, and the printers for help, results, errors,
// the doctor report and the settings. Printed output goes through
// lipgloss.Fprint, which downgrades colors to what the terminal supports and
// drops them when the output is not a terminal.
package ui

import "charm.land/lipgloss/v2"

// Palette. The accent red reads on both dark and light backgrounds; green
// and amber only mark success and warnings.
var (
	colorRed    = lipgloss.Color("#E5484D")
	colorRedHot = lipgloss.Color("#FF6369")
	colorGreen  = lipgloss.Color("#30A46C")
	colorAmber  = lipgloss.Color("#F5A524")
	colorMuted  = lipgloss.Color("#8B8D98")
	colorTrack  = lipgloss.Color("#3A3A40")
	colorWhite  = lipgloss.Color("#FFFFFF")
)

var (
	badgeStyle   = lipgloss.NewStyle().Bold(true).Foreground(colorWhite).Background(colorRed).Padding(0, 1)
	accentStyle  = lipgloss.NewStyle().Foreground(colorRed)
	strongStyle  = lipgloss.NewStyle().Bold(true)
	headingStyle = lipgloss.NewStyle().Bold(true).Foreground(colorRed)
	mutedStyle   = lipgloss.NewStyle().Foreground(colorMuted)
	okStyle      = lipgloss.NewStyle().Foreground(colorGreen)
	warnStyle    = lipgloss.NewStyle().Foreground(colorAmber)
	errStyle     = lipgloss.NewStyle().Bold(true).Foreground(colorRed)
	trackStyle   = lipgloss.NewStyle().Foreground(colorTrack)
)

// Status icons.
const (
	iconOK   = "✓"
	iconWarn = "!"
	iconFail = "✗"
	iconNote = "›"
)

// badge renders the red "dotboot" tag that opens every screen.
func badge() string {
	return badgeStyle.Render("dotboot")
}
