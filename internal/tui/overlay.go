// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// The overlays' sizes: the least margin either side of one, the widest the
// help and a question get, and what a frame takes of either.
const (
	margin        = 2
	helpWidth     = 100
	questionWidth = 64
	frameWidth    = 4 // the border and a column of padding, either side
	frameHeight   = 2 // the border, above and below
)

// overlaid is the screen, lines, shaded but for its keys at the bottom,
// which still apply, with box over it: centered across, and in the tab's
// room, from top, centered down when it is shorter.
func (m *model) overlaid(lines []string, keys int, box string, top, room int) string {
	for i := range lines[:len(lines)-keys] {
		lines[i] = shadeStyle.Render(ansi.Strip(lines[i]))
	}
	x := max((m.width-lipgloss.Width(box))/2, 0)
	y := top + max((room-lipgloss.Height(box))/2, 0)
	c := lipgloss.NewCanvas(m.width, m.height)
	c.Compose(lipgloss.NewCompositor(
		lipgloss.NewLayer(strings.Join(lines, "\n")),
		lipgloss.NewLayer(box).X(x).Y(y).Z(1),
	))
	return c.Render()
}

// questionBox is the question waiting for y or n, wrapped in its frame,
// and the keys that answer it.
func (m *model) questionBox() string {
	width := max(min(m.width-2*margin, questionWidth)-frameWidth, 1)
	width = min(width, lipgloss.Width(m.question))
	lines := strings.Split(ansi.Wrap(m.question, width, " "), "\n")
	lines = append(lines, "", keyStyle.Render("y")+" "+faint.Render("yes")+"  "+keyStyle.Render("n")+" "+faint.Render("no"))
	return framed(&questionFrame, lines, width)
}

// framed is lines in frame, each padded to width.
func framed(frame *lipgloss.Style, lines []string, width int) string {
	for i, l := range lines {
		lines[i] = l + strings.Repeat(" ", max(width-lipgloss.Width(l), 0))
	}
	return frame.Render(strings.Join(lines, "\n"))
}
