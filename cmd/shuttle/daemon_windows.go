// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package main

import (
	"context"
	"errors"
	"io"
	"log/slog"

	"github.com/fqazzazee/shuttle/internal/bus"
	"github.com/fqazzazee/shuttle/internal/daemon"
	"github.com/fqazzazee/shuttle/internal/secrets"
	"github.com/fqazzazee/shuttle/internal/secrets/credman"
)

// newStore is where share passwords are kept: Credential Manager.
func newStore() secrets.Store { return credman.New() }

// runDaemon runs every shuttle in the user's config, and serves this user's
// pipe once their state is held, until ctx is done. Notifications and
// metered connections are Linux's for now.
func runDaemon(ctx context.Context, stderr io.Writer) error {
	paths, err := daemon.DefaultPaths()
	if err != nil {
		return err
	}
	log := slog.New(slog.NewTextHandler(stderr, nil))
	d, err := daemon.New(&daemon.Options{Paths: paths, Store: newStore(), Log: log})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	runErr := make(chan error, 1)
	go func() { runErr <- d.Run(ctx) }()
	select {
	case err := <-runErr: // failed before it held the state, e.g. already running
		return err
	case <-d.Ready():
	}
	pipe, err := bus.PipeName()
	if err != nil {
		cancel()
		return errors.Join(err, <-runErr)
	}
	serveErr := make(chan error, 1)
	go func() { serveErr <- bus.Serve(ctx, d) }()
	log.Info("serving the pipe", "name", pipe)

	// Whichever ends first ends the other: a daemon nobody can reach, or a
	// pipe with no daemon behind it, is no use.
	select {
	case err := <-serveErr:
		cancel()
		return errors.Join(err, <-runErr)
	case err := <-runErr:
		cancel()
		return errors.Join(err, <-serveErr)
	}
}
