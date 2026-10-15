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
	"github.com/fqazzazee/shuttle/internal/fsys"
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
	switch {
	case st.Metered && st.Pending:
		parts = append(parts, "a run waits for a connection that is not metered")
	case st.Metered:
		parts = append(parts, "paused while the connection is metered")
	case st.Pending:
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

// Selection says what of its folder a shuttle syncs.
func Selection(s *config.Settings) string {
	switch {
	case len(s.Include) > 0:
		return "only " + strings.Join(s.Include, ", ")
	case len(s.Exclude) > 0:
		return "all but " + strings.Join(s.Exclude, ", ")
	}
	return "everything"
}

// Runs says what starts a shuttle's runs: "as files change, every 1h".
// With no triggers, "only when you ask".
func Runs(s *config.Settings) string {
	if len(s.Triggers) == 0 {
		return "only when you ask"
	}
	var parts []string
	for _, t := range s.Triggers {
		switch t {
		case config.TriggerRealtime:
			parts = append(parts, "as files change")
		case config.TriggerShareAvailable:
			parts = append(parts, "when the share comes online")
		case config.TriggerSchedule:
			if s.Interval > 0 {
				parts = append(parts, "every "+Interval(s.Interval))
			}
			if len(s.At) > 0 {
				at := make([]string, len(s.At))
				for i, t := range s.At {
					at[i] = t.String()
				}
				parts = append(parts, "at "+strings.Join(at, ", "))
			}
		}
	}
	return strings.Join(parts, ", ")
}

// Next is when the schedule runs a shuttle next ("next in 3h"), or "" while
// it runs or without a schedule.
func Next(st *daemon.Status, now time.Time) string {
	if st.State == daemon.StateRunning {
		return ""
	}
	if next := Until(st.NextRun, now); next != "" {
		return "next " + next
	}
	return ""
}

// LastRun is what the last run did: "it failed", "nothing to do", or
// "3 copied, 1 deleted".
func LastRun(st *daemon.Status) string {
	if st.Error != "" {
		return "it failed"
	}
	if st.Copied == 0 && st.Deleted == 0 {
		return "nothing to do"
	}
	var parts []string
	if st.Copied > 0 {
		parts = append(parts, fmt.Sprintf("%d copied", st.Copied))
	}
	if st.Deleted > 0 {
		parts = append(parts, fmt.Sprintf("%d deleted", st.Deleted))
	}
	return strings.Join(parts, ", ")
}

// Interval is an interval as the config writes it: "90m", "1h", "2d".
func Interval(d time.Duration) string {
	switch {
	case d%(24*time.Hour) == 0:
		return fmt.Sprintf("%dd", d/(24*time.Hour))
	case d%time.Hour == 0:
		return fmt.Sprintf("%dh", d/time.Hour)
	case d%time.Minute == 0:
		return fmt.Sprintf("%dm", d/time.Minute)
	}
	return d.String()
}

// Room is room in words: "1.2 TB free of 4.0 TB".
func Room(sp fsys.Space) string {
	return Size(sp.Free) + " free of " + Size(sp.Total)
}

// Low reports whether less than a tenth of the room is left.
func Low(sp fsys.Space) bool {
	return sp.Total > 0 && sp.Free < sp.Total/10
}

// Where says where a share is: smb://user@host/share, nfs://host/export, or
// the mounted folder.
func Where(sh *config.Share) string {
	host := sh.Host
	if sh.Port != 0 {
		host += ":" + strconv.Itoa(sh.Port)
	}
	switch sh.Type {
	case config.ShareSMB:
		user := sh.User
		if sh.Domain != "" {
			user = sh.Domain + ";" + user
		}
		if user != "" {
			user += "@"
		}
		return "smb://" + user + host + "/" + sh.Share
	case config.ShareNFS3:
		return "nfs://" + host + sh.Export
	}
	return sh.Path
}
