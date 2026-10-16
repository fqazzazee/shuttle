// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

//go:build !windows

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sync"

	"github.com/godbus/dbus/v5"

	"github.com/fqazzazee/shuttle/internal/bus"
	"github.com/fqazzazee/shuttle/internal/daemon"
	"github.com/fqazzazee/shuttle/internal/network"
	"github.com/fqazzazee/shuttle/internal/notify"
	"github.com/fqazzazee/shuttle/internal/secrets"
	"github.com/fqazzazee/shuttle/internal/secrets/secretservice"
	"github.com/fqazzazee/shuttle/internal/terminal"
)

// newStore is where share passwords are kept: the Secret Service.
func newStore() secrets.Store { return secretservice.New() }

// runDaemon runs every shuttle in the user's config, and serves the D-Bus
// interface once their state is held and shows desktop notifications, until
// ctx is done.
func runDaemon(ctx context.Context, stderr io.Writer) error {
	paths, err := daemon.DefaultPaths()
	if err != nil {
		return err
	}
	log := slog.New(slog.NewTextHandler(stderr, nil))
	notifier := notify.New(log)
	d, err := daemon.New(&daemon.Options{Paths: paths, Store: newStore(), Log: log, OnEvent: notifier.Send})
	if err != nil {
		return err
	}
	notifier.Open = func(tab string) { openInTerminal(d.Terminal(), tab, log) }
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return fmt.Errorf("connecting to the session bus: %w", err)
	}
	defer conn.Close()
	// Whether the connection is metered, from NetworkManager on the system
	// bus. Without one, it never is: shuttles run as they always would.
	sys, sysErr := dbus.ConnectSystemBus()
	if sysErr != nil {
		log.Warn("not following metered connections: no system bus", "error", sysErr)
	} else {
		defer sys.Close()
	}

	var notifying sync.WaitGroup
	defer notifying.Wait() // after cancel, before conn closes
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	notifying.Go(func() { notifier.Run(ctx, conn) })
	if sys != nil {
		notifying.Go(func() {
			if err := network.Watch(ctx, sys, d.SetMetered); err != nil {
				log.Warn("not following metered connections", "error", err)
			}
		})
	}
	runErr := make(chan error, 1)
	go func() { runErr <- d.Run(ctx) }()
	select {
	case err := <-runErr: // failed before it held the state, e.g. already running
		return err
	case <-d.Ready():
	}
	serveErr := make(chan error, 1)
	go func() { serveErr <- bus.Serve(ctx, conn, d) }()
	log.Info("serving D-Bus", "name", bus.Name)

	// Whichever ends first ends the other: a daemon nobody can reach, or a
	// bus name with no daemon behind it, is no use.
	select {
	case err := <-serveErr:
		cancel()
		return errors.Join(err, <-runErr)
	case err := <-runErr:
		cancel()
		return errors.Join(err, <-serveErr)
	}
}

// openInTerminal opens the TUI on tab in a new terminal window: the
// notifications' "Open shuttle" button.
func openInTerminal(configured []string, tab string, log *slog.Logger) {
	self, err := os.Executable()
	if err == nil {
		var argv []string
		if argv, err = terminal.Command(configured, exec.LookPath, self, "open", tab); err == nil {
			err = terminal.Start(argv, exec.LookPath)
		}
	}
	if err != nil {
		log.Warn("opening shuttle in a terminal", "err", err)
	}
}
