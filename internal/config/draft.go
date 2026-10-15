// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"slices"
	"strings"
)

// A Draft is a shuttle as an editor has it before it is saved: its folders,
// its profile, and the settings it has of its own, as typed in their rows.
// Nothing is written until Save; until then Rows and Check read the config
// file with the draft in it, as it would be saved. The phone's wizard keeps
// one; the TUI's keeps its own form.
type Draft struct {
	Name    string
	Profile string // "" for none
	Remote  string // "nas:/Photos"
	Local   string // a full path, or one from ~
	// Include and Exclude are what it selects of its folder, as the config
	// takes them; one or neither.
	Include, Exclude []string
	// Settings are its own, by row key, as typed: a row not here takes its
	// profile's value, or the default.
	Settings map[string]string
	// New is set for a shuttle not in the config file yet: one by its name
	// there already is refused, not edited.
	New bool
}

// DraftOf is the shuttle name as the config file's data has it, to edit.
func DraftOf(data []byte, name string) (*Draft, error) {
	own, err := RawTable(data, "shuttles", name)
	if err != nil {
		return nil, err
	}
	if own == nil {
		return nil, fmt.Errorf("%q: %w", name, ErrNoShuttle)
	}
	d := &Draft{Name: name, Settings: map[string]string{}}
	d.Profile, _ = own["profile"].(string)
	d.Remote, _ = own["remote"].(string)
	d.Local, _ = own["local"].(string)
	d.Include, d.Exclude = strs(own["include"]), strs(own["exclude"])
	for _, r := range Rows {
		if v := RawText(Lookup(own, r.Key)); v != "" {
			d.Settings[r.Key] = v
		}
	}
	return d, nil
}

// CheckName is why the draft's name will not do, or nil: a new shuttle's
// must be a name the config takes, and no other shuttle's.
func (d *Draft) CheckName(data []byte) error {
	if !d.New {
		return nil // an edit keeps its name
	}
	if d.Name == "" {
		return errors.New("the shuttle needs a name")
	}
	if !namePattern.MatchString(d.Name) || strings.Contains(d.Name, "..") {
		return errors.New("a name starts with a letter or digit and uses only letters, digits, '.', '_' and '-'")
	}
	own, err := RawTable(data, "shuttles", d.Name)
	if err != nil {
		return err
	}
	if own != nil {
		return fmt.Errorf("there is a shuttle named %s already", d.Name)
	}
	return nil
}

// Rows are the draft's settings as rows, as ShuttleRows has a saved
// shuttle's, over the config file's data.
func (d *Draft) Rows(data []byte) ([]RowState, error) {
	base, err := d.base(data)
	if err != nil {
		return nil, err
	}
	return tableRows(d.value, base, "profile "+d.Profile), nil
}

// Set sets the draft's setting key to text, as SetShuttleRow does a saved
// shuttle's: "" removes it, so its profile's or the default applies. A
// value its row refuses, or one the config would not load for, is refused,
// saying why, and the draft is as it was. What the shuttle as a whole still
// lacks, such as an interval for a schedule just turned on, is left to
// Check: a draft is set one row at a time.
func (d *Draft) Set(data []byte, home, key, text string) error {
	r := RowOf(key)
	if r == nil {
		return fmt.Errorf("%w: %s", ErrNoSetting, key)
	}
	if r.Choices != nil && text != "" && !slices.Contains(r.Choices, text) {
		return fmt.Errorf("%s: %q is none of %v", r.Label, text, r.Choices)
	}
	base, err := d.base(data)
	if err != nil {
		return err
	}
	was, had := d.Settings[key]
	if d.Settings == nil {
		d.Settings = map[string]string{}
	}
	d.Settings[key] = text
	if text == "" {
		delete(d.Settings, key)
	}
	err = nil
	if r.Needs != nil && text != "" {
		if why := r.Needs(effectiveText(d.value, base)); why != "" {
			err = fmt.Errorf("%s does not apply: %s", r.Label, why)
		}
	}
	if err == nil {
		err = errors.Join(d.about(split(d.Check(data, home)), r)...)
	}
	if err != nil {
		delete(d.Settings, key)
		if had {
			d.Settings[key] = was
		}
	}
	return err
}

// Check is why the config file's data, with the draft in it, would not
// load: two shuttles on one folder, a profile that is not there, a mode
// that is missing. nil when it would.
func (d *Draft) Check(data []byte, home string) error {
	after, err := d.write(data)
	if err != nil {
		return err
	}
	_, err = Parse(after, home)
	return err
}

// Moved reports whether a saved shuttle the draft edits would sync other
// folders than it does: it then starts afresh, as on its first run.
func (d *Draft) Moved(data []byte, home string) (bool, error) {
	if d.New {
		return false, nil
	}
	was, err := Parse(data, home)
	if err != nil {
		return false, err
	}
	after, err := d.write(data)
	if err != nil {
		return false, err
	}
	now, err := Parse(after, home)
	if err != nil {
		return false, err
	}
	a, ok := was.Shuttle(d.Name)
	b, ok2 := now.Shuttle(d.Name)
	if !ok || !ok2 {
		return false, fmt.Errorf("%q: %w", d.Name, ErrNoShuttle)
	}
	return a.Share != b.Share || a.Remote != b.Remote || a.Local != b.Local, nil
}

// Save writes the draft into the config file at path: its table as the
// draft has it, the rest of the file as written, and nothing if the result
// would not load. home is for Parse.
func (d *Draft) Save(path, home string) error {
	var err error
	// Another editor saving meanwhile is not an error: read it again.
	for range 3 {
		var before []byte
		before, err = os.ReadFile(path) //nolint:gosec // the user's own config file
		if errors.Is(err, fs.ErrNotExist) {
			before, err = nil, nil
		}
		if err != nil {
			return err
		}
		var after []byte
		if after, err = d.write(before); err != nil {
			return err
		}
		if err = Save(path, before, after, home); !errors.Is(err, ErrChanged) {
			break
		}
	}
	return err
}

// write is the config file's data with the draft in it. Only the draft's
// keys are written: the rest of its table, such as enabled, stays.
func (d *Draft) write(data []byte) ([]byte, error) {
	if err := d.CheckName(data); err != nil {
		return nil, err
	}
	if !d.New {
		// Removed meanwhile: saving would add it back, half made.
		if own, err := RawTable(data, "shuttles", d.Name); err != nil || own == nil {
			return nil, errors.Join(err, fmt.Errorf("%q: %w", d.Name, ErrNoShuttle))
		}
	}
	doc, err := ParseDoc(data)
	if err != nil {
		return nil, err
	}
	base, err := d.base(data)
	if err != nil {
		return nil, err
	}
	table := []string{"shuttles", d.Name}
	for _, kv := range []struct {
		key string
		v   any
	}{{"profile", d.Profile}, {"local", d.Local}, {"remote", d.Remote}, {"include", d.Include}, {"exclude", d.Exclude}} {
		empty := kv.v == ""
		if l, ok := kv.v.([]string); ok {
			empty = len(l) == 0
		}
		if empty {
			err = doc.Delete(table, kv.key)
		} else {
			err = doc.Set(table, kv.key, kv.v)
		}
		if err != nil {
			return nil, err
		}
	}
	off := func(k string) string {
		if r := RowOf(k); r != nil && r.Needs != nil {
			return r.Needs(effectiveText(d.value, base))
		}
		return ""
	}
	if err := WriteRows(doc, table, d.value, off); err != nil {
		return nil, err
	}
	return doc.Bytes(), nil
}

// value is the draft's own text for key.
func (d *Draft) value(key string) string { return d.Settings[key] }

// base is the draft's profile's table, as written; nil without one, or for
// one the config does not have, which Check says.
func (d *Draft) base(data []byte) (map[string]any, error) {
	if d.Profile == "" {
		return nil, nil
	}
	return RawTable(data, "profiles", d.Profile)
}

// strs is a raw list of strings, as written; paths are taken whole, commas
// and all.
func strs(v any) []string {
	l, _ := v.([]any)
	var out []string
	for _, x := range l {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// decodePos is where a value the config could not read is: the setting
// just made, as the rest of the file read before.
var decodePos = regexp.MustCompile(`^line \d+, column \d+: (toml: )?`)

// about are the problems in errs that are r's own, in its label's words:
// its value would not read, or the config refuses it.
func (d *Draft) about(errs []error, r *Row) []error {
	at := "shuttles." + d.Name + "." + r.Key + ": "
	var out []error
	for _, e := range errs {
		msg := e.Error()
		if loc := decodePos.FindStringIndex(msg); loc != nil {
			out = append(out, fmt.Errorf("%s: %s", r.Label, msg[loc[1]:]))
		} else if rest, ok := strings.CutPrefix(msg, at); ok {
			out = append(out, fmt.Errorf("%s: %s", r.Label, rest))
		}
	}
	return out
}

// split is Parse's error as the problems it joins.
func split(err error) []error {
	if err == nil {
		return nil
	}
	if j, ok := err.(interface{ Unwrap() []error }); ok {
		return j.Unwrap()
	}
	return []error{err}
}
