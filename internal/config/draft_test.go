// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDraftName(t *testing.T) {
	for name, want := range map[string]string{
		"":       "needs a name",
		"-x":     "starts with a letter or digit",
		"a..b":   "starts with a letter or digit",
		"docs":   "there is a shuttle named docs already",
		"photos": "",
	} {
		err := (&Draft{Name: name, New: true}).CheckName([]byte(rowsConfig))
		if want == "" && err != nil || want != "" && (err == nil || !strings.Contains(err.Error(), want)) {
			t.Errorf("%q: %v; want %q", name, err, want)
		}
	}
	if err := (&Draft{Name: "docs"}).CheckName([]byte(rowsConfig)); err != nil {
		t.Errorf("an edit keeps its name: %v", err)
	}
}

// A new shuttle's settings, set one at a time before it is saved, as a saved
// one's are.
func TestDraftSet(t *testing.T) {
	data := []byte(rowsConfig)
	d := &Draft{Name: "photos", Profile: "live", Remote: "nas:/photos", Local: "/p", New: true}
	if err := d.Set(data, "/home/user", "conflicts", "ask"); err != nil {
		t.Fatal(err)
	}
	rows, err := d.Rows(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		switch r.Key {
		case "conflicts":
			if r.Value != "ask" {
				t.Errorf("conflicts: %+v; want its own ask", r)
			}
		case "metered":
			if r.Value != "" || r.Inherited != "run" || r.From != "profile live" {
				t.Errorf("metered: %+v; want run from profile live", r)
			}
		case "conflict_hold":
			if r.Off != "" {
				t.Errorf("conflict_hold: %+v; applies with ask", r)
			}
		}
	}

	for key, text := range map[string]string{
		"conflicts": "maybe",  // none of its choices
		"deletions": "mirror", // the profile syncs: it does not apply
		"interval":  "soon",   // does not apply without the schedule trigger
		"nope":      "x",
	} {
		if err := d.Set(data, "/home/user", key, text); err == nil {
			t.Errorf("%s = %q taken", key, text)
		}
	}
	// A schedule needs an interval or times, set next: not refused for it.
	if err := d.Set(data, "/home/user", "triggers", "schedule"); err != nil {
		t.Fatal(err)
	}
	if err := d.Check(data, "/home/user"); err == nil || !strings.Contains(err.Error(), "needs interval or at") {
		t.Errorf("a schedule without an interval: %v", err)
	}
	// What the config would not take is refused in the row's words, and the
	// draft is as it was.
	for text, why := range map[string]string{
		"soon": `Every: invalid duration "soon"`,
		"30s":  "Every: must be at least 1m",
	} {
		if err := d.Set(data, "/home/user", "interval", text); err == nil || !strings.HasPrefix(err.Error(), why) {
			t.Errorf("interval %q: %v; want %q", text, err, why)
		}
	}
	if err := d.Set(data, "/home/user", "interval", "6h"); err != nil {
		t.Fatal(err)
	}
	if err := d.Set(data, "/home/user", "interval", "soon"); err == nil || d.Settings["interval"] != "6h" {
		t.Errorf("a bad interval over a good one: %v, %q", err, d.Settings["interval"])
	}
	if err := d.Check(data, "/home/user"); err != nil {
		t.Error(err)
	}
	if err := d.Set(data, "/home/user", "versioning.kind", "simple"); err != nil {
		t.Fatal(err)
	}
	if err := d.Set(data, "/home/user", "versioning.count", "0"); err == nil || err.Error() != "Keep last: must be at least 1" {
		t.Errorf("versioning.count 0: %v", err)
	}
	if err := d.Set(data, "/home/user", "conflicts", ""); err != nil || d.Settings["conflicts"] != "" {
		t.Errorf("reset: %v, %v", err, d.Settings)
	}
}

// Without a profile, a new shuttle has no mode until it is set: what is set
// meanwhile is not refused for it, and Check says so.
func TestDraftWithoutMode(t *testing.T) {
	data := []byte(rowsConfig)
	d := &Draft{Name: "photos", Remote: "nas:/photos", Local: "/p", New: true}
	if err := d.Check(data, "/home/user"); err == nil || !strings.Contains(err.Error(), "mode") {
		t.Errorf("no mode: %v", err)
	}
	if err := d.Set(data, "/home/user", "metered", "run"); err != nil {
		t.Errorf("a setting, with the mode still to set: %v", err)
	}
	if err := d.Set(data, "/home/user", "mode", "push"); err != nil {
		t.Fatal(err)
	}
	if err := d.Check(data, "/home/user"); err != nil {
		t.Error(err)
	}
	d.Local = "/d/inside"
	if err := d.Check(data, "/home/user"); err == nil || !strings.Contains(err.Error(), "overlap") {
		t.Errorf("inside docs' folder: %v", err)
	}
}

// The folders are checked as soon as they are chosen, before the mode and
// the rest are.
func TestDraftCheckFolders(t *testing.T) {
	data := []byte(rowsConfig)
	d := &Draft{Name: "photos", Remote: "nas:/photos", Local: "/p", New: true}
	if err := d.CheckFolders(data, "/home/user"); err != nil {
		t.Errorf("good folders, no mode yet: %v", err)
	}
	for _, c := range []struct {
		remote, local string
		include       []string
		exclude       []string
		why           string
	}{
		{"nas:/photos", "/d/inside", nil, nil, "folders overlap"},
		{"nas:/photos", "relative", nil, nil, "shuttles.photos.local:"},
		{"ghost:/photos", "/p", nil, nil, "no share named"},
		{"nas:/photos", "/p", []string{"a"}, []string{"b"}, "set include or exclude"},
		{"nas:/photos", "/p", []string{"../a"}, nil, "not a clean path"},
	} {
		d.Remote, d.Local, d.Include, d.Exclude = c.remote, c.local, c.include, c.exclude
		if err := d.CheckFolders(data, "/home/user"); err == nil || !strings.Contains(err.Error(), c.why) || strings.Contains(err.Error(), "mode") {
			t.Errorf("%+v: %v; want %q alone", c, err, c.why)
		}
	}
}

func TestDraftSaveNew(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(rowsConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	d := &Draft{Name: "photos", Profile: "live", Remote: "nas:/photos", Local: "~/Pictures",
		Include: []string{"2024", "a, b.jpg"}, Settings: map[string]string{"conflicts": "ask"}, New: true}
	if err := d.Save(path, "/home/user"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path) //nolint:gosec // the test's own file
	if err != nil {
		t.Fatal(err)
	}
	want := rowsConfig + "\n[shuttles.photos]\nprofile = \"live\"\nlocal = \"~/Pictures\"\nremote = \"nas:/photos\"\n" +
		"include = [\"2024\", \"a, b.jpg\"]\nconflicts = \"ask\"\n"
	if string(got) != want {
		t.Errorf("saved:\n%s\nwant:\n%s", got, want)
	}
	// Twice is a shuttle by that name already: nothing more is written.
	if err := d.Save(path, "/home/user"); err == nil || !strings.Contains(err.Error(), "already") {
		t.Errorf("saved twice: %v", err)
	}
	if again, _ := os.ReadFile(path); string(again) != want { //nolint:gosec // the test's own file
		t.Errorf("saved twice:\n%s", again)
	}
	back, err := DraftOf(got, "photos")
	if err != nil || strings.Join(back.Include, "|") != "2024|a, b.jpg" || back.Settings["conflicts"] != "ask" || back.Profile != "live" {
		t.Errorf("read back: %+v, %v", back, err)
	}
}

// An edit writes only its own keys: comments, enabled and the rest of the
// file stay as written; a setting reset is removed.
func TestDraftSaveEdit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	data := strings.Replace(rowsConfig, "profile = \"live\"\n", "profile = \"live\"\nenabled = false\n", 1)
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	d, err := DraftOf([]byte(data), "docs")
	if err != nil {
		t.Fatal(err)
	}
	if d.Remote != "nas:d" || d.Local != "/d" || d.Settings["deletion_guard"] != "20" || d.Settings["ignore"] != "*.tmp, build/" || d.New {
		t.Fatalf("read: %+v", d)
	}
	if moved, err := d.Moved([]byte(data), "/home/user"); err != nil || moved {
		t.Errorf("unchanged: moved %v, %v", moved, err)
	}
	d.Remote = "nas:/d" // the same folder, written another way
	if moved, err := d.Moved([]byte(data), "/home/user"); err != nil || moved {
		t.Errorf("the same folder: moved %v, %v", moved, err)
	}
	d.Exclude = []string{"old"}
	if err := d.Set([]byte(data), "/home/user", "deletion_guard", ""); err != nil {
		t.Fatal(err)
	}
	if err := d.Save(path, "/home/user"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path) //nolint:gosec // the test's own file
	for _, s := range []string{"[shuttles.docs] # the documents", "enabled = false", "# what to leave alone, by hand",
		`remote = "nas:/d"`, `exclude = ["old"]`} {
		if !strings.Contains(string(got), s) {
			t.Errorf("no %q in:\n%s", s, got)
		}
	}
	if strings.Contains(string(got), "deletion_guard") {
		t.Errorf("deletion_guard reset, but still there:\n%s", got)
	}
	d.Local = "/e"
	if moved, err := d.Moved(got, "/home/user"); err != nil || !moved {
		t.Errorf("another folder: moved %v, %v", moved, err)
	}

	// Removed meanwhile: not added back, half made.
	gone := strings.Replace(string(got), "[shuttles.docs]", "[shuttles.other]", 1)
	if err := os.WriteFile(path, []byte(gone), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := d.Save(path, "/home/user"); !errors.Is(err, ErrNoShuttle) {
		t.Errorf("saved over a shuttle removed: %v", err)
	}
	if _, err := DraftOf([]byte(rowsConfig), "nope"); !errors.Is(err, ErrNoShuttle) {
		t.Errorf("an unknown shuttle: %v", err)
	}
}
