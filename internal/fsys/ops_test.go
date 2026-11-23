// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package fsys

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
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

// syncLog is a Local that records the order of the steps of a write.
type syncLog struct {
	*Local
	steps   []string
	syncErr error
}

type syncLogFile struct {
	io.WriteCloser
	log *syncLog
}

func (f syncLogFile) Sync() error {
	f.log.steps = append(f.log.steps, "sync")
	return f.log.syncErr
}

func (f syncLogFile) Close() error {
	f.log.steps = append(f.log.steps, "close")
	return f.WriteCloser.Close()
}

func (l *syncLog) Create(ctx context.Context, name string) (io.WriteCloser, error) {
	w, err := l.Local.Create(ctx, name)
	if err != nil {
		return nil, err
	}
	return syncLogFile{w, l}, nil
}

func (l *syncLog) Rename(ctx context.Context, oldname, newname string) error {
	l.steps = append(l.steps, "rename")
	return l.Local.Rename(ctx, oldname, newname)
}

// A durable state record must never describe a file whose data could still
// be lost, so the data reaches the disk before the name does.
func TestWriteFileSyncsBeforeRename(t *testing.T) {
	l, _ := newLocal(t)
	log := &syncLog{Local: l}

	if _, err := WriteFile(context.Background(), log, "f", strings.NewReader("data"), time.Now()); err != nil {
		t.Fatal(err)
	}
	if want := []string{"sync", "close", "rename"}; !slices.Equal(log.steps, want) {
		t.Errorf("steps = %v, want %v", log.steps, want)
	}
}

// A file that could not be flushed must not replace the good one.
func TestWriteFileSyncFailureLeavesOriginal(t *testing.T) {
	l, dir := newLocal(t)
	writeOS(t, filepath.Join(dir, "f"), "original")
	log := &syncLog{Local: l, syncErr: errDropped}

	_, err := WriteFile(context.Background(), log, "f", strings.NewReader("new"), time.Now())
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

// The cost of a flushed copy, on the file system of $TMPDIR: point it at a
// real disk, since a tmpfs flush costs nothing.
func BenchmarkWriteFile(b *testing.B) {
	for _, bc := range []struct {
		name  string
		files int
		size  int
	}{
		{"1000x4KiB", 1000, 4 << 10},
		{"1x256MiB", 1, 256 << 20},
	} {
		b.Run(bc.name, func(b *testing.B) {
			data := make([]byte, bc.size)
			if _, err := rand.Read(data); err != nil {
				b.Fatal(err)
			}
			b.SetBytes(int64(bc.files * bc.size))
			for b.Loop() {
				dir := b.TempDir()
				l, err := OpenLocal(dir)
				if err != nil {
					b.Fatal(err)
				}
				for i := range bc.files {
					name := "f" + strconv.Itoa(i)
					if _, err := WriteFile(context.Background(), l, name, bytes.NewReader(data), time.Now()); err != nil {
						b.Fatal(err)
					}
				}
				l.Close()
			}
		})
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

// readSizes records how much each Read asks for.
type readSizes struct {
	r     io.Reader
	sizes []int
}

func (s *readSizes) Read(p []byte) (int, error) {
	s.sizes = append(s.sizes, len(p))
	return s.r.Read(p)
}

// Each read from a share is a round trip: reading 32 KB at a time, as
// io.Copy does, made pulls over Wi-Fi five times slower.
func TestWriteFileReadsInLargeChunks(t *testing.T) {
	l, dir := newLocal(t)
	data := bytes.Repeat([]byte("0123456789abcdef"), 3<<20/16)
	src := &readSizes{r: bytes.NewReader(data)}

	if _, err := WriteFile(context.Background(), l, "f", src, time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := slices.Max(src.sizes); got < 1<<20 {
		t.Errorf("largest read asked for %d bytes, want 1 MB", got)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "f")); !bytes.Equal(b, data) {
		t.Errorf("content differs: %d bytes, want %d", len(b), len(data))
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
