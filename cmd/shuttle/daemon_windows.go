// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"

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
	out, closeLog, err := logTo(stderr, paths)
	if err != nil {
		return err
	}
	defer closeLog()
	log := slog.New(slog.NewTextHandler(out, nil))
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

// maxLog is how big the daemon's log file grows before it starts afresh,
// the one before kept beside it.
const maxLog = 10 << 20

// logTo is where the daemon logs: stderr, or, with no console to write to
// (shuttlew.exe, started at sign-in), daemon.log beside the state.
func logTo(stderr io.Writer, paths daemon.Paths) (io.Writer, func(), error) {
	if f, ok := stderr.(*os.File); !ok || writable(f) {
		return stderr, func() {}, nil
	}
	path := filepath.Join(filepath.Dir(paths.StateDir), "daemon.log")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, nil, err
	}
	if fi, err := os.Stat(path); err == nil && fi.Size() > maxLog {
		os.Rename(path, path+".old") //nolint:errcheck // a log too long is better than none
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600) //nolint:gosec // the user's own state folder
	if err != nil {
		return nil, nil, err
	}
	return f, func() { f.Close() }, nil
}

// writable says whether f is somewhere to write to: a program built without
// a console starts with no standard handles.
func writable(f *os.File) bool {
	t, err := windows.GetFileType(windows.Handle(f.Fd()))
	return err == nil && t != windows.FILE_TYPE_UNKNOWN
}
