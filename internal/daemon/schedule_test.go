// SPDX-License-Identifier: GPL-2.0-or-later

package daemon

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fqazzazee/shuttle/internal/config"
	"github.com/fqazzazee/shuttle/internal/state"
)

func scheduled(interval time.Duration, at ...config.TimeOfDay) *config.Settings {
	s := config.Defaults()
	s.Mode = config.ModePush
	s.Triggers = []config.Trigger{config.TriggerSchedule}
	s.Interval, s.At = interval, at
	return &s
}

func TestNextDue(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("no tzdata:", err)
	}
	at := func(y int, m time.Month, d, h, min int) time.Time { return time.Date(y, m, d, h, min, 0, 0, ny) }
	two := config.TimeOfDay{Hour: 2}
	tests := []struct {
		name         string
		s            *config.Settings
		lastRun, now time.Time
		want         time.Time
	}{
		{"interval, never ran: now",
			scheduled(time.Hour), time.Time{}, at(2026, 5, 1, 9, 0), at(2026, 5, 1, 9, 0)},
		{"interval counts from the last run",
			scheduled(time.Hour), at(2026, 5, 1, 9, 0), at(2026, 5, 1, 9, 20), at(2026, 5, 1, 10, 0)},
		{"interval overdue: now",
			scheduled(time.Hour), at(2026, 5, 1, 6, 0), at(2026, 5, 1, 9, 20), at(2026, 5, 1, 9, 20)},
		{"at: later today",
			scheduled(0, two), at(2026, 5, 1, 2, 0), at(2026, 5, 1, 1, 0), at(2026, 5, 1, 2, 0)},
		{"at: passed today and already ran, so tomorrow",
			scheduled(0, two), at(2026, 5, 1, 2, 0), at(2026, 5, 1, 9, 0), at(2026, 5, 2, 2, 0)},
		{"at: missed while off, so now (once)",
			scheduled(0, two), at(2026, 4, 29, 2, 0), at(2026, 5, 1, 9, 0), at(2026, 5, 1, 9, 0)},
		{"at: never ran waits for the slot",
			scheduled(0, two), time.Time{}, at(2026, 5, 1, 9, 0), at(2026, 5, 2, 2, 0)},
		{"two times a day: the nearer",
			scheduled(0, two, config.TimeOfDay{Hour: 14}), at(2026, 5, 1, 2, 0), at(2026, 5, 1, 9, 0), at(2026, 5, 1, 14, 0)},
		// 2026-11-01: clocks fall back 02:00 -> 01:00; 01:30 happens twice.
		{"fall back: runs once",
			scheduled(0, config.TimeOfDay{Hour: 1, Minute: 30}), at(2026, 10, 31, 1, 30), at(2026, 11, 1, 0, 0),
			time.Date(2026, 11, 1, 1, 30, 0, 0, ny)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := nextDue(tt.s, tt.lastRun, tt.now)
			if !ok || !got.Equal(tt.want) {
				t.Errorf("nextDue = %v, %v; want %v", got, ok, tt.want)
			}
		})
	}
	// 2026-03-08: clocks jump 02:00 -> 03:00, so 02:30 does not exist that
	// day. Go picks some instant near it; what matters is that the run happens
	// that day, once.
	t.Run("spring forward: the missing time runs once that day", func(t *testing.T) {
		s := scheduled(0, config.TimeOfDay{Hour: 2, Minute: 30})
		due, ok := nextDue(s, at(2026, 3, 7, 2, 30), at(2026, 3, 8, 0, 0))
		if !ok || due.Before(at(2026, 3, 8, 0, 30)) || due.After(at(2026, 3, 8, 4, 0)) {
			t.Fatalf("due %v; want early on 2026-03-08", due)
		}
		again, _ := nextDue(s, due, due.Add(time.Minute))
		if y, m, d := again.Date(); y != 2026 || m != 3 || d != 9 {
			t.Errorf("after running at %v, next due %v; want the next day, not again", due, again)
		}
	})
	// 2026-11-01: clocks fall back 02:00 -> 01:00, so 01:30 happens twice. The
	// run must not happen at both.
	t.Run("fall back: the repeated time runs once", func(t *testing.T) {
		s := scheduled(0, config.TimeOfDay{Hour: 1, Minute: 30})
		due, _ := nextDue(s, at(2026, 10, 31, 1, 30), at(2026, 11, 1, 0, 0))
		for _, later := range []time.Duration{time.Minute, 61 * time.Minute} { // during both 01:30s
			again, _ := nextDue(s, due, due.Add(later))
			if y, m, d := again.Date(); y != 2026 || m != 11 || d != 2 {
				t.Errorf("ran at %v; at %v the next run is %v, want the next day", due, due.Add(later), again)
			}
		}
	})
	manual := config.Defaults()
	if _, ok := nextDue(&manual, time.Time{}, time.Now()); ok {
		t.Error("a shuttle without the schedule trigger has a due time")
	}
}

// fast shortens the daemon's timers for a test.
func fast(t *testing.T) {
	old := [6]time.Duration{recheckEvery, probeEvery, probeTimeout, settleQuiet, settleMax, ownGrace}
	recheckEvery, probeEvery, probeTimeout = 20*time.Millisecond, 50*time.Millisecond, 500*time.Millisecond
	settleQuiet, settleMax, ownGrace = 100*time.Millisecond, 500*time.Millisecond, 300*time.Millisecond
	t.Cleanup(func() {
		recheckEvery, probeEvery, probeTimeout = old[0], old[1], old[2]
		settleQuiet, settleMax, ownGrace = old[3], old[4], old[5]
	})
}

func TestIntervalScheduleRunsByItself(t *testing.T) {
	fast(t)
	f := newFixture(t, "push", `triggers = ["schedule"]
interval = "1m"`)
	write(t, f.local, "a.txt", "a")
	f.build()
	// The config's floor is a minute; shorten it here, before Run starts.
	f.d.runners["s"].shuttle.Settings.Interval = 150 * time.Millisecond
	f.run()

	// Ends, not starts: the check below is about what a finished run did.
	f.waitFor("s", "two scheduled runs", func(Status) bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.ended >= 2
	})
	if !exists(f.mount, "a.txt") {
		t.Error("the scheduled run did not push")
	}
}

// A nightly push that was due while the machine was off runs at start.
func TestMissedScheduleRunsAtStart(t *testing.T) {
	fast(t)
	f := newFixture(t, "push", `triggers = ["schedule"]
at = ["02:00"]`)
	write(t, f.local, "a.txt", "a")
	if err := os.MkdirAll(f.paths.StateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := state.Open(filepath.Join(f.paths.StateDir, "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetLastComplete(time.Now().Add(-49 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	db.Close()

	f.start()
	f.settled("s")
	if !exists(f.mount, "a.txt") {
		t.Error("the missed nightly run did not catch up")
	}
}

func TestShareAvailableTrigger(t *testing.T) {
	fast(t)
	f := newFixture(t, "push", `triggers = ["share-available"]`)
	write(t, f.local, "a.txt", "a")
	// The share's folder is not there yet: like an unmounted NAS.
	away := f.mount + ".away"
	if err := os.Rename(f.mount, away); err != nil {
		t.Fatal(err)
	}
	f.start()
	f.waitFor("s", "the share seen offline", func(st Status) bool { return st.Share == ShareOffline })
	if f.runCount() != 0 {
		t.Fatal("ran while the share was away")
	}

	if err := os.Rename(away, f.mount); err != nil { // the share comes back
		t.Fatal(err)
	}
	st := f.waitFor("s", "a run after the share came back", func(st Status) bool {
		return st.Share == ShareOnline && !st.LastRun.IsZero() && st.State != StateRunning
	})
	if st.Copied != 1 || !exists(f.mount, "a.txt") {
		t.Errorf("status = %+v", st)
	}
	// Only the change to online triggers: while it stays online, later probes
	// must not start run after run.
	runs := f.runCount()
	time.Sleep(6 * probeEvery)
	if got := f.runCount(); got != runs {
		t.Errorf("%d more runs while the share stayed online, want none", got-runs)
	}
}

func TestShareStatusWithoutTheTrigger(t *testing.T) {
	fast(t)
	f := newFixture(t, "push", "")
	f.start()
	f.waitFor("s", "the share seen online", func(st Status) bool { return st.Share == ShareOnline })
	if f.runCount() != 0 {
		t.Error("a manual shuttle ran because its share is online")
	}
}
