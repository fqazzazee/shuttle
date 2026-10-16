// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// winHome is the home of the tests here, as the tests marked unixPaths have
// /home/user.
const winHome = `C:\Users\user`

// A local folder on Windows is a drive path, written however TOML allows:
// a literal string, forward slashes, or doubled backslashes. ~ is the user's
// home. A path share can be a drive path or a UNC path.
func TestWindowsPaths(t *testing.T) {
	cfg, err := Parse([]byte(minimalShare+`
[shares.usb]
type = "path"
path = 'D:\backup'

[shares.pc]
type = "path"
path = '\\pc.example\photos'

[shuttles.literal]
mode = "push"
local = 'C:\data\docs'
remote = "nas:docs"

[shuttles.slashes]
mode = "push"
local = "C:/data/music/"
remote = "usb:music"

[shuttles.escaped]
mode = "push"
local = "C:\\data\\video"
remote = "pc:video"

[shuttles.home]
mode = "push"
local = "~/Documents"
remote = "nas:documents"
`), winHome)
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{
		"literal": `C:\data\docs`,
		"slashes": `C:\data\music`,
		"escaped": `C:\data\video`,
		"home":    `C:\Users\user\Documents`,
	} {
		if got := mustShuttle(t, cfg, name).Local; got != want {
			t.Errorf("%s: local = %q, want %q", name, got, want)
		}
	}
	if got := cfg.Shares["usb"].Path; got != `D:\backup` {
		t.Errorf("usb: path = %q", got)
	}
	if got := cfg.Shares["pc"].Path; got != `\\pc.example\photos` {
		t.Errorf("pc: path = %q", got)
	}
}

// What is absolute on Linux is not on Windows: a path needs its drive (or
// a UNC server).
func TestWindowsPathsNotAbsolute(t *testing.T) {
	for _, local := range []string{"/data", `\data`, "C:data", "data"} {
		_, err := Parse([]byte(minimalShare+`
[shuttles.s]
mode = "push"
local = '`+local+`'
remote = "nas:s"
`), winHome)
		if err == nil || !strings.Contains(err.Error(), "must be an absolute path") {
			t.Errorf("local = %q: %v, want refused", local, err)
		}
	}
}

// A Windows path the config editor writes loads again as it was: written
// as it is into a basic string, `C:\Users` would be a bad \U escape.
func TestWindowsPathsSaved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(minimalShare), 0o600); err != nil {
		t.Fatal(err)
	}
	d := &Draft{Name: "pictures", Remote: "nas:pictures", Local: `C:\Users\user\Pictures`,
		Settings: map[string]string{"mode": "push"}, New: true}
	if err := d.Save(path, winHome); err != nil {
		t.Fatal(err)
	}
	data := []byte(edit(t, string(read(t, path)), setShare(&Share{Name: "pc", Type: SharePath, Path: `\\pc.example\photos`})))
	cfg, err := Parse(data, winHome)
	if err != nil {
		t.Fatalf("the saved file does not load: %v\n%s", err, data)
	}
	if got := mustShuttle(t, cfg, "pictures").Local; got != `C:\Users\user\Pictures` {
		t.Errorf("local = %q", got)
	}
	if got := cfg.Shares["pc"].Path; got != `\\pc.example\photos` {
		t.Errorf("pc: path = %q", got)
	}
}

func read(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // the test's own file
	if err != nil {
		t.Fatal(err)
	}
	return data
}
