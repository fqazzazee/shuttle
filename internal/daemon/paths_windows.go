// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package daemon

import (
	"errors"
	"os"
	"path/filepath"
)

// DefaultPaths are Windows' folders for a user's program:
// %APPDATA%\shuttle\config.toml for the config, %LOCALAPPDATA%\shuttle\state
// for the state, which is this machine's alone.
func DefaultPaths() (Paths, error) {
	cfgHome, err := os.UserConfigDir() // %APPDATA%
	if err != nil {
		return Paths{}, err
	}
	local := os.Getenv("LOCALAPPDATA")
	if local == "" {
		return Paths{}, errors.New("%LOCALAPPDATA% is not set")
	}
	return Paths{
		Config:   filepath.Join(cfgHome, "shuttle", "config.toml"),
		StateDir: filepath.Join(local, "shuttle", "state"),
	}, nil
}
