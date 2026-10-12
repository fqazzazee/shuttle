// SPDX-License-Identifier: GPL-2.0-or-later

package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/fqazzazee/shuttle/internal/daemon"
	"github.com/fqazzazee/shuttle/internal/engine"
	"github.com/fqazzazee/shuttle/internal/fsys"
)

var (
	older = now.Add(-26 * time.Hour)
	newer = now.Add(-2 * time.Hour)
)

func file(size int64, at time.Time) *fsys.FileInfo {
	return &fsys.FileInfo{Kind: fsys.KindFile, Size: size, ModTime: at}
}

// conflicted is a daemon with something of every kind waiting for the user.
func conflicted() *fake {
	f := newFake()
	f.shuttles = []daemon.Status{
		{Name: "docs", State: daemon.StateHeld, HeldDeletions: 12},
		{Name: "photos", State: daemon.StateHeld, Conflicts: []engine.Issue{
			{Path: "a.jpg", Reason: "changed on both sides", Outcome: "held", Local: file(5, older), Remote: file(15, newer)},
			{Path: "x", Reason: "a file here, a folder on the share", Outcome: "held", Local: file(3, older),
				Remote: &fsys.FileInfo{Kind: fsys.KindDir, ModTime: older}},
			{Path: "Readme.md", Reason: "differs only in case from README.md", Outcome: "held", Local: file(9, older)},
			{Path: "z.jpg", Reason: "changed on both sides", Outcome: "kept both: ...", Local: file(1, older), Remote: file(1, newer)},
		}},
	}
	return f
}

func (f *fake) set(mutate func(*fake)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	mutate(f)
}

func TestConflictsTab(t *testing.T) {
	m := start(t, conflicted(), 160, 16)
	press(t, m, "2")
	s := screen(m)
	for _, want := range []string{
		"2 Conflicts (3)",
		"SHUTTLE  PATH       THIS COMPUTER               THE SHARE                   WHY",
		"docs                                                                        12 deletions held back",
		"photos   a.jpg      " + pad("5 B, "+when(older, now), copyWidth) + pad("15 B, "+when(newer, now)+", newer", copyWidth) + "changed on both sides",
		"photos   x          " + pad("3 B, "+when(older, now), copyWidth) + "folder, " + when(older, now),
		"photos   Readme.md  " + pad("9 B, "+when(older, now), copyWidth) + "none",
		"a lets the next run make these deletions.",
		"a allow the deletions",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("screen lacks %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "z.jpg") {
		t.Errorf("a conflict already settled is listed:\n%s", s)
	}
	// The keys and the explanation follow the selection.
	for _, tc := range []struct{ keys, explain string }{
		{"l keep this computer's  r keep the share's  b keep both", "l keeps this computer's copy, r the share's"},
		{"b keep both  P", "A file against a folder: b keeps both"},
		{"P pause all", "rename one of them on this computer"},
	} {
		press(t, m, "down")
		s := screen(m)
		if !strings.Contains(s, tc.keys) || !strings.Contains(s, tc.explain) {
			t.Errorf("want %q and %q:\n%s", tc.keys, tc.explain, s)
		}
	}
}

func TestResolving(t *testing.T) {
	f := conflicted()
	m := start(t, f, 160, 16)
	press(t, m, "2", "down") // a.jpg

	// Replacing a copy asks first; n leaves it.
	press(t, m, "l")
	if s := screen(m); !strings.Contains(s, "Keep this computer's copy of a.jpg and replace the share's? (y/n)") ||
		!strings.Contains(s, "y yes  n no") {
		t.Errorf("no question:\n%s", s)
	}
	press(t, m, "1", "n") // other keys wait for the answer
	if len(f.took()) != 0 || m.tab != tabConflicts || !strings.Contains(screen(m), "left as it is") {
		t.Errorf("calls %q, tab %d after n:\n%s", f.took(), m.tab, screen(m))
	}

	press(t, m, "r", "y")
	if got := f.took(); len(got) != 1 || got[0] != "resolve photos a.jpg keep-remote" {
		t.Errorf("calls = %q", got)
	}
	s := screen(m)
	if !strings.Contains(s, "kept the share's copy of a.jpg") || !strings.Contains(s, "resolving…") {
		t.Errorf("after resolving:\n%s", s)
	}
	press(t, m, "b") // already resolving: nothing more
	if len(f.took()) != 1 {
		t.Errorf("calls = %q", f.took())
	}

	// Gone from the list after the run: the row in its place is selected.
	f.set(func(f *fake) { f.shuttles[1].Conflicts = f.shuttles[1].Conflicts[1:] })
	run(t, m, m.refresh())
	if m.conflictSel != "photos\x00x" || len(m.resolving) != 0 {
		t.Errorf("selected %q, resolving %v", m.conflictSel, m.resolving)
	}

	// Keeping both loses nothing: no question.
	press(t, m, "l", "r", "b") // a file against a folder: only b
	if got := f.took(); len(got) != 2 || got[1] != "resolve photos x keep-both" {
		t.Errorf("calls = %q", got)
	}

	// A case clash: nothing to choose.
	press(t, m, "down", "l", "r", "b")
	if len(f.took()) != 2 {
		t.Errorf("calls for a case clash: %q", f.took())
	}

	// Held deletions: allowed after a yes.
	press(t, m, "g", "a", "y")
	if got := f.took(); len(got) != 3 || got[2] != "allow docs" {
		t.Errorf("calls = %q", got)
	}
}

func TestResolveFails(t *testing.T) {
	f := conflicted()
	m := start(t, f, 160, 16)
	press(t, m, "2", "down")
	f.set(func(f *fake) { f.resolve = errors.New("a.jpg changed: resolve it again") })
	press(t, m, "b")
	s := screen(m)
	if !strings.Contains(s, "a.jpg changed: resolve it again") || len(m.resolving) != 0 || strings.Contains(s, "resolving…") {
		t.Errorf("after a failed resolve, resolving %v:\n%s", m.resolving, s)
	}
	press(t, m, "b") // and it can be tried again
	if len(f.took()) != 2 {
		t.Errorf("calls = %q", f.took())
	}
}

func TestNothingToResolve(t *testing.T) {
	f := newFake()
	f.shuttles = f.shuttles[:1]
	m := start(t, f, 120, 12)
	press(t, m, "2", "l", "b", "a", "down")
	if s := screen(m); !strings.Contains(s, "Nothing waiting for you") || len(f.took()) != 0 {
		t.Errorf("calls %q:\n%s", f.took(), s)
	}
}

func TestCopyText(t *testing.T) {
	if got := copyText(nil, file(1, now), now); got != "none" {
		t.Errorf("no copy: %q", got)
	}
	if got := copyText(file(2000, newer), file(1, older), now); got != "2.0 kB, "+when(newer, now)+", newer" {
		t.Errorf("the newer: %q", got)
	}
	if got := copyText(file(1, older), file(1, newer), now); strings.Contains(got, "newer") {
		t.Errorf("the older: %q", got)
	}
	lastYear := now.AddDate(-1, 0, 0)
	if got := when(lastYear, now); got != lastYear.Local().Format("2006-01-02") {
		t.Errorf("last year: %q", got)
	}
}
