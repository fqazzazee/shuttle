// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package daemon

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fqazzazee/shuttle/internal/engine"
)

// A run's progress and rates are published while it runs, a few times a
// second, and cleared when it ends.
func TestProgressWhileRunning(t *testing.T) {
	if testing.Short() {
		t.Skip("timed")
	}
	old := progressEvery
	progressEvery = 100 * time.Millisecond
	t.Cleanup(func() { progressEvery = old })
	// 768 KiB at 256 KiB/s: about three seconds.
	f := newFixture(t, "push", `bandwidth = { up = "256KiB/s" }`)
	write(t, f.local, "big.bin", strings.Repeat("x", 768<<10))
	f.start()

	var mu sync.Mutex
	var seen []Status
	var rates [][2]int64
	stop := f.d.Watch(func(st *Status) {
		up, down := f.d.Rates()
		mu.Lock()
		seen = append(seen, *st)
		rates = append(rates, [2]int64{up, down})
		mu.Unlock()
	})
	defer stop()
	st := f.syncNow()
	if st.Progress != (Progress{}) {
		t.Errorf("progress after the run = %+v, want none", st.Progress)
	}
	if up, down := f.d.Rates(); up != 0 || down != 0 {
		t.Errorf("rates after the run = %d, %d", up, down)
	}

	mu.Lock()
	defer mu.Unlock()
	var copying []Status
	for _, s := range seen {
		if s.State == StateRunning && s.Progress.Phase == engine.PhaseSyncing && s.Progress.Path == "big.bin" {
			copying = append(copying, s)
		}
	}
	if len(copying) < 5 {
		t.Fatalf("%d progress updates while copying, want several; saw %+v", len(copying), seen)
	}
	last := copying[len(copying)-1].Progress
	if last.Total != 1 || last.TotalBytes != 768<<10 || last.Bytes == 0 || last.Bytes > last.TotalBytes {
		t.Errorf("progress = %+v", last)
	}
	for i := 1; i < len(copying); i++ {
		if copying[i].Progress.Bytes < copying[i-1].Progress.Bytes {
			t.Errorf("bytes went back: %+v then %+v", copying[i-1].Progress, copying[i].Progress)
		}
	}
	// Past the first second, the rate is close to the limit.
	r := last.RateUp
	if r < 128<<10 || r > 512<<10 || last.RateDown != 0 {
		t.Errorf("rates up %d, down %d B/s; want about 256 KiB/s up", r, last.RateDown)
	}
	sawRate := false
	for _, rt := range rates {
		sawRate = sawRate || rt[0] > 0
	}
	if !sawRate {
		t.Error("the daemon's overall upload rate was never above 0")
	}
}
