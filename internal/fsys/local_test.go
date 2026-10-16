// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package fsys

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// newLocal returns a Local over a fresh temp dir and the dir's OS path.
func newLocal(t *testing.T) (*Local, string) {
	t.Helper()
	dir := t.TempDir()
	l, err := OpenLocal(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	return l, dir
}

// osPath turns a slash-separated test path into an OS path under dir.
func osPath(dir, slashPath string) string {
	return filepath.Join(dir, filepath.FromSlash(slashPath))
}

func writeOS(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestValidPath(t *testing.T) {
	tests := []struct {
		name string
		ok   bool
	}{
		{".", true},
		{"a", true},
		{"a/b.txt", true},
		{"with space/ünïcode", true},
		{"", false},
		{"/etc/passwd", false},
		{"..", false},
		{"../x", false},
		{"a/../../x", false},
		{"a/./b", false},
		{"a//b", false},
		{"a/", false},
		{"a\x00b", false},
	}
	for _, tt := range tests {
		if got := ValidPath(tt.name); got != tt.ok {
			t.Errorf("ValidPath(%q) = %v, want %v", tt.name, got, tt.ok)
		}
	}
}

func TestLocalRejectsInvalidPaths(t *testing.T) {
	l, _ := newLocal(t)
	ctx := context.Background()
	for _, name := range []string{"../escape", "/etc/passwd", "a/../../x"} {
		if _, err := l.Stat(ctx, name); !errors.Is(err, ErrInvalidPath) {
			t.Errorf("Stat(%q) error = %v, want ErrInvalidPath", name, err)
		}
		if _, err := l.Create(ctx, name); !errors.Is(err, ErrInvalidPath) {
			t.Errorf("Create(%q) error = %v, want ErrInvalidPath", name, err)
		}
		if err := l.Rename(ctx, "ok", name); !errors.Is(err, ErrInvalidPath) {
			t.Errorf("Rename(ok, %q) error = %v, want ErrInvalidPath", name, err)
		}
	}
}

// A symlink inside the tree must not become a way out of it, for reads or
// writes. This is the case a hostile file server could set up.
func TestLocalSymlinkCannotEscapeRoot(t *testing.T) {
	l, dir := newLocal(t)
	outside := t.TempDir()
	writeOS(t, filepath.Join(outside, "secret"), "do not read")
	if err := os.Symlink(outside, filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if _, err := l.Open(ctx, "link/secret"); err == nil {
		t.Error("Open through a symlink to outside the root succeeded")
	}
	if _, err := l.Create(ctx, "link/planted"); err == nil {
		t.Error("Create through a symlink to outside the root succeeded")
	}
	if _, err := os.Stat(filepath.Join(outside, "planted")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("file appeared outside the root: %v", err)
	}

	info, err := l.Stat(ctx, "link")
	if err != nil {
		t.Fatal(err)
	}
	if info.Kind != KindOther {
		t.Errorf("symlink Kind = %v, want other", info.Kind)
	}
}

func TestLocalStat(t *testing.T) {
	l, dir := newLocal(t)
	writeOS(t, osPath(dir, "d/f.txt"), "hello")
	mtime := time.Unix(1_700_000_000, 123_456_700) // NTFS keeps 100 ns
	if err := os.Chtimes(osPath(dir, "d/f.txt"), mtime, mtime); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	f, err := l.Stat(ctx, "d/f.txt")
	if err != nil {
		t.Fatal(err)
	}
	if f.Name != "d/f.txt" || f.Kind != KindFile || f.Size != 5 || !f.ModTime.Equal(mtime) {
		t.Errorf("Stat(d/f.txt) = %+v", f)
	}

	d, err := l.Stat(ctx, "d")
	if err != nil {
		t.Fatal(err)
	}
	if d.Kind != KindDir || d.Size != 0 {
		t.Errorf("Stat(d) = %+v, want a dir with size 0", d)
	}

	if _, err := l.Stat(ctx, "missing"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Stat(missing) error = %v, want fs.ErrNotExist", err)
	}
}

func TestLocalReadDirSortedWithFullNames(t *testing.T) {
	l, dir := newLocal(t)
	for _, name := range []string{"sub/c", "sub/a", "sub/b"} {
		writeOS(t, osPath(dir, name), name)
	}
	if err := os.Mkdir(osPath(dir, "sub/z-dir"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := l.ReadDir(context.Background(), "sub")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"sub/a", "sub/b", "sub/c", "sub/z-dir"}
	if len(got) != len(want) {
		t.Fatalf("ReadDir returned %d entries, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i].Name != want[i] {
			t.Errorf("entry %d = %q, want %q", i, got[i].Name, want[i])
		}
	}
	if got[3].Kind != KindDir {
		t.Errorf("sub/z-dir Kind = %v, want dir", got[3].Kind)
	}
}

func TestLocalCreateIsExclusive(t *testing.T) {
	l, dir := newLocal(t)
	writeOS(t, filepath.Join(dir, "exists"), "keep me")

	if _, err := l.Create(context.Background(), "exists"); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("Create(existing) error = %v, want fs.ErrExist", err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "exists")); string(b) != "keep me" {
		t.Errorf("existing file changed to %q", b)
	}
}

func TestLocalRenameReplacesFile(t *testing.T) {
	l, dir := newLocal(t)
	writeOS(t, filepath.Join(dir, "new"), "new")
	writeOS(t, filepath.Join(dir, "old"), "old")

	if err := l.Rename(context.Background(), "new", "old"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "old")); string(b) != "new" {
		t.Errorf("after rename, old = %q, want %q", b, "new")
	}
	if _, err := os.Stat(filepath.Join(dir, "new")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("source still exists after rename: %v", err)
	}
}

func TestLocalRemoveRefusesNonEmptyDir(t *testing.T) {
	l, dir := newLocal(t)
	writeOS(t, osPath(dir, "d/f"), "x")
	ctx := context.Background()

	if err := l.Remove(ctx, "d"); err == nil {
		t.Fatal("Remove of a non-empty directory succeeded")
	}
	if err := l.Remove(ctx, "d/f"); err != nil {
		t.Fatal(err)
	}
	if err := l.Remove(ctx, "d"); err != nil {
		t.Fatalf("Remove of the now-empty directory: %v", err)
	}
}

func TestLocalMkdirAllAndChtimes(t *testing.T) {
	l, dir := newLocal(t)
	ctx := context.Background()
	if err := l.MkdirAll(ctx, "a/b/c"); err != nil {
		t.Fatal(err)
	}
	if err := l.MkdirAll(ctx, "a/b/c"); err != nil {
		t.Errorf("MkdirAll on an existing dir: %v", err)
	}
	writeOS(t, osPath(dir, "a/b/c/f"), "x")

	mtime := time.Date(2024, 2, 29, 12, 0, 0, 500, time.UTC)
	if err := l.Chtimes(ctx, "a/b/c/f", mtime); err != nil {
		t.Fatal(err)
	}
	info, err := l.Stat(ctx, "a/b/c/f")
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime.Equal(mtime) {
		t.Errorf("ModTime = %v, want %v", info.ModTime, mtime)
	}
}

func TestLocalCancelledContext(t *testing.T) {
	l, dir := newLocal(t)
	writeOS(t, filepath.Join(dir, "f"), "x")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := l.Open(ctx, "f"); !errors.Is(err, context.Canceled) {
		t.Errorf("Open with cancelled ctx error = %v, want context.Canceled", err)
	}
	if err := l.Remove(ctx, "f"); !errors.Is(err, context.Canceled) {
		t.Errorf("Remove with cancelled ctx error = %v, want context.Canceled", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "f")); err != nil {
		t.Errorf("file removed despite cancelled context: %v", err)
	}
}

func TestLocalOpenReadsContent(t *testing.T) {
	l, dir := newLocal(t)
	writeOS(t, filepath.Join(dir, "f"), "content")

	r, err := l.Open(context.Background(), "f")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "content" {
		t.Errorf("read %q, want %q", b, "content")
	}
}

// TestAndroidSharedStorageNames is what a Galaxy SM-S948U1 on Android 17 did when
// asked to create each name in /sdcard.
func TestAndroidSharedStorageNames(t *testing.T) {
	l, err := OpenLocalWith(t.TempDir(), AndroidSharedStorage)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if !FoldsCase(l) {
		t.Error("FoldsCase = false, want true")
	}
	refused := []string{"a:b", "a?b", "a*b", `a"b`, "a<b", "a>b", "a|b", `a\b`, "a\x01b", "a\x1fb", "a\x7fb", "ok/a:b", "a:b/ok"}
	for _, name := range refused {
		if err := RefuseName(l, name); !errors.Is(err, ErrRefusedName) {
			t.Errorf("RefuseName(%q) = %v, want ErrRefusedName", name, err)
		}
	}
	accepted := []string{"trail.", "trail ", " lead", "CON", "nul.txt", "é.txt", "straße", "ſ"}
	for _, name := range accepted {
		if err := RefuseName(l, name); err != nil {
			t.Errorf("RefuseName(%q) = %v, want nil", name, err)
		}
	}
	if err := RefuseName(l, "a:b"); err == nil || err.Error() != "name refused: it contains ':'" {
		t.Errorf("RefuseName(a:b) = %v", err)
	}
}

// A local folder has its OS's rules: Linux's keep case and refuse nothing,
// Windows' fold case and refuse what NTFS cannot hold.
func TestLocalHasItsOSsRules(t *testing.T) {
	l, _ := newLocal(t)
	windows := runtime.GOOS == "windows"
	if FoldsCase(l) != windows {
		t.Errorf("FoldsCase = %v on %s", FoldsCase(l), runtime.GOOS)
	}
	for _, name := range []string{"a:b", "a\x01b", `a\b`} {
		if err := RefuseName(l, name); (err != nil) != windows {
			t.Errorf("RefuseName(%q) = %v on %s", name, err, runtime.GOOS)
		}
	}
}

func TestFolds(t *testing.T) {
	const nfc, nfd = "\u00e9", "e\u0301" // é, composed and decomposed
	tests := []struct {
		name     string
		fold     func(string) string
		one, two [][2]string
	}{
		{"SimpleFold", SimpleFold,
			[][2]string{{"README.md", "readme.md"}, {"Docs/Report.PDF", "docs/report.pdf"}, {"ſ", "s"}, {"ÉTÉ", "été"}},
			[][2]string{{"a.txt", "b.txt"}, {"straße", "STRASSE"}, {nfc, nfd}}},
		// What the phone's shared storage took as one name.
		{"FullFold", FullFold,
			[][2]string{{"README.md", "readme.md"}, {"É.txt", "é.txt"}, {"straße", "STRASSE"}, {"STRAßE", "strasse"}, {"ſ", "s"}, {nfc, nfd}, {"Dir/" + nfc, "dir/" + nfd}},
			[][2]string{{"a.txt", "b.txt"}, {"a/b", "a.b"}}},
	}
	for _, tt := range tests {
		for _, p := range tt.one {
			if tt.fold(p[0]) != tt.fold(p[1]) {
				t.Errorf("%s: %q and %q should be one name", tt.name, p[0], p[1])
			}
		}
		for _, p := range tt.two {
			if tt.fold(p[0]) == tt.fold(p[1]) {
				t.Errorf("%s: %q and %q should be two names", tt.name, p[0], p[1])
			}
		}
	}
}
