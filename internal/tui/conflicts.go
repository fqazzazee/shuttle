// SPDX-License-Identifier: GPL-2.0-or-later

package tui

import (
	"context"
	"fmt"
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/fqazzazee/shuttle/internal/engine"
	"github.com/fqazzazee/shuttle/internal/fsys"
	"github.com/fqazzazee/shuttle/internal/present"
)

// resolveTimeout bounds a resolve: it waits for a run in progress to
// finish its file.
const resolveTimeout = 2 * time.Minute

// item is a row of the Conflicts tab: a conflict waiting for the user, or a
// shuttle's deletions the deletion guard held back.
type item struct {
	shuttle   string
	conflict  *engine.Issue // nil for held deletions
	deletions int
}

func (it *item) key() string {
	if it.conflict == nil {
		return it.shuttle + "\x00"
	}
	return it.shuttle + "\x00" + it.conflict.Path
}

// choices are what can be done about an item: "l", "r" and "b" for a
// conflict between two files, "b" for a file against a folder, "a" for
// held deletions, none when only the user can sort it out (two names that
// differ only in case).
func (it *item) choices() string {
	c := it.conflict
	switch {
	case c == nil:
		return "a"
	case c.Local == nil || c.Remote == nil:
		return ""
	case c.Local.Kind != c.Remote.Kind:
		return "b"
	}
	return "lrb"
}

// items lists the Conflicts tab's rows, shuttle by shuttle.
func (m *model) items() []item {
	var out []item
	for i := range m.shuttles {
		st := &m.shuttles[i]
		for j := range st.Conflicts {
			if st.Conflicts[j].Outcome == "held" {
				out = append(out, item{shuttle: st.Name, conflict: &st.Conflicts[j]})
			}
		}
		if st.HeldDeletions > 0 {
			out = append(out, item{shuttle: st.Name, deletions: st.HeldDeletions})
		}
	}
	return out
}

func (m *model) currentItem(items []item) *item {
	i := slices.IndexFunc(items, func(it item) bool { return it.key() == m.conflictSel })
	if i < 0 {
		return nil
	}
	return &items[i]
}

// keepItemSelection keeps the selected row selected; when it is gone,
// resolved, the one now in its place.
func (m *model) keepItemSelection(before int) {
	items := m.items()
	if m.currentItem(items) != nil || len(items) == 0 {
		return
	}
	m.conflictSel = items[min(max(before, 0), len(items)-1)].key()
}

func (m *model) itemIndex() int {
	return slices.IndexFunc(m.items(), func(it item) bool { return it.key() == m.conflictSel })
}

func (m *model) conflictsKey(k string) tea.Cmd {
	items := m.items()
	if len(items) == 0 {
		return nil
	}
	i := max(m.itemIndex(), 0)
	switch k {
	case "up", "k":
		i--
	case "down", "j":
		i++
	case "home", "g":
		i = 0
	case "end", "G":
		i = len(items) - 1
	}
	i = min(max(i, 0), len(items)-1)
	m.conflictSel = items[i].key()
	it := &items[i]
	if m.resolving[it.key()] || !containsKey(it.choices(), k) {
		return nil
	}
	name := it.shuttle
	if it.conflict == nil {
		m.ask(fmt.Sprintf("Let %s delete the %s its deletion guard held back?", name, present.Plural(it.deletions, "file")),
			func() tea.Cmd {
				return m.do("deletions allowed for one run of "+name, func(ctx context.Context) error { return m.c.AllowDeletions(ctx, name) })
			})
		return nil
	}
	path := it.conflict.Path
	switch k {
	case "l":
		m.ask(fmt.Sprintf("Keep this computer's copy of %s and replace the share's?", path),
			m.resolve(it, engine.KeepLocal, "kept this computer's copy of "+path))
	case "r":
		m.ask(fmt.Sprintf("Keep the share's copy of %s and replace this computer's?", path),
			m.resolve(it, engine.KeepRemote, "kept the share's copy of "+path))
	case "b":
		// Nothing is lost: no need to ask.
		return m.resolve(it, engine.KeepBoth, "kept both copies of "+path)()
	}
	return nil
}

func containsKey(choices, k string) bool {
	return len(k) == 1 && choices != "" && slices.Contains([]byte(choices), k[0])
}

// resolve returns what resolves it: marking it as resolving until it is
// gone from the list, then asking the daemon. Asking the user first must
// not mark it, so this is a step before the command.
func (m *model) resolve(it *item, choice engine.Choice, done string) func() tea.Cmd {
	key, name, path := it.key(), it.shuttle, it.conflict.Path
	return func() tea.Cmd {
		m.resolving[key] = true
		m.say("resolving "+path+"…", false)
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(m.ctx, resolveTimeout)
			defer cancel()
			err := m.c.Resolve(ctx, name, path, choice)
			return resolvedMsg{key: key, text: done, err: err}
		}
	}
}

type resolvedMsg struct {
	key  string
	text string
	err  error
}

// ask puts a question to the user: y runs then, n or esc drops it.
func (m *model) ask(question string, then func() tea.Cmd) {
	m.question, m.then = question, then
}

func (m *model) answer(k string) tea.Cmd {
	switch k {
	case "y", "enter":
		then := m.then
		m.question, m.then = "", nil
		return then()
	case "n", "esc":
		m.question, m.then = "", nil
		m.say("left as it is", false)
	}
	return nil
}

// forgetResolved stops marking what is gone from the list.
func (m *model) forgetResolved() {
	now := map[string]bool{}
	for _, it := range m.items() {
		now[it.key()] = true
	}
	for k := range m.resolving {
		if !now[k] {
			delete(m.resolving, k)
		}
	}
}

// copyText describes one copy of a conflict, and says whether it is the
// newer: "5 B, Oct 1 09:00, newer".
func copyText(f, other *fsys.FileInfo, now time.Time) string {
	if f == nil {
		return "none"
	}
	s := present.Size(f.Size) + ", " + when(f.ModTime, now)
	if f.Kind == fsys.KindDir {
		s = "folder, " + when(f.ModTime, now)
	}
	if other != nil && f.ModTime.After(other.ModTime) {
		s += ", newer"
	}
	return s
}

// when is a date short enough for a column: the year only when it is not
// this one.
func when(t, now time.Time) string {
	t = t.Local()
	if t.Year() == now.Local().Year() {
		return t.Format("Jan 2 15:04")
	}
	return t.Format("2006-01-02")
}
