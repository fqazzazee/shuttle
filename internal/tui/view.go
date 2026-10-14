// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package tui

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/fqazzazee/shuttle/internal/daemon"
	"github.com/fqazzazee/shuttle/internal/engine"
	"github.com/fqazzazee/shuttle/internal/present"
)

func (m *model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.WindowTitle = "shuttle"
	return v
}

// render lays out the screen: the header, the tab's content, a status
// line and the keys; with the help or a question over it all but the keys.
func (m *model) render() string {
	header := m.headerLines()
	status, keys := m.statusLine(), m.keyLines()
	room := m.room(len(keys))
	body := m.content(room)
	if len(body) > room {
		body = body[:room]
	}
	lines := append(append(header, ""), body...)
	for len(lines) < m.height-1-len(keys) {
		lines = append(lines, "")
	}
	lines = append(append(lines, status), keys...)
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, m.width, "…")
	}
	switch top := len(header) + 1; {
	case m.question != "":
		return m.overlaid(lines, len(keys), m.questionBox(), top, room)
	case m.help:
		return m.overlaid(lines, len(keys), m.helpBox(), top, room)
	}
	return strings.Join(lines, "\n")
}

// keyLines are the keys, on two lines when one is too narrow for them; on
// one, cut short, when the terminal is too short to spare the room.
func (m *model) keyLines() []string {
	lines := strings.Split(m.keysLine(), "\n")
	most := 2
	if m.height < 12 {
		most = 1
	}
	if len(lines) > most {
		lines = append(lines[:most-1], strings.Join(lines[most-1:], "  "))
	}
	return lines
}

// room is how many lines a tab has: all but the header and a blank line,
// the status line and the keys below.
func (m *model) room(keys int) int {
	header := 1
	if m.tall() {
		header = busHeight
	}
	return max(m.height-header-2-keys, 1)
}

// tabsLine is the tabs after prefix. In a narrow terminal, the tabs but
// the open one are their numbers alone.
func (m *model) tabsLine(prefix string) string {
	if h := m.tabs(prefix, false); lipgloss.Width(h) <= m.width {
		return h
	}
	return m.tabs(prefix, true)
}

// tabs is the tabs after prefix; short, the tabs but the open one by
// their numbers alone.
func (m *model) tabs(prefix string, short bool) string {
	parts := []string{prefix}
	for i, name := range tabNames {
		if short && i != m.tab {
			name = ""
		}
		label := strings.TrimRight(fmt.Sprintf(" %d %s", i+1, name), " ")
		if i == tabConflicts {
			if n := m.heldConflicts(); n > 0 {
				label += fmt.Sprintf(" (%d)", n)
			}
		}
		label += " "
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
	return append(append(bigBus(), ""), text...)
}

// bigBus is present.Bus in color, as the app icon has it: the body in sky,
// the sync arrows on a dark destination sign, a windscreen that glows, the
// lights in amber, the grille and wheels dim. Stripped of its colors, it is
// present.Bus (TestBigBusIsTheBus).
func bigBus() []string {
	body := busStyle.Render
	return []string{
		body("  .----------."),
		body("  |  ") + signStyle.Render("[ ⇄  ]") + body("  |"),
		body(" =| .------. |="),
		body("  | |") + windowStyle.Render("      ") + body("| |"),
		body("  | '------' |"),
		body("  | ") + lightStyle.Render("o") + "  " + faint.Render("==") + "  " + lightStyle.Render("o") + body(" |"),
		body("  '-") + faint.Render("[]") + body("----") + faint.Render("[]") + body("-'"),
	}
}

// Column widths of the shuttles table; DETAILS takes the rest.
const (
	modeWidth     = 8
	arrowWidth    = 3 // MODE as its arrow alone, in a narrow terminal
	stateWidth    = 10
	shareWidth    = 9
	sizeWidth     = 10
	filesWidth    = 10
	lastRunWidth  = 10
	progressWidth = 26
	detailsMin    = 20 // what DETAILS keeps before columns go
)

// column is one of the shuttles table's columns, but DETAILS.
type column struct {
	title string
	width int
	least int // what it can shrink to in a narrow terminal; 0: it cannot
	cell  func(st *daemon.Status) string
}

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
	now := m.now()
	// PROGRESS can shrink to what it shows now.
	progressLeast := len("PROGRESS") + 2
	for i := range m.shuttles {
		progressLeast = max(progressLeast, lipgloss.Width(progress(&m.shuttles[i].Progress, m.frame))+2)
	}
	cols := []column{
		{"NAME", nameWidth, 0, func(st *daemon.Status) string { return st.Name }},
		{"MODE", modeWidth, arrowWidth, func(st *daemon.Status) string {
			return present.ModeArrow(st.Mode) + " " + string(st.Mode)
		}},
		{"STATE", stateWidth, 0, func(st *daemon.Status) string {
			if style, ok := stateStyles[st.State]; ok && st.Name != m.selected {
				return style.Render(string(st.State))
			}
			return string(st.State)
		}},
		{"SHARE", shareWidth, 0, func(st *daemon.Status) string {
			if st.Share == daemon.ShareOffline && st.Name != m.selected {
				return badStyle.Render(st.Share)
			}
			return cmp.Or(st.Share, "—")
		}},
		{"SIZE", sizeWidth, 0, func(st *daemon.Status) string { size, _ := present.Synced(st); return size }},
		{"FILES", filesWidth, 0, func(st *daemon.Status) string { _, files := present.Synced(st); return files }},
		{"LAST RUN", lastRunWidth, 0, func(st *daemon.Status) string { return present.Ago(st.LastRun, now) }},
		{"PROGRESS", progressWidth, min(progressLeast, progressWidth), func(st *daemon.Status) string {
			return progress(&st.Progress, m.frame)
		}},
	}
	cols = fitColumns(cols, m.width)
	row := func(cells func(c *column) string, details string) string {
		var b strings.Builder
		for i := range cols {
			b.WriteString(pad(cells(&cols[i]), cols[i].width))
		}
		return b.String() + details
	}
	lines := []string{headStyle.Render(row(func(c *column) string { return c.title }, "DETAILS"))}

	// The selected shuttle in full, under the table, when there is room
	// for it and a few rows: the table comes first.
	rows := room - 1
	panel := m.panel()
	if len(m.shuttles)+len(panel) > rows {
		rows = max(rows-len(panel), min(len(m.shuttles), 3))
	}
	// Scroll so the selected shuttle shows.
	first := 0
	if i := m.index(); i >= rows {
		first = i - rows + 1
	}
	for i := first; i < len(m.shuttles) && i < first+rows; i++ {
		st := &m.shuttles[i]
		line := row(func(c *column) string { return c.cell(st) }, present.Attention(st))
		if st.Name == m.selected {
			// Without the bar's colors: their resets would end the
			// highlight halfway along the row.
			line = selected.Render(ansi.Truncate(pad(ansi.Strip(line), m.width), m.width, "…"))
		}
		lines = append(lines, line)
	}
	return append(lines, panel...)
}

// fitColumns leaves DETAILS at least detailsMin columns of width, as far
// as it can: FILES goes first, then SIZE; then MODE keeps only its arrow,
// and PROGRESS shrinks to what it shows; then SHARE goes, whose offline
// STATE also says, then LAST RUN. NAME, STATE and PROGRESS stay.
func fitColumns(cols []column, width int) []column {
	used := func() int {
		n := 0
		for i := range cols {
			n += cols[i].width
		}
		return n
	}
	for _, step := range []string{"FILES", "SIZE", "MODE", "PROGRESS", "SHARE", "LAST RUN"} {
		if width-used() >= detailsMin {
			break
		}
		i := slices.IndexFunc(cols, func(c column) bool { return c.title == step })
		switch step {
		case "MODE":
			cols[i].title, cols[i].width = "", cols[i].least
			cols[i].cell = func(st *daemon.Status) string { return present.ModeArrow(st.Mode) }
		case "PROGRESS":
			cols[i].width = cols[i].least
		default:
			cols = slices.Delete(cols, i, i+1)
		}
	}
	return cols
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
func progress(p *daemon.Progress, frame int) string {
	var s string
	spinner := spinnerFrames[frame%len(spinnerFrames)]
	switch p.Phase {
	case "":
		return ""
	case engine.PhaseScanning:
		s = fmt.Sprintf("%s scanning %d", spinner, p.Done)
	case engine.PhaseComparing:
		s = fmt.Sprintf("%s comparing %d/%d", spinner, p.Done, p.Total)
	default:
		switch {
		case p.TotalBytes > 0:
			s = bar(float64(p.Bytes)/float64(p.TotalBytes)) + fmt.Sprintf(" %3d%%", p.Bytes*100/p.TotalBytes)
		case p.Total > 0:
			s = bar(float64(p.Done)/float64(p.Total)) + fmt.Sprintf(" %3d%%", p.Done*100/p.Total)
		default:
			s = spinner + " syncing"
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

// spinnerFrames turn while a run's length is not known yet.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// spins says whether a run's progress shows a spinner rather than a bar.
func spins(p *daemon.Progress) bool {
	switch p.Phase {
	case "":
		return false
	case engine.PhaseScanning, engine.PhaseComparing:
		return true
	}
	return p.TotalBytes == 0 && p.Total == 0
}

// barWidth is a progress bar's length, in columns: eight steps each.
const barWidth = 8

// bar is done, a fraction, as a progress bar: filled a column's eighth at
// a time, along the palette's gradient, on a track. The shapes say it
// without the colors, which a selected row has not.
func bar(done float64) string {
	steps := min(max(int(done*8*barWidth), 0), 8*barWidth)
	var b strings.Builder
	for i := range barWidth {
		b.WriteString(barCells[i][min(max(steps-8*i, 0), 8)])
	}
	return b.String()
}

// barEighths are a column filled by eighths, from none to full.
var barEighths = [9]string{"", "▏", "▎", "▍", "▌", "▋", "▊", "▉", "█"}

// barCellsFor is each column of a progress bar in each of its fills: in
// p's gradient on a filled track; with p nil, in the terminal's own colors
// on a faint one, which shows without colors.
func barCellsFor(p *palette) (cells [barWidth][9]string) {
	empty := faint.Render("░")
	fill := func(int) lipgloss.Style { return busyStyle }
	if p != nil {
		empty = lipgloss.NewStyle().Background(p.surface).Render(" ")
		colors := p.gradient(barWidth)
		fill = func(i int) lipgloss.Style { return lipgloss.NewStyle().Foreground(colors[i]).Background(p.surface) }
	}
	for i := range barWidth {
		cells[i][0] = empty
		for e := 1; e < len(barEighths); e++ {
			cells[i][e] = fill(i).Render(barEighths[e])
		}
	}
	return cells
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
		return wrapKeys(parts, m.width)
	}
	if m.tab == tabProfiles && !m.help {
		keys = append(keys, [2]string{"n", "new"}, [2]string{"e", "edit"}, [2]string{"c", "copy"}, [2]string{"x", "delete"})
	}
	if m.tab == tabShares && !m.help {
		keys = append(keys, [2]string{"n", "new"}, [2]string{"e", "edit"}, [2]string{"t", "test"}, [2]string{"x", "delete"})
	}
	if m.help {
		keys = append(keys, [2]string{"↑↓", "scroll"}, [2]string{"esc", "close"})
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
	return wrapKeys(parts, m.width)
}

// wrapKeys puts the keys on as many lines as the width needs, rather than
// cut short the last ones: help and quit.
func wrapKeys(parts []string, width int) string {
	var lines []string
	line := ""
	for _, p := range parts {
		switch {
		case line == "":
			line = p
		case lipgloss.Width(line)+2+lipgloss.Width(p) > width:
			lines = append(lines, line)
			line = p
		default:
			line += "  " + p
		}
	}
	return strings.Join(append(lines, line), "\n")
}

var choiceNames = map[rune]string{'l': "keep this computer's", 'r': "keep the share's", 'b': "keep both", 'a': "allow the deletions"}

// helpBox is the help in its frame, wrapped to it, from where it is
// scrolled to; the last line says when there is more below.
func (m *model) helpBox() string {
	width, room := m.helpSize()
	lines := m.helpWrapped(width)
	lines = lines[min(m.helpScroll, max(len(lines)-room, 0)):]
	if len(lines) > room {
		lines = append(lines[:max(room-1, 0)], faint.Render("  ↓ more"))
	}
	return framed(&helpFrame, lines, width)
}

// helpSize is the room inside the help's frame: the frame as tall as the
// tab's room, as wide as the terminal but a margin, up to helpWidth.
func (m *model) helpSize() (width, height int) {
	return max(min(m.width-2*margin, helpWidth)-frameWidth, 1), max(m.room(len(m.keyLines()))-frameHeight, 1)
}

// helpWrapped is the help wrapped to width.
func (m *model) helpWrapped(width int) []string {
	var out []string
	for _, l := range helpLines() {
		out = append(out, wrapHelp(l, width)...)
	}
	return out
}

// helpKey scrolls the help.
func (m *model) helpKey(k string) {
	width, room := m.helpSize()
	last := max(len(m.helpWrapped(width))-room, 0)
	switch k {
	case "up", "k":
		m.helpScroll--
	case "down", "j":
		m.helpScroll++
	case "pgup":
		m.helpScroll -= room
	case "pgdown", "space":
		m.helpScroll += room
	case "home", "g":
		m.helpScroll = 0
	case "end", "G":
		m.helpScroll = last
	}
	m.helpScroll = min(max(m.helpScroll, 0), last)
}

// wrapHelp wraps a line of the help to width, going on under its
// description: the text after the gap that ends the keys, if it has one.
func wrapHelp(line string, width int) []string {
	if lipgloss.Width(line) <= width {
		return []string{line}
	}
	indent := len(line) - len(strings.TrimLeft(line, " "))
	if i := strings.Index(line[indent:], "   "); i >= 0 {
		rest := line[indent+i:]
		indent += i + len(rest) - len(strings.TrimLeft(rest, " "))
	}
	if indent > width/2 {
		indent = 2
	}
	wrapped := strings.Split(ansi.Wrap(line[indent:], width-indent, " "), "\n")
	out := []string{line[:indent] + wrapped[0]}
	for _, w := range wrapped[1:] {
		out = append(out, strings.Repeat(" ", indent)+w)
	}
	return out
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
		"  Adding or editing a shuttle, on the share and on this computer:",
		"  right/left, l/h       open or close a folder; left on a top row goes up a folder",
		"  o, backspace          make the chosen folder the shuttle's folder; go up a folder",
		"  t                     type the folder's path instead; on this computer, ~ is your home",
		"  m                     sync everything, only what is marked, or all but what is marked",
		"  space                 mark a file or folder, or unmark it; unmarking one thing in a",
		"                        marked folder marks the rest of the folder instead; a mark made",
		"                        on one side shows on the other",
		"",
		"  On the Profiles tab:",
		"  n, e or enter, c, x   add a profile, edit it, copy it, delete it (only when no shuttle uses it)",
		"  in the form: tab, shift+tab to move; ←/→ choose; a setting left empty is not set, and its " +
			"default applies; ctrl+s saves; esc leaves it unsaved",
		"",
		"  On the Shares tab:",
		"  n, e or enter, x      add a share, edit it, delete it (only when no shuttle uses it)",
		"  t                     test the share: log in with the stored password and read it",
		"  in the form: tab, shift+tab to move; ←/→ change the type; ctrl+t tests what is typed; " +
			"ctrl+s or enter on the last field saves; esc leaves it unsaved",
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
