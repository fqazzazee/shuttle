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
	busStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("4"))
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
		if m.wizard != nil {
			return m.wizard.lines(m, room)
		}
		return m.shuttlesTable(room)
	case tabConflicts:
		return m.conflictsTable(room)
	case tabActivity:
		return m.activityLines(room)
	case tabProfiles:
		return m.profilesLines(room)
	default:
		return m.sharesLines(room)
	}
}

// withBus is an empty state on a first run: shuttle's icon, then text.
func withBus(text ...string) []string {
	lines := make([]string, 0, len(present.Bus)+1+len(text))
	for _, l := range present.Bus {
		lines = append(lines, busStyle.Render(l))
	}
	return append(append(lines, ""), text...)
}

// Column widths of the shuttles table; DETAILS takes the rest.
const (
	modeWidth     = 8
	stateWidth    = 10
	sizeWidth     = 10
	filesWidth    = 10
	lastRunWidth  = 10
	progressWidth = 24
)

func (m *model) shuttlesTable(room int) []string {
	switch {
	case !m.loaded || m.down != nil:
		return m.waiting()
	case len(m.shuttles) == 0:
		return withBus("No shuttles yet.", "Press n to add one.")
	}
	nameWidth := len("NAME")
	for i := range m.shuttles {
		nameWidth = max(nameWidth, lipgloss.Width(m.shuttles[i].Name))
	}
	nameWidth += 2
	row := func(name, mode, state, size, files, lastRun, progress, details string) string {
		return pad(name, nameWidth) + pad(mode, modeWidth) + pad(state, stateWidth) + pad(size, sizeWidth) +
			pad(files, filesWidth) + pad(lastRun, lastRunWidth) + pad(progress, progressWidth) + details
	}
	lines := []string{headStyle.Render(row("NAME", "MODE", "STATE", "SIZE", "FILES", "LAST RUN", "PROGRESS", "DETAILS"))}

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
		size, files := present.Synced(st)
		line := row(st.Name, present.ModeArrow(st.Mode)+" "+string(st.Mode), state, size, files,
			present.Ago(st.LastRun, now), progress(&st.Progress), present.Attention(st))
		if st.Name == m.selected {
			line = selected.Render(ansi.Truncate(pad(line, m.width), m.width, "…"))
		}
		lines = append(lines, line)
	}
	return lines
}

// waiting is what a tab shows until the daemon answers.
func (m *model) waiting() []string {
	if m.down == nil {
		return []string{faint.Render("Asking the daemon…")}
	}
	return []string{
		badStyle.Render(errorText(m.down)),
		"",
		"This screen catches up by itself once the daemon runs.",
	}
}

// Column widths of the conflicts table; WHY takes the rest.
const copyWidth = 28

func (m *model) conflictsTable(room int) []string {
	if !m.loaded || m.down != nil {
		return m.waiting()
	}
	items := m.items()
	if len(items) == 0 {
		return []string{"Nothing waiting for you: no conflicts held, no deletions held back."}
	}
	nameWidth, pathWidth := len("SHUTTLE"), len("PATH")
	for i := range items {
		nameWidth = max(nameWidth, lipgloss.Width(items[i].shuttle))
		if c := items[i].conflict; c != nil {
			pathWidth = max(pathWidth, lipgloss.Width(c.Path))
		}
	}
	nameWidth += 2
	pathWidth = min(pathWidth, 40) + 2
	row := func(name, path, local, remote, why string) string {
		return pad(name, nameWidth) + pad(path, pathWidth) + pad(local, copyWidth) + pad(remote, copyWidth) + why
	}
	lines := []string{headStyle.Render(row("SHUTTLE", "PATH", "THIS COMPUTER", "THE SHARE", "WHY"))}

	rows := room - 3 // the header, and a blank line and the selected row's explanation below
	first := 0
	if i := m.itemIndex(); i >= rows {
		first = i - rows + 1
	}
	now := m.now()
	for i := first; i < len(items) && i < first+rows; i++ {
		it := &items[i]
		var line string
		if c := it.conflict; c != nil {
			why := c.Reason
			if m.resolving[it.key()] {
				why = "resolving…"
			}
			line = row(it.shuttle, c.Path, copyText(c.Local, c.Remote, now), copyText(c.Remote, c.Local, now), why)
		} else {
			line = row(it.shuttle, "", "", "",
				present.Plural(it.deletions, "deletion")+" held back: too many at once for the deletion guard")
		}
		if it.key() == m.conflictSel {
			line = selected.Render(ansi.Truncate(pad(line, m.width), m.width, "…"))
		}
		lines = append(lines, line)
	}
	if it := m.currentItem(items); it != nil {
		lines = append(lines, "", faint.Render(explain(it)))
	}
	return lines
}

// explain says what can be done about an item.
func explain(it *item) string {
	switch it.choices() {
	case "a":
		return "a lets the next run make these deletions. Until then, nothing is deleted on either side."
	case "b":
		return "A file against a folder: b keeps both, renaming the file beside the folder."
	case "lrb":
		return "l keeps this computer's copy, r the share's; b keeps both, saving the older beside the newer under a conflict name."
	}
	return "Two names that differ only in case, which the share cannot hold apart: rename one of them on this computer."
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
	case m.question != "":
		return heldStyle.Render(m.question + " (y/n)")
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
	if m.question != "" {
		return keyStyle.Render("y") + " " + faint.Render("yes") + "  " + keyStyle.Render("n") + " " + faint.Render("no")
	}
	if m.tab == tabConflicts && !m.help {
		if it := m.currentItem(m.items()); it != nil && !m.resolving[it.key()] {
			for _, c := range it.choices() {
				keys = append(keys, [2]string{string(c), choiceNames[c]})
			}
		}
	}
	// A form's keys are all there is while it is open.
	var form [][2]string
	if m.tab == tabProfiles && !m.help && m.settings != nil {
		form = [][2]string{{"tab", "next"}, {"←→", "choose"}, {"ctrl+s", "save"}, {"esc", "cancel"}}
	}
	if m.tab == tabShuttles && !m.help && m.wizard != nil {
		form = m.wizard.keys()
	}
	if m.tab == tabShares && !m.help && m.form != nil {
		form = [][2]string{{"tab", "next"}, {"←→", "type"}, {"ctrl+t", "test"}, {"ctrl+s", "save"}, {"esc", "cancel"}}
	}
	if form != nil {
		parts := make([]string, 0, len(form))
		for _, k := range form {
			parts = append(parts, keyStyle.Render(k[0])+" "+faint.Render(k[1]))
		}
		return strings.Join(parts, "  ")
	}
	if m.tab == tabProfiles && !m.help {
		keys = append(keys, [2]string{"n", "new"}, [2]string{"e", "edit"}, [2]string{"c", "copy"}, [2]string{"x", "delete"})
	}
	if m.tab == tabShares && !m.help {
		keys = append(keys, [2]string{"n", "new"}, [2]string{"e", "edit"}, [2]string{"t", "test"}, [2]string{"x", "delete"})
	}
	if m.tab == tabActivity && !m.help {
		keys = append(keys, [2]string{"↑↓", "scroll"}, [2]string{"f", "filter"})
	}
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
		keys = append(keys, [2]string{"s", "sync"}, [2]string{"p", pause}, [2]string{"d", enable},
			[2]string{"n", "new"}, [2]string{"e", "edit"}, [2]string{"x", "delete"})
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

var choiceNames = map[rune]string{'l': "keep this computer's", 'r': "keep the share's", 'b': "keep both", 'a': "allow the deletions"}

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
		"  n, e or enter, x      add a shuttle, edit it, delete it (its files stay on both sides)",
		"",
		"  On the Conflicts tab:",
		"  l, r                  keep this computer's copy, or the share's, replacing the other",
		"  b                     keep both: the older is saved beside the newer under a conflict name",
		"  a                     let the deletions the deletion guard held back go ahead",
		"",
		"  On the Activity tab:",
		"  up/down, pgup/pgdown  scroll; home/end, g/G: the newest, the oldest",
		"  f                     show one shuttle, then the next, then every shuttle again",
		"",
		"  Adding or editing a shuttle, on the share:",
		"  right/left, l/h       open or close a folder; left on a top row goes up a folder",
		"  o, backspace          make the chosen folder the shuttle's folder; go up a folder",
		"  m                     sync everything, only what is marked, or all but what is marked",
		"  space                 mark a file or folder, or unmark it; unmarking one thing in a",
		"                        marked folder marks the rest of the folder instead",
		"",
		"  On the Profiles tab:",
		"  n, e or enter, c, x   add a profile, edit it, copy it, delete it (only when no shuttle uses it)",
		"  in the form: tab, shift+tab to move; ←/→ choose; a setting left empty is not set,",
		"  and its default applies; ctrl+s saves; esc leaves it unsaved",
		"",
		"  On the Shares tab:",
		"  n, e or enter, x      add a share, edit it, delete it (only when no shuttle uses it)",
		"  t                     test the share: log in with the stored password and read it",
		"  in the form: tab, shift+tab to move; ←/→ change the type; ctrl+t tests what is typed;",
		"  ctrl+s or enter on the last field saves; esc leaves it unsaved",
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
