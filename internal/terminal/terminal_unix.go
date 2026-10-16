// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

//go:build !windows

package terminal

import (
	"os"
	"os/exec"
	"syscall"
)

// known are the terminals tried, in order, and the words that make each
// run a command in a new window: xdg-terminal-exec, the user's choice
// where it is set up, then the usual ones, GNOME's first.
var known = []struct {
	name string
	run  []string
}{
	{"xdg-terminal-exec", nil},
	{"ptyxis", []string{"--new-window", "--"}},
	{"kgx", []string{"--"}},
	{"gnome-terminal", []string{"--"}},
	{"konsole", []string{"-e"}},
	{"xfce4-terminal", []string{"-x"}},
	{"foot", nil},
	{"kitty", nil},
	{"alacritty", []string{"-e"}},
	{"xterm", []string{"-e"}},
}

// example is a terminal setting, for ErrNone.
const example = "ptyxis --new-window --"

// ownWindow: a command started here gets no window of its own.
const ownWindow = false

// wrap runs argv in a unit of its own, through systemd-run, when under
// systemd: restarting the service that started it must not close the
// user's window.
func wrap(argv []string, lookPath func(string) (string, error)) []string {
	if run, err := lookPath("systemd-run"); err == nil && os.Getenv("INVOCATION_ID") != "" {
		return append([]string{run, "--user", "--collect", "--quiet", "--"}, argv...)
	}
	return argv
}

// detach starts cmd in a session of its own.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
