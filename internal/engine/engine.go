// SPDX-License-Identifier: GPL-2.0-or-later

// Package engine runs one shuttle: it scans both sides, compares them with
// the state of the last sync, plans what to copy, create and delete, and
// carries the plan out.
//
// Safety rules the engine keeps:
//   - A scan that fails anywhere stops the run before anything is planned, so
//     a partial listing is never read as deletions.
//   - Every action re-checks the paths it touches just before acting; anything
//     that changed since the scan is skipped and left for the next run.
//   - State records are written only after the change they describe is done
//     (see package state), and only for copies whose source did not change
//     while being read.
//
// This package must stay free of desktop-specific imports; see the
// portability rule in SPEC.md.
package engine

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"time"

	"github.com/fqazzazee/shuttle/internal/config"
	"github.com/fqazzazee/shuttle/internal/fsys"
	"github.com/fqazzazee/shuttle/internal/ignore"
	"github.com/fqazzazee/shuttle/internal/state"
)

// VersionsDir is the folder at the root of each side where versioning keeps
// replaced and deleted files. Scans never descend into it.
const VersionsDir = ".shuttle-versions"

// ModTimeWindow is how far apart two modification times can be and still
// count as equal, when a file of the same size exists on both sides with no
// sync history. It covers servers and file systems that round timestamps
// (FAT to 2 s, some SMB servers to 1 s). With history, files are compared
// by content instead.
const ModTimeWindow = 2 * time.Second

// ErrChanged marks an action skipped because a path changed after the scan.
var ErrChanged = errors.New("changed during the run; left for the next run")

// ErrStopped means Options.Stop asked the run to stop early: it finished the
// action in progress, recorded what it had done, and left the rest.
var ErrStopped = errors.New("stopped before finishing; the rest is left for the next run")

// Options are the inputs of one run.
type Options struct {
	Local, Remote fsys.FS
	State         *state.DB
	Settings      *config.Settings
	// LocalName and RemoteName label each side in conflict file names, e.g.
	// the machine's host name and the share's name. Empty means "local" and
	// "remote".
	LocalName, RemoteName string
	// Cap limits transfers across every shuttle (the global bandwidth cap),
	// on top of the shuttle's own limits. nil means no cap.
	Cap *Cap
	// AllowDeletions lets one run through the deletion guard, after the user
	// has confirmed that the deletions it held are wanted.
	AllowDeletions bool
	// Stop, if set, is asked before each action whether to stop. The action in
	// progress (a whole file copy, say) always finishes; the run then returns
	// ErrStopped. Pausing and shutting down use this.
	Stop func() bool
	// Tracker, if set, follows the run's progress as it goes.
	Tracker *Tracker
	// OnAction, if set, is called after each action with its outcome: nil when
	// it was carried out, an error wrapping ErrChanged when it was skipped.
	OnAction func(a *Action, err error)
	// Now returns the current time; nil means time.Now.
	Now func() time.Time
}

// Issue is a path a run could not handle, and why.
type Issue struct {
	Path   string
	Op     Op
	Reason string
	// Outcome says what was done about a conflict: "held", "newest wins: ...",
	// "kept both: ...".
	Outcome string
	// Local and Remote are a conflict's two copies as the run found them,
	// for the user to choose between; nil for a side without one.
	Local, Remote *fsys.FileInfo
}

// Result summarises a run.
type Result struct {
	Planned int
	Copied  int
	Created int // directories
	Deleted int
	Adopted int // already in sync, now recorded
	// Versioned counts files moved to the versions folder instead of being
	// overwritten or deleted; VersionsPruned, old versions retention removed.
	Versioned      int
	VersionsPruned int
	// TempsRemoved counts leftover temp files of interrupted writes removed.
	TempsRemoved  int
	BytesToLocal  int64
	BytesToRemote int64
	Conflicts     []Issue // changed on both sides, or a file/folder clash
	// Held is set when an unresolved "ask" conflict held the whole shuttle:
	// nothing but the conflicts was looked at.
	Held bool
	// HeldDeletions are deletions the deletion guard stopped: too large a
	// share of one side's files at once. They run once AllowDeletions is set.
	HeldDeletions []Issue
	Skipped       []Issue // changed during the run
	Errors        []Issue
	Unsupported   []string // symlinks, devices and the like: never synced
}

func newExecutor(ctx context.Context, o *Options, res *Result) *executor {
	e := &executor{ctx: ctx, o: o, res: res, now: o.Now,
		own: Limits{Up: NewLimiter(o.Settings.Bandwidth.Up), Down: NewLimiter(o.Settings.Bandwidth.Down)}}
	if e.now == nil {
		e.now = time.Now
	}
	if e.o.LocalName == "" || e.o.RemoteName == "" {
		named := *o
		named.LocalName, named.RemoteName = cmp.Or(o.LocalName, "local"), cmp.Or(o.RemoteName, "remote")
		e.o = &named
	}
	return e
}

// staleTemp is how old a leftover temp file must be before a run removes it.
// A write in progress keeps touching its temp file, so this only catches
// writes that died; the margin leaves alone another machine that is writing
// to the same share right now.
const staleTemp = time.Hour

// removeStaleTemps removes temp files left by writes that were interrupted,
// for example by a crash or a dropped connection. Failures are harmless (scans
// skip temp files) and are not reported.
func (e *executor) removeStaleTemps(f fsys.FS, temps []fsys.FileInfo) {
	for _, t := range temps {
		if e.now().Sub(t.ModTime) > staleTemp && f.Remove(e.ctx, t.Name) == nil {
			e.res.TempsRemoved++
		}
	}
}

// plan is Plan for a run: on a remote side that ignores case, the two scans
// are first lined up by matchCase.
func plan(o *Options, local, remote map[string]fsys.FileInfo, records map[string]state.Record) []Action {
	if !fsys.FoldsCase(o.Remote) {
		return Plan(o.Settings.Mode, o.Settings.Deletions, local, remote, records)
	}
	m := matchCase(local, remote, records)
	actions := slices.Concat(m.held, Plan(o.Settings.Mode, o.Settings.Deletions, m.local, m.remote, m.records))
	slices.SortStableFunc(actions, executionOrder)
	return actions
}

func onlyConflicts(actions []Action) []Action {
	var out []Action
	for _, a := range actions {
		if a.Op == OpConflict {
			out = append(out, a)
		}
	}
	return out
}

// Ignored is what a run of a shuttle with settings s leaves out of both
// scans: its ignore patterns, and what it does not select.
func Ignored(s *config.Settings) (*ignore.Matcher, error) {
	m, err := ignore.Compile(s.Ignore)
	if err != nil {
		return nil, err
	}
	m.Select(s.Include, s.Exclude)
	return m, nil
}

// Preview scans both sides and returns the plan without carrying it out.
func Preview(ctx context.Context, o *Options) ([]Action, error) {
	ignored, err := Ignored(o.Settings)
	if err != nil {
		return nil, err
	}
	local, remote, err := scanBoth(ctx, o.Local, o.Remote, ignored, o.Tracker)
	if err != nil {
		return nil, err
	}
	records, err := o.State.All()
	if err != nil {
		return nil, err
	}
	return plan(o, local.entries, remote.entries, records), nil
}

// Run syncs one shuttle once. The error is for problems that stop the whole
// run (a failed scan, an unusable state database, cancellation); problems
// with single paths are reported in the Result and the run carries on.
func Run(ctx context.Context, o *Options) (*Result, error) {
	res := &Result{}
	ignored, err := Ignored(o.Settings)
	if err != nil {
		return res, err
	}
	local, remote, err := scanBoth(ctx, o.Local, o.Remote, ignored, o.Tracker)
	if err != nil {
		return res, err
	}
	res.Unsupported = slices.Concat(local.unsupported, remote.unsupported)
	records, err := o.State.All()
	if err != nil {
		return res, err
	}
	actions := plan(o, local.entries, remote.entries, records)
	res.Planned = len(actions)
	if !o.AllowDeletions {
		actions, res.HeldDeletions = guardDeletions(actions, local.entries, remote.entries, o.Settings.DeletionGuard)
	}

	e := newExecutor(ctx, o, res)
	e.removeStaleTemps(o.Local, local.temps)
	e.removeStaleTemps(o.Remote, remote.temps)
	o.Tracker.phase(PhaseComparing, countOps(actions, func(a *Action) bool { return a.Op == OpCompare }), 0)
	if err := e.compareAll(actions); err != nil {
		return res, err
	}
	if o.Settings.Conflicts == config.ConflictAsk && o.Settings.ConflictHold == config.HoldShuttle {
		if conflicts := onlyConflicts(actions); len(conflicts) > 0 {
			// Hold the shuttle: report the conflicts, change nothing else.
			for i := range conflicts {
				e.hold(&conflicts[i])
			}
			res.Held = true
			return res, e.flush() // records of identical files found by compares
		}
	}
	o.Tracker.phase(PhaseSyncing, countOps(actions, counts), copyBytes(actions))
	if err := e.run(actions); err != nil {
		return res, err
	}
	e.prune(o.Local, "local")
	e.prune(o.Remote, "remote")
	if len(res.Errors) == 0 {
		if err := o.State.SetLastComplete(e.now()); err != nil {
			return res, err
		}
	}
	return res, nil
}
