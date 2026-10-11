// SPDX-License-Identifier: GPL-2.0-or-later

package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fqazzazee/shuttle/internal/secrets"
)

// noKeyring fails the test if the daemon asks for a password: these tests use
// mounted-folder shares, which need none.
type noKeyring struct{ t *testing.T }

func (k noKeyring) Get(context.Context, secrets.Key) (string, error) {
	k.t.Error("the keyring was asked for a password")
	return "", secrets.ErrNotFound
}
func (noKeyring) Set(context.Context, secrets.Key, string) error { return nil }
func (noKeyring) Delete(context.Context, secrets.Key) error      { return nil }

// fixture is a daemon over a "path" share in temp folders.
type fixture struct {
	t            *testing.T
	local, mount string
	paths        Paths
	d            *Daemon
	cancel       context.CancelFunc
	done         chan error
	mu           sync.Mutex
	runs         int   // how many times a shuttle entered StateRunning
	last         State // the state of the last change seen
}

// newFixture writes a config with one shuttle, s, between local and the
// mounted share; extra is added to the shuttle's table.
func newFixture(t *testing.T, mode, extra string) *fixture {
	t.Helper()
	f := &fixture{t: t, local: t.TempDir(), mount: t.TempDir()}
	dir := t.TempDir()
	f.paths = Paths{Config: filepath.Join(dir, "config.toml"), StateDir: filepath.Join(dir, "state")}
	f.writeConfig(fmt.Sprintf(`
[shares.mnt]
type = "path"
path = %q

[shuttles.s]
mode = %q
local = %q
remote = "mnt:"
%s
`, f.mount, mode, f.local, extra))
	return f
}

func (f *fixture) writeConfig(doc string) {
	f.t.Helper()
	if err := os.WriteFile(f.paths.Config, []byte(doc), 0o600); err != nil {
		f.t.Fatal(err)
	}
}

// start creates and runs the daemon; the test's cleanup stops it.
func (f *fixture) start() {
	f.t.Helper()
	d, err := New(&Options{Paths: f.paths, Store: noKeyring{f.t}, OnChange: f.onChange})
	if err != nil {
		f.t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	f.d, f.cancel, f.done = d, cancel, make(chan error, 1)
	go func() { f.done <- d.Run(ctx) }()
	f.t.Cleanup(f.stop)
	select {
	case <-d.Ready():
	case err := <-f.done:
		f.cancel = nil
		f.t.Fatalf("Run ended before it was ready: %v", err)
	case <-time.After(10 * time.Second):
		f.t.Fatal("the daemon was not ready within 10s")
	}
}

func (f *fixture) stop() {
	if f.cancel == nil {
		return
	}
	f.cancel()
	f.cancel = nil
	select {
	case err := <-f.done:
		if err != nil {
			f.t.Errorf("Run: %v", err)
		}
	case <-time.After(10 * time.Second):
		f.t.Error("the daemon did not stop within 10s")
	}
}

// onChange counts runs: changes into StateRunning. Triggers during a run
// report changes too, while the state stays StateRunning.
func (f *fixture) onChange(st *Status) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if st.State == StateRunning && f.last != StateRunning {
		f.runs++
	}
	f.last = st.State
}

func (f *fixture) runCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.runs
}

// waitFor polls the named shuttle's status until ok holds.
func (f *fixture) waitFor(name, what string, ok func(Status) bool) Status {
	f.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		all := f.d.Statuses()
		for i := range all {
			if all[i].Name == name && ok(all[i]) {
				return all[i]
			}
		}
		if time.Now().After(deadline) {
			f.t.Fatalf("timed out waiting for %s; statuses: %+v", what, f.d.Statuses())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// waitCopying waits until a file is being written to the share: a temp file
// there means a copy is in progress, which a fixed sleep cannot promise on a
// busy machine.
func (f *fixture) waitCopying() {
	f.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		entries, _ := os.ReadDir(f.mount)
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".shuttle-tmp-") {
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	f.t.Fatal("no copy started within 10s")
}

func (f *fixture) settled(name string) Status {
	f.t.Helper()
	return f.waitFor(name, "the run to end", func(st Status) bool {
		return st.State != StateRunning && !st.Pending && !st.LastRun.IsZero()
	})
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func exists(dir, name string) bool {
	_, err := os.Stat(filepath.Join(dir, name))
	return err == nil
}

func TestSyncNowRunsTheShuttle(t *testing.T) {
	f := newFixture(t, "push", "")
	write(t, f.local, "a.txt", "a")
	f.start()

	if err := f.d.SyncNow("s"); err != nil {
		t.Fatal(err)
	}
	st := f.settled("s")
	if st.State != StateIdle || st.Copied != 1 || st.LastOK.IsZero() {
		t.Errorf("status = %+v", st)
	}
	if !exists(f.mount, "a.txt") {
		t.Error("a.txt was not pushed")
	}
}

func TestNothingRunsUntilTriggered(t *testing.T) {
	f := newFixture(t, "push", "")
	write(t, f.local, "a.txt", "a")
	f.start()
	time.Sleep(200 * time.Millisecond)
	if exists(f.mount, "a.txt") || f.runCount() != 0 {
		t.Error("a manual shuttle ran without being asked")
	}
}

// slowFiles makes n files that each take about half a second to push at the
// bandwidth limit slow sets.
const slow = `bandwidth = { up = "64KiB/s" }`

func slowFiles(t *testing.T, dir string, n int) {
	for i := range n {
		write(t, dir, fmt.Sprintf("f%d.bin", i), strings.Repeat("x", 96<<10))
	}
}

func TestTriggersDuringARunAddUpToOneMore(t *testing.T) {
	f := newFixture(t, "push", slow)
	slowFiles(t, f.local, 2)
	f.start()

	f.d.SyncNow("s")
	f.waitFor("s", "the run to start", func(st Status) bool { return st.State == StateRunning })
	for range 5 {
		f.d.SyncNow("s")
	}
	f.settled("s")
	if got := f.runCount(); got != 2 {
		t.Errorf("ran %d times, want 2: one, plus one for all the triggers during it", got)
	}
}

func TestPausedShuttleRunsWhenResumed(t *testing.T) {
	for _, all := range []bool{false, true} {
		t.Run(fmt.Sprintf("all=%v", all), func(t *testing.T) {
			f := newFixture(t, "push", "")
			write(t, f.local, "a.txt", "a")
			f.start()
			name := "s"
			if all {
				name = ""
			}
			f.d.SetPaused(name, true)
			f.d.SyncNow("s")
			st := f.waitFor("s", "paused and pending", func(st Status) bool { return st.State == StatePaused && st.Pending })
			if !st.Paused || exists(f.mount, "a.txt") {
				t.Fatalf("status = %+v; a paused shuttle must not run", st)
			}
			f.d.SetPaused(name, false)
			if st := f.settled("s"); st.Copied != 1 || !exists(f.mount, "a.txt") {
				t.Errorf("after resume: %+v", st)
			}
		})
	}
}

// Pausing mid-run finishes the file being copied and starts no more; the
// rest follows on resume.
func TestPauseFinishesTheCurrentFile(t *testing.T) {
	f := newFixture(t, "push", slow)
	slowFiles(t, f.local, 3)
	f.start()
	f.d.SyncNow("s")
	f.waitCopying()
	f.d.SetPaused("s", true)

	st := f.waitFor("s", "the paused run to end", func(st Status) bool { return st.State == StatePaused })
	pushed := 0
	for i := range 3 {
		if exists(f.mount, fmt.Sprintf("f%d.bin", i)) {
			pushed++
		}
	}
	if pushed < 1 || pushed == 3 || !st.Pending {
		t.Errorf("pushed %d of 3 files, status %+v; want the file in progress finished, not all, and the rest pending", pushed, st)
	}
	entries, _ := os.ReadDir(f.mount)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".shuttle-tmp-") {
			t.Errorf("half-written file left: %s", e.Name())
		}
	}

	f.d.SetPaused("s", false)
	f.settled("s")
	for i := range 3 {
		if !exists(f.mount, fmt.Sprintf("f%d.bin", i)) {
			t.Errorf("f%d.bin missing after resume", i)
		}
	}
}

// Shutting down works like pausing: the file in progress finishes, and the
// state is released so the next daemon can start.
func TestShutdownFinishesAndReleasesState(t *testing.T) {
	f := newFixture(t, "push", slow)
	slowFiles(t, f.local, 3)
	f.start()
	f.d.SyncNow("s")
	f.waitCopying()
	f.stop()

	g := &fixture{t: t, paths: f.paths}
	g.start() // fails if the state database were still locked
}

func TestSecondDaemonIsRefused(t *testing.T) {
	f := newFixture(t, "push", "")
	f.start()
	d2, err := New(&Options{Paths: f.paths, Store: noKeyring{t}})
	if err != nil {
		t.Fatal(err)
	}
	// A deadline, so a second daemon that does start fails the test rather
	// than running forever.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := d2.Run(ctx); !errors.Is(err, ErrAlreadyRunning) {
		t.Errorf("second Run = %v, want ErrAlreadyRunning", err)
	}
}

// A missing local folder is usually an unmounted drive: an error, and never
// created (an empty stand-in would read as everything deleted).
func TestMissingLocalFolderIsAnError(t *testing.T) {
	f := newFixture(t, "sync", "")
	write(t, f.mount, "keep.txt", "k")
	os.Remove(f.local)
	f.start()
	f.d.SyncNow("s")
	st := f.settled("s")
	if st.State != StateError || !strings.Contains(st.Error, "is the drive mounted?") {
		t.Errorf("status = %+v", st)
	}
	if _, err := os.Stat(f.local); !errors.Is(err, os.ErrNotExist) {
		t.Error("the missing local folder was created")
	}
	if !exists(f.mount, "keep.txt") {
		t.Error("the share was touched")
	}
}

func TestUnreachableShareIsOffline(t *testing.T) {
	f := newFixture(t, "push", "")
	f.writeConfig(fmt.Sprintf(`
[shares.nas]
type = "smb"
host = "127.0.0.1"
port = 1
share = "x"

[shuttles.s]
mode = "push"
local = %q
remote = "nas:"
`, f.local))
	f.start()
	f.d.SyncNow("s")
	if st := f.settled("s"); st.State != StateOffline || st.Error == "" {
		t.Errorf("status = %+v, want offline", st)
	}
}

func TestHeldConflictsShowAsHeld(t *testing.T) {
	f := newFixture(t, "sync", `conflicts = "ask"`)
	write(t, f.local, "a.txt", "base")
	f.start()
	f.d.SyncNow("s")
	f.settled("s")
	write(t, f.local, "a.txt", "local edit")
	write(t, f.mount, "a.txt", "remote edit!")
	os.Chtimes(filepath.Join(f.mount, "a.txt"), time.Now().Add(time.Minute), time.Now().Add(time.Minute))

	f.d.SyncNow("s")
	st := f.waitFor("s", "held", func(st Status) bool { return st.State == StateHeld })
	if len(st.Conflicts) != 1 || st.Conflicts[0].Path != "a.txt" {
		t.Errorf("conflicts = %+v", st.Conflicts)
	}
}

func TestUnknownAndDisabledShuttles(t *testing.T) {
	f := newFixture(t, "push", "enabled = false")
	f.start()
	if err := f.d.SyncNow("nope"); !errors.Is(err, ErrUnknownShuttle) {
		t.Errorf("SyncNow(nope) = %v", err)
	}
	if err := f.d.SyncNow("s"); !errors.Is(err, ErrDisabled) {
		t.Errorf("SyncNow(disabled) = %v", err)
	}
	if st := f.d.Statuses()[0]; st.State != StateDisabled {
		t.Errorf("status = %+v", st)
	}
}

func TestDefaultPaths(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/cfg")
	t.Setenv("XDG_STATE_HOME", "/st")
	p, err := DefaultPaths()
	if err != nil || p.Config != "/cfg/shuttle/config.toml" || p.StateDir != "/st/shuttle" {
		t.Errorf("DefaultPaths = %+v, %v", p, err)
	}
}
