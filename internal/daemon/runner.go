// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

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

	"github.com/fqazzazee/shuttle/internal/activity"
	"github.com/fqazzazee/shuttle/internal/config"
	"github.com/fqazzazee/shuttle/internal/engine"
	"github.com/fqazzazee/shuttle/internal/fsys"
	"github.com/fqazzazee/shuttle/internal/shares"
	"github.com/fqazzazee/shuttle/internal/state"
)

// runner runs one shuttle, one run at a time. A reload that changes the
// shuttle replaces its runner with a new one.
type runner struct {
	d       *Daemon
	cfg     *config.Config // the config it was made from
	shuttle *config.Shuttle
	db      *state.DB // open between openState and stop
	// activity is the shuttle's activity log, open with db; nil if it
	// could not be opened.
	activity *activity.Log
	// kick wakes the loop. It holds at most one wake-up, so any number of
	// triggers during a run add up to one more run.
	kick     chan struct{}
	quit     chan struct{}  // closed to stop the triggers
	bg       sync.WaitGroup // the triggers: share probe, schedule, watcher
	loopDone chan struct{}  // closed when the loop has returned

	// notifyMu keeps changed's snapshot and its sending together, so
	// status changes go out in the order they happened. Taken before mu.
	notifyMu sync.Mutex

	mu        sync.Mutex
	stopping  bool // the run in progress finishes its file; no more start
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

func newRunner(d *Daemon, cfg *config.Config, s *config.Shuttle) *runner {
	r := &runner{d: d, cfg: cfg, shuttle: s, kick: make(chan struct{}, 1), quit: make(chan struct{})}
	r.st = Status{Name: s.Name, Mode: s.Settings.Mode, State: StateIdle}
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
func (r *runner) shouldStop() bool { return r.isPaused() || r.isStopping() }

func (r *runner) isStopping() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stopping
}

func (r *runner) loop() {
	for range r.kick {
		if r.isStopping() {
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

// changed sends the current status to the listeners. Several goroutines
// report changes (the run, the share probe, triggers): without notifyMu, a
// snapshot the probe took before a run started could go out after the
// run's "running", and show the shuttle idle mid-run.
func (r *runner) changed() {
	r.notifyMu.Lock()
	defer r.notifyMu.Unlock()
	st := r.status()
	r.d.notify(&st)
}

func (r *runner) runOnce(requested bool) {
	r.runMu.Lock()
	defer r.runMu.Unlock()
	log := r.d.o.Log.With("shuttle", r.shuttle.Name)
	r.changed() // the loop has set StateRunning
	log.Info("run started")

	t := &engine.Tracker{}
	stopFollowing := r.followProgress(t)
	res, err := r.sync(t)
	stopFollowing()
	// Even a failed run may have synced some files.
	r.countSynced()
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
	// Still shown as running until its events are out: whoever sees the run
	// over can count on them having been sent.
	st.State = StateRunning
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
	r.logRun(res, err)
	r.mu.Lock()
	r.st.State = state
	r.mu.Unlock()

	if err != nil && !errors.Is(err, engine.ErrStopped) {
		log.Warn("run failed", "state", state, "err", err)
	} else {
		log.Info("run ended", "state", state, "copied", res.Copied, "deleted", res.Deleted,
			"conflicts", len(res.Conflicts), "errors", len(res.Errors))
	}
}

// countSynced counts what the shuttle has synced, for its status.
func (r *runner) countSynced() {
	files, size, err := r.db.Totals()
	if err != nil {
		r.d.o.Log.Warn("cannot count the synced files", "shuttle", r.shuttle.Name, "err", err)
		return
	}
	r.mu.Lock()
	r.st.Files, r.st.Size = files, size
	r.mu.Unlock()
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
	rem, err := shares.OpenRemote(openCtx, r.cfg, r.shuttle, r.d.o.Store)
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
		LocalName: r.d.hostname, RemoteName: r.shuttle.Share, Cap: r.d.global,
		Stop: r.shouldStop, Now: r.d.o.Now, OnAction: r.onAction,
	}
}

// sync opens both sides and runs the engine once.
func (r *runner) sync(t *engine.Tracker) (*engine.Result, error) {
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
	o.AllowDeletions, o.Tracker = allow, t
	res, err := engine.Run(ctx, o)
	r.measure(local, remote)
	return res, err
}

// spaceTimeout bounds asking a side how much room it has: a share that
// stopped answering mid-run must not hold the run's end.
const spaceTimeout = 5 * time.Second

// measure records how much room each side has, while both are open. A
// side that cannot tell keeps what it last told.
func (r *runner) measure(local, remote fsys.FS) {
	ctx, cancel := context.WithTimeout(context.Background(), spaceTimeout)
	defer cancel()
	l, lErr := fsys.SpaceOf(ctx, local)
	rem, rErr := fsys.SpaceOf(ctx, remote)
	r.mu.Lock()
	defer r.mu.Unlock()
	if lErr == nil && l.Total > 0 {
		r.st.LocalSpace = l
	}
	if rErr == nil && rem.Total > 0 {
		r.st.RemoteSpace = rem
	}
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
// folder, and the folder on the server the share reaches as its user,
// whatever the share is called. Another user may see other files, so it is
// a new pair: starting afresh deletes nothing, while files the new user
// cannot see would read as deleted against the old history.
func pairOf(cfg *config.Config, s *config.Shuttle) string {
	sh := cfg.Shares[s.Share]
	var server string
	switch sh.Type {
	case config.ShareSMB:
		// SMB names are case-insensitive: host, share, user and domain.
		server = strings.ToLower(fmt.Sprintf("smb://%s;%s@%s:%d/%s", sh.Domain, sh.User, sh.Host, cmp.Or(sh.Port, 445), sh.Share))
	case config.ShareNFS3:
		// The IDs as sent: a uid spelled out that is the user's own is the
		// same user.
		uid, gid, _ := shares.NFSUser(&sh)
		server = fmt.Sprintf("nfs://%d:%d@%s:%d%s", uid, gid, strings.ToLower(sh.Host), cmp.Or(sh.Port, 2049), sh.Export)
	default:
		server = "path:" + sh.Path
	}
	return s.Local + "\n" + server + "\n" + s.Remote
}
