// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

//go:build !windows

package terminal

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestCommand(t *testing.T) {
	for _, tc := range []struct {
		configured []string
		installed  []string
		want       []string
	}{
		{nil, []string{"xterm", "ptyxis"}, []string{"/usr/bin/ptyxis", "--new-window", "--", "shuttle", "open"}},
		{nil, []string{"xterm", "xdg-terminal-exec"}, []string{"/usr/bin/xdg-terminal-exec", "shuttle", "open"}},
		{nil, []string{"konsole"}, []string{"/usr/bin/konsole", "-e", "shuttle", "open"}},
		{[]string{"foot", "--app-id", "shuttle"}, []string{"ptyxis"}, []string{"foot", "--app-id", "shuttle", "shuttle", "open"}},
	} {
		got, err := Command(tc.configured, installed(tc.installed...), "shuttle", "open")
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("Command(%q) with %q = %q, %v; want %q", tc.configured, tc.installed, got, err, tc.want)
		}
	}
	if _, err := Command(nil, installed(), "shuttle"); !errors.Is(err, ErrNone) {
		t.Errorf("nothing installed: %v", err)
	}
}

// script writes a shell script that records its arguments in out.
func script(t *testing.T, dir, name, out string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\necho \"$@\" >"+out+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestStart(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "ran")
	term := script(t, dir, "term", out)
	t.Setenv("INVOCATION_ID", "")
	if err := Start([]string{term, "shuttle", "open"}, installed("systemd-run")); err != nil {
		t.Fatal(err)
	}
	if got := waitFor(t, out); got != "shuttle open" {
		t.Errorf("ran with %q", got)
	}
}

// Started by the service, it goes through systemd-run.
func TestStartUnderSystemd(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "ran")
	run := script(t, dir, "systemd-run", out)
	t.Setenv("INVOCATION_ID", "0123")
	lookPath := func(name string) (string, error) {
		if name == "systemd-run" {
			return run, nil
		}
		return "", errors.New("not found")
	}
	if err := Start([]string{"/usr/bin/ptyxis", "--new-window", "--", "shuttle", "open"}, lookPath); err != nil {
		t.Fatal(err)
	}
	if got := waitFor(t, out); got != "--user --collect --quiet -- /usr/bin/ptyxis --new-window -- shuttle open" {
		t.Errorf("systemd-run got %q", got)
	}
	if err := Start([]string{filepath.Join(dir, "none")}, installed()); err == nil {
		t.Error("a missing program started")
	}
}
