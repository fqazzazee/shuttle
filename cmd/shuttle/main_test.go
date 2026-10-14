// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/fqazzazee/shuttle/internal/bus"
	"github.com/fqazzazee/shuttle/internal/daemon"
	"github.com/fqazzazee/shuttle/internal/dbustest"
	"github.com/fqazzazee/shuttle/internal/notify/notifytest"
)

func TestRun(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{"version", []string{"version"}, 0, "shuttle dev\n", ""},
		{"version flag", []string{"--version"}, 0, "shuttle dev\n", ""},
		{"help", []string{"help"}, 0, "  | o  ==  o |\n  '-[]----[]-'\n\nUsage: shuttle", ""},
		{"no args", nil, 2, "", "Usage: shuttle"},
		{"open, not in a terminal", []string{"open", "conflicts"}, 2, "", "the TUI needs a terminal"},
		{"open, too many", []string{"open", "a", "b"}, 2, "", "wrong arguments"},
		{"unknown", []string{"bogus"}, 2, "", `unknown command "bogus"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if got := run(context.Background(), tt.args, &stdout, &stderr); got != tt.wantCode {
				t.Errorf("exit code = %d, want %d", got, tt.wantCode)
			}
			if !strings.Contains(stdout.String(), tt.wantStdout) {
				t.Errorf("stdout = %q, want it to contain %q", stdout.String(), tt.wantStdout)
			}
			if !strings.Contains(stderr.String(), tt.wantStderr) {
				t.Errorf("stderr = %q, want it to contain %q", stderr.String(), tt.wantStderr)
			}
		})
	}
}

// A config file that does not load stops the daemon with EX_CONFIG: the
// unit does not restart on it. (No config file at all is no shuttles yet:
// see internal/daemon's TestNoConfigYet.)
func TestDaemonWithABrokenConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("XDG_STATE_HOME", dir)
	path := filepath.Join(dir, "shuttle", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("[shuttles.s]\nmode = \"sideways\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run(context.Background(), []string{"daemon"}, &stdout, &stderr); code != exitConfig {
		t.Errorf("exit code = %d, want %d (EX_CONFIG: the unit does not restart on it)", code, exitConfig)
	}
	if !strings.Contains(stderr.String(), path) {
		t.Errorf("stderr = %q, want it to name the config file", stderr.String())
	}
}

// privateSessionBus points DBUS_SESSION_BUS_ADDRESS at a dbus-daemon of
// the test's own, so the daemon under test never claims its name on the
// user's real session bus.
func privateSessionBus(t *testing.T) {
	t.Helper()
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", dbustest.Start(t))
}

// The daemon starts, serves D-Bus, shows notifications, runs until its
// context ends, and exits cleanly.
func TestDaemonRunsUntilStopped(t *testing.T) {
	privateSessionBus(t)
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	notes, err := notifytest.Serve(conn, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("XDG_STATE_HOME", dir)
	local := t.TempDir()
	cfg := "[shares.m]\ntype = \"path\"\npath = \"" + t.TempDir() + "\"\n[shuttles.s]\nmode = \"push\"\nlocal = \"" + local + "\"\nremote = \"m:\"\n"
	if err := os.MkdirAll(dir+"/shuttle", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/shuttle/config.toml", []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stdout, stderr bytes.Buffer
	done := make(chan int, 1)
	go func() { done <- run(ctx, []string{"daemon"}, &stdout, &stderr) }()

	// While it runs, it answers on the bus.
	c, err := bus.Connect()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var shuttles []daemon.Status
	for range 200 {
		if shuttles, err = c.Shuttles(context.Background()); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil || len(shuttles) != 1 || shuttles[0].Name != "s" {
		t.Fatalf("over the bus: %+v, %v", shuttles, err)
	}
	if err := c.SyncNow(context.Background(), "s"); err != nil {
		t.Fatal(err)
	}
	for i := 0; len(notes.Notes()) == 0; i++ {
		if i == 500 {
			t.Fatal("no notification within 5s of the run asked for")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if n := notes.Notes()[0]; n.Summary != "s: synced" {
		t.Errorf("notification = %+v, want the run's completion", n)
	}

	cancel()
	if code := <-done; code != 0 {
		t.Fatalf("exit code = %d; stderr:\n%s", code, stderr.String())
	}
	for _, want := range []string{"daemon started", "serving D-Bus", "daemon stopped"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("log lacks %q:\n%s", want, stderr.String())
		}
	}
}
