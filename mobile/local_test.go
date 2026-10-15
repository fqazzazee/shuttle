// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package mobile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The folder on the phone, browsed and made as the wizard does.
func TestLocal(t *testing.T) {
	dir := t.TempDir()
	for _, d := range []string{"DCIM/Camera", "Music", ".thumbnails"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := LocalJSON(dir)
	if want := `[{"Name":"DCIM","Dir":true},{"Name":"Music","Dir":true},{"Name":".thumbnails","Dir":true},{"Name":"a.txt","Dir":false}]`; err != nil || got != want {
		t.Errorf("the folder: %s, %v; want %s", got, err, want)
	}
	if _, err := LocalJSON(filepath.Join(dir, "nope")); err == nil {
		t.Error("a folder that is not there listed")
	}

	made, err := MakeFolder(filepath.Join(dir, "DCIM"), " Backups ")
	if want := filepath.Join(dir, "DCIM", "Backups"); err != nil || made != want {
		t.Errorf("made %q, %v; want %q", made, err, want)
	}
	if info, err := os.Stat(made); err != nil || !info.IsDir() {
		t.Errorf("the folder made: %v", err)
	}
	if again, err := MakeFolder(filepath.Join(dir, "DCIM"), "Backups"); err != nil || again != made {
		t.Errorf("made again: %q, %v", again, err)
	}
	if _, err := MakeFolder(dir, "a.txt"); err == nil {
		t.Error("a folder made over a file")
	}

	for _, c := range []struct{ dir, name, why string }{
		{dir, "  ", "needs a name"},
		{dir, "a/b", "cannot be a folder's name"},
		{dir, "..", "cannot be a folder's name"},
		{"DCIM", "x", "not a full path"},
		// Shared storage refuses what FAT does; nothing is made there to
		// say so.
		{"/storage/emulated/0", "a:b", `the phone cannot hold that name: it contains ':'`},
	} {
		if _, err := MakeFolder(c.dir, c.name); err == nil || !strings.Contains(err.Error(), c.why) {
			t.Errorf("MakeFolder(%q, %q): %v; want %q", c.dir, c.name, err, c.why)
		}
	}
	if _, err := os.Stat("DCIM"); err == nil {
		t.Error("a folder made from a path that is not full")
	}
}
