// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package config

import (
	"errors"
	"fmt"
)

// ErrNoProfile means the config file has no profile by that name.
var ErrNoProfile = errors.New("no profile by that name")

// ProfileRows are the settings of the profile name as rows, from the config
// file's data: the same as its editor in the TUI shows. Where it sets
// nothing, the default applies.
func ProfileRows(data []byte, name string) ([]RowState, error) {
	own, err := profileTable(data, name)
	if err != nil {
		return nil, err
	}
	return tableRows(own, nil, ""), nil
}

// SetProfileRow sets the profile name's setting key to text in the config
// file at path, as SetShuttleRow does a shuttle's: "" removes it, so the
// default applies again, and only that key is written. Every shuttle on the
// profile follows it, except where one sets that key itself.
func SetProfileRow(path, home, name, key, text string) (changed bool, err error) {
	// Another editor saving meanwhile is not an error: read it again.
	for range 3 {
		changed, err = setTableRow(path, home, []string{"profiles", name}, key, text,
			func(data []byte) (own, base map[string]any, err error) {
				own, err = profileTable(data, name)
				return own, nil, err
			})
		if !errors.Is(err, ErrChanged) {
			break
		}
	}
	return changed, err
}

// profileTable is the profile name's table, as written.
func profileTable(data []byte, name string) (map[string]any, error) {
	own, err := RawTable(data, "profiles", name)
	if err != nil {
		return nil, err
	}
	if own == nil {
		return nil, fmt.Errorf("%q: %w", name, ErrNoProfile)
	}
	return own, nil
}
