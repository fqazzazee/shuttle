// SPDX-License-Identifier: GPL-2.0-or-later

package daemon

import (
	"time"

	"github.com/fqazzazee/shuttle/internal/engine"
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
	State   State
	Paused  bool   // paused by the user, itself or with all shuttles
	Pending bool   // a run is waiting: triggered while paused or while running
	Share   string // ShareOnline or ShareOffline; "" before the first check

	LastRun time.Time // when the last run ended, however it ended
	LastOK  time.Time // when a run last completed without errors
	Error   string    // why the last run failed, for StateError and StateOffline

	// What the last run did.
	Copied, Deleted int
	Conflicts       []engine.Issue // as settled or held by the conflict policy
	HeldDeletions   int            // deletions the guard is holding back
	Errors          []engine.Issue // paths the last run could not handle
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
