// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package present

import (
	"strings"
	"testing"
	"time"

	"github.com/fqazzazee/shuttle/internal/config"
	"github.com/fqazzazee/shuttle/internal/daemon"
	"github.com/fqazzazee/shuttle/internal/engine"
	"github.com/fqazzazee/shuttle/internal/fsys"
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

// A shuttle waiting on a metered connection says so, rather than only that a
// run waits.
func TestAttentionMetered(t *testing.T) {
	for _, tc := range []struct {
		st   daemon.Status
		want string
	}{
		{daemon.Status{Pending: true}, "a run is waiting"},
		{daemon.Status{Metered: true, Pending: true}, "a run waits for a connection that is not metered"},
		{daemon.Status{Metered: true}, "paused while the connection is metered"},
	} {
		if got := Attention(&tc.st); got != tc.want {
			t.Errorf("Attention(%+v) = %q, want %q", tc.st, got, tc.want)
		}
	}
}

func TestSelection(t *testing.T) {
	for _, tt := range []struct {
		s    config.Settings
		want string
	}{
		{config.Settings{}, "everything"},
		{config.Settings{Include: []string{"Photos", "Docs/2026"}}, "only Photos, Docs/2026"},
		{config.Settings{Exclude: []string{"Cache"}}, "all but Cache"},
	} {
		if got := Selection(&tt.s); got != tt.want {
			t.Errorf("Selection(%+v) = %q, want %q", tt.s, got, tt.want)
		}
	}
}

func TestRuns(t *testing.T) {
	at := []config.TimeOfDay{{Hour: 2}, {Hour: 14, Minute: 30}}
	for _, tt := range []struct {
		s    config.Settings
		want string
	}{
		{config.Settings{}, "only when you ask"},
		{config.Settings{Triggers: []config.Trigger{config.TriggerRealtime}}, "as files change"},
		{config.Settings{Triggers: []config.Trigger{config.TriggerShareAvailable}}, "when the share comes online"},
		{config.Settings{Triggers: []config.Trigger{config.TriggerSchedule}, Interval: 90 * time.Minute, At: at},
			"every 90m, at 02:00, 14:30"},
		{config.Settings{Triggers: []config.Trigger{config.TriggerRealtime, config.TriggerSchedule}, Interval: 24 * time.Hour},
			"as files change, every 1d"},
	} {
		if got := Runs(&tt.s); got != tt.want {
			t.Errorf("Runs(%+v) = %q, want %q", tt.s, got, tt.want)
		}
	}
}

func TestNextAndLastRun(t *testing.T) {
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	if got := Next(&daemon.Status{NextRun: now.Add(3 * time.Hour)}, now); got != "next in 3h" {
		t.Errorf("Next = %q", got)
	}
	if got := Next(&daemon.Status{State: daemon.StateRunning, NextRun: now.Add(time.Hour)}, now); got != "" {
		t.Errorf("Next while running = %q", got)
	}
	if got := Next(&daemon.Status{}, now); got != "" {
		t.Errorf("Next without a schedule = %q", got)
	}
	for st, want := range map[*daemon.Status]string{
		{Error: "share offline"}: "it failed",
		{}:                       "nothing to do",
		{Copied: 3}:              "3 copied",
		{Copied: 3, Deleted: 1}:  "3 copied, 1 deleted",
		{Deleted: 2}:             "2 deleted",
	} {
		if got := LastRun(st); got != want {
			t.Errorf("LastRun(%+v) = %q, want %q", st, got, want)
		}
	}
}

func TestRoom(t *testing.T) {
	sp := fsys.Space{Free: 50, Total: 1000}
	if !Low(sp) || Low(fsys.Space{Free: 500, Total: 1000}) || Low(fsys.Space{}) {
		t.Error("Low: a tenth left is low; half is not; unknown is not")
	}
	if got := Room(fsys.Space{Free: 1 << 30, Total: 4 << 30}); !strings.Contains(got, " free of ") {
		t.Errorf("Room = %q", got)
	}
	if got := Interval(36 * time.Hour); got != "36h" {
		t.Errorf("Interval = %q", got)
	}
}

func TestWhere(t *testing.T) {
	for sh, want := range map[config.Share]string{
		{Type: config.ShareSMB, Host: "h", Port: 1445, Share: "s", User: "u", Domain: "D"}: "smb://D;u@h:1445/s",
		{Type: config.ShareSMB, Host: "h", Share: "s"}:                                     "smb://h/s",
		{Type: config.ShareNFS3, Host: "h", Export: "/e"}:                                  "nfs://h/e",
		{Type: config.SharePath, Path: "/mnt/usb"}:                                         "/mnt/usb",
	} {
		if got := Where(&sh); got != want {
			t.Errorf("Where(%+v) = %q, want %q", sh, got, want)
		}
	}
}
