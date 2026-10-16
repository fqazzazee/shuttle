// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

// Package terminal opens a command in a new terminal window, for "Open
// shuttle": the one the config names, or the first one installed.
//
// This package is desktop-specific; the sync core must not import it.
package terminal

import (
	"errors"
	"os/exec"
)

// ErrNone means no terminal was found.
var ErrNone = errors.New("no terminal found: set one in the config file, e.g. [global] terminal = \"" + example + "\"")

// Command is the command line that runs args in a new terminal window:
// with configured in front of it if set, else with the first known
// terminal lookPath finds, else, where the OS gives every command a window
// of its own, args alone.
func Command(configured []string, lookPath func(string) (string, error), args ...string) ([]string, error) {
	if len(configured) > 0 {
		return append(append([]string{}, configured...), args...), nil
	}
	for _, t := range known {
		if path, err := lookPath(t.name); err == nil {
			return append(append([]string{path}, t.run...), args...), nil
		}
	}
	if ownWindow {
		return append([]string{}, args...), nil
	}
	return nil, ErrNone
}

// Start starts a command line without waiting for it, apart from whatever
// started it (see wrap and detach).
func Start(argv []string, lookPath func(string) (string, error)) error {
	argv = wrap(argv, lookPath)
	cmd := exec.Command(argv[0], argv[1:]...) //nolint:gosec // a terminal the user chose or has installed
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait() //nolint:errcheck // reaped; how it ended is the terminal's affair
	return nil
}
