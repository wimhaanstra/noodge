// Package tui is the full-screen command browser.
//
// It never runs anything. It returns which command the user chose and the
// arguments they filled in, and the caller executes that — so the command
// inherits the real terminal, keeps its colours, and reports its own exit code.
package tui

import (
	"os"
	"strings"

	"charm.land/lipgloss/v2"
)

// Colours are chosen to be legible on a light and a dark background alike.
//
// Windows Terminal answers neither OSC 11 nor sets COLORFGBG, so asking the
// terminal which it is gives an unreliable answer often enough to matter. A
// palette that needs no detection is worth more than one that is occasionally
// perfect and occasionally unreadable. NOODGE_THEME overrides the one choice
// that genuinely differs.
type styles struct {
	title    lipgloss.Style
	path     lipgloss.Style
	selected lipgloss.Style
	item     lipgloss.Style
	muted    lipgloss.Style
	heading  lipgloss.Style
	group    lipgloss.Style
	label    lipgloss.Style
	required lipgloss.Style
	warn     lipgloss.Style
	sep      lipgloss.Style
	focus    lipgloss.Style
}

func newStyles() styles {
	// 39 is a mid blue and 244 a mid grey: both keep enough contrast against
	// white and against black.
	accent := lipgloss.Color("39")
	muted := lipgloss.Color("244")
	warn := lipgloss.Color("203")

	switch strings.ToLower(os.Getenv("NOODGE_THEME")) {
	case "light":
		muted = lipgloss.Color("240")
	case "dark":
		muted = lipgloss.Color("247")
	}

	return styles{
		title:    lipgloss.NewStyle().Bold(true).Foreground(accent),
		path:     lipgloss.NewStyle().Foreground(muted),
		selected: lipgloss.NewStyle().Bold(true).Foreground(accent),
		item:     lipgloss.NewStyle(),
		muted:    lipgloss.NewStyle().Foreground(muted),
		heading:  lipgloss.NewStyle().Bold(true),
		group:    lipgloss.NewStyle().Bold(true).Foreground(muted),
		label:    lipgloss.NewStyle().Bold(true),
		required: lipgloss.NewStyle().Foreground(warn),
		warn:     lipgloss.NewStyle().Foreground(warn),
		sep:      lipgloss.NewStyle().Foreground(muted),
		focus:    lipgloss.NewStyle().Bold(true).Foreground(accent),
	}
}

// Every glyph used in the interface is ASCII.
//
// conhost and Windows Terminal disagree about the width of emoji and of
// ambiguous-width characters, and a width miscalculation does not degrade a
// two-pane layout gracefully — it corrupts it, permanently, until redraw.
const (
	cursor    = "> "
	noCursor  = "  "
	separator = "|"
	checked   = "[x]"
	unchecked = "[ ]"
)

// truncate shortens s to fit width, marking that it was cut.
func truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	if width <= 3 {
		return strings.Repeat(".", width)
	}

	runes := []rune(s)
	for len(runes) > 0 && lipgloss.Width(string(runes))+3 > width {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "..."
}
