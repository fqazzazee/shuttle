// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package engine

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fqazzazee/shuttle/internal/config"
	"github.com/fqazzazee/shuttle/internal/fsys"
)

var resolvedAt = time.Date(2026, 10, 11, 15, 30, 0, 0, time.UTC)

// conflicted returns a sync harness where a.txt was synced and then edited
// on both sides: locally at localTime, remotely at remoteTime.
func conflicted(t *testing.T, policy config.ConflictPolicy, localTime, remoteTime time.Time) *harness {
	t.Helper()
	h := newHarness(t, config.ModeSync, config.DeletionsAdditive)
	h.settings.Conflicts = policy
	h.opts.LocalName, h.opts.RemoteName = "laptop", "nas"
	h.opts.Now = func() time.Time { return resolvedAt }
	write(t, h.localDir, "a.txt", "base", t0)
	h.run()
	write(t, h.localDir, "a.txt", "local edit", localTime)
	write(t, h.remoteDir, "a.txt", "remote edit", remoteTime)
	return h
}

func TestKeepBothNewerKeepsTheName(t *testing.T) {
	h := conflicted(t, config.ConflictKeepBoth, t1, t1.Add(time.Minute)) // remote is newer
	res := h.run()

	want := "a.sync-conflict-20261011-153000-laptop.txt=local edit a.txt=remote edit"
	for side, dir := range map[string]string{"local": h.localDir, "remote": h.remoteDir} {
		if got := listing(t, dir); got != want {
			t.Errorf("%s = %s\nwant    %s", side, got, want)
		}
	}
	if len(res.Conflicts) != 1 || !strings.HasPrefix(res.Conflicts[0].Outcome, "kept both: the laptop copy") {
		t.Errorf("conflicts = %+v", res.Conflicts)
	}
	if res := h.run(); res.Planned != 0 {
		t.Errorf("next run planned %d actions, want 0", res.Planned)
	}
}

func TestKeepBothTieKeepsTheLocalName(t *testing.T) {
	h := conflicted(t, config.ConflictKeepBoth, t1, t1)
	h.run()
	if got, _ := read(t, h.localDir, "a.txt"); got != "local edit" {
		t.Errorf("a.txt = %q, want the local edit to keep the name on a tie", got)
	}
	if got, _ := read(t, h.remoteDir, "a.sync-conflict-20261011-153000-nas.txt"); got != "remote edit" {
		t.Errorf("remote edit not saved under its conflict name: %q", got)
	}
}

func TestNewestWins(t *testing.T) {
	h := conflicted(t, config.ConflictNewestWins, t1.Add(time.Minute), t1) // local is newer
	res := h.run()
	for side, dir := range map[string]string{"local": h.localDir, "remote": h.remoteDir} {
		if got := listing(t, dir); got != "a.txt=local edit" {
			t.Errorf("%s = %s, want only the newer local edit", side, got)
		}
	}
	if len(res.Conflicts) != 1 || res.Conflicts[0].Outcome != "newest wins: kept the laptop copy" {
		t.Errorf("conflicts = %+v", res.Conflicts)
	}
}

func TestNewestWinsTieKeepsBoth(t *testing.T) {
	h := conflicted(t, config.ConflictNewestWins, t1, t1)
	h.run()
	if got := listing(t, h.remoteDir); !strings.Contains(got, "sync-conflict") {
		t.Errorf("remote = %s: a tie must not pick a winner silently", got)
	}
}

func TestAskHoldsOnlyTheFile(t *testing.T) {
	h := conflicted(t, config.ConflictAsk, t1, t1)
	write(t, h.localDir, "other.txt", "unrelated", t0)
	res := h.run()
	if len(res.Conflicts) != 1 || res.Conflicts[0].Outcome != "held" || res.Held {
		t.Errorf("result = %+v", res)
	}
	if _, ok := read(t, h.remoteDir, "other.txt"); !ok {
		t.Error("other changes should still sync when only the file is held")
	}
}

func TestAskHoldsTheShuttle(t *testing.T) {
	h := conflicted(t, config.ConflictAsk, t1, t1)
	h.settings.ConflictHold = config.HoldShuttle
	write(t, h.localDir, "other.txt", "unrelated", t0)

	for range 2 {
		res := h.run()
		if !res.Held || len(res.Conflicts) != 1 {
			t.Fatalf("result = %+v, want the shuttle held on a.txt", res)
		}
		if _, ok := read(t, h.remoteDir, "other.txt"); ok {
			t.Fatal("other.txt synced while the shuttle was held")
		}
	}

	if err := Resolve(context.Background(), &h.opts, "a.txt", KeepLocal); err != nil {
		t.Fatal(err)
	}
	if got, _ := read(t, h.remoteDir, "a.txt"); got != "local edit" {
		t.Errorf("after keep-local, remote a.txt = %q", got)
	}
	res := h.run()
	if res.Held || len(res.Conflicts) != 0 {
		t.Errorf("result after resolving = %+v", res)
	}
	if _, ok := read(t, h.remoteDir, "other.txt"); !ok {
		t.Error("other.txt did not sync once the conflict was resolved")
	}
}

func TestResolveChoices(t *testing.T) {
	ctx := context.Background()
	t.Run("keep-remote", func(t *testing.T) {
		h := conflicted(t, config.ConflictAsk, t1, t1)
		if err := Resolve(ctx, &h.opts, "a.txt", KeepRemote); err != nil {
			t.Fatal(err)
		}
		if got, _ := read(t, h.localDir, "a.txt"); got != "remote edit" {
			t.Errorf("local a.txt = %q", got)
		}
		if res := h.run(); res.Planned != 0 {
			t.Errorf("next run planned %d actions: the resolution was not recorded", res.Planned)
		}
	})
	t.Run("keep-both", func(t *testing.T) {
		h := conflicted(t, config.ConflictAsk, t1, t1)
		if err := Resolve(ctx, &h.opts, "a.txt", KeepBoth); err != nil {
			t.Fatal(err)
		}
		if got := listing(t, h.localDir); got != listing(t, h.remoteDir) || !strings.Contains(got, "sync-conflict") {
			t.Errorf("local = %s\nremote = %s", got, listing(t, h.remoteDir))
		}
	})
	t.Run("bad input", func(t *testing.T) {
		h := conflicted(t, config.ConflictAsk, t1, t1)
		for _, tc := range []struct {
			p      string
			choice Choice
		}{{"a.txt", "keep-neither"}, {"../a.txt", KeepLocal}, {"missing", KeepLocal}} {
			if err := Resolve(ctx, &h.opts, tc.p, tc.choice); err == nil {
				t.Errorf("Resolve(%q, %q) succeeded", tc.p, tc.choice)
			}
		}
		if got, _ := read(t, h.remoteDir, "a.txt"); got != "remote edit" {
			t.Errorf("a failed Resolve changed the remote file to %q", got)
		}
	})
}

func TestFileAgainstFolderKeepsBoth(t *testing.T) {
	h := newHarness(t, config.ModeSync, config.DeletionsAdditive)
	h.opts.Now = func() time.Time { return resolvedAt }
	write(t, h.localDir, "notes", "a file", t0)
	write(t, h.remoteDir, "notes/inside.txt", "in a folder", t0)

	res := h.run()
	if len(res.Conflicts) != 1 {
		t.Fatalf("conflicts = %+v", res.Conflicts)
	}
	h.run() // the folder and the renamed file sync now
	want := "notes/ notes/inside.txt=in a folder notes.sync-conflict-20261011-153000-local=a file"
	for side, dir := range map[string]string{"local": h.localDir, "remote": h.remoteDir} {
		if got := listing(t, dir); got != want {
			t.Errorf("%s = %s\nwant    %s", side, got, want)
		}
	}
	if err := Resolve(context.Background(), &h.opts, "notes", KeepLocal); err == nil {
		t.Error("keep-local on a file/folder clash should be refused")
	}
}

func TestConflictNames(t *testing.T) {
	for in, want := range map[string]string{
		"a.txt":          "a.X.txt",
		"dir/report.pdf": "dir/report.X.pdf",
		".bashrc":        ".bashrc.X",
		"dir/.env":       "dir/.env.X",
		"Makefile":       "Makefile.X",
		"a.tar.gz":       "a.tar.X.gz",
	} {
		if got := withSuffix(in, ".X"); got != want {
			t.Errorf("withSuffix(%q) = %q, want %q", in, got, want)
		}
	}
	// A label never adds a path separator, whatever the host is called.
	if got := conflictName("a.txt", resolvedAt, "my laptop/../x"); got != "a.sync-conflict-20261011-153000-my-laptop-..-x.txt" {
		t.Errorf("conflictName with an unsafe label = %q", got)
	}
	if got := label(""); got != "unknown" {
		t.Errorf("label(\"\") = %q", got)
	}
}

func TestConflictNameAvoidsExistingFiles(t *testing.T) {
	h := conflicted(t, config.ConflictKeepBoth, t1, t1)
	taken := "a.sync-conflict-20261011-153000-nas.txt"
	write(t, h.remoteDir, taken, "an earlier conflict", t0)
	h.run()
	if got, _ := read(t, h.remoteDir, taken); got != "an earlier conflict" {
		t.Errorf("an existing conflict copy was overwritten with %q", got)
	}
	if got, _ := read(t, h.localDir, "a.sync-conflict-20261011-153000-nas-2.txt"); got != "remote edit" {
		t.Errorf("the new conflict copy is missing or wrong: %q", got)
	}
}

// syncedTree returns a sync harness with n files synced to both sides.
func syncedTree(t *testing.T, n int) *harness {
	t.Helper()
	h := newHarness(t, config.ModeSync, config.DeletionsAdditive)
	for i := range n {
		write(t, h.localDir, "f"+strconv.Itoa(i), "x", t0)
	}
	h.run()
	return h
}

func TestDeletionGuardHoldsMassDeletion(t *testing.T) {
	h := syncedTree(t, 20)
	for i := range 15 { // 75% of the local files vanish, as from an empty mount
		os.Remove(filepath.Join(h.localDir, "f"+strconv.Itoa(i)))
	}
	write(t, h.localDir, "new.txt", "new", t0)

	res := h.run()
	if len(res.HeldDeletions) != 15 || res.Deleted != 0 {
		t.Fatalf("held %d deletions, deleted %d; want 15 held, 0 deleted", len(res.HeldDeletions), res.Deleted)
	}
	if _, ok := read(t, h.remoteDir, "new.txt"); !ok {
		t.Error("other changes should still run while deletions are held")
	}
	if got := len(strings.Fields(listing(t, h.remoteDir))); got != 21 {
		t.Errorf("remote has %d files, want all 20 plus new.txt", got)
	}

	// Still held on the next run, until the user confirms.
	if res := h.run(); len(res.HeldDeletions) != 15 {
		t.Errorf("next run held %d, want 15", len(res.HeldDeletions))
	}
	h.opts.AllowDeletions = true
	if res := h.run(); res.Deleted != 15 || len(res.HeldDeletions) != 0 {
		t.Errorf("confirmed run: deleted %d, held %d", res.Deleted, len(res.HeldDeletions))
	}
}

func TestDeletionGuardLeavesOrdinaryDeletions(t *testing.T) {
	t.Run("under the threshold", func(t *testing.T) {
		h := syncedTree(t, 40)
		for i := range 15 { // 37.5%
			os.Remove(filepath.Join(h.localDir, "f"+strconv.Itoa(i)))
		}
		if res := h.run(); res.Deleted != 15 || len(res.HeldDeletions) != 0 {
			t.Errorf("deleted %d, held %d", res.Deleted, len(res.HeldDeletions))
		}
	})
	t.Run("too few files to matter", func(t *testing.T) {
		h := syncedTree(t, 10)
		for i := range 9 {
			os.Remove(filepath.Join(h.localDir, "f"+strconv.Itoa(i)))
		}
		if res := h.run(); res.Deleted != 9 {
			t.Errorf("deleted %d, want 9", res.Deleted)
		}
	})
	t.Run("guard off", func(t *testing.T) {
		h := syncedTree(t, 20)
		h.settings.DeletionGuard = 0
		for i := range 20 {
			os.Remove(filepath.Join(h.localDir, "f"+strconv.Itoa(i)))
		}
		if res := h.run(); res.Deleted != 20 {
			t.Errorf("deleted %d, want 20", res.Deleted)
		}
	})
}

// A mirror push into a share folder that already holds unrelated files is
// the other way a guard earns its keep.
func TestDeletionGuardOnFirstMirrorPush(t *testing.T) {
	h := newHarness(t, config.ModePush, config.DeletionsMirror)
	write(t, h.localDir, "mine.txt", "m", t0)
	for i := range 12 {
		write(t, h.remoteDir, "theirs"+strconv.Itoa(i), "t", t0)
	}
	res := h.run()
	if len(res.HeldDeletions) != 12 || res.Copied != 1 {
		t.Errorf("held %d, copied %d", len(res.HeldDeletions), res.Copied)
	}
}

// Every conflict carries its two copies as the run found them, whatever
// was done about it: the user may want to know what was kept and lost.
func TestConflictsCarryBothCopies(t *testing.T) {
	for _, policy := range []config.ConflictPolicy{config.ConflictAsk, config.ConflictKeepBoth, config.ConflictNewestWins} {
		t.Run(string(policy), func(t *testing.T) {
			h := newHarness(t, config.ModeSync, config.DeletionsAdditive)
			h.settings.Conflicts = policy
			write(t, h.localDir, "a.txt", "base", t0)
			h.run()
			write(t, h.localDir, "a.txt", "local edit", t1)
			write(t, h.remoteDir, "a.txt", "remote, longer edit", t1.Add(time.Hour))
			res := h.run()
			if len(res.Conflicts) != 1 {
				t.Fatalf("conflicts = %+v", res.Conflicts)
			}
			c := res.Conflicts[0]
			if c.Local == nil || c.Local.Size != int64(len("local edit")) || !c.Local.ModTime.Equal(t1) {
				t.Errorf("local copy = %+v", c.Local)
			}
			if c.Remote == nil || c.Remote.Size != int64(len("remote, longer edit")) || !c.Remote.ModTime.Equal(t1.Add(time.Hour)) {
				t.Errorf("remote copy = %+v", c.Remote)
			}
		})
	}
	t.Run("a file against a folder", func(t *testing.T) {
		h := newHarness(t, config.ModeSync, config.DeletionsAdditive)
		write(t, h.localDir, "x", "file", t0)
		write(t, h.remoteDir, "x/inside.txt", "i", t0)
		res := h.run()
		if len(res.Conflicts) != 1 {
			t.Fatalf("conflicts = %+v", res.Conflicts)
		}
		if c := res.Conflicts[0]; c.Local == nil || c.Local.Kind != fsys.KindFile || c.Remote == nil || c.Remote.Kind != fsys.KindDir {
			t.Errorf("sides: %+v, %+v", c.Local, c.Remote)
		}
	})
}
