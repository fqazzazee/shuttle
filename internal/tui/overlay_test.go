// SPDX-License-Identifier: GPL-2.0-or-later

package tui

import (
	"fmt"
	"image/color"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// fgCode is c as a foreground color in an SGR sequence. The canvas that
// lays an overlay over the screen writes its own sequences, so styles'
// renderings are not found in its output whole.
func fgCode(c color.Color) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("38;2;%d;%d;%d", r>>8, g>>8, b>>8)
}

// framedText is the text in a frame, its lines joined by spaces.
func framedText(box string) string {
	lines := strings.Split(ansi.Strip(box), "\n")
	var text []string
	for _, l := range lines[1 : len(lines)-1] {
		text = append(text, strings.TrimSpace(strings.Trim(l, "│")))
	}
	return strings.Join(strings.Fields(strings.Join(text, " ")), " ")
}

// A question shows over the dimmed screen, in a frame, whole: at 80
// columns, the status line it was in cut the longest short. Its keys stay
// at the bottom, undimmed.
func TestQuestionOverlay(t *testing.T) {
	m := start(t, newFake(), 80, 24)
	press(t, m, "x")
	want := "Delete the shuttle docs from the config file? Its files stay where they are, on both sides."
	if got := framedText(m.questionBox()); got != want+" y yes n no" {
		t.Errorf("the question's frame says %q", got)
	}
	content := m.View().Content
	lines := strings.Split(content, "\n")
	if len(lines) != 24 {
		t.Errorf("%d lines, want the terminal's 24", len(lines))
	}
	if table := lines[busHeight+1]; !strings.Contains(table, fgCode(darkPalette.shade)) {
		t.Errorf("the screen behind the question is not shaded: %q", table)
	}
	if keys := lines[len(lines)-1]; !strings.Contains(keys, fgCode(darkPalette.amber)) {
		t.Errorf("the keys are not live under the question: %q", keys)
	}
	if s := screen(m); !strings.Contains(s, "│ Delete the shuttle docs from the config file? Its files stay │") {
		t.Errorf("the question is not over the screen:\n%s", s)
	}
	press(t, m, "n")
	if s := screen(m); strings.Contains(s, "╭───") && strings.Contains(s, "Delete the shuttle") {
		t.Errorf("the question is still shown after n:\n%s", s)
	}
}

// The help's frame is as tall as the tab's room, over the dimmed tab, and
// its keys stay at the bottom.
func TestHelpOverlay(t *testing.T) {
	m := start(t, newFake(), 120, 24)
	press(t, m, "?")
	s := screen(m)
	lines := strings.Split(s, "\n")
	top, bottom := -1, -1
	for i, l := range lines {
		if strings.Contains(l, "╭──────────") && !strings.HasPrefix(l, " ╭───────╮ ") {
			top = i
		}
		if strings.Contains(l, "╰──────────") && !strings.HasPrefix(l, " ╰─") {
			bottom = i
		}
	}
	room := m.room(len(m.keyLines()))
	if top != busHeight+1 || bottom-top+1 != room {
		t.Errorf("the help's frame runs from %d to %d; want from %d, %d rows:\n%s", top, bottom, busHeight+1, room, s)
	}
	if !strings.HasPrefix(lines[len(lines)-1], "↑↓ scroll  esc close") {
		t.Errorf("keys %q", lines[len(lines)-1])
	}
	press(t, m, "esc")
	if s := screen(m); strings.Contains(s, "choose a tab") {
		t.Errorf("the help is still shown after esc:\n%s", s)
	}
}

// In a small terminal the overlays stay inside it.
func TestOverlaysInASmallTerminal(t *testing.T) {
	m := start(t, newFake(), 30, 10)
	for _, keys := range [][]string{{"x"}, {"n", "?"}} {
		press(t, m, keys...)
		lines := strings.Split(screen(m), "\n")
		if len(lines) != 10 {
			t.Errorf("after %q: %d lines, want 10", keys, len(lines))
		}
		for _, l := range lines {
			if ansi.StringWidth(l) > 30 {
				t.Errorf("after %q: a line too wide: %q", keys, l)
			}
		}
	}
}
