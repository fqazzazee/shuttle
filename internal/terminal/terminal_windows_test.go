// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package terminal

import (
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
		{nil, []string{"wt"}, []string{"/usr/bin/wt", "-w", "new", "new-tab", "--", "shuttle", "open"}},
		// Without Windows Terminal, the command opens its own console.
		{nil, nil, []string{"shuttle", "open"}},
		{[]string{"alacritty", "-e"}, []string{"wt"}, []string{"alacritty", "-e", "shuttle", "open"}},
	} {
		got, err := Command(tc.configured, installed(tc.installed...), "shuttle", "open")
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("Command(%q) with %q = %q, %v; want %q", tc.configured, tc.installed, got, err, tc.want)
		}
	}
}

// The command starts in a console of its own, so it outlives its starter.
func TestStart(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "ran")
	script := filepath.Join(dir, "term.cmd")
	if err := os.WriteFile(script, []byte("@echo %* >\""+out+"\"\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Start([]string{"cmd.exe", "/c", script, "shuttle", "open"}, installed()); err != nil {
		t.Fatal(err)
	}
	if got := waitFor(t, out); got != "shuttle open" {
		t.Errorf("ran with %q", got)
	}
	if err := Start([]string{filepath.Join(dir, "none.exe")}, installed()); err == nil {
		t.Error("a missing program started")
	}
}
