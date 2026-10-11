// SPDX-License-Identifier: GPL-2.0-or-later

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
		h.opts.Shared = Limits{Up: NewLimiter(slowRate)}
		if took := timedRun(t, h, h.localDir); took < minLimited*9/10 {
			t.Errorf("push under the shared cap took %v, want at least %v", took, minLimited)
		}
	})
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
