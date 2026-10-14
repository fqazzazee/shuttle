// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package daemon

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/fqazzazee/shuttle/internal/activity"
	"github.com/fqazzazee/shuttle/internal/engine"
)

// What the activity log says of an action carried out, and of one that
// failed. Adopting, forgetting and comparing only update the records.
var (
	doneText = map[engine.Op]string{
		engine.OpCopyToRemote: "copied to the share",
		engine.OpCopyToLocal:  "copied from the share",
		engine.OpDeleteRemote: "deleted from the share",
		engine.OpDeleteLocal:  "deleted here",
		engine.OpMkdirRemote:  "folder created on the share",
		engine.OpMkdirLocal:   "folder created here",
	}
	failedText = map[engine.Op]string{
		engine.OpCopyToRemote: "could not copy to the share",
		engine.OpCopyToLocal:  "could not copy from the share",
		engine.OpDeleteRemote: "could not delete from the share",
		engine.OpDeleteLocal:  "could not delete here",
		engine.OpMkdirRemote:  "could not create the folder on the share",
		engine.OpMkdirLocal:   "could not create the folder here",
		engine.OpCompare:      "could not compare the two copies",
		engine.OpConflict:     "could not settle the conflict",
	}
)

// What the activity log says of a conflict the user resolved.
var resolvedText = map[engine.Choice]string{
	engine.KeepLocal:  "kept this computer's copy",
	engine.KeepRemote: "kept the share's copy",
	engine.KeepBoth:   "kept both",
}

func (d *Daemon) activityPath(name string) string {
	return filepath.Join(d.o.Paths.StateDir, name+".activity")
}

// openActivity opens the shuttle's activity log. The shuttle runs without
// one if it cannot be opened: the log is a record, not a need.
func (r *runner) openActivity() {
	l, err := activity.Open(r.d.activityPath(r.shuttle.Name))
	if err == nil {
		err = l.Trim(r.d.o.Now())
	}
	if err != nil {
		r.d.o.Log.Warn("activity log", "shuttle", r.shuttle.Name, "err", err)
	}
	r.activity = l
}

func (r *runner) addActivity(kind activity.Kind, path, text string) {
	if r.activity == nil {
		return
	}
	if err := r.activity.Add(&activity.Entry{Time: r.d.o.Now(), Kind: kind, Path: path, Text: text}); err != nil {
		r.d.o.Log.Warn("activity log", "shuttle", r.shuttle.Name, "err", err)
	}
}

// onAction is the engine's OnAction.
func (r *runner) onAction(a *engine.Action, err error) {
	r.noteOwnWrite(a, err)
	switch {
	case errors.Is(err, engine.ErrChanged):
		r.addActivity(activity.KindSkipped, a.Path, "changed during the run; left for the next run")
	case err != nil:
		r.addActivity(activity.KindError, a.Path, cmp.Or(failedText[a.Op], "could not sync it")+": "+err.Error())
	case doneText[a.Op] != "":
		r.addActivity(activity.KindDone, a.Path, doneText[a.Op])
	}
	// A conflict is logged with what was done about it, once the run ends.
}

// logRun adds what a run ended with to the activity log: its conflicts,
// held deletions, and a summary. A run that changed nothing and met no
// problem adds nothing: a real-time shuttle checks every minute.
func (r *runner) logRun(res *engine.Result, err error) {
	if r.activity == nil {
		return
	}
	switch {
	case errors.Is(err, engine.ErrStopped):
		r.addActivity(activity.KindRun, "", "stopped by a pause or shutdown"+runSummary(res, " after ")+
			"; the rest is left for the next run")
	case err != nil:
		r.addActivity(activity.KindError, "", "the run failed: "+err.Error())
	default:
		for _, c := range res.Conflicts {
			text := c.Reason + ": " + c.Outcome
			if c.Outcome == "held" {
				text = c.Reason + "; waiting for you to choose"
			}
			r.addActivity(activity.KindConflict, c.Path, text)
		}
		r.logRefused(res.Refused)
		if n := len(res.HeldDeletions); n > 0 {
			r.addActivity(activity.KindHeld, "", fmt.Sprintf(
				"%d deletions held by the deletion guard: too many at once; allow them to let them go ahead", n))
		}
		if s := runSummary(res, ""); s != "" {
			r.addActivity(activity.KindRun, "", s)
		}
	}
	if err := r.activity.Trim(r.d.o.Now()); err != nil {
		r.d.o.Log.Warn("activity log", "shuttle", r.shuttle.Name, "err", err)
	}
}

// logRefused logs the names a run left out that the run before did not:
// a real-time shuttle runs every minute, and they stay left out.
func (r *runner) logRefused(refused []engine.Issue) {
	seen := make(map[string]bool, len(refused))
	for _, i := range refused {
		seen[i.Path] = true
		if !r.refused[i.Path] {
			r.addActivity(activity.KindLeftOut, i.Path, i.Reason+"; left out, and not deleted on either side")
		}
	}
	r.refused = seen
}

// runSummary sums up what a run did, after prefix; "" if it did nothing.
func runSummary(res *engine.Result, prefix string) string {
	if res == nil {
		return ""
	}
	var parts []string
	add := func(n int, one, many string) {
		switch {
		case n == 1:
			parts = append(parts, "1 "+one)
		case n > 1:
			parts = append(parts, fmt.Sprintf("%d %s", n, many))
		}
	}
	add(res.Copied, "copied", "copied")
	add(res.Created, "folder created", "folders created")
	add(res.Deleted, "deleted", "deleted")
	add(len(res.Conflicts), "conflict", "conflicts")
	add(len(res.Skipped), "left for the next run", "left for the next run")
	add(len(res.Errors), "error", "errors")
	if len(parts) == 0 {
		return ""
	}
	return prefix + strings.Join(parts, ", ")
}

// Activity returns the newest entries of the named shuttle's activity log,
// or of every shuttle's together when name is "", newest first: at most
// limit of them (0: all).
func (d *Daemon) Activity(name string, limit int) ([]activity.Entry, error) {
	d.runnersMu.RLock()
	names := slices.Sorted(maps.Keys(d.runners))
	d.runnersMu.RUnlock()
	if name != "" {
		if !slices.Contains(names, name) {
			return nil, fmt.Errorf("%q: %w", name, ErrUnknownShuttle)
		}
		names = []string{name}
	}
	var logs [][]activity.Entry
	for _, n := range names {
		entries, err := activity.Read(d.activityPath(n), n, limit)
		if err != nil {
			return nil, err
		}
		logs = append(logs, entries)
	}
	return activity.Merge(limit, logs...), nil
}
