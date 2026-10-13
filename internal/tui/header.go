// SPDX-License-Identifier: GPL-2.0-or-later

package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/fqazzazee/shuttle/internal/daemon"
	"github.com/fqazzazee/shuttle/internal/present"
)

// part is a run of a drawing in one style.
type part struct {
	style *lipgloss.Style
	text  string
}

// smallBus is shuttle's icon in five rows, for the header: the front of
// the bus, the sync arrows on its destination sign, its windscreen, lights
// and wheels. Every row is busWidth columns, one a character.
var smallBus = [][]part{
	{{&busStyle, " ╭───────╮ "}},
	{{&busStyle, " │  "}, {&signStyle, " ⇄ "}, {&busStyle, "  │ "}},
	{{&busStyle, "━┥ "}, {&glassStyle, "▐███▌"}, {&busStyle, " ┝━"}},
	{{&busStyle, " │ "}, {&lightStyle, "●"}, {&faint, " ═ "}, {&lightStyle, "●"}, {&busStyle, " │ "}},
	{{&busStyle, " ╰─"}, {&faint, "▀"}, {&busStyle, "───"}, {&faint, "▀"}, {&busStyle, "─╯ "}},
}

// wordmark is "shuttle" in blocks, three rows high.
var wordmark = []string{
	"█▀▀ █ █ █ █ ▀█▀ ▀█▀ █   █▀▀",
	"▀▀█ █▀█ █ █  █   █  █   █▀▀",
	"▀▀▀ ▀ ▀ ▀▀▀  ▀   ▀  ▀▀▀ ▀▀▀",
}

// busWidth is smallBus's width, and gap what is between it and the rest.
const (
	busWidth = 11
	gap      = 3
)

// tallHeight is the least height the tall header is shown at: below it,
// the four rows it takes are needed more by the tab.
const tallHeight = 20

// headerLines are the bus beside the wordmark, a summary and the tabs, in
// a terminal with room for them; else a line: the tabs.
func (m *model) headerLines() []string {
	if !m.tall() {
		return []string{m.tabsLine(signStyle.Render(" ⇄ ") + " " + titleStyle.Render("shuttle") + " ")}
	}
	right := []string{
		wordmarkRow(0), wordmarkRow(1), wordmarkRow(2),
		m.summary(),
		m.tabsLine(""),
	}
	lines := make([]string, len(smallBus))
	for i, row := range smallBus {
		var b strings.Builder
		for _, p := range row {
			b.WriteString(p.style.Render(p.text))
		}
		lines[i] = b.String() + strings.Repeat(" ", gap) + right[i]
	}
	return lines
}

// tall says whether the terminal has room for the tall header: the height,
// and beside the bus, the width for the wordmark and the tabs. A form
// open needs the rows more.
func (m *model) tall() bool {
	switch {
	case m.tab == tabShuttles && m.wizard != nil, m.tab == tabProfiles && m.settings != nil, m.tab == tabShares && m.form != nil:
		return false
	}
	room := m.width - busWidth - gap
	return m.height >= tallHeight && lipgloss.Width(wordmark[0]) <= room && lipgloss.Width(m.tabs("", true)) <= room
}

// wordmarkRow is a row of the wordmark, in the palette's gradient from
// sky through teal and mint to amber, a color a column; bold in the
// terminal's own colors.
func wordmarkRow(i int) string {
	if wordColors == nil {
		return titleStyle.Render(wordmark[i])
	}
	var b strings.Builder
	for j, r := range []rune(wordmark[i]) {
		if r == ' ' {
			b.WriteRune(r)
			continue
		}
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(wordColors[j]).Render(string(r)))
	}
	return b.String()
}

// summaryStates are the states the summary counts, in the order it does.
var summaryStates = []daemon.State{
	daemon.StateRunning, daemon.StateHeld, daemon.StateOffline, daemon.StateError, daemon.StatePaused, daemon.StateDisabled,
}

// summary is how many shuttles there are, and how many are in each state
// but idle: "4 shuttles · 1 running · 1 offline".
func (m *model) summary() string {
	if !m.loaded || m.down != nil {
		return ""
	}
	n := map[daemon.State]int{}
	for i := range m.shuttles {
		n[m.shuttles[i].State]++
	}
	parts := []string{present.Plural(len(m.shuttles), "shuttle")}
	for _, st := range summaryStates {
		if n[st] == 0 {
			continue
		}
		s := fmt.Sprintf("%d %s", n[st], st)
		if style, ok := stateStyles[st]; ok {
			s = style.Render(s)
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, faint.Render(" · "))
}
