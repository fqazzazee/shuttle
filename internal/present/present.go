// SPDX-License-Identifier: GPL-2.0-or-later

// Package present words a shuttle's status for people: the command line and
// the TUI say things the same way.
package present

import (
	"fmt"
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

// ModeArrow is a mode as an arrow: → push, ← pull, ↔ sync.
func ModeArrow(m config.Mode) string {
	switch m {
	case config.ModePush:
		return "→"
	case config.ModePull:
		return "←"
	case config.ModeSync:
		return "↔"
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
func Size(n int64) string {
	if n < 1000 {
		return fmt.Sprintf("%d B", n)
	}
	f, unit := float64(n)/1000, "kB"
	for _, u := range []string{"MB", "GB", "TB"} {
		if f < 1000 {
			break
		}
		f, unit = f/1000, u
	}
	return fmt.Sprintf("%.1f %s", f, unit)
}

// Plural is n things: "1 file", "3 files".
func Plural(n int, thing string) string {
	if n == 1 {
		return "1 " + thing
	}
	return fmt.Sprintf("%d %ss", n, thing)
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
