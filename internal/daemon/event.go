// SPDX-License-Identifier: GPL-2.0-or-later

package daemon

import "github.com/fqazzazee/shuttle/internal/engine"

// EventKind is what happened, for a desktop notification.
type EventKind string

const (
	EventError          EventKind = "error"           // a run failed (notify.errors)
	EventConflicts      EventKind = "conflicts"       // a run met conflicts (notify.conflicts)
	EventDeletionsHeld  EventKind = "deletions-held"  // the deletion guard held deletions back (notify.conflicts)
	EventCompleted      EventKind = "completed"       // a run finished (notify.completed)
	EventShareLost      EventKind = "share-lost"      // a share went away (notify.share)
	EventShareConnected EventKind = "share-connected" // a share came back (notify.share)
)

// Event is something worth a desktop notification. The daemon sends each
// one only when the notify switch for it is on, and a problem that persists
// from run to run only once.
type Event struct {
	Kind    EventKind
	Shuttle string // "" for share events

	Share    string   // share events: the share's name
	Shuttles []string // share events: the enabled shuttles that use it

	Error         string         // EventError: why the run failed
	Errors        []engine.Issue // EventError: the paths that could not be synced
	Conflicts     []engine.Issue // EventConflicts: the conflicts not announced before
	HeldDeletions int            // EventDeletionsHeld
	Copied        int            // EventCompleted
	Deleted       int            // EventCompleted
}

// announced is what a runner has already told the user about. Only runOnce
// touches it, under runMu.
type announced struct {
	err       string          // the error of the last failed run
	held      map[string]bool // conflicts held after the last run
	deletions bool            // the guard was holding deletions after the last run
}

// runEvents works out what a finished run is worth telling the user, given
// its status. ran says whether the engine ran, so st carries its results.
// A run stopped by pause or shutdown is not finished, and is not passed here.
func (r *runner) runEvents(st *Status, ran, requested bool) []Event {
	n := r.shuttle.Settings.Notify
	a := &r.announced
	var out []Event
	switch st.State {
	case StateError:
		if st.Error != a.err && n.Errors {
			e := Event{Kind: EventError, Shuttle: st.Name, Error: st.Error}
			if ran { // otherwise st.Errors are an earlier run's
				e.Errors = st.Errors
			}
			out = append(out, e)
		}
		a.err = st.Error
	case StateIdle, StateHeld:
		a.err = ""
	}
	// Offline is not an error here: the share's own events tell of it.

	if ran {
		// Held conflicts come back every run until resolved; settled ones
		// are news each time.
		held := map[string]bool{}
		var fresh []engine.Issue
		for _, c := range st.Conflicts {
			if c.Outcome == "held" {
				held[c.Path] = true
				if a.held[c.Path] {
					continue
				}
			}
			fresh = append(fresh, c)
		}
		a.held = held
		if len(fresh) > 0 && n.Conflicts {
			out = append(out, Event{Kind: EventConflicts, Shuttle: st.Name, Conflicts: fresh})
		}
		if st.HeldDeletions > 0 && !a.deletions && n.Conflicts {
			out = append(out, Event{Kind: EventDeletionsHeld, Shuttle: st.Name, HeldDeletions: st.HeldDeletions})
		}
		a.deletions = st.HeldDeletions > 0
	}

	// A run nobody asked for that changed nothing is not news: a schedule
	// every few minutes would otherwise notify every few minutes.
	if (st.State == StateIdle || st.State == StateHeld) && n.Completed && (requested || st.Copied+st.Deleted > 0) {
		out = append(out, Event{Kind: EventCompleted, Shuttle: st.Name, Copied: st.Copied, Deleted: st.Deleted})
	}
	return out
}

// shareChanged is told after any shuttle's probe changes its view of its
// share. The share as a whole is online while any of its shuttles reaches
// it: their probes run apart, and a "path" share is probed per folder, so
// one folder missing is that shuttle's problem, not the share's. A change
// is announced once, whichever shuttle saw it; the first view after start
// is not news.
func (d *Daemon) shareChanged(share string) {
	d.shareMu.Lock()
	defer d.shareMu.Unlock()
	now, users, notify := "", []string(nil), false
	unknown := false
	for _, name := range d.names() {
		r := d.runners[name]
		if r.shuttle.Share != share || !r.shuttle.Enabled {
			continue
		}
		users = append(users, name)
		notify = notify || r.shuttle.Settings.Notify.Share
		switch r.share() {
		case ShareOnline:
			now = ShareOnline
		case "":
			unknown = true
		}
	}
	if now == "" && !unknown {
		now = ShareOffline
	}
	if now == "" {
		return // offline so far, but not every shuttle has looked yet
	}
	was := d.shares[share]
	d.shares[share] = now
	if was == "" || was == now || !notify {
		return
	}
	kind := EventShareConnected
	if now == ShareOffline {
		kind = EventShareLost
	}
	d.event(&Event{Kind: kind, Share: share, Shuttles: users})
}

func (d *Daemon) event(e *Event) {
	if d.o.OnEvent != nil {
		d.o.OnEvent(e)
	}
}
