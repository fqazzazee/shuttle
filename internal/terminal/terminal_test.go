// SPDX-License-Identifier: GPL-2.0-or-later

package terminal

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// installed is a PATH with only these programs.
func installed(names ...string) func(string) (string, error) {
	return func(name string) (string, error) {
		for _, n := range names {
			if n == name {
				return "/usr/bin/" + name, nil
			}
		}
		return "", errors.New("not found")
	}
}

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
	// The configured command is the config's: never written into, even
	// with room to spare behind it.
	configured := make([]string, 1, 4)
	configured[0] = "term"
	Command(configured, installed(), "a")
	if spare := configured[:2]; spare[1] != "" {
		t.Errorf("the configured command was written into: %q", spare)
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

func waitFor(t *testing.T, path string) string {
	t.Helper()
	for range 200 {
		if b, err := os.ReadFile(path); err == nil && len(b) > 0 {
			return strings.TrimSpace(string(b))
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s never written", path)
	return ""
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
