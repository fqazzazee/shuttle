// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package daemon

import (
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// A resume can land after the engine has stopped but before the run records
// that it is pending: it sees nothing to wake, so the run's end must.
func TestResumeWhileARunStopsWakesTheLoop(t *testing.T) {
	f := newFixture(t, "push", "")
	write(t, f.local, "a.txt", "a")
	f.paused = []string{"s"} // the engine stops at its first action
	f.build()
	r := f.runnerOf("s")
	if err := os.MkdirAll(f.paths.StateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := r.openState(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = closeAll([]*runner{r}) })
	// runOnce itself asks the time right after the engine returns; the
	// engine asks too, earlier, and a resume then would not stop the run.
	var once sync.Once
	f.d.o.Now = func() time.Time {
		pc, _, _, _ := runtime.Caller(1)
		if strings.HasSuffix(runtime.FuncForPC(pc).Name(), ".runOnce") {
			once.Do(func() {
				if err := f.d.SetPaused("s", false); err != nil {
					t.Error(err)
				}
			})
		}
		return time.Now()
	}

	r.runOnce(false)

	if r.isPaused() || exists(f.mount, "a.txt") {
		t.Fatal("the resume did not land between the engine stopping and the run's end: the test did not reach the window")
	}
	if !r.status().Pending {
		t.Fatal("the stopped run is not pending")
	}
	if len(r.kick) != 1 {
		t.Error("the pending run has nothing to wake it after the resume")
	}
}
