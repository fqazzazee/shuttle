// SPDX-License-Identifier: GPL-2.0-or-later

package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/fqazzazee/shuttle/internal/daemon"
	"github.com/fqazzazee/shuttle/internal/engine"
)

// The bus and the wordmark line up only if every row is as wide as the
// others and every character takes one column.
func TestHeaderArtLinesUp(t *testing.T) {
	rows := smallBus(&busStyle, &lightStyle, &lightStyle)
	if len(rows) != busHeight {
		t.Errorf("%d bus rows, want %d", len(rows), busHeight)
	}
	for i, row := range rows {
		l := ansi.Strip(row)
		if w, n := ansi.StringWidth(l), len([]rune(l)); w != busWidth || n != busWidth {
			t.Errorf("bus row %d %q: %d columns, %d characters; want %d", i, l, w, n, busWidth)
		}
	}
	for i, l := range wordmark {
		if w, n := ansi.StringWidth(l), len([]rune(l)); w != n || w != ansi.StringWidth(wordmark[0]) {
			t.Errorf("wordmark row %d %q: %d columns, %d characters", i, l, w, n)
		}
	}
	if len(wordColors) != ansi.StringWidth(wordmark[0]) {
		t.Errorf("%d wordmark colors for %d columns", len(wordColors), ansi.StringWidth(wordmark[0]))
	}
}

// With the room, the header is the bus beside the wordmark, a summary of
// the shuttles and the tabs; the screen keeps its height, keys at the
// bottom.
func TestTallHeader(t *testing.T) {
	m := start(t, newFake(), 120, 24)
	lines := strings.Split(screen(m), "\n")
	for i, want := range []string{
		" ╭───────╮    █▀▀ █ █ █ █ ▀█▀ ▀█▀ █   █▀▀",
		" │   ⇄   │    ▀▀█ █▀█ █ █  █   █  █   █▀▀",
		"━┥ ▐███▌ ┝━   ▀▀▀ ▀ ▀ ▀▀▀  ▀   ▀  ▀▀▀ ▀▀▀",
		" │ ● ═ ● │    3 shuttles · 1 held · 1 disabled",
		" ╰─▀───▀─╯     1 Shuttles  2 Conflicts (2)  3 Activity  4 Profiles  5 Shares",
		"",
		"NAME    MODE",
	} {
		if !strings.HasPrefix(lines[i], want) {
			t.Errorf("line %d %q, want %q", i, lines[i], want)
		}
	}
	if len(lines) != 24 || !strings.HasPrefix(lines[23], "s sync") {
		t.Errorf("%d lines, the last %q", len(lines), lines[len(lines)-1])
	}
}

// The header is a line where the tall one would cost the room: a short
// terminal, a narrow one, a form open.
func TestHeaderGivesWay(t *testing.T) {
	for _, tc := range []struct {
		name          string
		width, height int
		keys          []string
		tall          bool
	}{
		{"room", 120, tallHeight, nil, true},
		{"short", 120, tallHeight - 1, nil, false},
		{"narrow", busWidth + gap + 26, 30, nil, false},
		{"a form open", 120, 30, []string{"5", "n"}, false},
		{"the form closed", 120, 30, []string{"5", "n", "esc"}, true},
	} {
		m := start(t, newFake(), tc.width, tc.height)
		press(t, m, tc.keys...)
		lines := strings.Split(screen(m), "\n")
		if tall := strings.HasPrefix(lines[0], " ╭"); tall != tc.tall {
			t.Errorf("%s: tall %v, want %v:\n%s", tc.name, tall, tc.tall, screen(m))
		}
		if len(lines) != tc.height {
			t.Errorf("%s: %d lines, want the terminal's %d", tc.name, len(lines), tc.height)
		}
	}
}

// The summary says nothing until the daemon answers.
func TestSummaryWaitsForTheDaemon(t *testing.T) {
	f := newFake()
	f.down = errors.New("no daemon")
	m := start(t, f, 120, 24)
	if s := m.summary(); s != "" {
		t.Errorf("summary %q with the daemon down", s)
	}
}

// The bus is in the shuttles' state taken together, in the dashboard's
// colors for it: sky when all is well, faint while the daemon is down.
func TestBusTint(t *testing.T) {
	idle := func(f *fake) {
		for i := range f.shuttles {
			f.shuttles[i].State = daemon.StateIdle
		}
	}
	for _, tc := range []struct {
		name string
		set  func(f *fake)
		want *lipgloss.Style
	}{
		{"all well", idle, &busStyle},
		{"held", func(*fake) {}, &heldStyle},
		{"running", func(f *fake) { idle(f); f.shuttles[0].State = daemon.StateRunning }, &busyStyle},
		{"offline", func(f *fake) { idle(f); f.shuttles[1].State = daemon.StateOffline }, &badStyle},
		{"everything paused", func(f *fake) { idle(f); f.pausedAll = true }, &faint},
		{"a broken config", func(f *fake) { idle(f); f.configErr = "line 3: bad" }, &badStyle},
		{"the daemon down", func(f *fake) { f.down = errors.New("no daemon") }, &faint},
	} {
		f := newFake()
		tc.set(f)
		m := start(t, f, 120, 24)
		if top := tc.want.Render(" ╭───────╮ "); !strings.HasPrefix(m.View().Content, top) {
			t.Errorf("%s: the bus is not in %q:\n%q", tc.name, top, strings.SplitN(m.View().Content, "\n", 2)[0])
		}
	}
}

// A shuttle that starts running tints the bus as soon as its status
// arrives, as it does its row, not at the next refresh.
func TestBusTintFollowsChanges(t *testing.T) {
	f := newFake()
	for i := range f.shuttles {
		f.shuttles[i].State = daemon.StateIdle
	}
	m := start(t, f, 120, 24)
	st := f.shuttles[0]
	st.State = daemon.StateRunning
	send(t, m, changedMsg(st))
	if top := busyStyle.Render(" ╭───────╮ "); !strings.HasPrefix(m.View().Content, top) {
		t.Errorf("the bus is not running:\n%q", strings.SplitN(m.View().Content, "\n", 2)[0])
	}
}

// The bus's lights take turns while a shuttle runs, half a second each,
// whatever the bus's color: here, held. Still, they are both on.
func TestLightsTakeTurns(t *testing.T) {
	lights := func(left, right lipgloss.Style) string {
		return left.Render("●") + faint.Render(" ═ ") + right.Render("●")
	}
	m := start(t, newFake(), 120, 24)
	if s := m.View().Content; !strings.Contains(s, lights(lightStyle, lightStyle)) {
		t.Errorf("the lights are not both on with nothing running:\n%q", s)
	}
	send(t, m, changedMsg{Name: "docs", State: daemon.StateRunning})
	at := now.Truncate(blinkEvery)
	for i, want := range []string{lights(lightStyle, faint), lights(faint, lightStyle), lights(lightStyle, faint)} {
		m.now = func() time.Time { return at.Add(time.Duration(i) * blinkEvery) }
		if s := m.View().Content; !strings.Contains(s, want) {
			t.Errorf("half second %d: the lights are not %q:\n%q", i, want, s)
		}
	}
}

// While a shuttle copies, its bar needs no tick, but the bus's lights do,
// slower, where the bus shows: not under a form, not after the run.
func TestLightsTick(t *testing.T) {
	m := start(t, newFake(), 140, 24)
	m.every = time.Hour // as the program has it: spin() schedules, and the test does not wait
	copying := daemon.Progress{Phase: engine.PhaseSyncing, Done: 1, Total: 4}
	m.Update(changedMsg{Name: "docs", State: daemon.StateRunning, Progress: copying})
	if !m.spinning {
		t.Error("the lights do not blink while a run copies")
	}
	m.Update(changedMsg{Name: "docs", State: daemon.StateIdle})
	if _, cmd := m.Update(spinMsg{}); cmd != nil || m.spinning {
		t.Error("the lights still blink after the run")
	}
	press(t, m, "5", "n")
	m.Update(changedMsg{Name: "docs", State: daemon.StateRunning, Progress: copying})
	if m.spinning {
		t.Error("the lights tick under a form, where the bus does not show")
	}
}
