// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package daemon

import (
	"sync"
	"testing"
	"time"
)

// A shuttle's status changes go out in the order they happened, whichever
// goroutine reports them: the share probe's, taken before a run started,
// must not arrive after the run's "running" and show the shuttle idle.
func TestStatusChangesInOrder(t *testing.T) {
	f := newFixture(t, "push", "")
	var mu sync.Mutex
	var last time.Time
	backwards := 0
	d, err := New(&Options{Paths: f.paths, Store: noKeyring{t}, OnChange: func(st *Status) {
		mu.Lock()
		defer mu.Unlock()
		if st.LastRun.Before(last) {
			backwards++
		}
		last = st.LastRun
	}})
	if err != nil {
		t.Fatal(err)
	}
	r := d.runners["s"]
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 500 {
				r.mu.Lock()
				r.st.LastRun = r.st.LastRun.Add(time.Nanosecond) // a change, then its report
				r.mu.Unlock()
				r.changed()
			}
		})
	}
	wg.Wait()
	if backwards > 0 {
		t.Errorf("%d of 4000 changes arrived after a later one", backwards)
	}
}
