// SPDX-License-Identifier: GPL-2.0-or-later

package tui

import (
	"image/color"
	"os"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"

	"github.com/fqazzazee/shuttle/internal/activity"
	"github.com/fqazzazee/shuttle/internal/daemon"
)

// palette is what the TUI is drawn in: shuttle's own colors, vivid, after
// the app icon's bus yellow and sync blue.
type palette struct {
	dim     color.Color // what matters less: descriptions, what is paused
	bright  color.Color // text on the selection
	surface color.Color // the selection
	yellow  color.Color // the chosen tab, a bus-yellow fill; the bus's sync arrows
	ink     color.Color // text on yellow
	sign    color.Color // the bus's destination sign, behind its arrows in yellow
	glass   color.Color // the bus's windscreen
	amber   color.Color // shuttle itself: the title, the keys
	sky     color.Color // headings, the bus
	coral   color.Color // errors, an offline share
	mint    color.Color // what went well
	teal    color.Color // a run
	orange  color.Color // held, waiting for the user
}

// The palettes for dark and light backgrounds. The light one is darker,
// to keep its contrast on white (TestPaletteContrast).
var (
	darkPalette = palette{
		dim:     lipgloss.Color("#8e97a9"),
		bright:  lipgloss.Color("#eef1f6"),
		surface: lipgloss.Color("#3a4252"),
		yellow:  lipgloss.Color("#f5c451"),
		ink:     lipgloss.Color("#1b1e26"),
		sign:    lipgloss.Color("#13151b"),
		glass:   lipgloss.Color("#cde3fb"),
		amber:   lipgloss.Color("#f5c451"),
		sky:     lipgloss.Color("#61afef"),
		coral:   lipgloss.Color("#f7848d"),
		mint:    lipgloss.Color("#98c379"),
		teal:    lipgloss.Color("#56c2cf"),
		orange:  lipgloss.Color("#f0975a"),
	}
	lightPalette = palette{
		dim:     lipgloss.Color("#646b7a"),
		bright:  lipgloss.Color("#14171d"),
		surface: lipgloss.Color("#dfe6f0"),
		yellow:  lipgloss.Color("#f6d32d"),
		ink:     lipgloss.Color("#1b1e26"),
		sign:    lipgloss.Color("#241f31"),
		glass:   lipgloss.Color("#4787d6"),
		amber:   lipgloss.Color("#8a5b00"),
		sky:     lipgloss.Color("#1c64c8"),
		coral:   lipgloss.Color("#c8303f"),
		mint:    lipgloss.Color("#2a7430"),
		teal:    lipgloss.Color("#00788a"),
		orange:  lipgloss.Color("#b25000"),
	}
)

// The styles, set by useTheme.
var (
	titleStyle  lipgloss.Style
	activeTab   lipgloss.Style
	headStyle   lipgloss.Style
	faint       lipgloss.Style
	selected    lipgloss.Style
	badStyle    lipgloss.Style
	heldStyle   lipgloss.Style
	busyStyle   lipgloss.Style
	goodStyle   lipgloss.Style
	busStyle    lipgloss.Style
	signStyle   lipgloss.Style
	glassStyle  lipgloss.Style
	lightStyle  lipgloss.Style
	keyStyle    lipgloss.Style
	stateStyles map[daemon.State]lipgloss.Style
	kindStyles  map[activity.Kind]lipgloss.Style
	wordColors  []color.Color // the wordmark's, a column each; nil for the terminal's own
)

// Tests see the dark palette; Run picks one for the terminal.
func init() { useTheme(&darkPalette) }

// pickTheme sets the theme for the terminal on stdin and stdout.
func pickTheme() {
	useTheme(themeFor(colorprofile.Detect(os.Stdout, os.Environ()), func() bool {
		return lipgloss.HasDarkBackground(os.Stdin, os.Stdout)
	}))
}

// themeFor is the palette for a terminal: none where it shows fewer than
// 256 colors, as the palette would come out as the nearest of its 16 and
// they are the user's to choose; else the one for its background, which
// dark asks the terminal for.
func themeFor(p colorprofile.Profile, dark func() bool) *palette {
	switch {
	case p < colorprofile.ANSI256:
		return nil
	case dark():
		return &darkPalette
	}
	return &lightPalette
}

// useTheme sets the styles in p's colors; with p nil, in the terminal's
// own 16, with reverse and faint, which still show where colors do not
// (NO_COLOR).
func useTheme(p *palette) {
	s := lipgloss.NewStyle()
	if p == nil {
		titleStyle = s.Bold(true)
		activeTab = s.Bold(true).Reverse(true)
		headStyle = s.Bold(true)
		faint = s.Faint(true)
		selected = s.Reverse(true)
		badStyle = s.Foreground(lipgloss.Color("1"))
		heldStyle = s.Foreground(lipgloss.Color("3"))
		busyStyle = s.Foreground(lipgloss.Color("6"))
		goodStyle = s.Foreground(lipgloss.Color("2"))
		busStyle = s.Foreground(lipgloss.Color("4"))
		signStyle = s.Bold(true).Reverse(true)
		glassStyle = busStyle
		lightStyle = heldStyle
		keyStyle = s.Bold(true)
		wordColors = nil
	} else {
		titleStyle = s.Bold(true).Foreground(p.amber)
		activeTab = s.Bold(true).Foreground(p.ink).Background(p.yellow)
		headStyle = s.Bold(true).Foreground(p.sky)
		faint = s.Foreground(p.dim)
		selected = s.Foreground(p.bright).Background(p.surface)
		badStyle = s.Foreground(p.coral)
		heldStyle = s.Foreground(p.orange)
		busyStyle = s.Foreground(p.teal)
		goodStyle = s.Foreground(p.mint)
		busStyle = s.Foreground(p.sky)
		signStyle = s.Bold(true).Foreground(p.yellow).Background(p.sign)
		glassStyle = s.Foreground(p.glass)
		lightStyle = s.Foreground(p.amber)
		keyStyle = s.Bold(true).Foreground(p.amber)
		wordColors = lipgloss.Blend1D(lipgloss.Width(wordmark[0]), p.sky, p.teal, p.mint, p.amber)
	}
	stateStyles = map[daemon.State]lipgloss.Style{
		daemon.StateError:    badStyle,
		daemon.StateOffline:  badStyle,
		daemon.StateHeld:     heldStyle,
		daemon.StateRunning:  busyStyle,
		daemon.StatePaused:   faint,
		daemon.StateDisabled: faint,
	}
	kindStyles = map[activity.Kind]lipgloss.Style{
		activity.KindError:    badStyle,
		activity.KindConflict: heldStyle,
		activity.KindHeld:     heldStyle,
		activity.KindSkipped:  faint,
		activity.KindRun:      headStyle,
	}
}
