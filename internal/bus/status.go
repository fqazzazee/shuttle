// SPDX-License-Identifier: GPL-2.0-or-later

package bus

import (
	"math"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/fqazzazee/shuttle/internal/daemon"
	"github.com/fqazzazee/shuttle/internal/engine"
)

// issue is an engine.Issue on the bus: (path, reason, outcome).
type issue struct {
	Path, Reason, Outcome string
}

// encode turns a status into the a{sv} the bus carries. Keys are stable:
// the GNOME extension reads them by name.
func encode(st *daemon.Status) map[string]dbus.Variant {
	issues := func(in []engine.Issue) []issue {
		out := make([]issue, 0, len(in))
		for _, i := range in {
			out = append(out, issue{i.Path, i.Reason, i.Outcome})
		}
		return out
	}
	unix := func(t time.Time) int64 {
		if t.IsZero() {
			return 0
		}
		return t.Unix()
	}
	return map[string]dbus.Variant{
		"State":         dbus.MakeVariant(string(st.State)),
		"Paused":        dbus.MakeVariant(st.Paused),
		"Pending":       dbus.MakeVariant(st.Pending),
		"Share":         dbus.MakeVariant(st.Share),
		"Note":          dbus.MakeVariant(st.Note),
		"Error":         dbus.MakeVariant(st.Error),
		"LastRun":       dbus.MakeVariant(unix(st.LastRun)),
		"LastOK":        dbus.MakeVariant(unix(st.LastOK)),
		"Copied":        dbus.MakeVariant(u32(st.Copied)),
		"Deleted":       dbus.MakeVariant(u32(st.Deleted)),
		"HeldDeletions": dbus.MakeVariant(u32(st.HeldDeletions)),
		"Conflicts":     dbus.MakeVariant(issues(st.Conflicts)),
		"Errors":        dbus.MakeVariant(issues(st.Errors)),
	}
}

// u32 clamps a count into the bus's unsigned 32-bit integers.
func u32(n int) uint32 {
	return uint32(min(max(n, 0), math.MaxUint32)) //nolint:gosec // clamped on this line
}

// decode is encode's inverse. Missing or mistyped keys stay zero, so a
// newer daemon with extra keys still decodes.
func decode(name string, m map[string]dbus.Variant) daemon.Status {
	st := daemon.Status{Name: name}
	get := func(key string, into any) {
		if v, ok := m[key]; ok {
			_ = v.Store(into)
		}
	}
	var state string
	var lastRun, lastOK int64
	var copied, deleted, held uint32
	var conflicts, errs []issue
	get("State", &state)
	get("Paused", &st.Paused)
	get("Pending", &st.Pending)
	get("Share", &st.Share)
	get("Note", &st.Note)
	get("Error", &st.Error)
	get("LastRun", &lastRun)
	get("LastOK", &lastOK)
	get("Copied", &copied)
	get("Deleted", &deleted)
	get("HeldDeletions", &held)
	get("Conflicts", &conflicts)
	get("Errors", &errs)
	st.State = daemon.State(state)
	if lastRun != 0 {
		st.LastRun = time.Unix(lastRun, 0)
	}
	if lastOK != 0 {
		st.LastOK = time.Unix(lastOK, 0)
	}
	st.Copied, st.Deleted, st.HeldDeletions = int(copied), int(deleted), int(held)
	for _, i := range conflicts {
		st.Conflicts = append(st.Conflicts, engine.Issue{Path: i.Path, Reason: i.Reason, Outcome: i.Outcome, Op: engine.OpConflict})
	}
	for _, i := range errs {
		st.Errors = append(st.Errors, engine.Issue{Path: i.Path, Reason: i.Reason, Outcome: i.Outcome})
	}
	return st
}
