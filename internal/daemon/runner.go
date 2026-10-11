// SPDX-License-Identifier: GPL-2.0-or-later

package daemon

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/fqazzazee/shuttle/internal/config"
	"github.com/fqazzazee/shuttle/internal/engine"
	"github.com/fqazzazee/shuttle/internal/fsys"
	"github.com/fqazzazee/shuttle/internal/shares"
	"github.com/fqazzazee/shuttle/internal/state"
)

// runner runs one shuttle, one run at a time.
type runner struct {
	d       *Daemon
	shuttle *config.Shuttle
	db      *state.DB
	// kick wakes the loop. It holds at most one wake-up, so any number of
	// triggers during a run add up to one more run.
	kick chan struct{}

	mu        sync.Mutex
	st        Status
	paused    bool // this shuttle, by itself
	pending   bool
	attempted time.Time       // when a run was last triggered by the schedule, or completed before start
	own       map[string]bool // local paths this shuttle's run wrote; see startOwnWrites
	ownUntil  time.Time
}

func newRunner(d *Daemon, s *config.Shuttle) *runner {
	r := &runner{d: d, shuttle: s, kick: make(chan struct{}, 1)}
	r.st = Status{Name: s.Name, State: StateIdle}
	if !s.Enabled {
		r.st.State = StateDisabled
	}
	return r
}

// lastAttempt is when the shuttle last ran or was due: what the schedule
// counts from.
func (r *runner) lastAttempt() time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.st.LastRun.After(r.attempted) {
		return r.st.LastRun
	}
	return r.attempted
}

func (r *runner) markAttempt(t time.Time) {
	r.mu.Lock()
	r.attempted = t
	r.mu.Unlock()
}

func (r *runner) wake() {
	select {
	case r.kick <- struct{}{}:
	default:
	}
}

func (r *runner) trigger() {
	r.mu.Lock()
	r.pending = true
	r.mu.Unlock()
	r.wake()
	r.changed()
}

func (r *runner) setPaused(p bool) {
	r.mu.Lock()
	r.paused = p
	r.mu.Unlock()
	r.pausedChanged()
}

// pausedChanged updates the status, and on resume starts any run that was
// waiting.
func (r *runner) pausedChanged() {
	if !r.shuttle.Enabled {
		return
	}
	r.mu.Lock()
	if !r.isPausedLocked() && r.pending {
		r.mu.Unlock()
		r.wake()
	} else {
		r.mu.Unlock()
	}
	r.changed()
}

func (r *runner) isPausedLocked() bool { return r.paused || r.d.isPausedAll() }

func (r *runner) isPaused() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.isPausedLocked()
}

// shouldStop is the engine's Stop: pausing or shutting down ends a run after
// the file in progress.
func (r *runner) shouldStop() bool { return r.isPaused() || r.d.isStopping() }

func (r *runner) loop() {
	for range r.kick {
		if r.d.isStopping() {
			return
		}
		r.mu.Lock()
		run := r.pending && !r.isPausedLocked()
		if run {
			// Together, under one lock: in between, the status would claim
			// "idle, nothing pending" while a run is about to start.
			r.pending = false
			r.st.State = StateRunning
		}
		r.mu.Unlock()
		if run {
			r.runOnce()
		}
		r.changed()
	}
}

// status returns a copy of the current status with the live flags filled in.
func (r *runner) status() Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	st := r.st
	st.Pending = r.pending
	st.Paused = r.isPausedLocked()
	if st.Paused && st.State != StateRunning && st.State != StateDisabled {
		st.State = StatePaused
	}
	return st
}

func (r *runner) changed() {
	if r.d.o.OnChange != nil {
		st := r.status()
		r.d.o.OnChange(&st)
	}
}

func (r *runner) runOnce() {
	log := r.d.o.Log.With("shuttle", r.shuttle.Name)
	r.changed() // the loop has set StateRunning
	log.Info("run started")

	res, err := r.sync()
	now := r.d.o.Now()

	r.mu.Lock()
	st := &r.st
	st.LastRun = now
	st.Error = ""
	switch {
	case errors.Is(err, engine.ErrStopped):
		st.State = StateIdle // shown as paused while paused; resumes as pending
		r.pending = true
	case isOffline(err):
		st.State, st.Error = StateOffline, err.Error()
	case err != nil:
		st.State, st.Error = StateError, err.Error()
	case len(res.Errors) > 0:
		st.State, st.Error = StateError, fmt.Sprintf("%d paths could not be synced", len(res.Errors))
	case held(res):
		st.State = StateHeld
	default:
		st.State, st.LastOK = StateIdle, now
	}
	if res != nil {
		st.Copied, st.Deleted = res.Copied, res.Deleted
		st.Conflicts, st.Errors, st.HeldDeletions = res.Conflicts, res.Errors, len(res.HeldDeletions)
	}
	state := st.State
	r.mu.Unlock()

	if err != nil && !errors.Is(err, engine.ErrStopped) {
		log.Warn("run failed", "state", state, "err", err)
	} else {
		log.Info("run ended", "state", state, "copied", res.Copied, "deleted", res.Deleted,
			"conflicts", len(res.Conflicts), "errors", len(res.Errors))
	}
}

// sync opens both sides and runs the engine once.
func (r *runner) sync() (*engine.Result, error) {
	// Runs are not cancelled by shutdown: Stop ends them after the current
	// file instead. Only waiting for the keyring and the share is bounded.
	ctx := context.Background()
	local, err := fsys.OpenLocal(r.shuttle.Local)
	if err != nil {
		// Never create it: a missing folder is often an unmounted drive, and
		// syncing an empty stand-in would read as everything deleted.
		return nil, fmt.Errorf("local folder %s: %w (is the drive mounted?)", r.shuttle.Local, err)
	}
	defer local.Close()

	openCtx, cancel := context.WithTimeout(ctx, r.d.o.KeyringTimeout)
	remote, err := shares.OpenRemote(openCtx, r.d.cfg, r.shuttle, r.d.o.Store)
	cancel()
	if err != nil {
		return nil, err
	}
	defer remote.Close()

	r.startOwnWrites()
	defer r.endOwnWrites()
	return engine.Run(ctx, &engine.Options{
		Local: local, Remote: remote, State: r.db, Settings: &r.shuttle.Settings,
		LocalName: r.d.hostname, RemoteName: r.shuttle.Share,
		Stop: r.shouldStop, Now: r.d.o.Now, OnAction: r.noteOwnWrite,
	})
}

// isOffline tells "the share's server cannot be reached right now" apart
// from other failures: the connection could not be made or the name did not
// resolve. A missing folder is an error, not offline.
func isOffline(err error) bool {
	var netErr *net.OpError
	var dnsErr *net.DNSError
	return errors.As(err, &netErr) || errors.As(err, &dnsErr)
}
