// SPDX-License-Identifier: GPL-2.0-or-later

// Package terminal opens a command in a new terminal window, for "Open
// shuttle": the one the config names, or the first one installed.
//
// This package is desktop-specific; the sync core must not import it.
package terminal

import (
	"errors"
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

// ErrNone means no terminal was found.
var ErrNone = errors.New("no terminal found: set one in the config file, e.g. [global] terminal = \"ptyxis --new-window --\"")

// Command is the command line that runs args in a new terminal window:
// with configured in front of it if set, else with the first known
// terminal lookPath finds.
func Command(configured []string, lookPath func(string) (string, error), args ...string) ([]string, error) {
	if len(configured) > 0 {
		return append(append([]string{}, configured...), args...), nil
	}
	for _, t := range known {
		if path, err := lookPath(t.name); err == nil {
			return append(append([]string{path}, t.run...), args...), nil
		}
	}
	return nil, ErrNone
}

// Start starts a command line without waiting for it. Under systemd it
// runs in a unit of its own, through systemd-run: restarting the service
// that started it must not close the user's window.
func Start(argv []string, lookPath func(string) (string, error)) error {
	if run, err := lookPath("systemd-run"); err == nil && os.Getenv("INVOCATION_ID") != "" {
		argv = append([]string{run, "--user", "--collect", "--quiet", "--"}, argv...)
	}
	cmd := exec.Command(argv[0], argv[1:]...) //nolint:gosec // a terminal the user chose or has installed
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait() //nolint:errcheck // reaped; how it ended is the terminal's affair
	return nil
}
