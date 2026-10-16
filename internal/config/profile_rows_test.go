// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProfileRows(t *testing.T) {
	rows, err := ProfileRows([]byte(rowsConfig), "live")
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
	if r := get("metered"); r.Value != "run" {
		t.Errorf("metered: %+v; want the profile's own run", r)
	}
	if r := get("conflicts"); r.Value != "" || r.Inherited != "keep-both" || r.From != "default" {
		t.Errorf("conflicts: %+v; want the default", r)
	}
	if r := get("deletions"); r.Off == "" {
		t.Errorf("deletions: %+v; does not apply to a sync profile", r)
	}
	if _, err := ProfileRows([]byte(rowsConfig), "nope"); !errors.Is(err, ErrNoProfile) {
		t.Errorf("an unknown profile: %v", err)
	}
}

// One setting of the profile changes, and nothing else in the file; its
// shuttles follow it.
func TestSetProfileRow(t *testing.T) {
	unixPaths(t)
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
	set := func(key, text string) (bool, error) { return SetProfileRow(path, "/home/user", "live", key, text) }

	if changed, err := set("conflicts", "newest-wins"); err != nil || !changed {
		t.Fatalf("set conflicts: %v, %v", changed, err)
	}
	got := read()
	if !strings.Contains(got, "[profiles.live]\nmode = \"sync\"\nmetered = \"run\"\nconflicts = \"newest-wins\"\n") {
		t.Errorf("conflicts not set in the profile:\n%s", got)
	}
	if !strings.Contains(got, "[shuttles.docs] # the documents") || !strings.Contains(got, "# what to leave alone, by hand") {
		t.Errorf("the rest of the file changed:\n%s", got)
	}
	rows, err := ShuttleRows([]byte(got), "docs")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.Key == "conflicts" && (r.Inherited != "newest-wins" || r.From != "profile live") {
			t.Errorf("the shuttle's conflicts: %+v; want newest-wins from profile live", r)
		}
	}
	if changed, err := set("conflicts", "newest-wins"); err != nil || changed {
		t.Errorf("setting it again: %v, %v; want nothing to change", changed, err)
	}
	if _, err := set("conflicts", ""); err != nil || strings.Contains(read(), "conflicts") {
		t.Errorf("reset: %v\n%s", err, read())
	}

	before := read()
	for _, tc := range []struct{ key, text, want string }{
		{"nothing", "x", "no such setting"},
		{"metered", "sometimes", "is none of"},
		{"deletions", "mirror", "does not apply"},
		{"mode", "", "mode is not set"}, // docs has none of its own: the config refuses it
	} {
		if _, err := set(tc.key, tc.text); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s = %q: %v; want %q", tc.key, tc.text, err, tc.want)
		}
	}
	if read() != before {
		t.Errorf("a refused setting changed the file:\n%s", read())
	}
	if _, err := SetProfileRow(path, "/home/user", "nope", "metered", "run"); !errors.Is(err, ErrNoProfile) {
		t.Errorf("an unknown profile: %v", err)
	}
}
