// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

// Package present words a shuttle's status for people: the command line and
// the TUI say things the same way.
package present

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/fqazzazee/shuttle/internal/config"
	"github.com/fqazzazee/shuttle/internal/daemon"
	"github.com/fqazzazee/shuttle/internal/engine"
)

// Bus is shuttle's icon in text, for the terminal: the front of a shuttle
// bus with the sync arrows on its destination sign, as the app icon has
// them. One character per column, so it lines up in any monospace font.
var Bus = []string{
	"  .----------.",
	"  |  [ ⇄  ]  |",
	" =| .------. |=",
	"  | |      | |",
	"  | '------' |",
	"  | o  ==  o |",
	"  '-[]----[]-'",
}

// ModeArrow is a mode as an arrow: ↑ push, ↓ pull, ⇅ sync. Up and down,
// as for uploads and downloads and the transfer rates beside them: left and
// right would leave which side is which to guess.
func ModeArrow(m config.Mode) string {
	switch m {
	case config.ModePush:
		return "↑"
	case config.ModePull:
		return "↓"
	case config.ModeSync:
		return "⇅"
	}
	return "?"
}

// Attention lists what of a shuttle's status needs the user's attention,
// most pressing first, e.g. "2 conflicts waiting for you; a run is waiting".
func Attention(st *daemon.Status) string {
	var parts []string
	if st.Error != "" {
		parts = append(parts, st.Error)
	}
	if held := HeldConflicts(st); held > 0 {
		parts = append(parts, Plural(held, "conflict")+" waiting for you")
	}
	if st.HeldDeletions > 0 {
		parts = append(parts, Plural(st.HeldDeletions, "deletion")+" held by the guard")
	}
	if st.Pending {
		parts = append(parts, "a run is waiting")
	}
	if st.Note != "" {
		parts = append(parts, st.Note)
	}
	return strings.Join(parts, "; ")
}

// HeldConflicts counts the conflicts waiting for the user's choice.
func HeldConflicts(st *daemon.Status) int {
	n := 0
	for _, c := range st.Conflicts {
		if c.Outcome == "held" {
			n++
		}
	}
	return n
}

// Progress describes a run in progress, e.g. "syncing 12 of 30, 45.0 MB of
// 120.0 MB, ↑ 2.1 MB/s: docs/a.pdf"; "" when not running.
func Progress(p *daemon.Progress) string {
	var s string
	switch p.Phase {
	case "":
		return ""
	case engine.PhaseScanning:
		s = fmt.Sprintf("scanning, %d found", p.Done)
	case engine.PhaseComparing:
		s = fmt.Sprintf("comparing %d of %d", p.Done, p.Total)
	default:
		s = fmt.Sprintf("%s %d of %d", p.Phase, p.Done, p.Total)
		if p.TotalBytes > 0 {
			s += fmt.Sprintf(", %s of %s", Size(p.Bytes), Size(p.TotalBytes))
		}
	}
	if p.RateUp > 0 {
		s += ", ↑ " + Size(p.RateUp) + "/s"
	}
	if p.RateDown > 0 {
		s += ", ↓ " + Size(p.RateDown) + "/s"
	}
	if p.Path != "" {
		s += ": " + p.Path
	}
	return s
}

// Size is a byte count in decimal units, as GNOME shows them.
func Size(n int64) string { return engine.Size(n) }

// Count is a number with its thousands grouped: "31,204".
func Count(n int) string {
	s := strconv.Itoa(n)
	start := 0
	if n < 0 {
		start = 1
	}
	for i := len(s) - 3; i > start; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// Synced is what a shuttle has synced: its size and its number of files,
// "48.2 GB" and "31,204"; "—" for both until it has run or synced anything.
func Synced(st *daemon.Status) (size, files string) {
	if st.Files == 0 && st.LastRun.IsZero() {
		return "—", "—"
	}
	return Size(st.Size), Count(st.Files)
}

// Plural is n things: "1 file", "3 files".
func Plural(n int, thing string) string {
	if n == 1 {
		return "1 " + thing
	}
	return fmt.Sprintf("%d %ss", n, thing)
}

// Until is a short "how long until": "in 5m", "in 3h", "in 2d"; "soon"
// within the minute, or once due; "" for no time.
func Until(t, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := t.Sub(now)
	switch {
	case d < time.Minute:
		return "soon"
	case d < time.Hour:
		return fmt.Sprintf("in %dm", int(d/time.Minute))
	case d < 24*time.Hour:
		return fmt.Sprintf("in %dh", int(d/time.Hour))
	default:
		return fmt.Sprintf("in %dd", int(d/(24*time.Hour)))
	}
}

// Ago is a short "how long ago": "just now", "5m ago", "3h ago", "2d ago".
func Ago(t, now time.Time) string {
	if t.IsZero() {
		return "never"
	}
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d/time.Minute))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d/time.Hour))
	default:
		return fmt.Sprintf("%dd ago", int(d/(24*time.Hour)))
	}
}
