// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

// Package notice words the daemon's events for the user: what the desktop's
// notifications and the Android app's say, the same on both.
//
// It is part of the portable core: no desktop dependencies.
package notice

import (
	"fmt"
	"slices"
	"strings"

	"github.com/fqazzazee/shuttle/internal/daemon"
	"github.com/fqazzazee/shuttle/internal/engine"
)

// The kinds of notice, as the notify switches name them. On Android each is
// a notification channel of its own.
const (
	Errors    = "errors"
	Conflicts = "conflicts"
	Completed = "completed"
	Share     = "share"
)

// Note is what one notification says.
type Note struct {
	// Key: a note replaces the last one with the same key, so a shuttle's
	// latest error or a share's latest state is shown once, not stacked.
	// Conflicts have none: each batch is news of its own.
	Key           string
	Kind          string // Errors, Conflicts, Completed or Share
	Summary, Body string // plain text
	Open          string // the tab its "Open shuttle" button opens; "" for none
}

// Of words an event.
func Of(e *daemon.Event) Note {
	switch e.Kind {
	case daemon.EventError:
		lines := []string{e.Error}
		lines = append(lines, issueLines(e.Errors, func(i engine.Issue) string { return i.Reason })...)
		return Note{Key: "error/" + e.Shuttle, Kind: Errors,
			Summary: e.Shuttle + ": sync failed", Body: strings.Join(lines, "\n")}
	case daemon.EventConflicts:
		open := ""
		if slices.ContainsFunc(e.Conflicts, func(i engine.Issue) bool { return i.Outcome == "held" }) {
			open = "conflicts" // it waits for the user
		}
		return Note{Kind: Conflicts, Open: open,
			Summary: fmt.Sprintf("%s: %s", e.Shuttle, plural(len(e.Conflicts), "conflict")),
			Body: strings.Join(issueLines(e.Conflicts, func(i engine.Issue) string {
				if i.Outcome == "held" {
					return "waiting for you"
				}
				return i.Outcome
			}), "\n")}
	case daemon.EventDeletionsHeld:
		return Note{Key: "deletions/" + e.Shuttle, Kind: Conflicts, Open: "conflicts",
			Summary: fmt.Sprintf("%s: %s held back", e.Shuttle, plural(e.HeldDeletions, "deletion")),
			Body: "More files would be deleted at once than the deletion guard allows, so none were. " +
				"Check that both folders are there and mounted before allowing them."}
	case daemon.EventCompleted:
		return Note{Key: "completed/" + e.Shuttle, Kind: Completed,
			Summary: e.Shuttle + ": synced", Body: done(e.Copied, e.Deleted)}
	case daemon.EventShareLost:
		return Note{Key: "share/" + e.Share, Kind: Share,
			Summary: "Share " + e.Share + " lost", Body: "Waiting for it: " + names(e.Shuttles) + "."}
	case daemon.EventConfigError:
		return Note{Key: "config", Kind: Errors,
			Summary: "shuttle: config not loaded", Body: e.Error + "\nThe previous settings stay in use until it is fixed."}
	case daemon.EventShareConnected:
		return Note{Key: "share/" + e.Share, Kind: Share,
			Summary: "Share " + e.Share + " connected", Body: "Can sync again: " + names(e.Shuttles) + "."}
	}
	return Note{Kind: Errors, Summary: string(e.Kind), Body: e.Shuttle}
}

// maxLines is how many paths a note lists before "and N more".
const maxLines = 3

func issueLines(issues []engine.Issue, what func(engine.Issue) string) []string {
	var lines []string
	for i, is := range issues {
		if i == maxLines {
			lines = append(lines, fmt.Sprintf("and %d more", len(issues)-maxLines))
			break
		}
		lines = append(lines, clip(is.Path)+": "+what(is))
	}
	return lines
}

// clip shortens a path from the share to something a notification can show.
func clip(s string) string {
	const most = 80
	if r := []rune(s); len(r) > most {
		return "…" + string(r[len(r)-most+1:])
	}
	return s
}

func done(copied, deleted int) string {
	switch {
	case copied > 0 && deleted > 0:
		return fmt.Sprintf("Copied %s and deleted %d.", plural(copied, "file"), deleted)
	case copied > 0:
		return "Copied " + plural(copied, "file") + "."
	case deleted > 0:
		return "Deleted " + plural(deleted, "file") + "."
	}
	return "Nothing to do: already in step."
}

func names(n []string) string {
	switch {
	case len(n) == 0:
		return "no shuttles"
	case len(n) > 4:
		return strings.Join(n[:3], ", ") + fmt.Sprintf(" and %d more", len(n)-3)
	case len(n) == 1:
		return n[0]
	}
	return strings.Join(n[:len(n)-1], ", ") + " and " + n[len(n)-1]
}

func plural(n int, thing string) string {
	if n == 1 {
		return "1 " + thing
	}
	return fmt.Sprintf("%d %ss", n, thing)
}
