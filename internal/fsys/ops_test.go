// SPDX-License-Identifier: GPL-2.0-or-later

package fsys

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// failingReader returns some data and then an error, like a share dropping
// mid-transfer.
type failingReader struct{ sent bool }

var errDropped = errors.New("connection dropped")

func (f *failingReader) Read(p []byte) (int, error) {
	if f.sent {
		return 0, errDropped
	}
	f.sent = true
	return copy(p, "partial"), nil
}

func listOS(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestWriteFileCreatesWithMtime(t *testing.T) {
	l, dir := newLocal(t)
	ctx := context.Background()
	if err := l.MkdirAll(ctx, "d"); err != nil {
		t.Fatal(err)
	}
	mtime := time.Unix(1_600_000_000, 0)

	n, err := WriteFile(ctx, l, "d/f.txt", strings.NewReader("hello"), mtime)
	if err != nil {
		t.Fatal(err)
	}
	if n != 5 {
		t.Errorf("wrote %d bytes, want 5", n)
	}
	if b, _ := os.ReadFile(osPath(dir, "d/f.txt")); string(b) != "hello" {
		t.Errorf("content = %q", b)
	}
	info, err := l.Stat(ctx, "d/f.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime.Equal(mtime) {
		t.Errorf("ModTime = %v, want %v", info.ModTime, mtime)
	}
	if got := listOS(t, filepath.Join(dir, "d")); !slices.Equal(got, []string{"f.txt"}) {
		t.Errorf("directory holds %v, want only f.txt (no temp files)", got)
	}
}

func TestWriteFileReplacesExisting(t *testing.T) {
	l, dir := newLocal(t)
	writeOS(t, filepath.Join(dir, "f"), "old content")

	if _, err := WriteFile(context.Background(), l, "f", strings.NewReader("new"), time.Now()); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "f")); string(b) != "new" {
		t.Errorf("content = %q, want %q", b, "new")
	}
}

// An interrupted transfer must leave the destination exactly as it was and
// no temp file behind.
func TestWriteFileFailureLeavesOriginal(t *testing.T) {
	l, dir := newLocal(t)
	writeOS(t, filepath.Join(dir, "f"), "original")

	_, err := WriteFile(context.Background(), l, "f", &failingReader{}, time.Now())
	if !errors.Is(err, errDropped) {
		t.Fatalf("error = %v, want errDropped", err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "f")); string(b) != "original" {
		t.Errorf("destination changed to %q", b)
	}
	if got := listOS(t, dir); !slices.Equal(got, []string{"f"}) {
		t.Errorf("directory holds %v, want only f", got)
	}
}

func TestWriteFileCancelled(t *testing.T) {
	l, dir := newLocal(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := WriteFile(ctx, l, "f", strings.NewReader("x"), time.Now()); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if got := listOS(t, dir); len(got) != 0 {
		t.Errorf("directory holds %v, want nothing", got)
	}
}

func TestIsTemp(t *testing.T) {
	for name, want := range map[string]bool{
		TempPrefix + "abc":          true,
		"d/" + TempPrefix + "abc":   true,
		"file.txt":                  false,
		TempPrefix + "dir/file.txt": false,
	} {
		if got := IsTemp(name); got != want {
			t.Errorf("IsTemp(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestWalkOrderAndSkipDir(t *testing.T) {
	l, dir := newLocal(t)
	for _, name := range []string{"b/2", "b/1", "a", "c/skip/deep", "c/x", "d/1", "d/2", "d/3"} {
		writeOS(t, osPath(dir, name), name)
	}

	var got []string
	err := Walk(context.Background(), l, ".", func(info FileInfo) error {
		got = append(got, info.Name)
		switch info.Name {
		case "c/skip":
			return fs.SkipDir // skip this directory's contents
		case "d/2":
			return fs.SkipDir // skip the rest of d/
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a", "b", "b/1", "b/2", "c", "c/skip", "c/x", "d", "d/1", "d/2"}
	if !slices.Equal(got, want) {
		t.Errorf("visited %v\nwant    %v", got, want)
	}
}

func TestWalkStopsOnError(t *testing.T) {
	l, dir := newLocal(t)
	for _, name := range []string{"a", "b", "c"} {
		writeOS(t, osPath(dir, name), name)
	}
	stop := errors.New("stop")

	var got []string
	err := Walk(context.Background(), l, ".", func(info FileInfo) error {
		got = append(got, info.Name)
		if info.Name == "b" {
			return stop
		}
		return nil
	})
	if !errors.Is(err, stop) {
		t.Fatalf("error = %v, want stop", err)
	}
	if !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("visited %v, want [a b]", got)
	}
}

// A directory that cannot be listed must fail the walk, not be skipped:
// treating a partial scan as complete would turn unreadable files into
// deletions on the other side.
func TestWalkFailsOnUnreadableDir(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read any directory")
	}
	l, dir := newLocal(t)
	writeOS(t, osPath(dir, "locked/f"), "x")
	if err := os.Chmod(filepath.Join(dir, "locked"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(filepath.Join(dir, "locked"), 0o755) })

	err := Walk(context.Background(), l, ".", func(FileInfo) error { return nil })
	if !errors.Is(err, fs.ErrPermission) {
		t.Errorf("error = %v, want fs.ErrPermission", err)
	}
}
