// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const rowsConfig = minimalShare + `
[profiles.live]
mode = "sync"
metered = "run"

[shuttles.docs] # the documents
profile = "live"
local = "/d"
remote = "nas:d"
deletion_guard = 20
# what to leave alone, by hand
ignore = [
    "*.tmp",
    "build/",
]
`

func TestShuttleRows(t *testing.T) {
	rows, err := ShuttleRows([]byte(rowsConfig), "docs")
	if err != nil {
		t.Fatal(err)
	}
	get := func(key string) RowState {
		for _, r := range rows {
			if r.Key == key {
				return r
			}
		}
		t.Fatalf("no row %s", key)
		return RowState{}
	}
	if r := get("metered"); r.Value != "" || r.Inherited != "run" || r.From != "profile live" || r.Off != "" {
		t.Errorf("metered: %+v; want run from profile live", r)
	}
	if r := get("deletion_guard"); r.Value != "20" {
		t.Errorf("deletion_guard: %+v; want the shuttle's own 20", r)
	}
	if r := get("conflicts"); r.Inherited != "keep-both" || r.From != "default" {
		t.Errorf("conflicts: %+v; want the default", r)
	}
	if r := get("deletions"); r.Off == "" {
		t.Errorf("deletions: %+v; does not apply to a sync shuttle", r)
	}
	if r := get("ignore"); r.Value != "*.tmp, build/" {
		t.Errorf("ignore: %+v", r)
	}
	if _, err := ShuttleRows([]byte(rowsConfig), "nope"); !errors.Is(err, ErrNoShuttle) {
		t.Errorf("an unknown shuttle: %v", err)
	}
}

// One setting changes, and nothing else in the file: comments, and a list
// written over several lines, stay as they were.
func TestSetShuttleRow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(rowsConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	read := func() string {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	set := func(key, text string) (bool, error) { return SetShuttleRow(path, "/home/user", "docs", key, text) }

	if changed, err := set("metered", "pause"); err != nil || !changed {
		t.Fatalf("set metered: %v, %v", changed, err)
	}
	got := read()
	if !strings.Contains(got, `metered = "pause"`) {
		t.Errorf("metered not set:\n%s", got)
	}
	if !strings.Contains(got, "ignore = [\n    \"*.tmp\",\n    \"build/\",\n]") ||
		!strings.Contains(got, "[shuttles.docs] # the documents") || !strings.Contains(got, "# what to leave alone, by hand") {
		t.Errorf("the rest of the file changed:\n%s", got)
	}
	if changed, err := set("metered", "pause"); err != nil || changed {
		t.Errorf("setting it again: %v, %v; want nothing to change", changed, err)
	}
	if _, err := set("metered", ""); err != nil || strings.Contains(read(), `metered = "pause"`) {
		t.Errorf("reset: %v\n%s", err, read())
	}
	if _, err := set("versioning.kind", "simple"); err != nil {
		t.Fatal(err)
	}
	if _, err := set("versioning.count", "3"); err != nil || !strings.Contains(read(), `versioning = { kind = "simple", count = 3 }`) {
		t.Errorf("a key of a group: %v\n%s", err, read())
	}

	before := read()
	for _, tc := range []struct{ key, text, want string }{
		{"nothing", "x", "no such setting"},
		{"metered", "sometimes", "is none of"},
		{"deletions", "mirror", "does not apply"},
		{"poll_interval", "1m", "does not apply"},
		{"bandwidth.up", "fast", `invalid rate "fast"`}, // applies, and the config refuses it
	} {
		if _, err := set(tc.key, tc.text); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s = %q: %v; want %q", tc.key, tc.text, err, tc.want)
		}
	}
	if read() != before {
		t.Errorf("a refused setting changed the file:\n%s", read())
	}
	if _, err := SetShuttleRow(path, "/home/user", "nope", "metered", "run"); !errors.Is(err, ErrNoShuttle) {
		t.Errorf("an unknown shuttle: %v", err)
	}
}
