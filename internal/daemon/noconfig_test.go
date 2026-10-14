// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package daemon

import (
	"fmt"
	"path/filepath"
	"testing"
)

// Before the first config is written, the daemon has nothing to run, and
// says so by running: enabled straight after `make install`, it starts, and
// the shuttles the TUI then adds start without a restart. Its folder may not
// exist yet either.
func TestNoConfigYet(t *testing.T) {
	f := &fixture{t: t, local: t.TempDir(), mount: t.TempDir()}
	dir := t.TempDir()
	f.paths = Paths{Config: filepath.Join(dir, "config", "shuttle", "config.toml"), StateDir: filepath.Join(dir, "state")}
	f.start()
	if n := len(f.d.Statuses()); n != 0 {
		t.Fatalf("%d shuttles before there is a config", n)
	}
	if f.d.ConfigError() != "" {
		t.Errorf("no config yet is not an error: %s", f.d.ConfigError())
	}

	// The daemon made the folder, to watch it: the file can go straight in.
	f.writeConfig(fmt.Sprintf(`
[shares.mnt]
type = "path"
path = %q

[shuttles.s]
mode = "push"
local = %q
remote = "mnt:"
`, f.mount, f.local))
	f.waitFor("s", "the new shuttle", func(Status) bool { return true })
}
