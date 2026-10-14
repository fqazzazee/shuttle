// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package tui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// fakeTree is a tree on /top whose folders are listed from dirs, each
// folder listed as if opened.
func fakeTree(t *testing.T, dirs map[string][]entry) *tree {
	t.Helper()
	tr := newTree("on nas", "/top", nil)
	var fill func(n *node)
	fill = func(n *node) {
		tr.listed(treeListedMsg{id: tr.id, dir: n.path, entries: dirs[n.path]})
		for _, k := range n.kids {
			if k.dir {
				k.open = true
				fill(k)
			}
		}
	}
	fill(tr.root)
	return tr
}

// Unmarking something deep in a marked folder marks what else is in each
// folder on the way down instead, in either kind of marking.
func TestToggleSplitsAMarkedFolder(t *testing.T) {
	dirs := map[string][]entry{
		"/top":     {{name: "a", dir: true}, {name: "z.txt"}},
		"/top/a":   {{name: "b", dir: true}, {name: "c.txt"}},
		"/top/a/b": {{name: "d.txt"}, {name: "e.txt"}},
	}
	for _, pick := range []picking{pickInclude, pickExclude} {
		tr := fakeTree(t, dirs)
		tr.pick = pick
		tr.toggle(tr.find("/top/a"))
		tr.toggle(tr.find("/top/a/b/d.txt"))
		include, exclude := tr.selection()
		got := include
		if pick == pickExclude {
			got = exclude
		}
		if want := []string{"a/b/e.txt", "a/c.txt"}; !slices.Equal(got, want) {
			t.Errorf("%s: marks %q, want %q", pickingNames[pick], got, want)
		}
		all, some := tr.synced("/top/a")
		if all || !some {
			t.Errorf("%s: a is all=%v some=%v, want partly", pickingNames[pick], all, some)
		}
		// Marking a folder takes in the marks inside it.
		tr.toggle(tr.find("/top/a"))
		include, exclude = tr.selection()
		if got := slices.Concat(include, exclude); !slices.Equal(got, []string{"a"}) {
			t.Errorf("%s: marks %q after marking a again", pickingNames[pick], got)
		}
	}
}

// Marks outside the shuttle's folder are neither shown nor saved.
func TestMarksOutsideTheFolder(t *testing.T) {
	tr := fakeTree(t, map[string][]entry{"/top": {{name: "a", dir: true}}})
	tr.pick = pickInclude
	tr.marks["/elsewhere"] = true
	tr.marks["/"] = true
	if all, some := tr.synced("/top/a"); all || some {
		t.Error("a mark above the shuttle's folder counts")
	}
	if include, _ := tr.selection(); include != nil {
		t.Errorf("include = %q", include)
	}
	if s := tr.marksLine(80); !strings.Contains(s, "Nothing marked yet") {
		t.Errorf("marks line = %q", s)
	}
}

// A typed path is listed, for when the user comes back to the step.
func TestTypedPathIsListed(t *testing.T) {
	w := newWizardSetup(t, nil)
	m := w.m
	press(t, m, "n")
	typeIn(t, m, "x")
	key(t, m, tea.KeyEnter)
	press(t, m, "t")
	typeIn(t, m, "docs")
	key(t, m, tea.KeyEnter)
	if !strings.Contains(screen(m), "Folder here:") {
		t.Fatalf("not on the next step:\n%s", screen(m))
	}
	key(t, m, tea.KeyEscape)
	if s := screen(m); !strings.Contains(s, "Folder on nas: /docs") || !strings.Contains(s, "2026/") {
		t.Errorf("back on the share:\n%s", s)
	}
}
