// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package engine

import (
	"io"
	"sync"
)

// Phase is the stage a run is in.
type Phase string

const (
	PhaseScanning  Phase = "scanning"  // listing both sides
	PhaseComparing Phase = "comparing" // reading files on both sides to see if they differ
	PhaseSyncing   Phase = "syncing"   // carrying out the plan
)

// Progress is how far a run has got.
type Progress struct {
	Phase Phase
	// Done of Total: entries listed while scanning (Total is 0, not known
	// yet), files compared while comparing, files and folders copied,
	// created or deleted while syncing.
	Done, Total int
	// Bytes of TotalBytes: file contents copied while syncing, of what the
	// planned copies hold.
	Bytes, TotalBytes int64
	// Up and Down count the bytes sent to the share and read from it so far
	// in the run, compares included, as they go: the daemon's transfer rates
	// come from them.
	Up, Down int64
	Path     string // the file being copied or compared; "" between files
}

// Tracker is where a run keeps its Progress, for another goroutine to read.
// A nil *Tracker tracks nothing.
type Tracker struct {
	mu sync.Mutex
	p  Progress
}

// Progress returns how far the run has got.
func (t *Tracker) Progress() Progress {
	if t == nil {
		return Progress{}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.p
}

func (t *Tracker) update(f func(*Progress)) {
	if t == nil {
		return
	}
	t.mu.Lock()
	f(&t.p)
	t.mu.Unlock()
}

// phase starts a phase of total items.
func (t *Tracker) phase(ph Phase, total int, totalBytes int64) {
	t.update(func(p *Progress) {
		p.Phase, p.Done, p.Total, p.Bytes, p.TotalBytes, p.Path = ph, 0, total, 0, totalBytes, ""
	})
}

// counts reports whether an action counts towards a syncing phase's Done
// and Total: the ones that touch files. Adopting and forgetting only
// update the records.
func counts(a *Action) bool {
	return !a.skip && a.Op != OpAdopt && a.Op != OpForget && a.Op != OpCompare
}

func countOps(actions []Action, ok func(*Action) bool) int {
	n := 0
	for i := range actions {
		if ok(&actions[i]) {
			n++
		}
	}
	return n
}

// copyBytes is what the planned copies hold.
func copyBytes(actions []Action) int64 {
	local, remote := copyBytesTo(actions)
	return local + remote
}

// counted counts what passes through r as transferred, towards the share
// when up is set, and as copied file contents when copied is set.
type counted struct {
	r          io.Reader
	t          *Tracker
	up, copied bool
}

func (c *counted) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if n > 0 {
		c.t.update(func(pr *Progress) {
			if c.up {
				pr.Up += int64(n)
			} else {
				pr.Down += int64(n)
			}
			if c.copied {
				pr.Bytes += int64(n)
			}
		})
	}
	return n, err
}
