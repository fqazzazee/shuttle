// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package tui

import (
	"errors"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/fqazzazee/shuttle/internal/pick"
)

// A name on a share can hold an escape sequence: the picker shows it clean,
// and what it selects is still the file's exact name.
func TestTreeHoldsNoEscapes(t *testing.T) {
	const file, dir = "a\x1b]52;c;eA==\a.txt", "d\x1b[2Jir"
	tr := fakeTree(t, map[string][]entry{
		"/top":        {{name: dir, dir: true}, {name: file}},
		"/top/" + dir: {{name: file}},
	})
	tr.marks.Mode = pick.Include
	tr.toggle(tr.find("/top/" + file))
	tr.toggle(tr.find("/top/" + dir))
	tr.find("/top/" + dir).err = errors.New("cannot list " + dir)

	// The picker's own styling is escape sequences too: look for the names'.
	raw := func(s string) bool {
		return strings.Contains(s, "\x1b]52") || strings.Contains(s, "\x1b[2J") || strings.Contains(s, "\a")
	}
	s := strings.Join(tr.lines(40, 200), "\n")
	if raw(s) {
		t.Errorf("the picker shows an escape sequence: %q", s)
	}
	if !strings.Contains(s, "a�]52;c;eA==�.txt") {
		t.Errorf("the file is not shown cleaned: %q", s)
	}
	include, _ := tr.selection()
	if want := []string{file, dir}; !slices.Equal(include, want) {
		t.Errorf("selection = %q, want the raw names %q", include, want)
	}

	// The folder as the shuttle's folder, and the path typed over it.
	tr.setRoot(tr.find("/top/" + dir))
	if s := strings.Join(tr.lines(40, 200), "\n"); raw(s) {
		t.Errorf("the folder's header shows an escape sequence: %q", s)
	}
	ctx := t.Context()
	tr.key(ctx, tea.KeyPressMsg{Code: 't', Text: "t"})
	if v := tr.input.Value(); strings.ContainsAny(v, "\x1b\a") {
		t.Errorf("the path being typed = %q", v)
	}
	if _, chosen := tr.key(ctx, tea.KeyPressMsg{Code: tea.KeyEnter}); !chosen {
		t.Fatal("enter did not choose the path")
	}
	if got, want := tr.dir(), "/top/"+dir; got != want {
		t.Errorf("confirming the path unchanged chose %q, want %q", got, want)
	}
}

// A folder the wizard could not create is named in its error.
func TestWizardErrorHoldsNoEscapes(t *testing.T) {
	w := newWizardSetup(t, nil)
	m := w.m
	press(t, m, "n")
	m.wizard.result, m.wizard.failed = "mkdir /top/d\x1b[2Jir: permission denied", true
	if s := screen(m); strings.Contains(s, "\x1b[2J") || !strings.Contains(s, "d�[2Jir") {
		t.Errorf("the wizard's error = %q", s)
	}
}

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
	for _, mode := range []pick.Mode{pick.Include, pick.Exclude} {
		tr := fakeTree(t, dirs)
		tr.marks.Mode = mode
		tr.toggle(tr.find("/top/a"))
		tr.toggle(tr.find("/top/a/b/d.txt"))
		include, exclude := tr.selection()
		got := include
		if mode == pick.Exclude {
			got = exclude
		}
		if want := []string{"a/b/e.txt", "a/c.txt"}; !slices.Equal(got, want) {
			t.Errorf("%s: marks %q, want %q", pick.Names[mode], got, want)
		}
		all, some := tr.synced("/top/a")
		if all || !some {
			t.Errorf("%s: a is all=%v some=%v, want partly", pick.Names[mode], all, some)
		}
		// Marking a folder takes in the marks inside it.
		tr.toggle(tr.find("/top/a"))
		include, exclude = tr.selection()
		if got := slices.Concat(include, exclude); !slices.Equal(got, []string{"a"}) {
			t.Errorf("%s: marks %q after marking a again", pick.Names[mode], got)
		}
	}
}

// Marks outside the shuttle's folder are neither shown nor saved.
func TestMarksOutsideTheFolder(t *testing.T) {
	tr := fakeTree(t, map[string][]entry{"/top": {{name: "a", dir: true}}})
	tr.marks.Mode = pick.Include
	tr.marks.Add("/elsewhere")
	tr.marks.Add("/")
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

// Unmarking part of a carried folder leaves the rest of it carried, and a
// mark made on this tree stays on the file it was made on.
func TestCarriedMarksMoveWithTheFolder(t *testing.T) {
	tr := fakeTree(t, map[string][]entry{
		"/top":   {{name: "a", dir: true}, {name: "mine.txt"}},
		"/top/a": {{name: "b.txt"}, {name: "c.txt"}},
	})
	tr.marks.Mode = pick.Exclude
	tr.marks.Add("/top/a")
	tr.carryMarks()
	tr.toggle(tr.find("/top/a/b.txt")) // c.txt marked instead
	tr.toggle(tr.find("/top/mine.txt"))

	tr.setRoot(&node{path: "/", dir: true})
	if _, exclude := tr.selection(); !slices.Equal(exclude, []string{"a/c.txt", "top/mine.txt"}) {
		t.Errorf("exclude = %q", exclude)
	}
	tr.setRoot(&node{path: "/top", dir: true})
	if _, exclude := tr.selection(); !slices.Equal(exclude, []string{"a/c.txt", "mine.txt"}) {
		t.Errorf("back on the folder: exclude = %q", exclude)
	}
}

// A carried mark that passes over a mark made here does not take it along.
func TestCarriedMarkLeavesAMarkMadeHere(t *testing.T) {
	tr := fakeTree(t, map[string][]entry{
		"/top":     {{name: "sub", dir: true}, {name: "x"}},
		"/top/sub": {{name: "x"}},
	})
	tr.marks.Mode = pick.Exclude
	tr.marks.Add("/top/x")
	tr.carryMarks()
	tr.toggle(tr.find("/top/sub/x")) // made here, where the carried x is about to land

	tr.setRoot(&node{path: "/top/sub", dir: true})
	if _, exclude := tr.selection(); !slices.Equal(exclude, []string{"x"}) {
		t.Errorf("on sub: exclude = %q", exclude)
	}
	tr.setRoot(&node{path: "/top", dir: true})
	if _, exclude := tr.selection(); !slices.Equal(exclude, []string{"sub/x", "x"}) {
		t.Errorf("back on top: exclude = %q, want both marks", exclude)
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
