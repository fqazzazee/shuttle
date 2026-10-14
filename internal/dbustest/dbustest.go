// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

// Package dbustest starts private D-Bus daemons for tests, so no test ever
// touches the user's real session bus.
package dbustest

import (
	"bufio"
	"os/exec"
	"strings"
	"testing"

	"github.com/godbus/dbus/v5"
)

// Start starts a session dbus-daemon of the test's own, stopped when the
// test ends, and returns its address. It skips the test without dbus-daemon.
func Start(t testing.TB) string {
	t.Helper()
	bin, err := exec.LookPath("dbus-daemon")
	if err != nil {
		t.Skip("dbus-daemon not installed")
	}
	cmd := exec.Command(bin, "--session", "--nofork", "--print-address=1") //nolint:gosec // dbus-daemon from PATH, fixed arguments
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	addr, err := bufio.NewReader(out).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(addr)
}

// Bus starts a private bus and returns a function that opens connections to
// it, each closed when the test ends.
func Bus(t testing.TB) func() *dbus.Conn {
	t.Helper()
	addr := Start(t)
	return func() *dbus.Conn {
		conn, err := dbus.Connect(addr)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { conn.Close() })
		return conn
	}
}
