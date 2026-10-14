// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fqazzazee/shuttle/internal/config"
)

func TestIgnoredPathsAreNeverTouched(t *testing.T) {
	h := newHarness(t, config.ModePush, config.DeletionsMirror)
	h.settings.Ignore = []string{"*.tmp", "node_modules/"}
	write(t, h.localDir, "keep.txt", "k", t0)
	write(t, h.localDir, "cache.tmp", "c", t0)
	write(t, h.localDir, "web/node_modules/pkg/index.js", "js", t0)
	write(t, h.remoteDir, "theirs.tmp", "ignored on the share too", t0)
	write(t, h.remoteDir, "old.txt", "not ignored: mirror removes it", t0)

	h.run()
	// Mirror must not delete an ignored remote file, nor copy ignored local ones.
	if got, want := listing(t, h.remoteDir), "keep.txt=k theirs.tmp=ignored on the share too web/"; got != want {
		t.Errorf("remote = %s\nwant     %s", got, want)
	}
	if res := h.run(); res.Planned != 0 {
		t.Errorf("second run planned %d actions", res.Planned)
	}
}

// Only what a shuttle includes is synced; the rest is left alone on both
// sides, even by mirror.
func TestOnlyIncludedPathsAreSynced(t *testing.T) {
	h := newHarness(t, config.ModePush, config.DeletionsMirror)
	h.settings.Include = []string{"photos/2024", "notes.txt"}
	h.settings.Ignore = []string{"*.tmp"}
	write(t, h.localDir, "notes.txt", "n", t0)
	write(t, h.localDir, "todo.txt", "not included", t0)
	write(t, h.localDir, "photos/2024/a.jpg", "a", t0)
	write(t, h.localDir, "photos/2024/scratch.tmp", "ignored inside an included folder", t0)
	write(t, h.localDir, "photos/2023/b.jpg", "not included", t0)
	write(t, h.localDir, "photos/c.jpg", "next to an included folder", t0)
	write(t, h.remoteDir, "music/song.mp3", "not included: mirror leaves it", t0)

	h.run()
	want := "music/ music/song.mp3=not included: mirror leaves it notes.txt=n photos/ photos/2024/ photos/2024/a.jpg=a"
	if got := listing(t, h.remoteDir); got != want {
		t.Errorf("remote = %s\nwant     %s", got, want)
	}
	if res := h.run(); res.Planned != 0 {
		t.Errorf("second run planned %d actions", res.Planned)
	}
}

func TestExcludedPathsAreNeverTouched(t *testing.T) {
	h := newHarness(t, config.ModePush, config.DeletionsMirror)
	h.settings.Exclude = []string{"raw", "docs/draft.txt"}
	write(t, h.localDir, "raw/a.cr2", "excluded", t0)
	write(t, h.localDir, "docs/draft.txt", "excluded", t0)
	write(t, h.localDir, "docs/final.txt", "f", t0)
	write(t, h.remoteDir, "raw/b.cr2", "excluded: mirror leaves it", t0)

	h.run()
	want := "docs/ docs/final.txt=f raw/ raw/b.cr2=excluded: mirror leaves it"
	if got := listing(t, h.remoteDir); got != want {
		t.Errorf("remote = %s\nwant     %s", got, want)
	}
	if res := h.run(); res.Planned != 0 {
		t.Errorf("second run planned %d actions", res.Planned)
	}
}

// Ignoring a file that was already synced stops syncing it; it must not read
// as a deletion on either side.
func TestIgnoringASyncedFileDeletesNothing(t *testing.T) {
	h := newHarness(t, config.ModeSync, config.DeletionsAdditive)
	write(t, h.localDir, "notes.txt", "n", t0)
	h.run()

	h.settings.Ignore = []string{"notes.txt"}
	os.Remove(filepath.Join(h.localDir, "notes.txt"))
	h.run()
	if got, _ := read(t, h.remoteDir, "notes.txt"); got != "n" {
		t.Errorf("remote notes.txt = %q: an ignored file was deleted", got)
	}

	// And once no longer ignored, it is a plain first sync again.
	h.settings.Ignore = nil
	h.run()
	if got, _ := read(t, h.localDir, "notes.txt"); got != "n" {
		t.Errorf("local notes.txt = %q after un-ignoring", got)
	}
}

const (
	transferSize = 256 << 10 // bytes copied by the timed tests
	slowRate     = 512 << 10 // bytes per second
	// At slowRate, everything past the first burst takes this long.
	minLimited = time.Duration(float64(transferSize-maxBurst) / slowRate * float64(time.Second))
)

// timedRun makes a transferSize file on src, runs once, and returns how long
// the run took.
func timedRun(t *testing.T, h *harness, srcDir string) time.Duration {
	t.Helper()
	write(t, srcDir, "big.bin", strings.Repeat("x", transferSize), t0)
	start := time.Now()
	res := h.run()
	if res.Copied != 1 {
		t.Fatalf("copied %d files", res.Copied)
	}
	return time.Since(start)
}

func TestBandwidthLimits(t *testing.T) {
	if testing.Short() {
		t.Skip("timed")
	}
	t.Run("upload", func(t *testing.T) {
		h := newHarness(t, config.ModePush, config.DeletionsAdditive)
		h.settings.Bandwidth.Up = slowRate
		if took := timedRun(t, h, h.localDir); took < minLimited*9/10 {
			t.Errorf("push took %v, want at least %v at %d B/s", took, minLimited, slowRate)
		}
	})
	t.Run("download", func(t *testing.T) {
		h := newHarness(t, config.ModePull, config.DeletionsAdditive)
		h.settings.Bandwidth.Down = slowRate
		if took := timedRun(t, h, h.remoteDir); took < minLimited*9/10 {
			t.Errorf("pull took %v, want at least %v", took, minLimited)
		}
	})
	t.Run("upload limit does not slow downloads", func(t *testing.T) {
		h := newHarness(t, config.ModePull, config.DeletionsAdditive)
		h.settings.Bandwidth.Up = 1 // would take days if it applied
		if took := timedRun(t, h, h.remoteDir); took > 5*time.Second {
			t.Errorf("pull took %v", took)
		}
	})
	t.Run("shared cap", func(t *testing.T) {
		h := newHarness(t, config.ModePush, config.DeletionsAdditive)
		h.opts.Cap = NewCap(slowRate, 0)
		if took := timedRun(t, h, h.localDir); took < minLimited*9/10 {
			t.Errorf("push under the shared cap took %v, want at least %v", took, minLimited)
		}
	})
}

// A cap set while a run is going applies from its next file.
func TestCapChangesMidRun(t *testing.T) {
	if testing.Short() {
		t.Skip("timed")
	}
	h := newHarness(t, config.ModePush, config.DeletionsAdditive)
	c := NewCap(0, 0)
	h.opts.Cap = c
	write(t, h.localDir, "a.bin", strings.Repeat("x", transferSize), t0)
	write(t, h.localDir, "b.bin", strings.Repeat("x", transferSize), t0)
	var second time.Time
	h.opts.OnAction = func(*Action, error) {
		if second.IsZero() {
			c.Set(slowRate, 0)
			second = time.Now()
		}
	}
	if res := h.run(); res.Copied != 2 {
		t.Fatalf("copied %d files", res.Copied)
	}
	if took := time.Since(second); took < minLimited*9/10 {
		t.Errorf("the file after the cap was set took %v, want at least %v", took, minLimited)
	}
}

func TestCap(t *testing.T) {
	if l := (*Cap)(nil).Limits(); l.Up != nil || l.Down != nil {
		t.Error("a nil cap must not limit")
	}
	c := NewCap(1000, 0)
	first := c.Limits()
	if first.Up == nil || first.Down != nil {
		t.Fatalf("limits = %+v, want up only", first)
	}
	c.Set(1000, 2000)
	if l := c.Limits(); l.Up != first.Up || l.Down == nil {
		t.Errorf("an unchanged rate must keep its limiter (and what it has used); got %+v", l)
	}
	c.Set(500, 2000)
	if l := c.Limits(); l.Up == first.Up || l.Up.burst != 500 {
		t.Errorf("a changed rate must get a new limiter; got %+v", l)
	}
	c.Set(0, 0)
	if l := c.Limits(); l.Up != nil || l.Down != nil {
		t.Errorf("0 must lift the cap; got %+v", l)
	}
}

func TestNewLimiter(t *testing.T) {
	if NewLimiter(0) != nil || NewLimiter(-5) != nil {
		t.Error("0 and below must mean unlimited (nil)")
	}
	if l := NewLimiter(1000); l.burst != 1000 {
		t.Errorf("a slow limit's burst = %d, want the rate itself", l.burst)
	}
	if l := NewLimiter(1 << 30); l.burst != maxBurst {
		t.Errorf("a fast limit's burst = %d, want %d", l.burst, maxBurst)
	}
}
