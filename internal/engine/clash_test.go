// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fqazzazee/shuttle/internal/config"
	"github.com/fqazzazee/shuttle/internal/fsys"
)

// folderToFile replaces the folder notes under dir with a file of that name.
func folderToFile(t *testing.T, dir string) {
	t.Helper()
	if err := os.RemoveAll(filepath.Join(dir, "notes")); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "notes", "a file", t1)
}

func TestKeepBothKeepsTheFolderOverTwoRuns(t *testing.T) {
	for _, tc := range []struct {
		name   string
		remote bool // the side where the folder became a file
		want   string
	}{
		{"file on the share", true,
			"notes/ notes/inside.txt=in a folder notes.sync-conflict-20261011-153000-remote=a file other.txt=o"},
		{"file here", false,
			"notes/ notes/inside.txt=in a folder notes.sync-conflict-20261011-153000-local=a file other.txt=o"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, config.ModeSync, config.DeletionsMirror)
			h.opts.Now = func() time.Time { return resolvedAt }
			write(t, h.localDir, "notes/inside.txt", "in a folder", t0)
			write(t, h.localDir, "other.txt", "o", t0)
			h.run()
			if tc.remote {
				folderToFile(t, h.remoteDir)
			} else {
				folderToFile(t, h.localDir)
			}

			if res := h.run(); len(res.Conflicts) != 1 {
				t.Fatalf("conflicts = %+v", res.Conflicts)
			}
			if res := h.run(); res.Deleted != 0 {
				t.Errorf("second run deleted %d", res.Deleted)
			}
			for side, dir := range map[string]string{"local": h.localDir, "remote": h.remoteDir} {
				if got := listing(t, dir); got != tc.want {
					t.Errorf("%s = %s\nwant    %s", side, got, tc.want)
				}
			}
		})
	}
}

// renameWatch calls before ahead of every rename.
type renameWatch struct {
	fsys.FS
	before func()
}

func (r renameWatch) Rename(ctx context.Context, from, to string) error {
	r.before()
	return r.FS.Rename(ctx, from, to)
}

// A crash right after the rename must not leave records that read the
// renamed file's path as deleted, so they are gone before it happens.
func TestKeepBothDropsTheRecordsBeforeRenaming(t *testing.T) {
	h := newHarness(t, config.ModeSync, config.DeletionsMirror)
	write(t, h.localDir, "notes/inside.txt", "in a folder", t0)
	h.run()
	folderToFile(t, h.remoteDir)

	renamed := false
	h.opts.Remote = renameWatch{FS: h.opts.Remote, before: func() {
		renamed = true
		records, err := h.opts.State.All()
		if err != nil {
			t.Error(err)
		}
		for p := range records {
			if p == "notes" || strings.HasPrefix(p, "notes/") {
				t.Errorf("record for %s still there at the rename", p)
			}
		}
	}}
	h.run()
	if !renamed {
		t.Fatal("the file was not renamed")
	}
}

func TestOneWayFileAgainstFolderIsHeld(t *testing.T) {
	for _, tc := range []struct {
		name       string
		mode       config.Mode
		fileOnDest bool // the destination holds the file, the source the folder
	}{
		{"push, file on the source", config.ModePush, false},
		{"push, file on the destination", config.ModePush, true},
		{"pull, file on the source", config.ModePull, false},
		{"pull, file on the destination", config.ModePull, true},
	} {
		for _, policy := range []config.ConflictPolicy{config.ConflictKeepBoth, config.ConflictNewestWins, config.ConflictAsk} {
			t.Run(tc.name+", "+string(policy), func(t *testing.T) {
				h := newHarness(t, tc.mode, config.DeletionsMirror)
				h.settings.Conflicts = policy
				src, dst := h.localDir, h.remoteDir
				if tc.mode == config.ModePull {
					src, dst = h.remoteDir, h.localDir
				}
				write(t, src, "notes/inside.txt", "in a folder", t0)
				write(t, src, "other.txt", "o", t0)
				h.run()
				if tc.fileOnDest {
					folderToFile(t, dst)
				} else {
					folderToFile(t, src)
				}
				wantSrc, wantDst := listing(t, src), listing(t, dst)

				for run := range 2 {
					res := h.run()
					if len(res.Conflicts) != 1 || res.Conflicts[0].Path != "notes" || res.Conflicts[0].Outcome != "held" {
						t.Fatalf("run %d: conflicts = %+v, want notes held", run, res.Conflicts)
					}
					if res.Deleted != 0 || res.Copied != 0 {
						t.Errorf("run %d: deleted %d, copied %d", run, res.Deleted, res.Copied)
					}
					if got := listing(t, src); got != wantSrc {
						t.Errorf("run %d: source = %s\nwant     %s", run, got, wantSrc)
					}
					if got := listing(t, dst); got != wantDst {
						t.Errorf("run %d: destination = %s\nwant          %s", run, got, wantDst)
					}
				}
			})
		}
	}
}

// Choosing keep-both on a held one-way clash would rename on the source.
func TestResolveRefusesKeepBothOnAOneWayClash(t *testing.T) {
	for _, mode := range []config.Mode{config.ModePush, config.ModePull} {
		t.Run(string(mode), func(t *testing.T) {
			h := newHarness(t, mode, config.DeletionsMirror)
			write(t, h.localDir, "notes", "a file", t0)
			write(t, h.remoteDir, "notes/inside.txt", "in a folder", t0)
			wantLocal, wantRemote := listing(t, h.localDir), listing(t, h.remoteDir)

			if err := Resolve(context.Background(), &h.opts, "notes", KeepBoth); err == nil {
				t.Error("keep-both on a one-way clash should be refused")
			}
			if got := listing(t, h.localDir); got != wantLocal {
				t.Errorf("local = %s, want %s", got, wantLocal)
			}
			if got := listing(t, h.remoteDir); got != wantRemote {
				t.Errorf("remote = %s, want %s", got, wantRemote)
			}
		})
	}
}
