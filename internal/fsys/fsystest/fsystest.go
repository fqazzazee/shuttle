// SPDX-License-Identifier: GPL-2.0-or-later

// Package fsystest is the contract every fsys backend must meet: the
// behaviour the sync engine relies on, checked through the FS interface
// alone. Each backend runs it from its tests.
package fsystest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fqazzazee/shuttle/internal/fsys"
)

// Run checks that the FS from newFS behaves as the engine expects. newFS must
// return a fresh, empty FS each time it is called.
func Run(t *testing.T, newFS func(t *testing.T) fsys.FS) {
	ctx := context.Background()

	t.Run("missing paths", func(t *testing.T) {
		f := newFS(t)
		if _, err := f.Stat(ctx, "missing"); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("Stat = %v, want fs.ErrNotExist", err)
		}
		if _, err := f.Open(ctx, "missing"); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("Open = %v, want fs.ErrNotExist", err)
		}
		if _, err := f.ReadDir(ctx, "missing"); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("ReadDir = %v, want fs.ErrNotExist", err)
		}
		if err := f.Remove(ctx, "missing"); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("Remove = %v, want fs.ErrNotExist", err)
		}
	})

	t.Run("create, stat, read", func(t *testing.T) {
		f := newFS(t)
		put(t, f, "hello.txt", "hello, world")
		info, err := f.Stat(ctx, "hello.txt")
		if err != nil {
			t.Fatal(err)
		}
		if info.Name != "hello.txt" || info.Kind != fsys.KindFile || info.Size != 12 {
			t.Errorf("Stat = %+v", info)
		}
		if got := get(t, f, "hello.txt"); got != "hello, world" {
			t.Errorf("read %q", got)
		}
		root, err := f.Stat(ctx, ".")
		if err != nil || root.Kind != fsys.KindDir {
			t.Errorf(`Stat(".") = %+v, %v; want the root folder`, root, err)
		}
	})

	t.Run("create is exclusive", func(t *testing.T) {
		f := newFS(t)
		put(t, f, "a", "first")
		if _, err := f.Create(ctx, "a"); !errors.Is(err, fs.ErrExist) {
			t.Errorf("second Create = %v, want fs.ErrExist", err)
		}
		if got := get(t, f, "a"); got != "first" {
			t.Errorf("content changed to %q", got)
		}
	})

	t.Run("readdir is sorted with full names", func(t *testing.T) {
		f := newFS(t)
		for _, p := range []string{"d/c", "d/a", "d/b"} {
			mkdirs(t, f, "d")
			put(t, f, p, p)
		}
		mkdirs(t, f, "d/z-dir")
		got, err := f.ReadDir(ctx, "d")
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, e := range got {
			names = append(names, e.Name)
		}
		if !slices.Equal(names, []string{"d/a", "d/b", "d/c", "d/z-dir"}) {
			t.Errorf("ReadDir names = %v", names)
		}
		if got[3].Kind != fsys.KindDir || got[0].Kind != fsys.KindFile || got[0].Size != 3 {
			t.Errorf("entries = %+v", got)
		}
	})

	t.Run("rename replaces a file", func(t *testing.T) {
		f := newFS(t)
		put(t, f, "new", "new")
		put(t, f, "old", "old")
		if err := f.Rename(ctx, "new", "old"); err != nil {
			t.Fatal(err)
		}
		if got := get(t, f, "old"); got != "new" {
			t.Errorf("old = %q, want the renamed file's content", got)
		}
		if _, err := f.Stat(ctx, "new"); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("source still there: %v", err)
		}
		mkdirs(t, f, "into/sub")
		if err := f.Rename(ctx, "old", "into/sub/moved"); err != nil {
			t.Fatalf("Rename into a folder: %v", err)
		}
	})

	t.Run("remove", func(t *testing.T) {
		f := newFS(t)
		mkdirs(t, f, "d")
		put(t, f, "d/x", "x")
		if err := f.Remove(ctx, "d"); err == nil {
			t.Error("Remove of a non-empty folder succeeded")
		}
		if err := f.Remove(ctx, "d/x"); err != nil {
			t.Fatal(err)
		}
		if err := f.Remove(ctx, "d"); err != nil {
			t.Errorf("Remove of the emptied folder: %v", err)
		}
	})

	t.Run("mkdirall", func(t *testing.T) {
		f := newFS(t)
		mkdirs(t, f, "a/b/c")
		mkdirs(t, f, "a/b/c") // again: not an error
		if info, err := f.Stat(ctx, "a/b/c"); err != nil || info.Kind != fsys.KindDir {
			t.Errorf("Stat(a/b/c) = %+v, %v", info, err)
		}
	})

	t.Run("modification times round trip", func(t *testing.T) {
		f := newFS(t)
		put(t, f, "a", "x")
		// A whole number of 100 ns ticks: SMB's resolution.
		mtime := time.Date(2024, 2, 29, 12, 34, 56, 789_123_400, time.UTC)
		if err := f.Chtimes(ctx, "a", mtime); err != nil {
			t.Fatal(err)
		}
		info, err := f.Stat(ctx, "a")
		if err != nil {
			t.Fatal(err)
		}
		if !info.ModTime.Equal(mtime) {
			t.Errorf("ModTime = %v, want %v", info.ModTime, mtime)
		}
	})

	t.Run("atomic write", func(t *testing.T) {
		f := newFS(t)
		mkdirs(t, f, "d")
		mtime := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
		big := strings.Repeat("0123456789abcdef", 64<<10) // 1 MiB: several SMB writes
		n, err := fsys.WriteFile(ctx, f, "d/big.bin", strings.NewReader(big), mtime)
		if err != nil || n != int64(len(big)) {
			t.Fatalf("WriteFile = %d, %v", n, err)
		}
		if _, err := fsys.WriteFile(ctx, f, "d/big.bin", strings.NewReader("replaced"), mtime); err != nil {
			t.Fatalf("WriteFile over an existing file: %v", err)
		}
		if got := get(t, f, "d/big.bin"); got != "replaced" {
			t.Errorf("content = %.20q", got)
		}
		entries, err := f.ReadDir(ctx, "d")
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 || !entries[0].ModTime.Equal(mtime) {
			t.Errorf("folder holds %+v, want only big.bin with its mtime", entries)
		}
	})

	t.Run("names", func(t *testing.T) {
		f := newFS(t)
		for _, name := range []string{"with space", "ünïcode-名前.txt", ".hidden", "a.b.c"} {
			put(t, f, name, name)
			if got := get(t, f, name); got != name {
				t.Errorf("%q read back as %q", name, got)
			}
		}
	})

	t.Run("invalid paths", func(t *testing.T) {
		f := newFS(t)
		for _, name := range []string{"../escape", "/etc/passwd", "a/../../b", ""} {
			if _, err := f.Stat(ctx, name); !errors.Is(err, fsys.ErrInvalidPath) {
				t.Errorf("Stat(%q) = %v, want ErrInvalidPath", name, err)
			}
			if _, err := f.Create(ctx, name); !errors.Is(err, fsys.ErrInvalidPath) {
				t.Errorf("Create(%q) = %v, want ErrInvalidPath", name, err)
			}
		}
	})

	t.Run("cancelled context", func(t *testing.T) {
		f := newFS(t)
		put(t, f, "a", "x")
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		if err := f.Remove(cctx, "a"); err == nil {
			t.Error("Remove with a cancelled context succeeded")
		}
		if _, err := f.Stat(ctx, "a"); err != nil {
			t.Errorf("file gone after a cancelled Remove: %v", err)
		}
	})

	t.Run("concurrent scan", func(t *testing.T) {
		f := newFS(t)
		var want []string
		for i := range 6 {
			d := "d" + strconv.Itoa(i)
			mkdirs(t, f, d+"/sub")
			want = append(want, d, d+"/sub")
			for j := range 5 {
				p := d + "/sub/f" + strconv.Itoa(j)
				put(t, f, p, p)
				want = append(want, p)
			}
		}
		calls := 0 // not atomic: the race detector catches concurrent calls
		var got []string
		err := fsys.ScanTree(ctx, f, ".", 8, func(info fsys.FileInfo) error {
			calls++
			got = append(got, info.Name)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		slices.Sort(got)
		slices.Sort(want)
		if !slices.Equal(got, want) || calls != len(want) {
			t.Errorf("scanned %d entries:\n%v\nwant %d:\n%v", calls, got, len(want), want)
		}
	})

	t.Run("walk", func(t *testing.T) {
		f := newFS(t)
		mkdirs(t, f, "b/c")
		put(t, f, "a", "a")
		put(t, f, "b/c/d", "d")
		var got []string
		err := fsys.Walk(ctx, f, ".", func(info fsys.FileInfo) error {
			got = append(got, info.Name)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(got, []string{"a", "b", "b/c", "b/c/d"}) {
			t.Errorf("walked %v", got)
		}
	})
}

// SambaUser and SambaPassword log in to the test Samba server
// (testdata/samba). The password is distinctive on purpose, so tests can
// search for it to prove it did not leak.
const (
	SambaUser     = "shuttle"
	SambaPassword = "samba-throwaway-pw"
)

// UniqueName returns a name for a test's own folder on a shared server.
func UniqueName(prefix string) string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return prefix + "-" + hex.EncodeToString(b[:])
}

// Env returns the environment variable key, or skips the test when it is
// not set. Integration tests use it to find their server.
func Env(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		t.Skipf("%s not set; see `make samba-up` and `make test-integration`", key)
	}
	return v
}

func put(t *testing.T, f fsys.FS, name, content string) {
	t.Helper()
	w, err := f.Create(context.Background(), name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(w, content); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

func get(t *testing.T, f fsys.FS, name string) string {
	t.Helper()
	r, err := f.Open(context.Background(), name)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func mkdirs(t *testing.T, f fsys.FS, name string) {
	t.Helper()
	if err := f.MkdirAll(context.Background(), name); err != nil {
		t.Fatal(err)
	}
}
