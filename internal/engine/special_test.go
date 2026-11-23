// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package engine

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/fqazzazee/shuttle/internal/config"
	"github.com/fqazzazee/shuttle/internal/fsys"
)

// replaceWithLink moves p out of dir and leaves a symlink to it in its place,
// the way a folder gets moved to a bigger disk.
func replaceWithLink(t *testing.T, dir, p string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(p))
	moved := filepath.Join(t.TempDir(), filepath.Base(full))
	if err := os.Rename(full, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(moved, full); err != nil {
		t.Fatal(err)
	}
}

func TestVersionsNameThatIsNoFolderStopsTheRun(t *testing.T) {
	for name, place := range map[string]func(t *testing.T, full string){
		"file": func(t *testing.T, full string) {
			if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		},
		"symlink": func(t *testing.T, full string) {
			if err := os.Symlink(t.TempDir(), full); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, config.ModeSync, config.DeletionsMirror)
			for _, p := range []string{"docs/a.txt", "photo.jpg", "zeta.txt"} {
				write(t, h.localDir, p, p, t0)
			}
			h.run()
			want := listing(t, h.remoteDir)
			place(t, filepath.Join(h.localDir, VersionsDir))

			for range 2 {
				res, err := Run(context.Background(), &h.opts)
				if err == nil {
					t.Errorf("Run succeeded with a %s in place of the versions folder", name)
				}
				if res.Deleted != 0 {
					t.Errorf("deleted = %d, want 0", res.Deleted)
				}
				if got := listing(t, h.remoteDir); got != want {
					t.Fatalf("remote = %s\nwant     %s", got, want)
				}
			}
		})
	}
}

func TestSyncedPathNowASymlinkIsHeld(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mode   config.Mode
		remote bool // the link is on the remote side
		p      string
	}{
		{"sync, folder, local", config.ModeSync, false, "Photos"},
		{"sync, folder, remote", config.ModeSync, true, "Photos"},
		{"sync, file, local", config.ModeSync, false, "Photos/a.jpg"},
		{"sync, file, remote", config.ModeSync, true, "Photos/a.jpg"},
		{"push, source", config.ModePush, false, "Photos"},
		{"push, destination", config.ModePush, true, "Photos"},
		{"pull, source", config.ModePull, true, "Photos"},
		{"pull, destination", config.ModePull, false, "Photos"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, tc.mode, config.DeletionsMirror)
			seed := h.localDir
			if tc.mode == config.ModePull {
				seed = h.remoteDir
			}
			write(t, seed, "Photos/a.jpg", "photo", t0)
			write(t, seed, "Photos/b.jpg", "other photo", t0)
			write(t, seed, "other.txt", "other", t0)
			h.run()

			linked, kept, label := h.localDir, h.remoteDir, "local: "+tc.p
			if tc.remote {
				linked, kept, label = h.remoteDir, h.localDir, "remote: "+tc.p
			}
			want := listing(t, kept)
			replaceWithLink(t, linked, tc.p)

			for run := range 2 {
				res := h.run()
				if res.Deleted != 0 || res.Copied != 0 || res.Versioned != 0 {
					t.Errorf("run %d: %+v, want nothing changed", run, res)
				}
				if !slices.Contains(res.Unsupported, label) {
					t.Errorf("run %d: unsupported = %v, want %s", run, res.Unsupported, label)
				}
				if got := listing(t, kept); got != want {
					t.Fatalf("run %d: other side = %s\nwant          %s", run, got, want)
				}
				fi, err := os.Lstat(filepath.Join(linked, filepath.FromSlash(tc.p)))
				if err != nil || fi.Mode()&os.ModeSymlink == 0 {
					t.Fatalf("run %d: the link is gone: %v, %v", run, fi, err)
				}
			}
		})
	}
}

// A folder holding a held path stays, even when the other side deleted it.
func TestFolderAroundAHeldPathIsNotDeleted(t *testing.T) {
	h := newHarness(t, config.ModeSync, config.DeletionsMirror)
	write(t, h.localDir, "Photos/sub/a.jpg", "photo", t0)
	write(t, h.localDir, "Photos/b.jpg", "other photo", t0)
	h.run()
	replaceWithLink(t, h.localDir, "Photos/sub")
	if err := os.RemoveAll(filepath.Join(h.remoteDir, "Photos")); err != nil {
		t.Fatal(err)
	}

	for range 2 {
		h.run() // fails on an error, such as removing the non-empty folder
	}
	if _, err := os.Lstat(filepath.Join(h.localDir, "Photos", "sub")); err != nil {
		t.Errorf("the link went with its folder: %v", err)
	}
}

// On a side that ignores case, what is under a link is found by any spelling.
func TestHeldPathMatchesAcrossSpellings(t *testing.T) {
	h := newHarness(t, config.ModePush, config.DeletionsMirror)
	phone, err := fsys.OpenLocalWith(h.localDir, fsys.AndroidSharedStorage)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { phone.Close() })
	h.opts.Local = phoneFS{phone}
	write(t, h.localDir, "Photos/a.jpg", "photo", t0)
	h.run()
	if err := os.Rename(filepath.Join(h.remoteDir, "Photos"), filepath.Join(h.remoteDir, "photos")); err != nil {
		t.Fatal(err)
	}
	replaceWithLink(t, h.localDir, "Photos")

	for range 2 {
		if res := h.run(); res.Deleted != 0 {
			t.Fatalf("deleted = %d, want 0", res.Deleted)
		}
		if got, ok := read(t, h.remoteDir, "photos/a.jpg"); !ok || got != "photo" {
			t.Fatalf("remote photos/a.jpg = %q, %v", got, ok)
		}
	}
}
