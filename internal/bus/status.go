// SPDX-License-Identifier: GPL-2.0-or-later

package bus

import (
	"math"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/fqazzazee/shuttle/internal/config"
	"github.com/fqazzazee/shuttle/internal/daemon"
	"github.com/fqazzazee/shuttle/internal/engine"
	"github.com/fqazzazee/shuttle/internal/fsys"
)

// issue is an engine.Issue on the bus: (path, reason, outcome).
type issue struct {
	Path, Reason, Outcome string
}

// conflict is a conflict on the bus: (path, reason, outcome, local copy,
// remote copy).
type conflict struct {
	Path, Reason, Outcome string
	Local, Remote         side
}

// side is one copy of a conflict: (kind, size, modified in unix seconds).
// Kind is "file" or "dir", or "" when that side has no copy.
type side struct {
	Kind    string
	Size    uint64
	ModTime int64
}

func toSide(f *fsys.FileInfo) side {
	if f == nil {
		return side{}
	}
	return side{f.Kind.String(), u64(f.Size), f.ModTime.Unix()}
}

func fromSide(path string, s side) *fsys.FileInfo {
	kind, ok := map[string]fsys.Kind{"file": fsys.KindFile, "dir": fsys.KindDir}[s.Kind]
	if !ok {
		return nil
	}
	return &fsys.FileInfo{Name: path, Kind: kind, Size: s64(s.Size), ModTime: time.Unix(s.ModTime, 0)}
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
	conflicts := func(in []engine.Issue) []conflict {
		out := make([]conflict, 0, len(in))
		for _, c := range in {
			out = append(out, conflict{c.Path, c.Reason, c.Outcome, toSide(c.Local), toSide(c.Remote)})
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
		"Mode":          dbus.MakeVariant(string(st.Mode)),
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
		"Conflicts":     dbus.MakeVariant(conflicts(st.Conflicts)),
		"Errors":        dbus.MakeVariant(issues(st.Errors)),
		"Files":         dbus.MakeVariant(u32(st.Files)),
		"Size":          dbus.MakeVariant(u64(st.Size)),
		"LocalFree":     dbus.MakeVariant(u64(st.LocalSpace.Free)),
		"LocalTotal":    dbus.MakeVariant(u64(st.LocalSpace.Total)),
		"RemoteFree":    dbus.MakeVariant(u64(st.RemoteSpace.Free)),
		"RemoteTotal":   dbus.MakeVariant(u64(st.RemoteSpace.Total)),
		// The run in progress; zero when not running.
		"Phase":      dbus.MakeVariant(string(st.Progress.Phase)),
		"Done":       dbus.MakeVariant(u32(st.Progress.Done)),
		"Total":      dbus.MakeVariant(u32(st.Progress.Total)),
		"Bytes":      dbus.MakeVariant(u64(st.Progress.Bytes)),
		"TotalBytes": dbus.MakeVariant(u64(st.Progress.TotalBytes)),
		"Path":       dbus.MakeVariant(st.Progress.Path),
		"RateUp":     dbus.MakeVariant(u64(st.Progress.RateUp)),
		"RateDown":   dbus.MakeVariant(u64(st.Progress.RateDown)),
	}
}

// u32 clamps a count into the bus's unsigned 32-bit integers.
func u32(n int) uint32 {
	return uint32(min(max(n, 0), math.MaxUint32)) //nolint:gosec // clamped on this line
}

// u64 clamps a byte count or rate into the bus's unsigned 64-bit integers.
func u64(n int64) uint64 {
	return uint64(max(n, 0)) //nolint:gosec // clamped on this line
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
	var state, mode string
	var lastRun, lastOK int64
	var copied, deleted, held, files uint32
	var size, localFree, localTotal, remoteFree, remoteTotal uint64
	var conflicts []conflict
	var errs []issue
	get("State", &state)
	get("Mode", &mode)
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
	get("Files", &files)
	get("Size", &size)
	get("LocalFree", &localFree)
	get("LocalTotal", &localTotal)
	get("RemoteFree", &remoteFree)
	get("RemoteTotal", &remoteTotal)
	var phase string
	var done, total uint32
	var bytes, totalBytes, rateUp, rateDown uint64
	get("Phase", &phase)
	get("Done", &done)
	get("Total", &total)
	get("Bytes", &bytes)
	get("TotalBytes", &totalBytes)
	get("Path", &st.Progress.Path)
	get("RateUp", &rateUp)
	get("RateDown", &rateDown)
	st.Progress.Phase = engine.Phase(phase)
	st.Progress.Done, st.Progress.Total = int(done), int(total)
	st.Progress.Bytes, st.Progress.TotalBytes = s64(bytes), s64(totalBytes)
	st.Progress.RateUp, st.Progress.RateDown = s64(rateUp), s64(rateDown)
	st.State, st.Mode = daemon.State(state), config.Mode(mode)
	if lastRun != 0 {
		st.LastRun = time.Unix(lastRun, 0)
	}
	if lastOK != 0 {
		st.LastOK = time.Unix(lastOK, 0)
	}
	st.Copied, st.Deleted, st.HeldDeletions = int(copied), int(deleted), int(held)
	st.Files, st.Size = int(files), s64(size)
	st.LocalSpace = fsys.Space{Free: s64(localFree), Total: s64(localTotal)}
	st.RemoteSpace = fsys.Space{Free: s64(remoteFree), Total: s64(remoteTotal)}
	for _, i := range conflicts {
		st.Conflicts = append(st.Conflicts, engine.Issue{Path: i.Path, Reason: i.Reason, Outcome: i.Outcome, Op: engine.OpConflict,
			Local: fromSide(i.Path, i.Local), Remote: fromSide(i.Path, i.Remote)})
	}
	for _, i := range errs {
		st.Errors = append(st.Errors, engine.Issue{Path: i.Path, Reason: i.Reason, Outcome: i.Outcome})
	}
	return st
}

func s64(n uint64) int64 {
	return int64(min(n, math.MaxInt64)) //nolint:gosec // clamped on this line
}
