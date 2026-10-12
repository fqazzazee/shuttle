// SPDX-License-Identifier: GPL-2.0-or-later

// Command fakenotify stands in for the desktop's notification server in the
// systemd exit test: it owns org.freedesktop.Notifications on the session
// bus and appends each notification to a file, one line each:
// category|summary|body, with the body's newlines as " / ". It clicks every
// "Open shuttle" button it is shown, as a user would, and notes that too:
// clicked|summary|open.
package main

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"

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
	var mu sync.Mutex
	var srv *notifytest.Server
	srv, err = notifytest.Serve(conn, true, func(n notifytest.Note) {
		mu.Lock()
		defer mu.Unlock()
		fmt.Fprintf(out, "%s|%s|%s\n", n.Category, n.Summary, strings.ReplaceAll(n.Body, "\n", " / "))
		if slices.Contains(n.Actions, "open") {
			fmt.Fprintf(out, "clicked|%s|open\n", n.Summary)
			go func() { _ = srv.Invoke(n.ID, "open") }()
		}
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	select {}
}
