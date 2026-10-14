// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package daemon

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/fqazzazee/shuttle/internal/config"
	"github.com/fqazzazee/shuttle/internal/engine"
	"github.com/fqazzazee/shuttle/internal/fsys"
	"github.com/fqazzazee/shuttle/internal/ignore"
)

// A burst of changes becomes one run: it starts once the folder has been
// quiet for settleQuiet, and at most settleMax after the first change.
// ownGrace is how long after a run its own writes are still recognised:
// inotify delivers them with a little delay.
var (
	settleQuiet   = 2 * time.Second
	settleMax     = 10 * time.Second
	ownGrace      = 2 * time.Second
	runCheckEvery = 100 * time.Millisecond // how often queued changes wait for a run to end
)

// errTooManyWatches means the kernel's inotify watch limit was reached.
var errTooManyWatches = errors.New("too many folders to watch; raise fs.inotify.max_user_watches")

// localWatcher reports changes under one folder tree, as slash paths
// relative to its root. inotify is not recursive, so every folder has a
// watch of its own, and folders created later get one as they appear.
type localWatcher struct {
	w       *fsnotify.Watcher
	root    string
	ignored *ignore.Matcher
}

// newLocalWatcher is a variable so tests can make watching fail.
var newLocalWatcher = func(root string, s *config.Settings) (*localWatcher, error) {
	ignored, err := engine.Ignored(s)
	if err != nil {
		return nil, err
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	lw := &localWatcher{w: w, root: root, ignored: ignored}
	if err := lw.addTree(root); err != nil {
		w.Close()
		return nil, err
	}
	return lw, nil
}

func (lw *localWatcher) rel(p string) (string, bool) {
	r, err := filepath.Rel(lw.root, p)
	if err != nil || r == "." || strings.HasPrefix(r, "..") {
		return "", false
	}
	return filepath.ToSlash(r), true
}

// skip reports whether rel is never synced, so its changes do not matter:
// the versions folder, in-progress temp files, ignored paths.
func (lw *localWatcher) skip(rel string, isDir bool) bool {
	return rel == engine.VersionsDir || strings.HasPrefix(rel, engine.VersionsDir+"/") ||
		fsys.IsTemp(rel) || lw.ignored.Match(rel, isDir)
}

// addTree watches dir and every folder below it that is synced.
func (lw *localWatcher) addTree(dir string) error {
	return filepath.WalkDir(dir, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil // gone again already
			}
			return err
		}
		if !e.IsDir() {
			return nil
		}
		if rel, ok := lw.rel(p); ok && lw.skip(rel, true) {
			return filepath.SkipDir
		}
		if err := lw.w.Add(p); err != nil {
			if errors.Is(err, syscall.ENOSPC) {
				return errTooManyWatches
			}
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return fmt.Errorf("watching %s: %w", p, err)
		}
		return nil
	})
}

// event turns one inotify event into the changed path, watching new folders
// on the way. ok is false for changes that do not matter.
func (lw *localWatcher) event(ev fsnotify.Event) (rel string, ok bool, err error) {
	rel, ok = lw.rel(ev.Name)
	if !ok {
		return "", false, nil
	}
	isDir := false
	if ev.Has(fsnotify.Create) {
		if info, statErr := os.Lstat(ev.Name); statErr == nil && info.IsDir() {
			isDir = true
			if !lw.skip(rel, true) {
				// Files created in it before the watch existed are covered by
				// the run this event triggers, which scans everything.
				err = lw.addTree(ev.Name)
			}
		}
	}
	if lw.skip(rel, isDir) {
		return "", false, err
	}
	return rel, true, err
}

func (lw *localWatcher) Close() error { return lw.w.Close() }

// realtime runs a real-time shuttle: on local changes, once they settle, and
// every poll_interval to pick up changes on the share. Until quit.
func (r *runner) realtime(quit <-chan struct{}) {
	log := r.d.o.Log.With("shuttle", r.shuttle.Name)
	var events <-chan fsnotify.Event
	var watchErrs <-chan error
	lw, err := newLocalWatcher(r.shuttle.Local, &r.shuttle.Settings)
	if err != nil {
		r.setNote(fmt.Sprintf("not watching local changes (%v); checking every %v instead", err, r.shuttle.Settings.PollInterval))
		log.Warn("not watching local changes", "err", err)
	} else {
		defer lw.Close()
		events, watchErrs = lw.w.Events, lw.w.Errors
	}

	// Changes made while the daemon was not running are only seen by a run:
	// run at start, however recent the last run, then poll from now.
	r.markAttempt(r.d.o.Now())
	r.trigger()

	var first, last time.Time // of the changes waiting to settle
	// Changes seen while a run is in progress wait here: inotify reports a
	// run's writes while the run is still making them, before it has noted
	// them as its own, so they can only be judged once the run has ended.
	var duringRun []string
	noteChange := func(at time.Time) {
		if first.IsZero() {
			first = at
		}
		last = at
	}
	for {
		now := r.d.o.Now()
		if len(duringRun) > 0 && !r.isRunning() {
			for _, rel := range duringRun {
				if !r.ownWrite(rel) {
					noteChange(now)
					break
				}
			}
			duringRun = nil
		}
		wait := recheckEvery
		if len(duringRun) > 0 {
			wait = runCheckEvery // to judge them soon after the run ends
		}
		if due := r.lastAttempt().Add(r.shuttle.Settings.PollInterval); !due.After(now) {
			r.markAttempt(now)
			r.trigger()
			continue
		} else if d := due.Sub(now); d < wait {
			wait = d
		}
		if !last.IsZero() {
			settle := last.Add(settleQuiet)
			if limit := first.Add(settleMax); limit.Before(settle) {
				settle = limit
			}
			if !settle.After(now) {
				first, last = time.Time{}, time.Time{}
				r.trigger()
				continue
			}
			wait = min(wait, settle.Sub(now))
		}

		select {
		case <-quit:
			return
		case <-time.After(wait):
		case ev, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			rel, changed, err := lw.event(ev)
			if err != nil {
				log.Warn("watching a new folder", "err", err)
				if errors.Is(err, errTooManyWatches) {
					r.setNote(fmt.Sprintf("not watching every folder (%v)", err))
				}
			}
			switch {
			case !changed:
			case r.isRunning():
				duringRun = append(duringRun, rel)
			case !r.ownWrite(rel):
				noteChange(now)
			}
		case err, ok := <-watchErrs:
			if !ok {
				watchErrs = nil
				continue
			}
			log.Warn("watching local changes", "err", err)
		}
	}
}

// Own writes. While a run writes to the local folder, the paths it touches
// (and their parent folders, which a copy may create) are noted, and
// changes to them are not taken as the user's until ownGrace after the run.

func (r *runner) startOwnWrites() {
	r.mu.Lock()
	r.own = map[string]bool{}
	r.ownUntil = time.Time{} // while the run lasts
	r.mu.Unlock()
}

func (r *runner) endOwnWrites() {
	r.mu.Lock()
	r.ownUntil = r.d.o.Now().Add(ownGrace)
	r.mu.Unlock()
}

// noteOwnWrite is the engine's OnAction: it records actions that write to
// the local side.
func (r *runner) noteOwnWrite(a *engine.Action, _ error) {
	switch a.Op {
	case engine.OpCopyToLocal, engine.OpDeleteLocal, engine.OpMkdirLocal, engine.OpConflict:
	default:
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for p := a.Path; p != "." && p != "/"; p = path.Dir(p) {
		r.own[p] = true
	}
}

func (r *runner) isRunning() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.st.State == StateRunning
}

func (r *runner) ownWrite(rel string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.own == nil || (!r.ownUntil.IsZero() && r.d.o.Now().After(r.ownUntil)) {
		return false
	}
	return r.own[rel]
}

func (r *runner) setNote(note string) {
	r.mu.Lock()
	r.st.Note = note
	r.mu.Unlock()
	r.changed()
}

// hasRealtime is here so daemon.go reads plainly.
func hasRealtime(s *config.Settings) bool { return s.Has(config.TriggerRealtime) }
