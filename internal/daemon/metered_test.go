// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package daemon

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// On a metered connection a shuttle's own runs wait, paused for that alone,
// and start once the connection is not metered.
func TestMeteredHoldsRunsUntilUnmetered(t *testing.T) {
	f := newFixture(t, "push", "")
	write(t, f.local, "a.txt", "a")
	f.start()
	f.d.SetMetered(true)
	f.runnerOf("s").trigger() // as a watcher or the schedule would
	st := f.waitFor("s", "waiting, metered", func(st Status) bool { return st.Metered && st.Pending })
	if st.State != StatePaused || st.Paused || exists(f.mount, "a.txt") {
		t.Fatalf("status %+v: want paused for the metered connection, not by the user, and nothing copied", st)
	}
	// Nothing to wait for but the absence of a run: give the loop the time
	// it would take to start one.
	for deadline := time.Now().Add(500 * time.Millisecond); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if n := f.runCount(); n != 0 {
			t.Fatalf("%d runs started: a held run must not start at all", n)
		}
	}
	f.d.SetMetered(false)
	if st := f.settled("s"); st.Copied != 1 || st.Metered || !exists(f.mount, "a.txt") {
		t.Errorf("after the connection stopped being metered: %+v", st)
	}
}

// A connection that becomes metered mid-run stops it after the file in
// progress, as a pause does; the rest follows when it is not metered.
func TestMeteredStopsARunAfterItsFile(t *testing.T) {
	f := newFixture(t, "push", slow)
	slowFiles(t, f.local, 3)
	f.start()
	f.runnerOf("s").trigger()
	f.waitCopying(f.mount)
	f.d.SetMetered(true)

	st := f.waitFor("s", "the run to stop", func(st Status) bool { return st.Metered })
	pushed := 0
	for i := range 3 {
		if exists(f.mount, fmt.Sprintf("f%d.bin", i)) {
			pushed++
		}
	}
	if pushed < 1 || pushed == 3 || !st.Pending {
		t.Errorf("pushed %d of 3, status %+v; want the file in progress finished, not all, the rest pending", pushed, st)
	}
	entries, _ := os.ReadDir(f.mount)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".shuttle-tmp-") {
			t.Errorf("half-written file left: %s", e.Name())
		}
	}
	f.d.SetMetered(false)
	f.settled("s")
	for i := range 3 {
		if !exists(f.mount, fmt.Sprintf("f%d.bin", i)) {
			t.Errorf("f%d.bin missing once the connection was not metered", i)
		}
	}
}

// A run the user asks for runs on a metered connection, and is not stopped
// by one either.
func TestSyncNowRunsOnAMeteredConnection(t *testing.T) {
	f := newFixture(t, "push", slow)
	slowFiles(t, f.local, 2)
	f.start()
	f.d.SetMetered(true)
	if err := f.d.SyncNow("s"); err != nil {
		t.Fatal(err)
	}
	f.waitCopying(f.mount)
	f.d.SetMetered(true) // again, mid-run: nothing changes
	st := f.settled("s")
	if st.Copied != 2 || f.runCount() != 1 {
		t.Errorf("status %+v after %d runs; want both files in the one run the user asked for", st, f.runCount())
	}
}

// A shuttle set to run on metered connections is not held by one.
func TestMeteredRunSetting(t *testing.T) {
	f := newFixture(t, "push", `metered = "run"`)
	write(t, f.local, "a.txt", "a")
	f.start()
	f.d.SetMetered(true)
	f.runnerOf("s").trigger()
	if st := f.settled("s"); st.Copied != 1 || st.Metered {
		t.Errorf("status %+v: a shuttle set to run must run on a metered connection", st)
	}
}
