// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package tui

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// This computer's folders in the picker: the drives at the top, a folder
// as /C:/Users/me, and back to C:\Users\me for the config.
func TestLocalPathsOnWindows(t *testing.T) {
	for _, tt := range []struct{ native, slash string }{
		{`C:\Users\me`, "/C:/Users/me"},
		{`C:\Users\me\`, "/C:/Users/me"},
		{`c:\data`, "/C:/data"},
		{`D:\`, "/D:"},
	} {
		if got := localSlash(tt.native); got != tt.slash {
			t.Errorf("localSlash(%q) = %q, want %q", tt.native, got, tt.slash)
		}
	}
	for _, tt := range []struct{ slash, native string }{
		{"/C:/Users/me", `C:\Users\me`},
		{"/D:", `D:\`},
		{"/", ""}, // the drives: no folder
		{"data", "data"},
	} {
		if got := localNative(tt.slash); got != tt.native {
			t.Errorf("localNative(%q) = %q, want %q", tt.slash, got, tt.native)
		}
	}
	if got := localSlash("data"); got != "data" {
		t.Errorf("a path with no drive = %q, kept for the wizard to refuse", got)
	}
}

// The picker lists a real folder, and above its drive, the drives.
func TestLocalTreeOnWindows(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "Papers"), 0o755); err != nil {
		t.Fatal(err)
	}
	tr := localTree(dir, dir)
	if tr.dir() != filepath.Clean(dir) {
		t.Errorf("dir() = %q, want %q", tr.dir(), dir)
	}
	ctx := context.Background()
	entries, err := listLocal(ctx, tr.root.path)
	if err != nil || !slices.ContainsFunc(entries, func(e entry) bool { return e.name == "Papers" && e.dir }) {
		t.Errorf("listing %s: %+v, %v", tr.root.path, entries, err)
	}
	top, err := listLocal(ctx, "/")
	drive := strings.ToUpper(filepath.VolumeName(dir))
	if err != nil || !slices.ContainsFunc(top, func(e entry) bool { return e.name == drive && e.dir }) {
		t.Errorf("the drives: %+v, %v; want %s among them", top, err, drive)
	}
	for tr.root.path != "/" { // up to the drives, as backspace goes
		tr.up(ctx)
	}
	if tr.dir() != "" {
		t.Errorf("at the drives, dir() = %q, want none", tr.dir())
	}
}

// A Windows user has no Unix IDs of their own: the form says the share needs
// them, never "yours (-1)".
func TestNFSIDHintsOnWindows(t *testing.T) {
	for _, h := range []string{uidHint(), gidHint()} {
		if !strings.Contains(h, "needed on Windows") || strings.Contains(h, "-1") {
			t.Errorf("hint = %q", h)
		}
	}
}
