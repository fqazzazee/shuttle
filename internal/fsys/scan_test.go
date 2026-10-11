// SPDX-License-Identifier: GPL-2.0-or-later

package fsys

import (
	"context"
	"errors"
	"io/fs"
	"runtime"
	"slices"
	"strconv"
	"testing"
	"time"
)

// tree makes n folders of n files each under a fresh local FS.
func scanTree(t *testing.T, n int) (*Local, string) {
	t.Helper()
	l, dir := newLocal(t)
	for i := range n {
		for j := range n {
			writeOS(t, osPath(dir, "d"+strconv.Itoa(i)+"/f"+strconv.Itoa(j)), "x")
		}
	}
	return l, dir
}

func TestScanTreeMatchesWalk(t *testing.T) {
	l, _ := scanTree(t, 12)
	ctx := context.Background()
	var walked, scanned []string
	if err := Walk(ctx, l, ".", func(i FileInfo) error { walked = append(walked, i.Name); return nil }); err != nil {
		t.Fatal(err)
	}
	for _, workers := range []int{0, 1, 3, 16} {
		scanned = scanned[:0]
		err := ScanTree(ctx, l, ".", workers, func(i FileInfo) error { scanned = append(scanned, i.Name); return nil })
		if err != nil {
			t.Fatal(err)
		}
		slices.Sort(scanned)
		if !slices.Equal(scanned, walked) {
			t.Errorf("workers=%d: scanned %d entries, walked %d", workers, len(scanned), len(walked))
		}
	}
}

func TestScanTreeSkipDir(t *testing.T) {
	l, dir := newLocal(t)
	for _, p := range []string{"keep/a", "skip/deep/b", "x/1", "x/2", "x/3"} {
		writeOS(t, osPath(dir, p), p)
	}
	var got []string
	err := ScanTree(context.Background(), l, ".", 4, func(i FileInfo) error {
		got = append(got, i.Name)
		switch i.Name {
		case "skip":
			return fs.SkipDir
		case "x/1":
			return fs.SkipDir // the rest of x/
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(got)
	if want := []string{"keep", "keep/a", "skip", "x", "x/1"}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// failingDir fails listing one folder, as when the share drops mid-scan.
type failingDir struct {
	FS
	bad string
}

var errListing = errors.New("listing failed")

func (f failingDir) ReadDir(ctx context.Context, name string) ([]FileInfo, error) {
	if name == f.bad {
		return nil, errListing
	}
	time.Sleep(time.Millisecond) // keep other listings in flight
	return f.FS.ReadDir(ctx, name)
}

// One failed listing must fail the whole scan, never leave it looking
// complete, and leave no workers behind.
func TestScanTreeStopsOnAListingError(t *testing.T) {
	l, _ := scanTree(t, 20)
	before := runtime.NumGoroutine()
	err := ScanTree(context.Background(), failingDir{l, "d7"}, ".", 8, func(FileInfo) error { return nil })
	if !errors.Is(err, errListing) {
		t.Fatalf("ScanTree = %v, want the listing error", err)
	}
	time.Sleep(50 * time.Millisecond)
	if after := runtime.NumGoroutine(); after > before {
		t.Errorf("%d goroutines left behind", after-before)
	}
}

func TestScanTreeStopsOnFnError(t *testing.T) {
	l, _ := scanTree(t, 10)
	stop := errors.New("stop")
	calls := 0
	err := ScanTree(context.Background(), l, ".", 4, func(FileInfo) error {
		calls++
		if calls == 5 {
			return stop
		}
		return nil
	})
	if !errors.Is(err, stop) || calls != 5 {
		t.Errorf("err = %v after %d calls, want stop after 5", err, calls)
	}
}

func TestScanTreeCancelled(t *testing.T) {
	l, _ := scanTree(t, 10)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := ScanTree(ctx, l, ".", 4, func(FileInfo) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Errorf("ScanTree = %v, want context.Canceled", err)
	}
}
