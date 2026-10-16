// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

//go:build !windows

package daemon

import (
	"cmp"
	"os"
	"path/filepath"
)

// DefaultPaths follows the XDG base directories:
// $XDG_CONFIG_HOME/shuttle/config.toml and $XDG_STATE_HOME/shuttle.
func DefaultPaths() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, err
	}
	cfgHome := cmp.Or(os.Getenv("XDG_CONFIG_HOME"), filepath.Join(home, ".config"))
	stateHome := cmp.Or(os.Getenv("XDG_STATE_HOME"), filepath.Join(home, ".local", "state"))
	return Paths{
		Config:   filepath.Join(cfgHome, "shuttle", "config.toml"),
		StateDir: filepath.Join(stateHome, "shuttle"),
	}, nil
}
