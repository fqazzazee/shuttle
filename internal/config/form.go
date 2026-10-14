// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package config

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// Row is one setting as a settings form has it, in the TUI and in GNOME's
// preferences: a config key, "group.key" for one inside versioning,
// bandwidth or notify, with what it is called and what it takes. One table
// for both, so they cannot drift apart.
type Row struct {
	Key, Label string
	Choices    []string // a choice among these; nil: typed text
	List       bool     // typed as a comma-separated list
	Number     bool
	Default    string // what applies when it is not set, as said to the user
	Hint       string
	// Needs says why the setting does not apply, given the effective value
	// of the others; "" when it does. nil: it always applies.
	Needs func(get func(key string) string) string
}

// withTrigger: the setting applies only with that trigger.
func withTrigger(trigger string) func(func(string) string) string {
	return func(get func(string) string) string {
		if slices.Contains(ListItems(get("triggers")), trigger) {
			return ""
		}
		return "only with the " + trigger + " trigger"
	}
}

// whenIs: the setting applies only while key is one of values. An unknown
// value (no mode anywhere yet) leaves it applying: better than guessing.
func whenIs(key, what string, values ...string) func(func(string) string) string {
	return func(get func(string) string) string {
		if v := get(key); v == "" || slices.Contains(values, v) {
			return ""
		}
		return "only " + what
	}
}

// ListItems splits a list as typed: "realtime, schedule"; "none" is empty.
func ListItems(text string) []string {
	var items []string
	for _, x := range strings.Split(text, ",") {
		if x = strings.TrimSpace(x); x != "" && x != "none" {
			items = append(items, x)
		}
	}
	return items
}

// defaults are the built-in values the dependencies between settings look
// at, as typed; config.Defaults is where they come from.
var defaults = func() map[string]string {
	d := Defaults()
	return map[string]string{
		"conflicts": string(d.Conflicts), "deletions": string(d.Deletions),
		"versioning.kind": string(d.Versioning.Kind),
	}
}()

// DefaultText is the built-in value of key, as typed, for the settings the
// others depend on; "" for the rest.
func DefaultText(key string) string { return defaults[key] }

var Rows = []Row{
	{Key: "mode", Label: "Mode", Choices: []string{"push", "pull", "sync"}, Default: "none: each shuttle must say",
		Hint: "↑ push: this computer to the share; ↓ pull: the share to this computer; ⇅ sync: both ways"},
	{Key: "triggers", Label: "Triggers", List: true, Default: "manual only",
		Hint: "any of realtime, schedule, share-available"},
	{Key: "interval", Label: "Every", Default: "-", Hint: "with the schedule trigger: run this often, e.g. 6h",
		Needs: withTrigger("schedule")},
	{Key: "at", Label: "At", List: true, Default: "-", Hint: "with the schedule trigger: times of day, e.g. 02:00, 14:30",
		Needs: withTrigger("schedule")},
	{Key: "poll_interval", Label: "Poll every", Default: "60s", Hint: "with the realtime trigger: how often the share is checked for changes",
		Needs: withTrigger("realtime")},
	{Key: "conflicts", Label: "Conflicts", Choices: []string{"keep-both", "newest-wins", "ask"}, Default: "keep-both",
		Hint: "a file changed on both sides: keep both, take the newer, or ask you"},
	{Key: "conflict_hold", Label: "Ask holds", Choices: []string{"file", "shuttle"}, Default: "file",
		Hint: "with ask: wait on the file alone, or on the whole shuttle", Needs: whenIs("conflicts", "when conflicts are ask", "ask")},
	{Key: "deletions", Label: "Deletions", Choices: []string{"additive", "mirror"}, Default: "additive",
		Hint:  "push and pull: additive never deletes on the other side; mirror deletes there too",
		Needs: whenIs("mode", "for push and pull: sync always deletes on both sides", "push", "pull")},
	{Key: "deletion_guard", Label: "Guard", Number: true, Default: "50",
		Hint: "hold deletions of more than this percent of the files at once; 0 is off",
		Needs: func(get func(string) string) string {
			if get("mode") == "sync" || get("mode") == "" || get("deletions") == "mirror" {
				return ""
			}
			return "only when something is deleted: sync, or deletions mirror"
		}},
	{Key: "versioning.kind", Label: "Versions", Choices: []string{"none", "trash", "simple", "staggered"}, Default: "trash",
		Hint: "what replaced and deleted files leave behind: none, all for a time, the last few, or thinned out"},
	{Key: "versioning.keep", Label: "Keep for", Default: "30d", Hint: "trash and staggered: how long, e.g. 30d",
		Needs: whenIs("versioning.kind", "with trash or staggered versions", "trash", "staggered")},
	{Key: "versioning.count", Label: "Keep last", Number: true, Default: "-", Hint: "simple: how many versions",
		Needs: whenIs("versioning.kind", "with simple versions", "simple")},
	{Key: "versioning.size_cap", Label: "Size cap", Default: "none", Hint: "the most the versions may take, e.g. 10GB",
		Needs: whenIs("versioning.kind", "while versions are kept", "trash", "simple", "staggered")},
	{Key: "ignore", Label: "Ignore", List: true, Default: "nothing", Hint: "patterns, e.g. *.tmp, node_modules/"},
	{Key: "metered", Label: "Metered", Choices: []string{"pause", "run"}, Default: "pause",
		Hint: "on a metered connection, such as a phone's hotspot: wait for another, or run anyway"},
	{Key: "bandwidth.up", Label: "Upload", Default: "unlimited", Hint: "e.g. 20MB/s"},
	{Key: "bandwidth.down", Label: "Download", Default: "unlimited", Hint: "e.g. 50MB/s"},
	{Key: "notify.errors", Label: "Tell errors", Choices: []string{"on", "off"}, Default: "on", Hint: "desktop notifications"},
	{Key: "notify.conflicts", Label: "Tell conflicts", Choices: []string{"on", "off"}, Default: "on", Hint: "desktop notifications"},
	{Key: "notify.completed", Label: "Tell done", Choices: []string{"on", "off"}, Default: "on, off for real-time",
		Hint: "desktop notifications: a run that changed something"},
	{Key: "notify.share", Label: "Tell share", Choices: []string{"on", "off"}, Default: "on",
		Hint: "desktop notifications: a share lost or back"},
}

// RowOf is the row for key; nil for none.
func RowOf(key string) *Row {
	i := slices.IndexFunc(Rows, func(r Row) bool { return r.Key == key })
	if i < 0 {
		return nil
	}
	return &Rows[i]
}

// Lookup finds "group.key" in a raw table.
func Lookup(raw map[string]any, key string) any {
	group, sub, ok := strings.Cut(key, ".")
	if !ok {
		return raw[key]
	}
	g, _ := raw[group].(map[string]any)
	return g[sub]
}

// RawText is a raw value as typed: lists joined with commas ("none" for an
// empty one), bools as on and off.
func RawText(v any) string {
	switch v := v.(type) {
	case nil:
		return ""
	case bool:
		return map[bool]string{true: "on", false: "off"}[v]
	case []any:
		if len(v) == 0 {
			return "none"
		}
		parts := make([]string, 0, len(v))
		for _, x := range v {
			parts = append(parts, fmt.Sprint(x))
		}
		return strings.Join(parts, ", ")
	}
	return fmt.Sprint(v)
}

// WriteRows writes settings into table, the text of each from value: each
// one set is written, each one not set, or off (greyed out: off says why),
// is removed; versioning, bandwidth and notify as one inline table each.
func WriteRows(d *Doc, table []string, value, off func(key string) string) error {
	// The plain settings first, then each group once, in that order: a new
	// table reads as it always has.
	var groups []string
	for _, s := range Rows {
		if group, _, inGroup := strings.Cut(s.Key, "."); inGroup {
			if !slices.Contains(groups, group) {
				groups = append(groups, group)
			}
			continue
		}
		if err := WriteRow(d, table, s.Key, value, off); err != nil {
			return err
		}
	}
	for _, g := range groups {
		if err := WriteRow(d, table, g+".", value, off); err != nil {
			return err
		}
	}
	return nil
}

// WriteRow writes the one setting key into table as WriteRows does, and
// leaves the rest of the table as written. A key in versioning, bandwidth
// or notify writes that group's inline table again, from value and off for
// each of its keys: the group is one TOML value.
func WriteRow(d *Doc, table []string, key string, value, off func(key string) string) error {
	group, _, inGroup := strings.Cut(key, ".")
	if !inGroup {
		r := RowOf(key)
		if r == nil {
			return fmt.Errorf("%w: %s", ErrNoSetting, key)
		}
		text := value(key)
		if off(key) != "" {
			text = ""
		}
		if text == "" {
			return d.Delete(table, key)
		}
		v, err := typed(r, text)
		if err != nil {
			return err
		}
		return d.Set(table, key, v)
	}
	var fields Inline
	for i := range Rows {
		r := &Rows[i]
		g, sub, ok := strings.Cut(r.Key, ".")
		if !ok || g != group {
			continue
		}
		text := value(r.Key)
		if text == "" || off(r.Key) != "" {
			continue
		}
		v, err := typed(r, text)
		if err != nil {
			return err
		}
		fields = append(fields, Field{Key: sub, Value: v})
	}
	if fields == nil {
		return d.Delete(table, group)
	}
	return d.Set(table, group, fields)
}

// ErrNoSetting means a key is none of the settings' rows.
var ErrNoSetting = errors.New("no such setting")

// typed is text, as typed in r's row, as the TOML value it is written as:
// a list of strings, a number, a bool for on and off, or the text itself.
func typed(r *Row, text string) (any, error) {
	switch {
	case r.List:
		items := []string{} // "none" is an empty list: no triggers, nothing ignored
		if text == "none" {
			return items, nil
		}
		for _, x := range strings.Split(text, ",") {
			if x = strings.TrimSpace(x); x != "" {
				items = append(items, x)
			}
		}
		return items, nil
	case r.Number:
		n, err := strconv.Atoi(text)
		if err != nil {
			return nil, fmt.Errorf("%s must be a number, not %q", strings.ToLower(r.Label), text)
		}
		return n, nil
	case r.Choices != nil && (text == "on" || text == "off"):
		return text == "on", nil
	}
	return text, nil
}
