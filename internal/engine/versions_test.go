// SPDX-License-Identifier: GPL-2.0-or-later

package engine

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/fqazzazee/shuttle/internal/config"
	"github.com/fqazzazee/shuttle/internal/fsys"
)

// versionedAt is when the tests' versions are made, in local time like real
// version names.
var versionedAt = time.Date(2026, 10, 11, 15, 30, 0, 0, time.Local)

// versionsListing renders the versions folder under dir like listing does.
func versionsListing(t *testing.T, dir string) string {
	t.Helper()
	root := filepath.Join(dir, VersionsDir)
	if _, err := os.Stat(root); errors.Is(err, os.ErrNotExist) {
		return ""
	}
	return listing(t, root)
}

func TestVersionNames(t *testing.T) {
	for in, want := range map[string]string{
		"a.txt":          ".shuttle-versions/a~20261011-153000.txt",
		"dir/report.pdf": ".shuttle-versions/dir/report~20261011-153000.pdf",
		".bashrc":        ".shuttle-versions/.bashrc~20261011-153000",
		"Makefile":       ".shuttle-versions/Makefile~20261011-153000",
	} {
		got := versionPath(in, versionedAt)
		if got != want {
			t.Errorf("versionPath(%q) = %q, want %q", in, got, want)
		}
		v, ok := parseVersion(got, 1)
		if !ok || v.original != in || !v.at.Equal(versionedAt) {
			t.Errorf("parseVersion(%q) = %+v, %v; want original %q", got, v, ok, in)
		}
	}
	if v, ok := parseVersion(".shuttle-versions/a~20261011-153000-3.txt", 1); !ok || v.original != "a.txt" {
		t.Errorf("a same-second version parsed as %+v, %v", v, ok)
	}
	// Anything shuttle did not write is not a version, and is never pruned.
	for _, p := range []string{
		".shuttle-versions/notes.txt",
		".shuttle-versions/a~2026-10-11.txt",
		".shuttle-versions/~20261011-153000.txt",
		"a~20261011-153000.txt",
	} {
		if v, ok := parseVersion(p, 1); ok {
			t.Errorf("parseVersion(%q) = %+v, want not a version", p, v)
		}
	}
}

// versions makes one version of original per age, newest first or not.
func versions(original string, now time.Time, ages ...time.Duration) []version {
	var out []version
	for _, age := range ages {
		at := now.Add(-age)
		out = append(out, version{path: versionPath(original, at), original: original, at: at, size: 10})
	}
	return out
}

func ages(now time.Time, vs []version) []time.Duration {
	var out []time.Duration
	for _, v := range vs {
		out = append(out, now.Sub(v.at))
	}
	slices.Sort(out)
	return out
}

func TestExpired(t *testing.T) {
	now := versionedAt
	h, d := time.Hour, 24*time.Hour
	tests := []struct {
		name     string
		v        config.Versioning
		versions []version
		want     []time.Duration // ages of the versions that go
	}{
		{"trash keeps what is young enough",
			config.Versioning{Kind: config.VersioningTrash, Keep: 30 * d},
			versions("a", now, h, 29*d, 31*d, 400*d), []time.Duration{31 * d, 400 * d}},
		{"simple keeps the newest N per file",
			config.Versioning{Kind: config.VersioningSimple, Count: 2},
			append(versions("a", now, h, 2*h, 3*h), versions("b", now, 5*h)...), []time.Duration{3 * h}},
		{"staggered: all in the first hour, one per hour, per day, per week",
			config.Versioning{Kind: config.VersioningStaggered, Keep: 365 * d},
			versions("a", now,
				time.Minute, 2*time.Minute, // first hour: both kept
				90*time.Minute, 100*time.Minute, // same hour bucket: the newer stays
				50*h, 60*h, // same day bucket (day 2): the newer stays
				40*d, 41*d, // same week bucket: the newer stays
				400*d, // past the maximum age
			),
			[]time.Duration{100 * time.Minute, 60 * h, 41 * d, 400 * d}},
		{"staggered with no maximum age keeps old versions",
			config.Versioning{Kind: config.VersioningStaggered},
			versions("a", now, 1000*d), nil},
		{"size cap drops oldest first across files",
			config.Versioning{Kind: config.VersioningTrash, Keep: 30 * d, SizeCap: 25},
			append(versions("a", now, h, 3*h), versions("b", now, 2*h, 4*h)...), // 40 bytes
			[]time.Duration{3 * h, 4 * h}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ages(now, expired(tt.v, tt.versions, now))
			if !slices.Equal(got, tt.want) {
				t.Errorf("expired ages = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPushKeepsTheReplacedVersion(t *testing.T) {
	h := newHarness(t, config.ModePush, config.DeletionsMirror)
	h.opts.Now = func() time.Time { return versionedAt }
	write(t, h.localDir, "a.txt", "v1", t0)
	h.run()
	write(t, h.localDir, "a.txt", "v2", t1)
	write(t, h.remoteDir, "theirs.txt", "only on the share", t0)

	res := h.run()
	if got, _ := read(t, h.remoteDir, "a.txt"); got != "v2" {
		t.Errorf("remote a.txt = %q", got)
	}
	want := "a~20261011-153000.txt=v1 theirs~20261011-153000.txt=only on the share"
	if got := versionsListing(t, h.remoteDir); got != want {
		t.Errorf("remote versions = %s\nwant              %s", got, want)
	}
	if res.Versioned != 2 {
		t.Errorf("Versioned = %d, want 2", res.Versioned)
	}
	if got := versionsListing(t, h.localDir); got != "" {
		t.Errorf("local versions = %s, want none: nothing was replaced locally", got)
	}
}

func TestSyncDeletionIsVersionedOnTheSideItLeaves(t *testing.T) {
	h := newHarness(t, config.ModeSync, config.DeletionsAdditive)
	h.opts.Now = func() time.Time { return versionedAt }
	write(t, h.localDir, "dir/doomed.txt", "keep me somewhere", t0)
	h.run()
	os.Remove(filepath.Join(h.localDir, "dir", "doomed.txt"))

	h.run()
	if _, ok := read(t, h.remoteDir, "dir/doomed.txt"); ok {
		t.Error("the deletion did not reach the remote side")
	}
	if got, _ := read(t, h.remoteDir, ".shuttle-versions/dir/doomed~20261011-153000.txt"); got != "keep me somewhere" {
		t.Errorf("remote version = %q", got)
	}
}

func TestVersioningNone(t *testing.T) {
	h := newHarness(t, config.ModePull, config.DeletionsMirror)
	h.settings.Versioning = config.Versioning{Kind: config.VersioningNone}
	write(t, h.remoteDir, "a.txt", "v1", t0)
	write(t, h.localDir, "mine.txt", "m", t0)
	h.run()
	write(t, h.remoteDir, "a.txt", "v2", t1)

	if res := h.run(); res.Versioned != 0 {
		t.Errorf("Versioned = %d", res.Versioned)
	}
	if _, err := os.Stat(filepath.Join(h.localDir, VersionsDir)); !errors.Is(err, os.ErrNotExist) {
		t.Error("a versions folder was created with versioning off")
	}
}

func TestPruningRemovesExpiredVersions(t *testing.T) {
	h := newHarness(t, config.ModePush, config.DeletionsAdditive)
	h.settings.Versioning = config.Versioning{Kind: config.VersioningTrash, Keep: time.Hour}
	now := versionedAt
	h.opts.Now = func() time.Time { return now }
	write(t, h.localDir, "deep/dir/a.txt", "v1", t0)
	h.run()
	write(t, h.localDir, "deep/dir/a.txt", "v2", t1)
	h.run()
	write(t, h.remoteDir, VersionsDir+"/README", "not a version: never pruned", t0)
	if got := versionsListing(t, h.remoteDir); !strings.Contains(got, "a~20261011-153000.txt=v1") {
		t.Fatalf("remote versions = %s", got)
	}

	now = now.Add(2 * time.Hour)
	res := h.run()
	if res.VersionsPruned != 1 {
		t.Errorf("VersionsPruned = %d, want 1", res.VersionsPruned)
	}
	// The emptied deep/dir folders go too; the stranger stays.
	if got := versionsListing(t, h.remoteDir); got != "README=not a version: never pruned" {
		t.Errorf("remote versions = %s", got)
	}
}

// failingFS fails every new file while fail is set, like a share that drops
// during a transfer.
type failingFS struct {
	fsys.FS
	fail bool
}

var errInjected = errors.New("injected write failure")

func (f *failingFS) Create(ctx context.Context, name string) (io.WriteCloser, error) {
	if f.fail {
		return nil, errInjected
	}
	return f.FS.Create(ctx, name)
}

func TestFailedCopyPutsTheOldVersionBack(t *testing.T) {
	h := newHarness(t, config.ModePush, config.DeletionsAdditive)
	write(t, h.localDir, "a.txt", "v1", t0)
	h.run()
	write(t, h.localDir, "a.txt", "v2", t1)

	remote := &failingFS{FS: h.opts.Remote, fail: true}
	h.opts.Remote = remote
	res, err := Run(context.Background(), &h.opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Errors) != 1 || res.Versioned != 0 {
		t.Errorf("result = %+v", res)
	}
	if got, _ := read(t, h.remoteDir, "a.txt"); got != "v1" {
		t.Errorf("remote a.txt = %q, want v1 back in place", got)
	}
	if got := versionsListing(t, h.remoteDir); got != "" {
		t.Errorf("remote versions = %s, want the version moved back", got)
	}

	remote.fail = false
	h.run()
	if got, _ := read(t, h.remoteDir, "a.txt"); got != "v2" {
		t.Errorf("after recovery, remote a.txt = %q", got)
	}
}
