// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package pick

import (
	"slices"
	"testing"
)

// kids lists the folders of a share whose shuttle is on /top.
var kids = map[string][]string{
	"/top":     {"/top/a", "/top/z.txt"},
	"/top/a":   {"/top/a/b", "/top/a/c.txt"},
	"/top/a/b": {"/top/a/b/d.txt", "/top/a/b/e.txt"},
}

func list(dir string) []string { return kids[dir] }

// Unmarking something deep in a marked folder marks what else is in each
// folder on the way down instead, in either kind of marking.
func TestToggleSplitsAMarkedFolder(t *testing.T) {
	for _, mode := range []Mode{Include, Exclude} {
		m := Marks{Mode: mode}
		m.Toggle("/top/a", "/top", list)
		m.Toggle("/top/a/b/d.txt", "/top", list)
		include, exclude := m.Selection("/top")
		got := include
		if mode == Exclude {
			got = exclude
		}
		if want := []string{"a/b/e.txt", "a/c.txt"}; !slices.Equal(got, want) {
			t.Errorf("%s: marks %q, want %q", Names[mode], got, want)
		}
		if all, some := m.Synced("/top/a", "/top"); all || !some {
			t.Errorf("%s: a is all=%v some=%v, want partly", Names[mode], all, some)
		}
		// Marking a folder takes in the marks inside it.
		m.Toggle("/top/a", "/top", list)
		include, exclude = m.Selection("/top")
		if got := slices.Concat(include, exclude); !slices.Equal(got, []string{"a"}) {
			t.Errorf("%s: marks %q after marking a again", Names[mode], got)
		}
	}
}

func TestSynced(t *testing.T) {
	var m Marks
	if all, some := m.Synced("/top/a", "/top"); !all || !some {
		t.Error("with nothing marked and everything synced, a is not")
	}
	m.Add("/top/a/b")
	m.Add("/elsewhere") // outside the shuttle's folder: kept, not counted
	for _, tc := range []struct {
		mode      Mode
		p         string
		all, some bool
	}{
		{All, "/top/z.txt", true, true},
		{Include, "/top/a/b/d.txt", true, true},
		{Include, "/top/a", false, true},
		{Include, "/top/z.txt", false, false},
		{Exclude, "/top/a/b", false, false},
		{Exclude, "/top/a", false, true},
		{Exclude, "/top/z.txt", true, true},
	} {
		m.Mode = tc.mode
		if all, some := m.Synced(tc.p, "/top"); all != tc.all || some != tc.some {
			t.Errorf("%s, %s: all=%v some=%v, want %v %v", Names[tc.mode], tc.p, all, some, tc.all, tc.some)
		}
	}
	if got := m.Inside("/top"); !slices.Equal(got, []string{"/top/a/b"}) {
		t.Errorf("Inside = %q", got)
	}
}

// The config's include or exclude marks the same files again.
func TestSetSelection(t *testing.T) {
	var m Marks
	m.SetSelection("/top", nil, []string{"a/b", "z.txt"})
	if include, exclude := m.Selection("/top"); m.Mode != Exclude || include != nil || !slices.Equal(exclude, []string{"a/b", "z.txt"}) {
		t.Errorf("exclude: mode %v, %q %q", m.Mode, include, exclude)
	}
	var none Marks
	none.SetSelection("/top", nil, nil)
	if none.Mode != All {
		t.Errorf("nothing included or excluded: mode %v, want All", none.Mode)
	}
}

func TestWithin(t *testing.T) {
	for _, tc := range []struct {
		p, top string
		want   bool
	}{
		{"/a/b", "/a", true},
		{"/a", "/a", true},
		{"/ab", "/a", false},
		{"/x", "/", true},
	} {
		if got := Within(tc.p, tc.top); got != tc.want {
			t.Errorf("Within(%q, %q) = %v", tc.p, tc.top, got)
		}
	}
}
