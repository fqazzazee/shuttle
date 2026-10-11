// SPDX-License-Identifier: GPL-2.0-or-later

package daemon

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
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
	requested bool            // the user asked for the pending run (SyncNow)
	attempted time.Time       // when a run was last triggered by the schedule, or completed before start
	own       map[string]bool // local paths this shuttle's run wrote; see startOwnWrites
	ownUntil  time.Time

	allowDeletions bool       // the next run may pass the deletion guard
	runMu          sync.Mutex // held by a run, and by Resolve: they never overlap
	announced      announced
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
		requested := r.requested
		if run {
			// Together, under one lock: in between, the status would claim
			// "idle, nothing pending" while a run is about to start.
			r.pending, r.requested = false, false
			r.st.State = StateRunning
		}
		r.mu.Unlock()
		if run {
			r.runOnce(requested)
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

// share is what the last probe found: ShareOnline, ShareOffline or "".
func (r *runner) share() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.st.Share
}

func (r *runner) changed() {
	st := r.status()
	r.d.notify(&st)
}

func (r *runner) runOnce(requested bool) {
	r.runMu.Lock()
	defer r.runMu.Unlock()
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
		r.requested = r.requested || requested
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
	done := *st
	r.mu.Unlock()

	if !errors.Is(err, engine.ErrStopped) {
		events := r.runEvents(&done, res != nil, requested)
		for i := range events {
			r.d.event(&events[i])
		}
	}
	if isOffline(err) {
		// Tell at once, rather than at the next probe.
		r.shareChanged(false)
	}

	if err != nil && !errors.Is(err, engine.ErrStopped) {
		log.Warn("run failed", "state", state, "err", err)
	} else {
		log.Info("run ended", "state", state, "copied", res.Copied, "deleted", res.Deleted,
			"conflicts", len(res.Conflicts), "errors", len(res.Errors))
	}
}

// open opens both sides of the shuttle. The caller closes them.
func (r *runner) open(ctx context.Context) (local, remote fsys.FS, err error) {
	l, err := fsys.OpenLocal(r.shuttle.Local)
	if err != nil {
		// Never create it: a missing folder is often an unmounted drive, and
		// syncing an empty stand-in would read as everything deleted.
		return nil, nil, fmt.Errorf("local folder %s: %w (is the drive mounted?)", r.shuttle.Local, err)
	}
	openCtx, cancel := context.WithTimeout(ctx, r.d.o.KeyringTimeout)
	defer cancel()
	rem, err := shares.OpenRemote(openCtx, r.d.cfg, r.shuttle, r.d.o.Store)
	if err != nil {
		l.Close()
		return nil, nil, err
	}
	return l, rem, nil
}

// options are the engine options for this shuttle's two sides.
func (r *runner) options(local, remote fsys.FS) *engine.Options {
	return &engine.Options{
		Local: local, Remote: remote, State: r.db, Settings: &r.shuttle.Settings,
		LocalName: r.d.hostname, RemoteName: r.shuttle.Share,
		Stop: r.shouldStop, Now: r.d.o.Now, OnAction: r.noteOwnWrite,
	}
}

// sync opens both sides and runs the engine once.
func (r *runner) sync() (*engine.Result, error) {
	// Runs are not cancelled by shutdown: Stop ends them after the current
	// file instead. Only waiting for the keyring and the share is bounded.
	ctx := context.Background()
	local, remote, err := r.open(ctx)
	if err != nil {
		return nil, err
	}
	defer local.Close()
	defer remote.Close()

	r.mu.Lock()
	allow := r.allowDeletions
	r.allowDeletions = false
	r.mu.Unlock()

	r.startOwnWrites()
	defer r.endOwnWrites()
	o := r.options(local, remote)
	o.AllowDeletions = allow
	return engine.Run(ctx, o)
}

// isOffline tells "the share's server cannot be reached right now" apart
// from other failures: the connection could not be made or the name did not
// resolve. A missing folder is an error, not offline.
func isOffline(err error) bool {
	var netErr *net.OpError
	var dnsErr *net.DNSError
	return errors.As(err, &netErr) || errors.As(err, &dnsErr)
}

// pairOf names the two folders a shuttle syncs, for state.Bind: the local
// folder, and the folder on the server the share reaches, whatever the
// share is called. A user name, a password or a setting changing is not a
// new pair; the server, the share on it or either folder is.
func pairOf(cfg *config.Config, s *config.Shuttle) string {
	sh := cfg.Shares[s.Share]
	var server string
	switch sh.Type {
	case config.ShareSMB:
		// Host names and SMB share names are case-insensitive.
		server = fmt.Sprintf("smb://%s:%d/%s", strings.ToLower(sh.Host), cmp.Or(sh.Port, 445), strings.ToLower(sh.Share))
	case config.ShareNFS3:
		server = fmt.Sprintf("nfs://%s:%d%s", strings.ToLower(sh.Host), cmp.Or(sh.Port, 2049), sh.Export)
	default:
		server = "path:" + sh.Path
	}
	return s.Local + "\n" + server + "\n" + s.Remote
}
