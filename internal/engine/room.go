// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package engine

import (
	"context"
	"fmt"

	"github.com/fqazzazee/shuttle/internal/fsys"
)

// NoRoomError means a side has less room than a run would copy to it. The
// run stops before copying anything, rather than fail part way with a
// side full.
type NoRoomError struct {
	Side       string // "this computer", "the share nas"
	Need, Free int64
}

func (e *NoRoomError) Error() string {
	return fmt.Sprintf("not enough room on %s: the run would copy %s there, and %s is free",
		e.Side, Size(e.Need), Size(e.Free))
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

// copyBytesTo is what the planned copies write to each side.
func copyBytesTo(actions []Action) (local, remote int64) {
	for i := range actions {
		a := &actions[i]
		switch {
		case a.skip:
		case a.Op == OpCopyToRemote && a.Local != nil:
			remote += a.Local.Size
		case a.Op == OpCopyToLocal && a.Remote != nil:
			local += a.Remote.Size
		}
	}
	return local, remote
}

// checkRoom asks each side the run copies to whether what it copies fits.
// A side that cannot tell, or does not answer, is not held to it: the
// copies themselves then report a full side, file by file.
func checkRoom(ctx context.Context, o *Options, actions []Action) error {
	toLocal, toRemote := copyBytesTo(actions)
	remoteName := "the share"
	if o.RemoteName != "" {
		remoteName += " " + o.RemoteName
	}
	for _, side := range []struct {
		f    fsys.FS
		name string
		need int64
	}{{o.Local, "this computer", toLocal}, {o.Remote, remoteName, toRemote}} {
		if side.need == 0 {
			continue // nothing to ask: most runs copy nothing
		}
		sp, err := fsys.SpaceOf(ctx, side.f)
		if err == nil && sp.Total > 0 && side.need > sp.Free {
			return &NoRoomError{Side: side.name, Need: side.need, Free: sp.Free}
		}
	}
	return nil
}
