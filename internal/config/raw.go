// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package config

import (
	"github.com/pelletier/go-toml/v2"
)

// RawTable returns a table of the config file as written, e.g. a profile's
// settings with "30d" still "30d", for an editor to show: strings,
// int64s, bools, []any, and map[string]any for tables within. It is nil if
// there is no such table.
func RawTable(data []byte, path ...string) (map[string]any, error) {
	var all map[string]any
	if err := toml.Unmarshal(data, &all); err != nil {
		return nil, err
	}
	t := all
	for _, key := range path {
		next, ok := t[key].(map[string]any)
		if !ok {
			return nil, nil
		}
		t = next
	}
	return t, nil
}
