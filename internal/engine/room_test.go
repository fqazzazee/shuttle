// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package engine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/fqazzazee/shuttle/internal/config"
	"github.com/fqazzazee/shuttle/internal/fsys"
)

// roomy is an FS that says it has the room given; asked counts the asking.
type roomy struct {
	fsys.FS
	space fsys.Space
	asked int
}

func (r *roomy) Space(context.Context) (fsys.Space, error) {
	r.asked++
	return r.space, nil
}

// A run that would not fit stops before copying anything, and says how
// much it needed where; once there is room, it goes ahead.
func TestNoRoomStopsTheRun(t *testing.T) {
	h := newHarness(t, config.ModeSync, config.DeletionsAdditive)
	h.opts.RemoteName = "nas"
	remote := &roomy{FS: h.opts.Remote, space: fsys.Space{Free: 1500, Total: 1 << 20}}
	local := &roomy{FS: h.opts.Local, space: fsys.Space{Free: 1 << 20, Total: 1 << 20}}
	h.opts.Remote, h.opts.Local = remote, local
	kB := strings.Repeat("x", 1000)
	write(t, h.localDir, "a.bin", kB, t0)
	write(t, h.localDir, "b.bin", kB, t0)
	write(t, h.remoteDir, "c.txt", "from the share", t0)

	_, err := Run(context.Background(), &h.opts)
	var noRoom *NoRoomError
	if !errors.As(err, &noRoom) || noRoom.Need != 2000 || noRoom.Free != 1500 ||
		err.Error() != "not enough room on the share nas: the run would copy 2.0 kB there, and 1.5 kB is free" {
		t.Fatalf("Run = %v", err)
	}
	if got := listing(t, h.remoteDir); got != "c.txt=from the share" {
		t.Errorf("remote = %s: something was copied", got)
	}
	if got := listing(t, h.localDir); got != "a.bin="+kB+" b.bin="+kB {
		t.Error("the other side was copied to")
	}

	// This side, by the name the daemon gives it.
	local.space.Free, remote.space.Free = 10, 1<<20
	write(t, h.remoteDir, "d.txt", "from the share too", t0)
	h.opts.Here = "this phone"
	if _, err := Run(context.Background(), &h.opts); err == nil || !strings.HasPrefix(err.Error(), "not enough room on this phone:") {
		t.Fatalf("Run = %v", err)
	}
	local.space.Free = 1 << 20
	h.run()
	if got := listing(t, h.localDir); !strings.Contains(got, "c.txt=from the share") || !strings.Contains(listing(t, h.remoteDir), "a.bin=") {
		t.Errorf("local = %q after making room", got)
	}

	// Nothing to copy: nobody is asked.
	before := remote.asked + local.asked
	h.run()
	if remote.asked+local.asked != before {
		t.Error("a run that copies nothing asked for room")
	}
}

// A side that cannot tell is not held to anything.
func TestUnknownRoomDoesNotStopTheRun(t *testing.T) {
	h := newHarness(t, config.ModePush, config.DeletionsAdditive)
	h.opts.Remote = &roomy{FS: h.opts.Remote} // zero: cannot tell
	write(t, h.localDir, "a.txt", "a", t0)
	if res := h.run(); res.Copied != 1 {
		t.Errorf("copied %d", res.Copied)
	}
}
