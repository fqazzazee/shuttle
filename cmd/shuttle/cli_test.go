// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fqazzazee/shuttle/internal/daemon"
	"github.com/fqazzazee/shuttle/internal/engine"
)

func TestAgo(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	for d, want := range map[time.Duration]string{
		10 * time.Second: "just now", 5 * time.Minute: "5m ago",
		3 * time.Hour: "3h ago", 50 * time.Hour: "2d ago",
	} {
		if got := ago(now.Add(-d), now); got != want {
			t.Errorf("ago(-%v) = %q, want %q", d, got, want)
		}
	}
	if got := ago(time.Time{}, now); got != "never" {
		t.Errorf("ago(zero) = %q", got)
	}
}

func TestProgressDetails(t *testing.T) {
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
		if got := progress(&tc.p); got != tc.want {
			t.Errorf("progress(%+v) = %q, want %q", tc.p, got, tc.want)
		}
	}
	for n, want := range map[int64]string{0: "0 B", 999: "999 B", 1000: "1.0 kB", 2_100_000: "2.1 MB",
		7_300_000_000: "7.3 GB", 5_000_000_000_000_000: "5000.0 TB"} {
		if got := size(n); got != want {
			t.Errorf("size(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestDetails(t *testing.T) {
	st := &daemon.Status{
		Error:         "share nas: connection refused",
		Conflicts:     []engine.Issue{{Outcome: "held"}, {Outcome: "held"}, {Outcome: "kept both: ..."}},
		HeldDeletions: 1,
		Pending:       true,
		Note:          "polling instead",
	}
	want := "share nas: connection refused; 2 conflicts waiting for you; 1 deletion held by the guard; a run is waiting; polling instead"
	if got := details(st); got != want {
		t.Errorf("details =\n%q\nwant\n%q", got, want)
	}
	if got := details(&daemon.Status{}); got != "" {
		t.Errorf("details of a quiet shuttle = %q", got)
	}
}

// cli runs the shuttle command and returns its exit code and output.
func cli(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

// startDaemon writes a config with two push shuttles, a and b, runs
// `shuttle daemon` on a private session bus, and waits until it answers.
// It returns the folder a pushes to.
func startDaemon(t *testing.T) (localA, mountA string) {
	t.Helper()
	privateSessionBus(t)
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("XDG_STATE_HOME", dir)
	localA, mountA = t.TempDir(), t.TempDir()
	cfg := "[shares.m]\ntype = \"path\"\npath = \"" + mountA + "\"\n" +
		"[shares.n]\ntype = \"path\"\npath = \"" + t.TempDir() + "\"\n" +
		"[shuttles.a]\nmode = \"push\"\nlocal = \"" + localA + "\"\nremote = \"m:\"\n" +
		"[shuttles.b]\nmode = \"push\"\nlocal = \"" + t.TempDir() + "\"\nremote = \"n:\"\n"
	if err := os.MkdirAll(filepath.Join(dir, "shuttle"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "shuttle", "config.toml"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	var stderr bytes.Buffer
	go func() { done <- run(ctx, []string{"daemon"}, &bytes.Buffer{}, &stderr) }()
	t.Cleanup(func() {
		cancel()
		if code := <-done; code != 0 {
			t.Errorf("daemon exit code %d:\n%s", code, stderr.String())
		}
	})
	for range 300 {
		if code, _, _ := cli(t, "status"); code == 0 {
			return localA, mountA
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the daemon never answered:\n%s", stderr.String())
	return "", ""
}

func TestStatusSyncPauseResume(t *testing.T) {
	local, mount := startDaemon(t)
	if err := os.WriteFile(filepath.Join(local, "x.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	code, out, _ := cli(t, "status")
	if code != 0 || !strings.HasPrefix(out, "NAME") || !strings.Contains(out, "a     idle") || !strings.Contains(out, "never") {
		t.Errorf("status (%d):\n%s", code, out)
	}

	if code, out, _ := cli(t, "sync", "a"); code != 0 || out != "syncing a\n" {
		t.Errorf("sync a = %d %q", code, out)
	}
	for range 300 {
		if _, err := os.Stat(filepath.Join(mount, "x.txt")); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := os.Stat(filepath.Join(mount, "x.txt")); err != nil {
		t.Fatal("sync a did not push")
	}
	time.Sleep(50 * time.Millisecond)
	if _, out, _ := cli(t, "status"); !strings.Contains(out, "just now") {
		t.Errorf("status after a sync:\n%s", out)
	}

	if code, out, _ := cli(t, "pause"); code != 0 || out != "paused all shuttles\n" {
		t.Errorf("pause = %d %q", code, out)
	}
	if _, out, _ := cli(t, "status"); !strings.HasPrefix(out, "All shuttles are paused") || !strings.Contains(out, "paused") {
		t.Errorf("status while paused:\n%s", out)
	}
	if code, out, _ := cli(t, "resume"); code != 0 || out != "resumed all shuttles\n" {
		t.Errorf("resume = %d %q", code, out)
	}
	if code, out, _ := cli(t, "pause", "b"); code != 0 || out != "paused b\n" {
		t.Errorf("pause b = %d %q", code, out)
	}
	if _, out, _ := cli(t, "status"); !strings.Contains(out, "b     paused") || strings.Contains(out, "All shuttles") {
		t.Errorf("status with b paused:\n%s", out)
	}
}

func TestCLIErrors(t *testing.T) {
	startDaemon(t)
	for _, tt := range []struct {
		args []string
		code int
		msg  string
	}{
		{[]string{"sync", "nope"}, 1, "no shuttle by that name"},
		{[]string{"sync"}, 2, "wrong arguments"},
		{[]string{"status", "extra"}, 2, "wrong arguments"},
		{[]string{"pause", "a", "b"}, 2, "wrong arguments"},
		{[]string{"disable", "nope"}, 1, "no shuttle by that name"},
		{[]string{"enable"}, 2, "wrong arguments"},
		{[]string{"activity", "nope"}, 1, "no shuttle by that name"},
		{[]string{"activity", "a", "b"}, 2, "wrong arguments"},
	} {
		code, _, stderr := cli(t, tt.args...)
		if code != tt.code || !strings.Contains(stderr, tt.msg) {
			t.Errorf("shuttle %v = %d, %q; want %d and %q", tt.args, code, stderr, tt.code, tt.msg)
		}
	}
}

func TestCLIWithoutADaemon(t *testing.T) {
	privateSessionBus(t)
	code, _, stderr := cli(t, "status")
	if code != 1 || !strings.Contains(stderr, "not running") || !strings.Contains(stderr, "systemctl --user start shuttle") {
		t.Errorf("status without a daemon = %d, %q", code, stderr)
	}
}

func TestEnableDisable(t *testing.T) {
	startDaemon(t)
	if code, out, stderr := cli(t, "disable", "b"); code != 0 || out != "disabled b\n" {
		t.Errorf("disable b = %d %q %q", code, out, stderr)
	}
	waitForStatus(t, "b     disabled")
	if code, out, stderr := cli(t, "enable", "b"); code != 0 || out != "enabled b\n" {
		t.Errorf("enable b = %d %q %q", code, out, stderr)
	}
	waitForStatus(t, "b     idle")
}

// Without a daemon, enable and disable edit the config file themselves.
func TestEnableDisableWithoutADaemon(t *testing.T) {
	privateSessionBus(t)
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	path := filepath.Join(dir, "shuttle", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	doc := "[shares.m]\ntype = \"path\"\npath = \"/mnt\"\n[shuttles.a]\nmode = \"push\"\nlocal = \"/l\"\nremote = \"m:\"\n"
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, stderr := cli(t, "disable", "a")
	if code != 0 || !strings.HasPrefix(out, "disabled a (the daemon is not running") {
		t.Errorf("disable a = %d %q %q", code, out, stderr)
	}
	if b, _ := os.ReadFile(path); string(b) != doc+"enabled = false\n" {
		t.Errorf("config after disabling:\n%s", b)
	}
	if code, _, _ := cli(t, "enable", "a"); code != 0 {
		t.Errorf("enable a = %d", code)
	}
	if b, _ := os.ReadFile(path); string(b) != doc {
		t.Errorf("config after enabling:\n%s", b)
	}
	if code, _, stderr := cli(t, "enable", "nope"); code != 1 || !strings.Contains(stderr, "no shuttle by that name") {
		t.Errorf("enable nope = %d %q", code, stderr)
	}

	// No session bus at all, as over ssh.
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path="+filepath.Join(dir, "no-bus"))
	if code, out, stderr := cli(t, "disable", "a"); code != 0 || !strings.HasPrefix(out, "disabled a (") {
		t.Errorf("disable a without a bus = %d %q %q", code, out, stderr)
	}
}

func waitForStatus(t *testing.T, want string) {
	t.Helper()
	var out string
	for range 300 {
		if _, out, _ = cli(t, "status"); strings.Contains(out, want) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Errorf("status never showed %q:\n%s", want, out)
}

func TestActivity(t *testing.T) {
	local, mount := startDaemon(t)
	if code, out, _ := cli(t, "activity"); code != 0 || !strings.HasPrefix(out, "Nothing yet") {
		t.Errorf("activity before any run = %d %q", code, out)
	}
	if err := os.WriteFile(filepath.Join(local, "x.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cli(t, "sync", "a")
	for range 300 {
		if _, out, _ := cli(t, "activity", "a"); strings.Contains(out, "1 copied") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	code, out, _ := cli(t, "activity", "a")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if code != 0 || len(lines) != 2 || !strings.HasSuffix(lines[0], "a  x.txt: copied to the share") ||
		!strings.HasSuffix(lines[1], "a  1 copied") {
		t.Errorf("activity a = %d:\n%s", code, out)
	}
	if _, err := os.Stat(filepath.Join(mount, "x.txt")); err != nil {
		t.Error(err)
	}
	if _, all, _ := cli(t, "activity"); all != out {
		t.Errorf("every shuttle's activity:\n%s\nwant a's alone:\n%s", all, out)
	}
}
