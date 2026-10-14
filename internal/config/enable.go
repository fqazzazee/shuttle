// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package config

import (
	"errors"
	"fmt"
	"os"
	"slices"
)

// ErrNoShuttle means the config file has no shuttle by that name.
var ErrNoShuttle = errors.New("no shuttle by that name")

// SetEnabled turns a shuttle on or off in the config file at path, touching
// only its enabled key: off writes enabled = false, on removes the key, as
// shuttles are on unless it says otherwise. A shuttle already as asked is
// left alone. It reports whether the file changed. home is for Parse.
func SetEnabled(path, home, name string, on bool) (changed bool, err error) {
	// Another editor saving meanwhile is not an error: read it again.
	for range 3 {
		changed, err = setEnabled(path, home, name, on)
		if !errors.Is(err, ErrChanged) {
			break
		}
	}
	return changed, err
}

func setEnabled(path, home, name string, on bool) (bool, error) {
	before, err := os.ReadFile(path) //nolint:gosec // the user's own config file
	if err != nil {
		return false, err
	}
	cfg, err := Parse(before, home)
	if err != nil {
		return false, err
	}
	i := slices.IndexFunc(cfg.Shuttles, func(s Shuttle) bool { return s.Name == name })
	if i < 0 {
		return false, fmt.Errorf("%q: %w", name, ErrNoShuttle)
	}
	if cfg.Shuttles[i].Enabled == on {
		return false, nil
	}
	doc, err := ParseDoc(before)
	if err != nil {
		return false, err
	}
	table := []string{"shuttles", name}
	if on {
		err = doc.Delete(table, "enabled")
	} else {
		err = doc.Set(table, "enabled", false)
	}
	if err != nil {
		return false, err
	}
	return true, Save(path, before, doc.Bytes(), home)
}
