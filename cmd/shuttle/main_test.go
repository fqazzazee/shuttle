// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/fqazzazee/shuttle/internal/dbustest"
	"github.com/fqazzazee/shuttle/internal/fsys/fsystest"
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

// isolate points the config and the state at dir, on every OS, so that no
// test reaches the user's own: XDG's folders on Linux, %APPDATA% and
// %LOCALAPPDATA% on Windows; and, on Windows, the daemon's pipe at one of
// the test's, not the user's daemon's.
func isolate(t *testing.T, dir string) {
	t.Helper()
	for _, v := range []string{"XDG_CONFIG_HOME", "XDG_STATE_HOME", "APPDATA", "LOCALAPPDATA"} {
		t.Setenv(v, dir)
	}
	t.Setenv("SHUTTLE_PIPE", `\\.\pipe\`+fsystest.UniqueName("shuttle-test"))
}

// A config file that does not load stops the daemon with EX_CONFIG: the
// unit does not restart on it. (No config file at all is no shuttles yet:
// see internal/daemon's TestNoConfigYet.)
func TestDaemonWithABrokenConfig(t *testing.T) {
	dir := t.TempDir()
	isolate(t, dir)
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
	if runtime.GOOS == "windows" {
		// No bus: a pipe of the test's own, never the user's daemon's.
		t.Setenv("SHUTTLE_PIPE", `\\.\pipe\`+fsystest.UniqueName("shuttle-test"))
		return
	}
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", dbustest.Start(t))
}
