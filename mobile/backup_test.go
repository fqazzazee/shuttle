// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package mobile

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A backup saved, checked and restored over another setup, as the app does.
func TestBackup(t *testing.T) {
	f := newFixture(t)
	m := f.start(t)
	saved, err := m.ConfigText()
	if err != nil || !strings.Contains(saved, "[shuttles.s]") {
		t.Fatalf("backup: %q, %v", saved, err)
	}

	// Another setup: an SMB share with no password here, and a shuttle on a
	// folder this phone does not have.
	other := fmt.Sprintf("[shares.nas]\ntype = \"smb\"\nhost = \"nas.example\"\nshare = \"home\"\nuser = \"user\"\n\n"+
		"[shares.mnt]\ntype = \"path\"\npath = %q\n\n[profiles.live]\nmode = \"sync\"\n\n"+
		"[shuttles.photos]\nprofile = \"live\"\nlocal = %q\nremote = \"nas:/photos\"\n", f.share, filepath.Join(t.TempDir(), "gone"))
	got, err := m.CheckBackup(other)
	want := `{"Shares":["mnt","nas"],"Profiles":["live"],"Shuttles":["photos"],"NoPassword":["nas"],"NoFolder":["photos"]}`
	if err != nil || got != want {
		t.Errorf("CheckBackup = %s, %v; want %s", got, err, want)
	}
	if _, err := m.CheckBackup("[shuttles.x]\nmode = \"sideways\"\n"); err == nil {
		t.Error("a backup that does not load checked")
	}
	if err := m.RestoreBackup("[shuttles.x]\nmode = \"sideways\"\n"); err == nil {
		t.Error("a backup that does not load restored")
	}
	if now, _ := m.ConfigText(); now != saved {
		t.Errorf("config changed by a refused restore:\n%s", now)
	}

	if err := m.RestoreBackup(other); err != nil {
		t.Fatal(err)
	}
	if now, _ := m.ConfigText(); now != other {
		t.Errorf("restored:\n%s", now)
	}
	// The daemon follows: s is gone, photos is there.
	deadline := time.Now().Add(10 * time.Second)
	for st := m.StatusesJSON(); !strings.Contains(st, `"Name":"photos"`) || strings.Contains(st, `"Name":"s"`); st = m.StatusesJSON() {
		if time.Now().After(deadline) {
			t.Fatalf("the daemon did not follow: %s", st)
		}
		time.Sleep(50 * time.Millisecond)
	}

	// And back: the first backup restores the first setup.
	if err := m.RestoreBackup(saved); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(f.opts.ConfigPath); err != nil {
		t.Fatal(err)
	}
	if text, err := m.ConfigText(); text != "" || err != nil {
		t.Errorf("no config: %q, %v", text, err)
	}
}
