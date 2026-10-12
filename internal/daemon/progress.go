// SPDX-License-Identifier: GPL-2.0-or-later

package daemon

import (
	"time"

	"github.com/fqazzazee/shuttle/internal/engine"
)

// Progress is how far a shuttle's run has got; zero when it is not running.
type Progress struct {
	Phase engine.Phase
	// Done of Total, and Bytes of TotalBytes: see engine.Progress.
	Done, Total       int
	Bytes, TotalBytes int64
	Path              string // the file being copied or compared
	// Transfer rates to and from the share, in bytes per second, over the
	// last few seconds.
	RateUp, RateDown int64
}

// A run's progress is published this often, when it has moved, so that
// listeners on the bus get a steady trickle rather than one signal per
// read. Rates are averaged over rateWindow.
var progressEvery = 500 * time.Millisecond

const rateWindow = 3 * time.Second

// followProgress publishes t's progress in the status until the returned
// function is called, which clears it.
func (r *runner) followProgress(t *engine.Tracker) (stop func()) {
	quit, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		type sample struct {
			at       time.Time
			up, down int64
		}
		samples := []sample{{at: time.Now()}}
		tick := time.NewTicker(progressEvery)
		defer tick.Stop()
		for {
			select {
			case <-quit:
				return
			case <-tick.C:
			}
			p := t.Progress()
			now := time.Now()
			samples = append(samples, sample{now, p.Up, p.Down})
			for len(samples) > 2 && now.Sub(samples[1].at) >= rateWindow {
				samples = samples[1:]
			}
			first := samples[0]
			rate := func(n, was int64) int64 {
				return (n - was) * int64(time.Second) / max(int64(now.Sub(first.at)), 1)
			}
			next := Progress{Phase: p.Phase, Done: p.Done, Total: p.Total, Bytes: p.Bytes,
				TotalBytes: p.TotalBytes, Path: p.Path, RateUp: rate(p.Up, first.up), RateDown: rate(p.Down, first.down)}
			r.mu.Lock()
			moved := r.st.Progress != next
			r.st.Progress = next
			r.mu.Unlock()
			if moved {
				r.changed()
			}
		}
	}()
	return func() {
		close(quit)
		<-done
		r.mu.Lock()
		r.st.Progress = Progress{}
		r.mu.Unlock()
	}
}

// Rates returns the transfer rates of every shuttle together, to and from
// their shares, in bytes per second.
func (d *Daemon) Rates() (up, down int64) {
	all := d.Statuses()
	for i := range all {
		up += all[i].Progress.RateUp
		down += all[i].Progress.RateDown
	}
	return up, down
}
