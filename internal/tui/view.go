// SPDX-License-Identifier: GPL-2.0-or-later

package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/fqazzazee/shuttle/internal/daemon"
	"github.com/fqazzazee/shuttle/internal/engine"
	"github.com/fqazzazee/shuttle/internal/present"
)

// Styles use the terminal's own 16 colors, so they follow its theme.
var (
	titleStyle  = lipgloss.NewStyle().Bold(true)
	activeTab   = lipgloss.NewStyle().Bold(true).Reverse(true)
	headStyle   = lipgloss.NewStyle().Bold(true)
	faint       = lipgloss.NewStyle().Faint(true)
	selected    = lipgloss.NewStyle().Reverse(true)
	badStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	heldStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	busyStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	goodStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	keyStyle    = lipgloss.NewStyle().Bold(true)
	stateStyles = map[daemon.State]lipgloss.Style{
		daemon.StateError:    badStyle,
		daemon.StateOffline:  badStyle,
		daemon.StateHeld:     heldStyle,
		daemon.StateRunning:  busyStyle,
		daemon.StatePaused:   faint,
		daemon.StateDisabled: faint,
	}
)

func (m *model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.WindowTitle = "shuttle"
	return v
}

// render lays out the screen: tabs, the tab's content, a status line and
// the keys.
func (m *model) render() string {
	header := m.header()
	status, keys := m.statusLine(), m.keysLine()
	room := max(m.height-4, 1) // the header and a blank line, the two lines below
	var body []string
	if m.help {
		body = helpLines()
	} else {
		body = m.content(room)
	}
	if len(body) > room {
		body = body[:room]
	}
	lines := append([]string{header, ""}, body...)
	for len(lines) < m.height-2 {
		lines = append(lines, "")
	}
	lines = append(lines, status, keys)
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, m.width, "…")
	}
	return strings.Join(lines, "\n")
}

func (m *model) header() string {
	parts := []string{titleStyle.Render("shuttle") + " "}
	for i, name := range tabNames {
		label := fmt.Sprintf(" %d %s ", i+1, name)
		if i == tabConflicts {
			if n := m.heldConflicts(); n > 0 {
				label = fmt.Sprintf(" %d %s (%d) ", i+1, name, n)
			}
		}
		if i == m.tab {
			label = activeTab.Render(label)
		}
		parts = append(parts, label)
	}
	return strings.Join(parts, "")
}

func (m *model) heldConflicts() int {
	n := 0
	for i := range m.shuttles {
		n += present.HeldConflicts(&m.shuttles[i])
	}
	return n
}

func (m *model) content(room int) []string {
	switch m.tab {
	case tabShuttles:
		return m.shuttlesTable(room)
	case tabConflicts:
		return []string{
			"Not built yet: held conflicts will be listed here, with both copies,",
			"to keep this computer's, the share's, or both.",
			"",
			fmt.Sprintf("Waiting for you now: %s.", present.Plural(m.heldConflicts(), "conflict")),
		}
	case tabActivity:
		return []string{"Not built yet: what each shuttle did lately, file by file.", "", "Until then: shuttle activity"}
	case tabProfiles:
		return []string{"Not built yet: profiles to create, edit and duplicate, and the shuttles using each."}
	default:
		return []string{"Not built yet: shares to add and edit, test, and store passwords for."}
	}
}

// Column widths of the shuttles table; DETAILS takes the rest.
const (
	modeWidth     = 8
	stateWidth    = 10
	lastRunWidth  = 10
	progressWidth = 24
)

func (m *model) shuttlesTable(room int) []string {
	switch {
	case !m.loaded:
		return []string{faint.Render("Asking the daemon…")}
	case m.down != nil:
		return []string{
			badStyle.Render(errorText(m.down)),
			"",
			"This screen catches up by itself once the daemon runs.",
		}
	case len(m.shuttles) == 0:
		return []string{"No shuttles in the config file yet."}
	}
	nameWidth := len("NAME")
	for i := range m.shuttles {
		nameWidth = max(nameWidth, lipgloss.Width(m.shuttles[i].Name))
	}
	nameWidth += 2
	row := func(name, mode, state, lastRun, progress, details string) string {
		return pad(name, nameWidth) + pad(mode, modeWidth) + pad(state, stateWidth) +
			pad(lastRun, lastRunWidth) + pad(progress, progressWidth) + details
	}
	lines := []string{headStyle.Render(row("NAME", "MODE", "STATE", "LAST RUN", "PROGRESS", "DETAILS"))}

	// Scroll so the selected shuttle shows.
	rows := room - 1
	first := 0
	if i := m.index(); i >= rows {
		first = i - rows + 1
	}
	now := m.now()
	for i := first; i < len(m.shuttles) && i < first+rows; i++ {
		st := &m.shuttles[i]
		state := string(st.State)
		if style, ok := stateStyles[st.State]; ok && st.Name != m.selected {
			state = style.Render(state)
		}
		line := row(st.Name, present.ModeArrow(st.Mode)+" "+string(st.Mode), state,
			present.Ago(st.LastRun, now), progress(&st.Progress), present.Attention(st))
		if st.Name == m.selected {
			line = selected.Render(ansi.Truncate(pad(line, m.width), m.width, "…"))
		}
		lines = append(lines, line)
	}
	return lines
}

// progress is a run's progress in a few characters: "45% ↑ 2.1 MB/s".
func progress(p *daemon.Progress) string {
	var s string
	switch p.Phase {
	case "":
		return ""
	case engine.PhaseScanning:
		s = fmt.Sprintf("scanning %d", p.Done)
	case engine.PhaseComparing:
		s = fmt.Sprintf("comparing %d/%d", p.Done, p.Total)
	default:
		switch {
		case p.TotalBytes > 0:
			s = fmt.Sprintf("%d%%", p.Bytes*100/p.TotalBytes)
		case p.Total > 0:
			s = fmt.Sprintf("%d%%", p.Done*100/p.Total)
		default:
			s = "syncing"
		}
	}
	if p.RateUp > 0 {
		s += " ↑ " + present.Size(p.RateUp) + "/s"
	}
	if p.RateDown > 0 {
		s += " ↓ " + present.Size(p.RateDown) + "/s"
	}
	return s
}

// statusLine says what needs saying most: a broken config, the last
// action, everything paused; and the overall transfer rates.
func (m *model) statusLine() string {
	var left string
	fresh := m.message != "" && m.now().Sub(m.messageAt) < messageFor
	switch {
	case m.configErr != "":
		left = badStyle.Render("The config file has an error, so the previous settings are in use: " + firstLine(m.configErr))
	case fresh && m.failed:
		left = badStyle.Render(m.message)
	case fresh:
		left = goodStyle.Render(m.message)
	case m.pausedAll:
		left = heldStyle.Render("All shuttles are paused.")
	}
	var up, down int64
	for i := range m.shuttles {
		up += m.shuttles[i].Progress.RateUp
		down += m.shuttles[i].Progress.RateDown
	}
	if up == 0 && down == 0 {
		return left
	}
	right := fmt.Sprintf("↑ %s/s  ↓ %s/s", present.Size(up), present.Size(down))
	gap := max(m.width-lipgloss.Width(left)-lipgloss.Width(right), 1)
	return left + strings.Repeat(" ", gap) + right
}

func (m *model) keysLine() string {
	var keys [][2]string
	if m.tab == tabShuttles && !m.help {
		pause, enable := "pause", "disable"
		if st := m.current(); st != nil {
			if st.Paused && !m.pausedAll {
				pause = "resume"
			}
			if st.State == daemon.StateDisabled {
				enable = "enable"
			}
		}
		keys = append(keys, [2]string{"s", "sync"}, [2]string{"p", pause}, [2]string{"d", enable})
	}
	all := "pause all"
	if m.pausedAll {
		all = "resume all"
	}
	keys = append(keys, [2]string{"P", all}, [2]string{"1-5", "tabs"}, [2]string{"?", "help"}, [2]string{"q", "quit"})
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, keyStyle.Render(k[0])+" "+faint.Render(k[1]))
	}
	return strings.Join(parts, "  ")
}

func helpLines() []string {
	return []string{
		headStyle.Render("Keys"),
		"",
		"  1-5, tab, shift+tab   choose a tab",
		"  up/down, k/j          choose a shuttle; home/end, g/G: the first, the last",
		"  s                     sync the shuttle now",
		"  p                     pause or resume the shuttle: a run in progress finishes its file",
		"  P                     pause or resume every shuttle",
		"  d                     disable or enable the shuttle, saved in the config file",
		"  ?, esc                show or hide this help",
		"  q, ctrl+c             quit; the daemon carries on",
	}
}

// pad pads s with spaces to width columns, or cuts it short.
func pad(s string, width int) string {
	if w := lipgloss.Width(s); w < width {
		return s + strings.Repeat(" ", width-w)
	}
	return ansi.Truncate(s, width-1, "…") + " "
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i] + " …"
	}
	return s
}
