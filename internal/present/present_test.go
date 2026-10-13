// SPDX-License-Identifier: GPL-2.0-or-later

package present

import (
	"testing"
	"time"

	"github.com/fqazzazee/shuttle/internal/config"
	"github.com/fqazzazee/shuttle/internal/daemon"
	"github.com/fqazzazee/shuttle/internal/engine"
)

func TestAgo(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	for d, want := range map[time.Duration]string{
		10 * time.Second: "just now", 5 * time.Minute: "5m ago",
		3 * time.Hour: "3h ago", 50 * time.Hour: "2d ago",
	} {
		if got := Ago(now.Add(-d), now); got != want {
			t.Errorf("Ago(-%v) = %q, want %q", d, got, want)
		}
	}
	if got := Ago(time.Time{}, now); got != "never" {
		t.Errorf("Ago(zero) = %q", got)
	}
}

func TestUntil(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	for d, want := range map[time.Duration]string{
		-time.Minute: "soon", 10 * time.Second: "soon", 5 * time.Minute: "in 5m",
		3 * time.Hour: "in 3h", 50 * time.Hour: "in 2d",
	} {
		if got := Until(now.Add(d), now); got != want {
			t.Errorf("Until(%v) = %q, want %q", d, got, want)
		}
	}
	if got := Until(time.Time{}, now); got != "" {
		t.Errorf("Until(zero) = %q", got)
	}
}

func TestProgress(t *testing.T) {
	for _, tc := range []struct {
		p    daemon.Progress
		want string
	}{
		{daemon.Progress{}, ""},
		{daemon.Progress{Phase: engine.PhaseScanning, Done: 1234}, "scanning, 1234 found"},
		{daemon.Progress{Phase: engine.PhaseComparing, Done: 3, Total: 10, RateDown: 999, Path: "a.txt"},
			"comparing 3 of 10, ↓ 999 B/s: a.txt"},
		{daemon.Progress{Phase: engine.PhaseSyncing, Done: 12, Total: 30, Bytes: 45_000_000, TotalBytes: 120_000_000,
			RateUp: 2_100_000, Path: "docs/a.pdf"}, "syncing 12 of 30, 45.0 MB of 120.0 MB, ↑ 2.1 MB/s: docs/a.pdf"},
		{daemon.Progress{Phase: engine.PhaseSyncing, Done: 1, Total: 2}, "syncing 1 of 2"},
	} {
		if got := Progress(&tc.p); got != tc.want {
			t.Errorf("Progress(%+v) = %q, want %q", tc.p, got, tc.want)
		}
	}
	for n, want := range map[int64]string{0: "0 B", 999: "999 B", 1000: "1.0 kB", 2_100_000: "2.1 MB",
		7_300_000_000: "7.3 GB", 5_000_000_000_000_000: "5000.0 TB"} {
		if got := Size(n); got != want {
			t.Errorf("Size(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestModeArrow(t *testing.T) {
	for m, want := range map[config.Mode]string{config.ModePush: "↑", config.ModePull: "↓", config.ModeSync: "⇅", "": "?"} {
		if got := ModeArrow(m); got != want {
			t.Errorf("ModeArrow(%q) = %q, want %q", m, got, want)
		}
	}
}

func TestCount(t *testing.T) {
	for n, want := range map[int]string{0: "0", 999: "999", 1000: "1,000", 31204: "31,204", 1234567: "1,234,567", -1234: "-1,234"} {
		if got := Count(n); got != want {
			t.Errorf("Count(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestSynced(t *testing.T) {
	for _, tc := range []struct {
		st          daemon.Status
		size, files string
	}{
		{daemon.Status{}, "—", "—"},
		{daemon.Status{LastRun: time.Unix(1, 0)}, "0 B", "0"},
		{daemon.Status{Files: 31204, Size: 48_200_000_000}, "48.2 GB", "31,204"},
	} {
		if size, files := Synced(&tc.st); size != tc.size || files != tc.files {
			t.Errorf("Synced(%d files, %d bytes) = %q, %q; want %q, %q", tc.st.Files, tc.st.Size, size, files, tc.size, tc.files)
		}
	}
}
