// SPDX-License-Identifier: GPL-2.0-or-later

// Package tui is shuttle's terminal interface: a client of the daemon, with
// a tab per subject (SPEC.md, "TUI"). It follows the daemon live: statuses
// as they change, and a full refresh every couple of seconds for what the
// signals do not carry.
//
// This package is desktop-specific; the sync core must not import it.
package tui

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/fqazzazee/shuttle/internal/activity"
	"github.com/fqazzazee/shuttle/internal/config"
	"github.com/fqazzazee/shuttle/internal/daemon"
	"github.com/fqazzazee/shuttle/internal/engine"
	"github.com/fqazzazee/shuttle/internal/fsys"
	"github.com/fqazzazee/shuttle/internal/secrets"
)

// Client is what the TUI asks of the daemon. *bus.Client has it.
type Client interface {
	Shuttles(ctx context.Context) ([]daemon.Status, error)
	State(ctx context.Context) (daemon.State, bool, error)
	ConfigError(ctx context.Context) (string, error)
	SyncNow(ctx context.Context, name string) error
	SetPaused(ctx context.Context, name string, paused bool) error
	SetEnabled(ctx context.Context, name string, on bool) error
	Resolve(ctx context.Context, name, path string, choice engine.Choice) error
	AllowDeletions(ctx context.Context, name string) error
	Activity(ctx context.Context, name string, limit int) ([]activity.Entry, error)
	Changes(ctx context.Context) (<-chan daemon.Status, error)
}

// Options are what the TUI does itself rather than through the daemon:
// share setup works with the daemon stopped.
type Options struct {
	Config string        // the config file
	Home   string        // the user's home, for "~" in the config
	Store  secrets.Store // where share passwords are kept
	// Test tries a share, with password if given, and says how much room it
	// has (shares.Test).
	Test func(ctx context.Context, share *config.Share, password string, store secrets.Store) (fsys.Space, error)
	// Browse opens a share's top folder to look for folders in.
	Browse func(ctx context.Context, share *config.Share, store secrets.Store) (fsys.FS, error)
	// Tab is the tab shown first; see Tab.
	Tab int
}

// Tab is the tab of a name, as shuttle open takes it: shuttles,
// conflicts, activity, profiles or shares.
func Tab(name string) (int, bool) {
	for i, n := range tabNames {
		if strings.EqualFold(n, name) {
			return i, true
		}
	}
	return 0, false
}

// Run shows the TUI until the user quits or ctx is done.
func Run(ctx context.Context, c Client, o Options) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	_, err := tea.NewProgram(newModel(ctx, c, o), tea.WithContext(ctx)).Run()
	return err
}

// callTimeout bounds one request to the daemon, refreshEvery how often the
// whole picture is asked for again. keyringTimeout bounds what may wait for
// the user to unlock the keyring, or for a share's server.
const (
	callTimeout    = 5 * time.Second
	refreshEvery   = 2 * time.Second
	keyringTimeout = 2 * time.Minute
)
