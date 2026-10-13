// SPDX-License-Identifier: GPL-2.0-or-later

package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/fqazzazee/shuttle/internal/activity"
)

// logged is a daemon whose shuttles have done a few things, newest first.
func logged(n int) *fake {
	f := newFake()
	f.entries = []activity.Entry{
		{Time: now.Add(-time.Minute), Shuttle: "photos", Kind: activity.KindError, Path: "b.jpg", Text: "could not copy to the share: denied"},
		{Time: now.Add(-2 * time.Minute), Shuttle: "docs", Kind: activity.KindRun, Text: "1 copied"},
		{Time: now.Add(-3 * time.Minute), Shuttle: "docs", Kind: activity.KindDone, Path: "a.txt", Text: "copied to the share"},
	}
	for i := range n {
		f.entries = append(f.entries, activity.Entry{Time: now.Add(-time.Duration(i+4) * time.Hour), Shuttle: "music",
			Kind: activity.KindDone, Path: fmt.Sprintf("track%02d.flac", i), Text: "copied from the share"})
	}
	return f
}

// An error stands out in the log, in the theme's color for errors.
func TestActivityErrorInColor(t *testing.T) {
	m := start(t, logged(0), 120, 12)
	press(t, m, "3")
	if want := badStyle.Render("b.jpg: could not copy to the share: denied"); !strings.Contains(m.View().Content, want) {
		t.Errorf("the error is not in %q:\n%q", want, m.View().Content)
	}
}

func TestActivityTab(t *testing.T) {
	f := logged(0)
	m := start(t, f, 120, 12)
	if len(f.asked) != 0 {
		t.Errorf("activity asked for before the tab was shown: %q", f.asked)
	}
	press(t, m, "3")
	s := screen(m)
	for _, want := range []string{
		"Every shuttle  (f: show docs only)",
		stamp(now.Add(-time.Minute), now) + "  photos  b.jpg: could not copy to the share: denied",
		"docs    1 copied",
		"docs    a.txt: copied to the share",
		"↑↓ scroll  f filter",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("screen lacks %q:\n%s", want, s)
		}
	}

	// f shows one shuttle, then the next, then all again. Until the
	// daemon answers, the entries of the old filter are gone.
	cmd := m.activityKey("f")
	if s := screen(m); !strings.Contains(s, "Asking the daemon…") || strings.Contains(s, "b.jpg") {
		t.Errorf("while asking:\n%s", s)
	}
	run(t, m, cmd)
	s = screen(m)
	at := stamp(now.Add(-3*time.Minute), now)
	if !strings.Contains(s, "docs  (f: show music only)") || strings.Contains(s, "b.jpg") ||
		!strings.Contains(s, at+"  a.txt: copied to the share") {
		t.Errorf("docs only, without the shuttle column:\n%s", s)
	}
	press(t, m, "f", "f", "f")
	if got := strings.Join(f.asked, ","); got != ",docs,music,photos," {
		t.Errorf("asked for %q", got)
	}
	if !strings.Contains(screen(m), "Every shuttle") {
		t.Errorf("not back to every shuttle:\n%s", screen(m))
	}
}

func TestActivityFollowsAndScrolls(t *testing.T) {
	f := logged(30)
	m := start(t, f, 120, 12)
	press(t, m, "3")
	asked := len(f.asked)
	send(t, m, tickMsg{})
	if len(f.asked) != asked+1 {
		t.Errorf("a tick on the Activity tab did not ask again")
	}

	press(t, m, "down", "down")
	top := m.entries[m.scroll]
	// New entries arrive: the view stays on what was being read.
	f.set(func(f *fake) {
		f.entries = append([]activity.Entry{{Time: now, Shuttle: "docs", Kind: activity.KindRun, Text: "2 copied"}}, f.entries...)
	})
	send(t, m, tickMsg{})
	if !same(&m.entries[m.scroll], &top) {
		t.Errorf("the view moved: top is %+v, was %+v", m.entries[m.scroll], top)
	}
	press(t, m, "g")
	if !strings.Contains(screen(m), "docs    2 copied") {
		t.Errorf("the newest is not at the top:\n%s", screen(m))
	}
	press(t, m, "G")
	if !strings.Contains(screen(m), "track29.flac") {
		t.Errorf("the oldest is not in view:\n%s", screen(m))
	}
	press(t, m, "pgup")
	if m.scroll != len(m.entries)-1-(12-7) {
		t.Errorf("scroll = %d after a page up from the end", m.scroll)
	}
	for range 10 {
		press(t, m, "pgup")
	}
	if m.scroll != 0 {
		t.Errorf("scroll = %d after paging up past the top", m.scroll)
	}

	// Other tabs do not ask.
	press(t, m, "1")
	asked = len(f.asked)
	send(t, m, tickMsg{})
	if len(f.asked) != asked {
		t.Errorf("activity asked for on another tab")
	}
}

func TestActivityAnswersInOrder(t *testing.T) {
	m := start(t, logged(0), 120, 12)
	press(t, m, "3", "f") // docs only
	m.gotActivity(activityMsg{filter: "", entries: []activity.Entry{{Shuttle: "photos", Text: "late"}}})
	if strings.Contains(screen(m), "late") {
		t.Errorf("an answer for the old filter was shown:\n%s", screen(m))
	}
}

func TestActivityProblems(t *testing.T) {
	m := start(t, newFake(), 120, 12)
	press(t, m, "3")
	if s := screen(m); !strings.Contains(s, "Nothing yet") {
		t.Errorf("empty log:\n%s", s)
	}
	m.filter = "nope"
	run(t, m, m.loadActivity())
	if s := screen(m); !strings.Contains(s, `"nope": no shuttle by that name`) {
		t.Errorf("error not shown:\n%s", s)
	}
}

func TestStamp(t *testing.T) {
	today := time.Date(2026, 10, 12, 9, 5, 7, 0, time.Local)
	for at, want := range map[time.Time]string{
		today.Add(-time.Hour):         "08:05:07",
		today.AddDate(0, 0, -3):       "Oct 9 09:05:07",
		today.AddDate(-1, 0, 0):       "2025-10-12 09:05",
		today.Add(2 * 24 * time.Hour): "Oct 14 09:05:07",
	} {
		if got := stamp(at, today); got != want {
			t.Errorf("stamp(%v) = %q, want %q", at, got, want)
		}
	}
}
