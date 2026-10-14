// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package tui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/fqazzazee/shuttle/internal/daemon"
	"github.com/fqazzazee/shuttle/internal/present"
)

// smallBus is shuttle's icon in busHeight rows, for the header: the front
// of the bus, the sync arrows on its destination sign, its windscreen,
// lights and wheels; its body in body, its lights in left and right. Every
// row is busWidth columns, one a character.
func smallBus(body, left, right *lipgloss.Style) []string {
	return []string{
		body.Render(" ╭───────╮ "),
		body.Render(" │  ") + signStyle.Render(" ⇄ ") + body.Render("  │ "),
		body.Render("━┥ ") + glassStyle.Render("▐███▌") + body.Render(" ┝━"),
		body.Render(" │ ") + left.Render("●") + faint.Render(" ═ ") + right.Render("●") + body.Render(" │ "),
		body.Render(" ╰─") + faint.Render("▀") + body.Render("───") + faint.Render("▀") + body.Render("─╯ "),
	}
}

// wordmark is "shuttle" in blocks, three rows high.
var wordmark = []string{
	"█▀▀ █ █ █ █ ▀█▀ ▀█▀ █   █▀▀",
	"▀▀█ █▀█ █ █  █   █  █   █▀▀",
	"▀▀▀ ▀ ▀ ▀▀▀  ▀   ▀  ▀▀▀ ▀▀▀",
}

// busWidth and busHeight are smallBus's size, gap what is between it and
// the rest.
const (
	busWidth  = 11
	busHeight = 5
	gap       = 3
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
	beside := []string{
		wordmarkRow(0), wordmarkRow(1), wordmarkRow(2),
		m.summary(),
		m.tabsLine(""),
	}
	body := m.busTint()
	left, right := m.lights()
	lines := smallBus(&body, &left, &right)
	for i := range lines {
		lines[i] += strings.Repeat(" ", gap) + beside[i]
	}
	return lines
}

// blinkEvery is how long a light of the bus stays on while it blinks.
const blinkEvery = 500 * time.Millisecond

// lights are the styles of the bus's lights: both on, but while a shuttle
// runs, whatever the bus's color says, when they take turns.
func (m *model) lights() (left, right lipgloss.Style) {
	if !m.running() {
		return lightStyle, lightStyle
	}
	if m.now().UnixMilli()/blinkEvery.Milliseconds()%2 == 0 {
		return lightStyle, faint
	}
	return faint, lightStyle
}

// running says whether a shuttle runs.
func (m *model) running() bool {
	return slices.ContainsFunc(m.shuttles, func(st daemon.Status) bool { return st.State == daemon.StateRunning })
}

// busTint is the bus's color: the shuttles' state taken together, as the
// daemon works it out for its icon in the top bar, in the dashboard's
// colors for it; sky when all is well, faint while the daemon is down.
func (m *model) busTint() lipgloss.Style {
	switch {
	case m.down != nil:
		return faint
	case !m.loaded:
		return busStyle
	}
	if style, ok := stateStyles[daemon.Overall(m.shuttles, m.pausedAll, m.configErr)]; ok {
		return style
	}
	return busStyle
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
