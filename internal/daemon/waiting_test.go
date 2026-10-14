// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package daemon

import (
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fqazzazee/shuttle/internal/config"
)

// waitWhile makes the fixture's daemon hold every shuttle for reason while
// it is set, as the Android app does for a permission it lacks.
func waitWhile(f *fixture, reason *atomic.Value) {
	f.build()
	f.d.o.Waiting = func(*config.Config, *config.Shuttle) string { return reason.Load().(string) }
	f.run()
}

// A shuttle waiting for a permission starts no run, not even Sync now, says
// why, and runs what was asked once Recheck finds it can.
func TestWaitingHoldsEveryRun(t *testing.T) {
	f := newFixture(t, "push", "")
	write(t, f.local, "a.txt", "a")
	var reason atomic.Value
	reason.Store("needs All files access")
	waitWhile(f, &reason)
	if err := f.d.SyncNow("s"); err != nil {
		t.Fatal(err)
	}
	st := f.waitFor("s", "waiting", func(st Status) bool { return st.Pending })
	if st.State != StatePaused || st.Paused || st.Waiting != "needs All files access" {
		t.Fatalf("status %+v: want paused for the permission, not by the user", st)
	}
	for deadline := time.Now().Add(500 * time.Millisecond); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if n := f.runCount(); n != 0 {
			t.Fatalf("%d runs started while waiting", n)
		}
	}
	reason.Store("")
	f.d.Recheck()
	if st := f.settled("s"); st.Copied != 1 || st.Waiting != "" || !exists(f.mount, "a.txt") {
		t.Errorf("after the permission was given: %+v", st)
	}
}

// A permission taken back mid-run stops the run after the file in progress.
func TestWaitingStopsARunAfterItsFile(t *testing.T) {
	f := newFixture(t, "push", slow)
	slowFiles(t, f.local, 3)
	var reason atomic.Value
	reason.Store("")
	waitWhile(f, &reason)
	f.runnerOf("s").trigger()
	f.waitCopying(f.mount)
	reason.Store("needs All files access")
	f.d.Recheck()

	st := f.waitFor("s", "the run to stop", func(st Status) bool { return st.Waiting != "" && st.State == StatePaused })
	pushed := 0
	for i := range 3 {
		if exists(f.mount, fmt.Sprintf("f%d.bin", i)) {
			pushed++
		}
	}
	if pushed == 3 || !st.Pending {
		t.Errorf("pushed %d of 3, status %+v; want the run stopped, the rest pending", pushed, st)
	}
}

// The app keeps its foreground service while an enabled shuttle is
// real-time, and only then.
func TestTriggered(t *testing.T) {
	f := newFixture(t, "push", `triggers = ["realtime", "schedule"]
interval = "1h"`)
	f.start()
	if !f.d.Triggered(config.TriggerRealtime) || !f.d.Triggered(config.TriggerSchedule) || f.d.Triggered(config.TriggerShareAvailable) {
		t.Error("triggers of the enabled shuttle not seen")
	}
	if err := f.d.SetEnabled("s", false); err != nil {
		t.Fatal(err)
	}
	f.waitFor("s", "disabled", func(st Status) bool { return st.State == StateDisabled })
	if f.d.Triggered(config.TriggerRealtime) {
		t.Error("a disabled shuttle's trigger counted")
	}
}

// ProbeNow finds a share that came back without waiting for the next probe,
// and a share-available shuttle runs.
func TestProbeNow(t *testing.T) {
	fast(t)
	probeEvery = time.Hour // only the probe at start, and ProbeNow's
	f := newFixture(t, "push", `triggers = ["share-available"]`)
	write(t, f.local, "a.txt", "a")
	gone := f.mount + ".gone"
	if err := os.Rename(f.mount, gone); err != nil {
		t.Fatal(err)
	}
	f.start()
	f.waitFor("s", "the probe at start", func(st Status) bool { return st.Share == ShareOffline })
	if err := os.Rename(gone, f.mount); err != nil {
		t.Fatal(err)
	}
	f.d.ProbeNow()
	if st := f.waitFor("s", "the share back, and the run", func(st Status) bool { return st.Copied == 1 }); st.Share != ShareOnline {
		t.Errorf("status = %+v", st)
	}
}

func TestConfigAndNextRuns(t *testing.T) {
	f := newFixture(t, "push", `triggers = ["schedule"]
interval = "1h"`)
	f.start()
	got := f.d.Config().Shuttles
	if len(got) != 1 || got[0].Name != "s" || got[0].Local != f.local || !got[0].Settings.Has(config.TriggerSchedule) {
		t.Errorf("Config().Shuttles = %+v", got)
	}
	f.settled("s") // the run at start: it never ran before
	next := f.d.NextRuns()["s"]
	if d := time.Until(next); d < 59*time.Minute || d > time.Hour {
		t.Errorf("NextRuns = %v, want an hour after the run", next)
	}
}

// A slot the scheduler triggered but whose run has not happened is still
// due: a daemon the Android app stops at once must not push it an
// interval away.
func TestNextRunsCountFromRunsThatEnded(t *testing.T) {
	f := newFixture(t, "push", `triggers = ["schedule"]
interval = "1h"`)
	f.build()
	f.d.o.Waiting = func(*config.Config, *config.Shuttle) string { return "held, so the due run cannot start" }
	f.run()
	f.waitFor("s", "the due run triggered", func(st Status) bool { return st.Pending })
	if next := f.d.NextRuns()["s"]; time.Until(next) > time.Second {
		t.Errorf("NextRuns = %v, want now: the run never happened", next)
	}
}
