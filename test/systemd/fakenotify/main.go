// SPDX-License-Identifier: GPL-2.0-or-later

// Command fakenotify stands in for the desktop's notification server in the
// systemd exit test: it owns org.freedesktop.Notifications on the session
// bus and appends each notification to a file, one line each:
// category|summary|body, with the body's newlines as " / ".
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/godbus/dbus/v5"

	"github.com/fqazzazee/shuttle/internal/notify/notifytest"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: fakenotify <file>")
		os.Exit(2)
	}
	out, err := os.OpenFile(os.Args[1], os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600) //nolint:gosec // the test names its own output file
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if _, err := notifytest.Serve(conn, true, func(n notifytest.Note) {
		fmt.Fprintf(out, "%s|%s|%s\n", n.Category, n.Summary, strings.ReplaceAll(n.Body, "\n", " / "))
	}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	select {}
}
