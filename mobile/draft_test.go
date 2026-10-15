// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package mobile

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A shuttle made a step at a time, as the wizard does, saved, and run.
func TestDraft(t *testing.T) {
	f := newFixture(t)
	card := t.TempDir()
	doc, err := os.ReadFile(f.opts.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	doc = append(doc, fmt.Sprintf("\n[shares.card]\ntype = \"path\"\npath = %q\n", card)...)
	if err := os.WriteFile(f.opts.ConfigPath, doc, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(card, "photos", "2024"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(card, "photos", "2024", "a.jpg"), []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := f.start(t)
	d := m.NewDraft()

	if err := d.SetName("s"); err == nil || !strings.Contains(err.Error(), "already") {
		t.Errorf("another shuttle's name: %v", err)
	}
	if err := d.SetName("photos"); err != nil {
		t.Fatal(err)
	}
	// s's folder on the phone is taken.
	if err := d.SetFolders("card", "/photos", f.local, `{"Include":null,"Exclude":null}`); err == nil || !strings.Contains(err.Error(), "overlap") {
		t.Errorf("s's folder: %v", err)
	}
	local := filepath.Join(t.TempDir(), "Pictures")
	if err := os.Mkdir(local, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := d.SetFolders("card", "/photos", local, `{"Include":["2024"],"Exclude":null}`); err != nil {
		t.Errorf("folders, with the mode still to set: %v", err)
	}

	out, err := d.SettingsJSON()
	if err != nil || !strings.Contains(out, `"Key":"mode","Label":"Mode"`) {
		t.Errorf("rows: %s, %v", out, err)
	}
	// Without a mode it would not load: nothing is written.
	before, _ := os.ReadFile(f.opts.ConfigPath)
	if err := d.Save(); err == nil || !strings.Contains(err.Error(), "mode") {
		t.Errorf("saved without a mode: %v", err)
	}
	if after, _ := os.ReadFile(f.opts.ConfigPath); !bytes.Equal(before, after) {
		t.Errorf("config changed by a refused save:\n%s", after)
	}
	if err := d.Set("conflicts", "maybe"); err == nil {
		t.Error("a choice not offered taken")
	}
	if err := d.Set("mode", "pull"); err != nil {
		t.Fatal(err)
	}
	if d.SetProfile("none-such"); d.Save() == nil {
		t.Error("saved on a profile that is not there")
	}
	d.SetProfile("")
	if err := d.Save(); err != nil {
		t.Fatal(err)
	}
	saved, _ := os.ReadFile(f.opts.ConfigPath)
	for _, want := range []string{"[shuttles.photos]", `remote = "card:/photos"`, `include = ["2024"]`, `mode = "pull"`} {
		if !strings.Contains(string(saved), want) {
			t.Errorf("config lacks %s:\n%s", want, saved)
		}
	}
	if err := d.Save(); err == nil || !strings.Contains(err.Error(), "already") {
		t.Errorf("saved twice: %v", err)
	}

	// The daemon follows the file, and pulls what was marked when asked.
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(m.StatusesJSON(), `"Name":"photos"`) {
		if time.Now().After(deadline) {
			t.Fatalf("the new shuttle is not there: %s", m.StatusesJSON())
		}
		time.Sleep(50 * time.Millisecond)
	}
	if _, err := m.SyncAndWait("photos", 10_000); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(local, "2024", "a.jpg")); err != nil || string(b) != "a" {
		t.Errorf("pulled a.jpg: %q, %v", b, err)
	}
}

// A saved shuttle in the wizard: where it syncs, for its steps to start
// from, and whether an edit moves it.
func TestEditDraft(t *testing.T) {
	f := newFixture(t)
	m := f.start(t)
	if _, err := m.EditDraft("nope"); err == nil {
		t.Error("an unknown shuttle edited")
	}
	d, err := m.EditDraft("s")
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf(`{"Name":"s","Share":"mnt","Folder":"/","Local":%q,"Profile":"","Selection":{"Include":null,"Exclude":null}}`, f.local)
	if got := d.JSON(); got != want {
		t.Errorf("JSON = %s; want %s", got, want)
	}
	if moved, err := d.Moved(); err != nil || moved {
		t.Errorf("unchanged: moved %v, %v", moved, err)
	}
	if err := d.SetName("s"); err != nil {
		t.Errorf("an edit keeps its name: %v", err)
	}
	if err := d.SetFolders("mnt", "/", f.local, `{"Include":null,"Exclude":["cache"]}`); err != nil {
		t.Fatal(err)
	}
	if moved, err := d.Moved(); err != nil || moved {
		t.Errorf("only what it selects changed: moved %v, %v", moved, err)
	}
	other := t.TempDir()
	if err := d.SetFolders("mnt", "/", other, `{"Include":null,"Exclude":["cache"]}`); err != nil {
		t.Fatal(err)
	}
	if moved, err := d.Moved(); err != nil || !moved {
		t.Errorf("another folder on the phone: moved %v, %v", moved, err)
	}
	if err := d.Set("conflicts", "ask"); err != nil {
		t.Fatal(err)
	}
	if err := d.Save(); err != nil {
		t.Fatal(err)
	}
	saved, _ := os.ReadFile(f.opts.ConfigPath)
	for _, w := range []string{"[shuttles.s]", `mode = "push"`, fmt.Sprintf("local = %q", other), `exclude = ["cache"]`, `conflicts = "ask"`} {
		if !strings.Contains(string(saved), w) {
			t.Errorf("config lacks %s:\n%s", w, saved)
		}
	}
}
