// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package tui

import (
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/fqazzazee/shuttle/internal/activity"
	"github.com/fqazzazee/shuttle/internal/daemon"
)

// activityShown is how many of the newest entries the Activity tab asks for.
const activityShown = 500

type activityMsg struct {
	filter  string // the shuttle asked for; "" for all
	entries []activity.Entry
	err     error
}

// loadActivity asks for the newest entries, of the filtered shuttle or all.
func (m *model) loadActivity() tea.Cmd {
	filter := m.filter
	return func() tea.Msg {
		ctx, cancel := m.call()
		defer cancel()
		es, err := m.c.Activity(ctx, filter, activityShown)
		return activityMsg{filter, es, err}
	}
}

// gotActivity takes in entries. Scrolled down the list, the view stays on
// the same entries as new ones arrive at the top.
func (m *model) gotActivity(msg activityMsg) {
	if msg.filter != m.filter {
		return // asked for before the filter changed
	}
	m.activityErr = msg.err
	if msg.err != nil {
		return
	}
	if m.scroll > 0 && len(m.entries) > 0 {
		if i := slices.IndexFunc(msg.entries, func(e activity.Entry) bool { return same(&e, &m.entries[0]) }); i > 0 {
			m.scroll += i
		}
	}
	m.entries = msg.entries
	m.scroll = min(m.scroll, max(len(m.entries)-1, 0))
}

func same(a, b *activity.Entry) bool {
	return a.Time.Equal(b.Time) && a.Shuttle == b.Shuttle && a.Kind == b.Kind && a.Path == b.Path && a.Text == b.Text
}

func (m *model) activityKey(k string) tea.Cmd {
	page := max(m.room(len(m.keyLines()))-2, 1) // the title and a blank line above the entries
	switch k {
	case "up", "k":
		m.scroll--
	case "down", "j":
		m.scroll++
	case "pgup":
		m.scroll -= page
	case "pgdown", "space":
		m.scroll += page
	case "home", "g":
		m.scroll = 0
	case "end", "G":
		m.scroll = len(m.entries)
	case "f":
		m.filter = nextFilter(m.filter, m.shuttles)
		m.entries, m.scroll, m.activityErr = nil, 0, nil
		return m.loadActivity()
	}
	m.scroll = min(max(m.scroll, 0), max(len(m.entries)-1, 0))
	return nil
}

// nextFilter is the filter after f: every shuttle, then each in turn.
func nextFilter(f string, shuttles []daemon.Status) string {
	i := slices.IndexFunc(shuttles, func(st daemon.Status) bool { return st.Name == f })
	if i+1 < len(shuttles) {
		return shuttles[i+1].Name
	}
	return ""
}

func (m *model) activityLines(room int) []string {
	if !m.loaded || m.down != nil {
		return m.waiting()
	}
	title := "Every shuttle"
	if m.filter != "" {
		title = m.filter
	}
	lines := []string{headStyle.Render(title) + faint.Render("  (f: "+nextLabel(m.filter, m.shuttles)+")"), ""}
	switch {
	case m.activityErr != nil:
		return append(lines, badStyle.Render(errorText(m.activityErr)))
	case m.entries == nil:
		return append(lines, faint.Render("Asking the daemon…"))
	case len(m.entries) == 0:
		return append(lines, "Nothing yet: entries appear as shuttles copy, delete or run into problems.")
	}
	now := m.now()
	nameWidth, timeWidth := 0, 0
	for i := range m.entries {
		nameWidth = max(nameWidth, lipgloss.Width(m.entries[i].Shuttle))
		timeWidth = max(timeWidth, len(stamp(m.entries[i].Time, now)))
	}
	for i := m.scroll; i < len(m.entries) && len(lines) < room; i++ {
		e := &m.entries[i]
		what := e.Text
		if e.Path != "" {
			what = e.Path + ": " + e.Text
		}
		if style, ok := kindStyles[e.Kind]; ok {
			what = style.Render(what)
		}
		line := pad(stamp(e.Time, now), timeWidth+2)
		if m.filter == "" {
			line += pad(e.Shuttle, nameWidth+2)
		}
		lines = append(lines, line+what)
	}
	return lines
}

func nextLabel(f string, shuttles []daemon.Status) string {
	if n := nextFilter(f, shuttles); n != "" {
		return "show " + n + " only"
	}
	return "show every shuttle"
}

// stamp is an entry's time: the time alone today, the date too before.
func stamp(t, now time.Time) string {
	t, now = t.Local(), now.Local()
	switch {
	case t.YearDay() == now.YearDay() && t.Year() == now.Year():
		return t.Format("15:04:05")
	case t.Year() == now.Year():
		return t.Format("Jan 2 15:04:05")
	}
	return t.Format("2006-01-02 15:04")
}
