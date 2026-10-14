// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package daemon

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fqazzazee/shuttle/internal/config"
	"github.com/fqazzazee/shuttle/internal/state"
)

// realtime starts a daemon whose shuttle s has the real-time trigger, with
// polling so rare it does not get in the way, and waits for the first run.
func realtimeFixture(t *testing.T, mode, extra string) *fixture {
	t.Helper()
	fast(t)
	f := newFixture(t, mode, `triggers = ["realtime"]
`+extra)
	f.build()
	f.d.runners["s"].shuttle.Settings.PollInterval = time.Hour
	f.run()
	f.settled("s") // a real-time shuttle runs once at start
	return f
}

// quiet waits long enough for any change to have settled into a run.
func quiet() { time.Sleep(4 * (settleQuiet + ownGrace)) }

func TestRealtimeRunsAtStart(t *testing.T) {
	fast(t)
	f := newFixture(t, "push", `triggers = ["realtime"]`)
	write(t, f.local, "a.txt", "a")
	f.start()
	f.settled("s")
	if !exists(f.mount, "a.txt") {
		t.Error("the first run did not push")
	}
}

// Changes made while the daemon was not running are only seen by a run, so
// a real-time shuttle runs at start even if its last run was a moment ago,
// as after a crash and restart.
func TestRealtimeRunsAtStartAfterARecentRun(t *testing.T) {
	fast(t)
	f := newFixture(t, "push", `triggers = ["realtime"]`)
	write(t, f.local, "while-down.txt", "w")
	if err := os.MkdirAll(f.paths.StateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := state.Open(filepath.Join(f.paths.StateDir, "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetLastComplete(time.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	db.Close()
	f.build()
	f.d.runners["s"].shuttle.Settings.PollInterval = time.Hour
	f.run()
	f.waitFor("s", "the run at start", func(Status) bool { return exists(f.mount, "while-down.txt") })
	quiet()
	if got := f.runCount(); got != 1 {
		t.Errorf("%d runs at start, want 1", got)
	}
}

func TestRealtimePushesALocalChange(t *testing.T) {
	f := realtimeFixture(t, "push", "")
	before := f.runCount()
	write(t, f.local, "new.txt", "n")
	f.waitFor("s", "the change to be pushed", func(Status) bool { return exists(f.mount, "new.txt") })
	quiet()
	if got := f.runCount() - before; got != 1 {
		t.Errorf("%d runs for one change, want 1", got)
	}
}

func TestABurstOfChangesIsOneRun(t *testing.T) {
	f := realtimeFixture(t, "push", "")
	before := f.runCount()
	for i := range 20 {
		write(t, f.local, "f"+strconv.Itoa(i), "x")
		time.Sleep(settleQuiet / 10)
	}
	f.waitFor("s", "the burst to be pushed", func(Status) bool { return exists(f.mount, "f19") })
	quiet()
	if got := f.runCount() - before; got != 1 {
		t.Errorf("%d runs for one burst, want 1", got)
	}
}

func TestNewFoldersAreWatched(t *testing.T) {
	f := realtimeFixture(t, "push", "")
	if err := os.MkdirAll(filepath.Join(f.local, "deep", "er"), 0o755); err != nil {
		t.Fatal(err)
	}
	quiet() // the folder's own run; its watch is in place now
	write(t, filepath.Join(f.local, "deep", "er"), "inside.txt", "i")
	f.waitFor("s", "the file in the new folder to be pushed", func(Status) bool {
		return exists(filepath.Join(f.mount, "deep", "er"), "inside.txt")
	})
}

// The daemon's own writes to the local folder (pulling files in) must not
// trigger another run: every run would otherwise cost a second full scan.
func TestOwnWritesDoNotRetrigger(t *testing.T) {
	fast(t)
	f := newFixture(t, "pull", `triggers = ["realtime"]`)
	for i := range 5 {
		write(t, f.mount, "f"+strconv.Itoa(i), "x")
	}
	if err := os.MkdirAll(filepath.Join(f.mount, "sub", "dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(f.mount, "sub", "dir"), "nested", "n")
	f.build()
	f.d.runners["s"].shuttle.Settings.PollInterval = time.Hour
	f.run()
	f.settled("s")
	quiet()
	if got := f.runCount(); got != 1 {
		t.Errorf("%d runs; the pull's own writes started %d more", got, got-1)
	}
	if !exists(filepath.Join(f.local, "sub", "dir"), "nested") {
		t.Error("the pull did not happen")
	}
}

// A change the user makes while a run is writing is not the run's own: it
// gets a run of its own afterwards.
func TestUserEditDuringARunTriggersAnother(t *testing.T) {
	fast(t)
	f := newFixture(t, "sync", `triggers = ["realtime"]
bandwidth = { down = "64KiB/s" }`)
	for i := range 2 {
		write(t, f.mount, "slow"+strconv.Itoa(i), strings.Repeat("x", 96<<10))
	}
	f.build()
	f.d.runners["s"].shuttle.Settings.PollInterval = time.Hour
	f.run()
	f.waitCopying(f.local) // the first run is writing into the local folder
	write(t, f.local, "mine.txt", "made during the run")
	f.waitFor("s", "the user's file to be pushed", func(Status) bool { return exists(f.mount, "mine.txt") })
	if got := f.runCount(); got != 2 {
		t.Errorf("%d runs, want 2: the first, and one for the user's edit", got)
	}
}

func TestIgnoredAndInternalChangesDoNotTrigger(t *testing.T) {
	f := realtimeFixture(t, "push", `ignore = ["*.tmp", "cache/"]`)
	before := f.runCount()
	write(t, f.local, "scratch.tmp", "x")
	write(t, f.local, ".shuttle-tmp-abc", "an in-progress write")
	os.MkdirAll(filepath.Join(f.local, ".shuttle-versions", "d"), 0o755)
	write(t, filepath.Join(f.local, ".shuttle-versions", "d"), "old~20261011-120000", "v")
	os.MkdirAll(filepath.Join(f.local, "cache", "x"), 0o755)
	write(t, filepath.Join(f.local, "cache", "x"), "blob", "b")
	quiet()
	if got := f.runCount() - before; got != 0 {
		t.Errorf("%d runs for changes that are never synced, want 0", got)
	}
}

// A shuttle that includes only some paths runs for changes there, not for
// changes to what it leaves out.
func TestChangesOutsideTheIncludedPathsDoNotTrigger(t *testing.T) {
	f := realtimeFixture(t, "push", `include = ["docs"]`)
	before := f.runCount()
	write(t, f.local, "other.txt", "x")
	os.MkdirAll(filepath.Join(f.local, "music", "a"), 0o755)
	write(t, filepath.Join(f.local, "music", "a"), "song", "s")
	quiet()
	if got := f.runCount() - before; got != 0 {
		t.Errorf("%d runs for changes that are not included, want 0", got)
	}
	os.MkdirAll(filepath.Join(f.local, "docs", "sub"), 0o755)
	write(t, filepath.Join(f.local, "docs", "sub"), "a.txt", "a")
	f.waitFor("s", "the included file to be pushed", func(Status) bool {
		return exists(filepath.Join(f.mount, "docs", "sub"), "a.txt")
	})
	if exists(f.mount, "other.txt") {
		t.Error("a file that is not included was pushed")
	}
}

// Changes on the share have no events here: polling picks them up.
func TestPollingPicksUpShareChanges(t *testing.T) {
	fast(t)
	f := newFixture(t, "sync", `triggers = ["realtime"]`)
	f.build()
	f.d.runners["s"].shuttle.Settings.PollInterval = 200 * time.Millisecond
	f.run()
	f.settled("s")
	write(t, f.mount, "from-the-share.txt", "s")
	f.waitFor("s", "the share's change to arrive", func(Status) bool { return exists(f.local, "from-the-share.txt") })
}

// If the folder cannot be watched (too many folders for inotify), the
// shuttle still works by polling, and its status says so.
func TestWatchFailureFallsBackToPolling(t *testing.T) {
	fast(t)
	old := newLocalWatcher
	newLocalWatcher = func(string, *config.Settings) (*localWatcher, error) { return nil, errTooManyWatches }
	t.Cleanup(func() { newLocalWatcher = old })

	f := newFixture(t, "push", `triggers = ["realtime"]`)
	f.build()
	f.d.runners["s"].shuttle.Settings.PollInterval = 200 * time.Millisecond
	f.run()
	st := f.waitFor("s", "the note", func(st Status) bool { return st.Note != "" })
	if !strings.Contains(st.Note, "max_user_watches") || !strings.Contains(st.Note, "checking every") {
		t.Errorf("note = %q", st.Note)
	}
	write(t, f.local, "a.txt", "a")
	f.waitFor("s", "polling to push", func(Status) bool { return exists(f.mount, "a.txt") })
}
