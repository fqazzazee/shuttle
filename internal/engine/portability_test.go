// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package engine

import (
	"os/exec"
	"strings"
	"testing"
)

// The sync core and the daemon must stay usable without a Linux desktop, so
// the Android app can reuse them (SPEC.md, portability rule). This fails the
// build the day one of them picks up a desktop dependency, directly or
// indirectly.
func TestCoreHasNoDesktopDependencies(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go not installed")
	}
	core := []string{"engine", "config", "fsys", "state", "ignore", "activity", "nfs3",
		"secrets", "shares", "daemon", "notice", "present"}
	args := []string{"list", "-deps", "-f", "{{.ImportPath}}"}
	for _, p := range core {
		args = append(args, "github.com/fqazzazee/shuttle/internal/"+p)
	}
	args = append(args, "github.com/fqazzazee/shuttle/mobile") // the Android app's binding
	out, err := exec.Command(goBin, args...).Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	forbidden := []string{
		"github.com/fqazzazee/shuttle/internal/secrets/secretservice",
		"github.com/fqazzazee/shuttle/internal/bus",
		"github.com/fqazzazee/shuttle/internal/notify",
		"github.com/fqazzazee/shuttle/internal/network",
		"github.com/fqazzazee/shuttle/internal/terminal",
		"github.com/fqazzazee/shuttle/internal/tui",
		"github.com/godbus/",                                            // D-Bus
		"github.com/coreos/go-systemd",                                  // systemd
		"github.com/zalando/go-keyring", "github.com/99designs/keyring", // Secret Service
		"github.com/charmbracelet/", "charm.land/", // TUI
		"os/exec", "os/signal", // processes and signals belong to the daemon
	}
	for _, dep := range strings.Fields(string(out)) {
		for _, f := range forbidden {
			if strings.HasPrefix(dep, f) {
				t.Errorf("core package depends on %s", dep)
			}
		}
	}
}
