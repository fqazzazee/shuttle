// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package config

import (
	"errors"
	"fmt"
	"os"
	"slices"
)

// RowState is a setting's row as it stands for one shuttle, or one
// profile: what it sets itself, what applies where it does not, and
// whether the row applies at all.
type RowState struct {
	Row
	Value     string // the shuttle's (or profile's) own, as typed; "" when it sets none
	Inherited string // what applies when it sets none: a shuttle's profile's, else the default
	From      string // where Inherited comes from: "profile <name>", or "default"
	Off       string // why the row does not apply as things stand; "" when it does
}

// ShuttleRows are the settings of the shuttle name as rows, from the config
// file's data: the same as its editor in the TUI shows.
func ShuttleRows(data []byte, name string) ([]RowState, error) {
	own, base, profile, err := shuttleTables(data, name)
	if err != nil {
		return nil, err
	}
	return tableRows(ownText(own), base, "profile "+profile), nil
}

// ownText is a table's own text for each key, as written.
func ownText(own map[string]any) func(string) string {
	return func(key string) string { return RawText(Lookup(own, key)) }
}

// tableRows are a table's settings as rows: its own text from value, over
// base, which from names; past base, the defaults apply.
func tableRows(value func(string) string, base map[string]any, from string) []RowState {
	effective := effectiveText(value, base)
	rows := make([]RowState, 0, len(Rows))
	for _, r := range Rows {
		st := RowState{Row: r, Value: value(r.Key), Inherited: r.Default, From: "default"}
		if v := RawText(Lookup(base, r.Key)); v != "" {
			st.Inherited, st.From = v, from
		}
		if r.Needs != nil {
			st.Off = r.Needs(effective)
		}
		rows = append(rows, st)
	}
	return rows
}

// SetShuttleRow sets the shuttle name's setting key to text in the config
// file at path, as its row in the TUI would; "" removes it, so its
// profile's value or the default applies again. Only that key is written,
// or its group's inline table for one in versioning, bandwidth or notify:
// the rest of the file stays as written. A row that does not apply as
// things stand is refused, with why. It reports whether the file changed.
// home is for Parse, which checks the result before it is written.
func SetShuttleRow(path, home, name, key, text string) (changed bool, err error) {
	// Another editor saving meanwhile is not an error: read it again.
	for range 3 {
		changed, err = setShuttleRow(path, home, name, key, text)
		if !errors.Is(err, ErrChanged) {
			break
		}
	}
	return changed, err
}

func setShuttleRow(path, home, name, key, text string) (bool, error) {
	return setTableRow(path, home, []string{"shuttles", name}, key, text, func(data []byte) (own, base map[string]any, err error) {
		own, base, _, err = shuttleTables(data, name)
		return own, base, err
	})
}

// setTableRow sets key to text in the config file's table, as its row
// would, given its own settings and those under them (tables).
func setTableRow(path, home string, table []string, key, text string,
	tables func(data []byte) (own, base map[string]any, err error),
) (bool, error) {
	r := RowOf(key)
	if r == nil {
		return false, fmt.Errorf("%w: %s", ErrNoSetting, key)
	}
	if r.Choices != nil && text != "" && !slices.Contains(r.Choices, text) {
		return false, fmt.Errorf("%s: %q is none of %v", r.Label, text, r.Choices)
	}
	before, err := os.ReadFile(path) //nolint:gosec // the user's own config file
	if err != nil {
		return false, err
	}
	own, base, err := tables(before)
	if err != nil {
		return false, err
	}
	value := func(k string) string {
		if k == key {
			return text
		}
		return RawText(Lookup(own, k))
	}
	if value(key) == RawText(Lookup(own, key)) {
		return false, nil
	}
	off := func(k string) string {
		if row := RowOf(k); row != nil && row.Needs != nil {
			return row.Needs(effectiveText(value, base))
		}
		return ""
	}
	if why := off(key); why != "" && text != "" {
		return false, fmt.Errorf("%s does not apply: %s", r.Label, why)
	}
	doc, err := ParseDoc(before)
	if err != nil {
		return false, err
	}
	if err := WriteRow(doc, table, key, value, off); err != nil {
		return false, err
	}
	return true, Save(path, before, doc.Bytes(), home)
}

// shuttleTables are the shuttle name's own table and its profile's, as
// written, and the profile's name; base is nil without a profile.
func shuttleTables(data []byte, name string) (own, base map[string]any, profile string, err error) {
	own, err = RawTable(data, "shuttles", name)
	if err != nil {
		return nil, nil, "", err
	}
	if own == nil {
		return nil, nil, "", fmt.Errorf("%q: %w", name, ErrNoShuttle)
	}
	if p, ok := own["profile"].(string); ok && p != "" {
		profile = p
		if base, err = RawTable(data, "profiles", p); err != nil {
			return nil, nil, "", err
		}
	}
	return own, base, profile, nil
}

// effectiveText is what applies for a key: value's, else base's, else the
// default, as the rows' Needs ask for it.
func effectiveText(value func(string) string, base map[string]any) func(string) string {
	return func(key string) string {
		if v := value(key); v != "" {
			return v
		}
		if v := RawText(Lookup(base, key)); v != "" {
			return v
		}
		return DefaultText(key)
	}
}
