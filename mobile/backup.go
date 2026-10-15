// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package mobile

import (
	"context"
	"errors"
	"io/fs"
	"maps"
	"os"
	"slices"

	"github.com/fqazzazee/shuttle/internal/config"
	"github.com/fqazzazee/shuttle/internal/shares"
)

// A backup of the app's setup is its config file: the shares, profiles and
// shuttles, as the desktop's TUI also reads it. Passwords are never in it
// (the maintainer's choice): a restored share needs its password again.

// ConfigText is the config file as it is, for a backup; "" when there is
// none yet.
func (m *Daemon) ConfigText() (string, error) {
	data, err := os.ReadFile(m.configPath)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	return string(data), err
}

// backupSummary is what a backup holds, for the app to say before it
// restores it: what it brings, the shares whose password this phone does
// not have, and the shuttles whose folder is not on this phone.
type backupSummary struct {
	Shares, Profiles, Shuttles []string
	NoPassword                 []string
	NoFolder                   []string
}

// CheckBackup reads text as a backup: an error, saying why, when it would
// not load; else what it holds, as JSON (backupSummary).
func (m *Daemon) CheckBackup(text string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	cfg, err := config.Parse([]byte(text), home)
	if err != nil {
		return "", err
	}
	s := backupSummary{Shares: slices.Sorted(maps.Keys(cfg.Shares)), Profiles: cfg.Profiles, Shuttles: []string{}}
	for _, name := range s.Shares {
		sh := cfg.Shares[name]
		if k, ok := shares.Key(&sh); ok {
			if _, err := (store{m.passwords}).Get(context.Background(), k); err != nil {
				s.NoPassword = append(s.NoPassword, name)
			}
		}
	}
	for i := range cfg.Shuttles {
		st := &cfg.Shuttles[i]
		s.Shuttles = append(s.Shuttles, st.Name)
		if info, err := os.Stat(st.Local); err != nil || !info.IsDir() {
			s.NoFolder = append(s.NoFolder, st.Name)
		}
	}
	return toJSON(s), nil
}

// RestoreBackup puts text in place of the config file, if it loads, or
// leaves the file as it was. The daemon follows the file.
func (m *Daemon) RestoreBackup(text string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	return config.Replace(m.configPath, home, []byte(text))
}
