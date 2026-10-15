// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

// Package pick is what a shuttle syncs of its folder, as the shuttle wizard
// marks it: everything, only what is marked (the shuttle's include), or all
// but what is marked (its exclude). The TUI's tree and the phone's wizard
// both mark through it.
package pick

import (
	"path"
	"slices"
	"strings"
)

// Mode is what the marks mean.
type Mode int

const (
	All     Mode = iota // everything is synced; marks are kept, and do not count
	Include             // only what is marked: the shuttle's include
	Exclude             // all but what is marked: its exclude
)

// Names are the modes as the wizard says them.
var Names = []string{"everything", "only what is marked", "all but what is marked"}

// Marks are the files and folders marked, as slash paths from the top of
// the share or of the device, and what they mean. Marks outside the
// shuttle's folder (root, in each call) are kept, as the folder may change
// back, but do not count. The zero value has no marks and syncs everything.
type Marks struct {
	Mode Mode
	set  map[string]bool
}

// Add marks p.
func (m *Marks) Add(p string) {
	if m.set == nil {
		m.set = map[string]bool{}
	}
	m.set[p] = true
}

// Marked reports whether p or a folder it is in is marked, inside root.
func (m *Marks) Marked(p, root string) bool {
	for ; Within(p, root) && p != root; p = path.Dir(p) {
		if m.set[p] {
			return true
		}
	}
	return false
}

// Partly reports whether something inside p is marked.
func (m *Marks) Partly(p string) bool {
	for k := range m.set {
		if Within(k, p) && k != p {
			return true
		}
	}
	return false
}

// Synced reports whether the shuttle syncs p, inside root: all of it, some
// of it.
func (m *Marks) Synced(p, root string) (all, some bool) {
	switch {
	case m.Mode == All:
		return true, true
	case m.Marked(p, root):
		return m.Mode == Include, m.Mode == Include
	case m.Partly(p):
		return false, true
	}
	return m.Mode == Exclude, m.Mode == Exclude
}

// Toggle marks p, inside root, or unmarks it. Unmarking something in a
// marked folder unmarks the folder and marks what else is in it instead,
// down to p: kids lists what is in a folder, as slash paths, and is asked
// only of the folders on the way, which are listed, as p is shown.
func (m *Marks) Toggle(p, root string, kids func(dir string) []string) {
	if !m.Marked(p, root) {
		for k := range m.set {
			if Within(k, p) {
				delete(m.set, k)
			}
		}
		m.Add(p)
		return
	}
	for !m.set[p] {
		parent := path.Dir(p)
		for _, k := range kids(parent) {
			if k != p {
				m.set[k] = true
			}
		}
		p = parent
	}
	delete(m.set, p)
}

// Inside is the marks inside root, sorted.
func (m *Marks) Inside(root string) []string {
	var out []string
	for k := range m.set {
		if Within(k, root) && k != root {
			out = append(out, k)
		}
	}
	slices.Sort(out)
	return out
}

// Selection is what the shuttle includes or excludes: the marks inside
// root, relative to it, sorted.
func (m *Marks) Selection(root string) (include, exclude []string) {
	var rel []string
	for _, k := range m.Inside(root) {
		rel = append(rel, strings.TrimPrefix(strings.TrimPrefix(k, root), "/"))
	}
	switch m.Mode {
	case Include:
		return rel, nil
	case Exclude:
		return nil, rel
	}
	return nil, nil
}

// SetSelection marks what a shuttle on root includes or excludes, as in the
// config, over what was marked.
func (m *Marks) SetSelection(root string, include, exclude []string) {
	m.Mode = All
	marks := include
	switch {
	case len(include) > 0:
		m.Mode = Include
	case len(exclude) > 0:
		m.Mode, marks = Exclude, exclude
	}
	for _, k := range marks {
		m.Add(path.Join(root, k))
	}
}

// Within reports whether the slash path p is top or inside it.
func Within(p, top string) bool {
	if top == "/" {
		return true
	}
	rest, ok := strings.CutPrefix(p, top)
	return ok && (rest == "" || rest[0] == '/')
}
