// SPDX-License-Identifier: GPL-2.0-or-later

package tui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/fqazzazee/shuttle/internal/activity"
	"github.com/fqazzazee/shuttle/internal/bus"
	"github.com/fqazzazee/shuttle/internal/config"
	"github.com/fqazzazee/shuttle/internal/daemon"
	"github.com/fqazzazee/shuttle/internal/engine"
)

var now = time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)

// fake is a daemon in memory.
type fake struct {
	mu        sync.Mutex
	down      error
	shuttles  []daemon.Status
	pausedAll bool
	configErr string
	resolve   error            // what Resolve returns
	entries   []activity.Entry // the activity log, newest first
	asked     []string         // whose activity was asked for
	calls     []string
	changes   chan daemon.Status
}

func newFake() *fake {
	return &fake{changes: make(chan daemon.Status, 8), shuttles: []daemon.Status{
		{Name: "docs", Mode: config.ModeSync, State: daemon.StateIdle, LastRun: now.Add(-5 * time.Minute)},
		{Name: "photos", Mode: config.ModePush, State: daemon.StateHeld, LastRun: now.Add(-3 * time.Hour),
			Conflicts: []engine.Issue{{Path: "a.jpg", Outcome: "held"}, {Path: "b.jpg", Outcome: "held"}}},
		{Name: "music", Mode: config.ModePull, State: daemon.StateDisabled},
	}}
}

func (f *fake) record(format string, args ...any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fmt.Sprintf(format, args...))
	return f.down
}

func (f *fake) Shuttles(context.Context) ([]daemon.Status, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.down != nil {
		return nil, f.down
	}
	// Sorted by name, as the bus client returns them.
	out := slices.Clone(f.shuttles)
	slices.SortFunc(out, func(a, b daemon.Status) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

func (f *fake) State(context.Context) (daemon.State, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return daemon.StateIdle, f.pausedAll, f.down
}

func (f *fake) ConfigError(context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.configErr, f.down
}

func (f *fake) SyncNow(_ context.Context, name string) error { return f.record("sync %s", name) }
func (f *fake) SetPaused(_ context.Context, name string, p bool) error {
	return f.record("pause %q %v", name, p)
}
func (f *fake) SetEnabled(_ context.Context, name string, on bool) error {
	return f.record("enable %s %v", name, on)
}
func (f *fake) Changes(context.Context) (<-chan daemon.Status, error) { return f.changes, nil }
func (f *fake) Resolve(_ context.Context, name, path string, c engine.Choice) error {
	if err := f.record("resolve %s %s %s", name, path, c); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.resolve
}
func (f *fake) AllowDeletions(_ context.Context, name string) error {
	return f.record("allow %s", name)
}
func (f *fake) Activity(_ context.Context, name string, limit int) ([]activity.Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, name)
	if f.down != nil {
		return nil, f.down
	}
	if name == "nope" {
		return nil, fmt.Errorf("%q: %w", name, daemon.ErrUnknownShuttle)
	}
	out := []activity.Entry{}
	for _, e := range f.entries {
		if (name == "" || e.Shuttle == name) && len(out) < limit {
			out = append(out, e)
		}
	}
	return out, nil
}

func (f *fake) took() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

var _ Client = (*bus.Client)(nil) // the real client fits

// start makes a model on f, sized w×h, and runs its first commands.
func start(t *testing.T, f *fake, w, h int) *model {
	t.Helper()
	m := newModel(context.Background(), f, testOptions(t))
	m.every, m.now = 0, func() time.Time { return now }
	send(t, m, tea.WindowSizeMsg{Width: w, Height: h})
	run(t, m, m.Init())
	return m
}

// send updates m with msg and runs the commands that follow.
func send(t *testing.T, m *model, msg tea.Msg) {
	t.Helper()
	_, cmd := m.Update(msg)
	run(t, m, cmd)
}

func press(t *testing.T, m *model, keys ...string) {
	t.Helper()
	for _, k := range keys {
		var key tea.KeyPressMsg
		switch k {
		case "down":
			key = tea.KeyPressMsg{Code: tea.KeyDown}
		case "up":
			key = tea.KeyPressMsg{Code: tea.KeyUp}
		case "tab":
			key = tea.KeyPressMsg{Code: tea.KeyTab}
		case "esc":
			key = tea.KeyPressMsg{Code: tea.KeyEscape}
		case "shift+tab":
			key = tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
		case "pgup":
			key = tea.KeyPressMsg{Code: tea.KeyPgUp}
		case "pgdown":
			key = tea.KeyPressMsg{Code: tea.KeyPgDown}
		default:
			key = tea.KeyPressMsg{Code: rune(strings.ToLower(k)[0]), Text: k}
		}
		send(t, m, key)
	}
}

// run runs cmd and feeds its messages back, as the program would. A
// command still waiting after a moment (for a status change) is left.
func run(t *testing.T, m *model, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	got := make(chan tea.Msg, 1)
	go func() { got <- cmd() }()
	var msg tea.Msg
	select {
	case msg = <-got:
	case <-time.After(50 * time.Millisecond):
		return
	}
	switch msg := msg.(type) {
	case nil:
	case tea.BatchMsg:
		for _, c := range msg {
			run(t, m, c)
		}
	case tea.QuitMsg:
		t.Log("quit")
	default:
		send(t, m, msg)
	}
}

func screen(m *model) string { return ansi.Strip(m.View().Content) }

func TestShuttlesTab(t *testing.T) {
	m := start(t, newFake(), 120, 12)
	s := screen(m)
	for _, want := range []string{
		"shuttle  1 Shuttles  2 Conflicts (2)  3 Activity",
		"NAME    MODE    STATE     LAST RUN  PROGRESS                DETAILS",
		"docs    ↔ sync  idle      5m ago",
		"music   ← pull  disabled  never",
		"photos  → push  held      3h ago                            2 conflicts waiting for you",
		"s sync  p pause  d disable  n new  e edit  x delete  P pause all  1-5 tabs  ? help  q quit",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("screen lacks %q:\n%s", want, s)
		}
	}
	if lines := strings.Split(s, "\n"); len(lines) != 12 {
		t.Errorf("%d lines, want the terminal's 12", len(lines))
	}
	if !m.View().AltScreen {
		t.Error("not on the alternate screen")
	}
}

func TestActions(t *testing.T) {
	f := newFake()
	m := start(t, f, 120, 12)
	press(t, m, "s", "down", "d", "down", "p", "P")
	want := []string{"sync docs", "enable music true", `pause "photos" true`, `pause "" true`}
	if got := f.took(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("calls = %q, want %q", got, want)
	}
	if !strings.Contains(screen(m), "paused all shuttles") {
		t.Errorf("the last action is not shown:\n%s", screen(m))
	}

	// The keys follow the selection: a disabled shuttle can be enabled.
	press(t, m, "up")
	if s := screen(m); !strings.Contains(s, "d enable") {
		t.Errorf("keys for a disabled shuttle:\n%s", s)
	}
	// While everything is paused, p says how to resume.
	f.mu.Lock()
	f.pausedAll = true
	f.mu.Unlock()
	run(t, m, m.refresh())
	press(t, m, "p")
	if s := screen(m); !strings.Contains(s, "All shuttles are paused: P resumes them.") || !strings.Contains(s, "P resume all") {
		t.Errorf("p while all are paused:\n%s", s)
	}
	// The message fades; what stays true shows again.
	m.now = func() time.Time { return now.Add(messageFor) }
	if s := screen(m); strings.Contains(s, "P resumes them") || !strings.Contains(s, "All shuttles are paused.") {
		t.Errorf("after the message faded:\n%s", s)
	}
	if n := len(f.took()); n != 4 {
		t.Errorf("%d calls, want no more", n)
	}
	press(t, m, "P")
	if got := f.took(); got[len(got)-1] != `pause "" false` {
		t.Errorf("P while all are paused: %q, want resume all", got[len(got)-1])
	}
}

func TestSelectionStaysWithTheShuttle(t *testing.T) {
	f := newFake()
	m := start(t, f, 120, 12)
	press(t, m, "down", "down", "down") // stops at the last
	if m.selected != "photos" {
		t.Fatalf("selected %q", m.selected)
	}
	f.mu.Lock()
	f.shuttles = append(f.shuttles, daemon.Status{Name: "books", State: daemon.StateIdle})
	f.mu.Unlock()
	run(t, m, m.refresh())
	if m.selected != "photos" {
		t.Errorf("selected %q after a shuttle was added before it", m.selected)
	}
	press(t, m, "g")
	if m.selected != "books" {
		t.Errorf("g selected %q, want the first", m.selected)
	}
}

func TestLiveProgress(t *testing.T) {
	f := newFake()
	m := start(t, f, 140, 12)
	// As the daemon's signal arrives (the wait for it is the program's).
	send(t, m, changedMsg{Name: "docs", Mode: config.ModeSync, State: daemon.StateRunning,
		Progress: daemon.Progress{Phase: engine.PhaseSyncing, Done: 3, Total: 10, Bytes: 450, TotalBytes: 1000,
			RateUp: 2_100_000, Path: "a.pdf"}})
	s := screen(m)
	if !strings.Contains(s, "docs    ↔ sync  running") || !strings.Contains(s, "45% ↑ 2.1 MB/s") {
		t.Errorf("progress not shown:\n%s", s)
	}
	if !strings.Contains(s, "↑ 2.1 MB/s  ↓ 0 B/s") {
		t.Errorf("overall rates not shown:\n%s", s)
	}
	for _, p := range []struct {
		p    daemon.Progress
		want string
	}{
		{daemon.Progress{Phase: engine.PhaseScanning, Done: 1234}, "scanning 1234"},
		{daemon.Progress{Phase: engine.PhaseComparing, Done: 2, Total: 5, RateDown: 1000}, "comparing 2/5 ↓ 1.0 kB/s"},
		{daemon.Progress{Phase: engine.PhaseSyncing, Done: 1, Total: 4}, "25%"},
		{daemon.Progress{Phase: engine.PhaseSyncing}, "syncing"},
		{daemon.Progress{}, ""},
	} {
		if got := progress(&p.p); got != p.want {
			t.Errorf("progress(%+v) = %q, want %q", p.p, got, p.want)
		}
	}
}

func TestDaemonDown(t *testing.T) {
	f := newFake()
	f.down = bus.ErrNotRunning
	m := start(t, f, 120, 12)
	if s := screen(m); !strings.Contains(s, "the shuttle daemon is not running (start it with: systemctl --user start shuttle)") {
		t.Errorf("screen:\n%s", s)
	}
	press(t, m, "s") // nothing selected: no call
	if len(f.took()) != 0 {
		t.Errorf("calls with the daemon down: %q", f.took())
	}
	f.mu.Lock()
	f.down = nil
	f.mu.Unlock()
	send(t, m, tickMsg{})
	if s := screen(m); !strings.Contains(s, "docs") {
		t.Errorf("not caught up after the daemon started:\n%s", s)
	}
	// And when it stops again, what it said before is not acted on.
	f.mu.Lock()
	f.down = bus.ErrNotRunning
	f.mu.Unlock()
	send(t, m, tickMsg{})
	press(t, m, "s", "d")
	if len(f.took()) != 0 {
		t.Errorf("calls after the daemon stopped: %q", f.took())
	}
}

func TestFailedActionAndConfigError(t *testing.T) {
	m := start(t, newFake(), 120, 12)
	send(t, m, doneMsg{"syncing docs", fmt.Errorf("%q: %w", "docs", daemon.ErrDisabled)})
	if s := screen(m); !strings.Contains(s, `"docs": shuttle is disabled in the config`) {
		t.Errorf("failed action not shown:\n%s", s)
	}
	f := newFake()
	f.configErr = "line 3: unknown key \"bogus\"\nmore"
	m = start(t, f, 160, 12)
	if s := screen(m); !strings.Contains(s, `The config file has an error, so the previous settings are in use: line 3: unknown key "bogus" …`) {
		t.Errorf("config error not shown:\n%s", s)
	}
	if got := errorText(fmt.Errorf("%w: x\ny", daemon.ErrConfig)); got != "the config file: config: x …" {
		t.Errorf("errorText = %q", got)
	}
}

func TestTabsAndHelp(t *testing.T) {
	f := newFake()
	m := start(t, f, 120, 12)
	press(t, m, "2", "s", "d", "p")
	if len(f.took()) != 0 {
		t.Errorf("the shuttles' keys acted on another tab: %q", f.took())
	}
	if s := screen(m); !strings.Contains(s, "SHUTTLE  PATH") || strings.Contains(s, "s sync") {
		t.Errorf("conflicts tab:\n%s", s)
	}
	press(t, m, "tab", "tab", "tab")
	if m.tab != tabShares {
		t.Errorf("tab = %d after three tabs from 2, want Shares", m.tab)
	}
	press(t, m, "tab")
	if m.tab != tabShuttles {
		t.Errorf("tab = %d, want back to the first", m.tab)
	}
	press(t, m, "?")
	if s := screen(m); !strings.Contains(s, "pause or resume every shuttle") {
		t.Errorf("help:\n%s", s)
	}
	press(t, m, "s") // keys of the tab do nothing under the help
	press(t, m, "esc")
	if m.help {
		t.Error("esc did not close the help")
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if cmd == nil || cmd() != tea.Quit() {
		t.Error("q does not quit")
	}
}

// However narrow or short the terminal, every line fits, and the keys stay
// at the bottom.
func TestSmallTerminals(t *testing.T) {
	f := newFake()
	for i := range 20 {
		f.shuttles = append(f.shuttles, daemon.Status{Name: fmt.Sprintf("s%02d", i), State: daemon.StateIdle})
	}
	for _, size := range [][2]int{{40, 8}, {20, 5}, {200, 50}} {
		m := start(t, f, size[0], size[1])
		lines := strings.Split(screen(m), "\n")
		if len(lines) != size[1] {
			t.Errorf("%v: %d lines", size, len(lines))
		}
		for _, l := range lines {
			if w := ansi.StringWidth(l); w > size[0] {
				t.Errorf("%v: line %q is %d wide", size, l, w)
			}
		}
		if !strings.HasPrefix(lines[len(lines)-1], "s sync") {
			t.Errorf("%v: last line %q, want the keys", size, lines[len(lines)-1])
		}
	}
	// Scrolled to keep the selection in view.
	m := start(t, f, 80, 10)
	press(t, m, "G")
	if s := screen(m); !strings.Contains(s, "s19") {
		t.Errorf("the last shuttle is not in view:\n%s", s)
	}
}

func TestErrorsDoNotPanic(t *testing.T) {
	m := start(t, newFake(), 80, 10)
	send(t, m, subscribedMsg{err: errors.New("no bus")})
	send(t, m, refreshedMsg{err: errors.New("gone")})
	press(t, m, "s", "p", "d", "down", "up")
	_ = screen(m)
}

func TestFirstTab(t *testing.T) {
	for name, want := range map[string]int{"shuttles": tabShuttles, "Conflicts": tabConflicts, "activity": tabActivity,
		"profiles": tabProfiles, "shares": tabShares} {
		if got, ok := Tab(name); !ok || got != want {
			t.Errorf("Tab(%q) = %d, %v", name, got, ok)
		}
	}
	if _, ok := Tab("settings"); ok {
		t.Error("an unknown tab")
	}
	f := newFake()
	o := testOptions(t)
	o.Tab = tabActivity
	m := newModel(context.Background(), f, o)
	m.every, m.now = 0, func() time.Time { return now }
	send(t, m, tea.WindowSizeMsg{Width: 120, Height: 12})
	run(t, m, m.Init())
	if m.tab != tabActivity || len(f.asked) != 1 || !strings.Contains(screen(m), "Every shuttle") {
		t.Errorf("tab %d, asked %q:\n%s", m.tab, f.asked, screen(m))
	}
}
