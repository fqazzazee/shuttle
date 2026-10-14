// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package daemon

import (
	"time"

	"github.com/fqazzazee/shuttle/internal/config"
	"github.com/fqazzazee/shuttle/internal/engine"
	"github.com/fqazzazee/shuttle/internal/fsys"
)

// State is what a shuttle is doing, as the TUI and the GNOME extension show it.
type State string

const (
	StateIdle     State = "idle"     // waiting for a trigger; the last run went fine
	StateRunning  State = "running"  // a run is in progress
	StatePaused   State = "paused"   // paused by the user; triggers wait for resume
	StateHeld     State = "held"     // conflicts or deletions wait for the user
	StateError    State = "error"    // the last run failed; see Error
	StateOffline  State = "offline"  // the share could not be reached
	StateDisabled State = "disabled" // enabled = false in the config
)

// Status is one shuttle's status.
type Status struct {
	Name    string
	Mode    config.Mode // push, pull or sync
	State   State
	Paused  bool   // paused by the user, itself or with all shuttles
	Metered bool   // waiting for a connection that is not metered, as its settings ask
	Waiting string // why it cannot run now, e.g. "needs All files access" (Options.Waiting)
	Pending bool   // a run is waiting: triggered while paused or while running
	Share   string // ShareOnline or ShareOffline; "" before the first check
	Note    string // something degraded but working, e.g. polling instead of watching

	LastRun time.Time // when the last run ended, however it ended
	LastOK  time.Time // when a run last completed without errors
	NextRun time.Time // when the schedule runs it next; zero without a schedule
	Error   string    // why the last run failed, for StateError and StateOffline

	// What the last run did.
	Copied, Deleted int
	Conflicts       []engine.Issue // as settled or held by the conflict policy
	HeldDeletions   int            // deletions the guard is holding back
	Errors          []engine.Issue // paths the last run could not handle
	Refused         []engine.Issue // names one side cannot hold: left out, never synced

	// What is synced: the files in the state database and their size, as
	// of the last run or the daemon's start.
	Files int
	Size  int64
	// How much room each side had at the end of the last run that could
	// tell; zero until one could.
	LocalSpace, RemoteSpace fsys.Space

	Progress Progress // of the run in progress
}

// held reports whether something waits for the user's decision.
func held(res *engine.Result) bool {
	if res.Held || len(res.HeldDeletions) > 0 {
		return true
	}
	for _, c := range res.Conflicts {
		if c.Outcome == "held" {
			return true
		}
	}
	return false
}
