// SPDX-License-Identifier: GPL-2.0-or-later

package engine

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/fqazzazee/shuttle/internal/config"
	"github.com/fqazzazee/shuttle/internal/fsys"
)

// spied is a file system that calls open whenever a file is opened.
type spied struct {
	fsys.FS
	open func(name string)
}

func (s *spied) Open(ctx context.Context, name string) (io.ReadCloser, error) {
	s.open(name)
	return s.FS.Open(ctx, name)
}

func TestProgressOfARun(t *testing.T) {
	h := newHarness(t, config.ModeSync, config.DeletionsMirror)
	write(t, h.localDir, "a.txt", strings.Repeat("a", 1000), t0)
	write(t, h.localDir, "dir/b.txt", strings.Repeat("b", 300), t0)
	write(t, h.remoteDir, "c.txt", strings.Repeat("c", 50), t0)
	tr := &Tracker{}
	h.opts.Tracker = tr
	var during []Progress
	h.opts.Local = &spied{FS: h.opts.Local, open: func(string) { during = append(during, tr.Progress()) }}
	h.opts.Remote = &spied{FS: h.opts.Remote, open: func(string) { during = append(during, tr.Progress()) }}

	res := h.run()
	got := tr.Progress()
	// Copied: a.txt and dir/b.txt up, c.txt down; created: dir.
	want := Progress{Phase: PhaseSyncing, Done: 4, Total: 4, Bytes: 1350, TotalBytes: 1350, Up: 1300, Down: 50}
	if got != want {
		t.Errorf("progress after the run = %+v\nwant %+v", got, want)
	}
	if res.Copied != 3 || res.Created != 1 {
		t.Fatalf("copied %d, created %d", res.Copied, res.Created)
	}
	if len(during) != 3 {
		t.Fatalf("%d opens, want 3", len(during))
	}
	for _, p := range during {
		if p.Phase != PhaseSyncing || p.Path == "" || p.Done >= p.Total {
			t.Errorf("progress while copying = %+v, want syncing, the file and not done", p)
		}
	}

	// Nothing to do: the phases still run, with nothing to count.
	h.opts.Local, h.opts.Remote = h.opts.Local.(*spied).FS, h.opts.Remote.(*spied).FS
	h.run()
	if got := tr.Progress(); got.Phase != PhaseSyncing || got.Total != 0 || got.Up != 1300 {
		t.Errorf("progress of a run with nothing to do = %+v", got)
	}
}

// Compares read the share, which counts as download, and are counted
// as they go.
func TestProgressOfCompares(t *testing.T) {
	h := newHarness(t, config.ModeSync, config.DeletionsAdditive)
	write(t, h.localDir, "a.txt", "base", t0)
	h.run()
	write(t, h.localDir, "a.txt", "same edit", t1)
	write(t, h.remoteDir, "a.txt", "same edit", t1.Add(10*ModTimeWindow))
	tr := &Tracker{}
	h.opts.Tracker = tr
	var during Progress
	h.opts.Remote = &spied{FS: h.opts.Remote, open: func(string) { during = tr.Progress() }}
	res := h.run()
	if res.Adopted != 1 {
		t.Fatalf("adopted %d, want a.txt found the same", res.Adopted)
	}
	if during.Phase != PhaseComparing || during.Path != "a.txt" || during.Total != 1 {
		t.Errorf("progress while comparing = %+v", during)
	}
	if got := tr.Progress(); got.Down != int64(len("same edit")) || got.Up != 0 || got.Bytes != 0 {
		t.Errorf("progress after = %+v, want the share's copy read as download, nothing copied", got)
	}
}

func TestProgressOfAScan(t *testing.T) {
	h := newHarness(t, config.ModePush, config.DeletionsAdditive)
	write(t, h.localDir, "a.txt", "a", t0)
	write(t, h.localDir, "d/b.txt", "b", t0)
	write(t, h.remoteDir, "c.txt", "c", t0)
	h.settings.Ignore = []string{"*.tmp"}
	write(t, h.localDir, "x.tmp", "ignored", t0)
	tr := &Tracker{}
	h.opts.Tracker = tr
	if _, err := Preview(context.Background(), &h.opts); err != nil {
		t.Fatal(err)
	}
	// a.txt, d, d/b.txt and c.txt; not the ignored file.
	if got := tr.Progress(); got.Phase != PhaseScanning || got.Done != 4 {
		t.Errorf("progress after a scan = %+v, want 4 entries listed", got)
	}
}

func TestNilTracker(t *testing.T) {
	var tr *Tracker
	tr.update(func(*Progress) { t.Error("a nil tracker must not update") })
	if tr.Progress() != (Progress{}) {
		t.Error("a nil tracker has progress")
	}
}

// A conflict's copies are transferred, but are not planned copies: the
// bytes copied stay within the planned total.
func TestProgressOfConflictCopies(t *testing.T) {
	h := newHarness(t, config.ModeSync, config.DeletionsAdditive)
	h.settings.Conflicts = config.ConflictKeepBoth
	write(t, h.localDir, "a.txt", strings.Repeat("a", 100), t0)
	write(t, h.localDir, "clash.txt", strings.Repeat("l", 1000), t0)
	write(t, h.remoteDir, "clash.txt", strings.Repeat("r", 2000), t1)
	tr := &Tracker{}
	h.opts.Tracker = tr
	if res := h.run(); len(res.Conflicts) != 1 {
		t.Fatalf("conflicts = %+v", res.Conflicts)
	}
	got := tr.Progress()
	if got.Bytes != 100 || got.TotalBytes != 100 || got.Done != got.Total {
		t.Errorf("progress = %+v, want 100 of 100 bytes: a.txt only", got)
	}
	if got.Up+got.Down <= 2000 {
		t.Errorf("progress = %+v, want the conflict's copies counted as transfers", got)
	}
}
