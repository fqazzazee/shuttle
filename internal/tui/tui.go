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
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/fqazzazee/shuttle/internal/daemon"
)

// Client is what the TUI asks of the daemon. *bus.Client has it.
type Client interface {
	Shuttles(ctx context.Context) ([]daemon.Status, error)
	State(ctx context.Context) (daemon.State, bool, error)
	ConfigError(ctx context.Context) (string, error)
	SyncNow(ctx context.Context, name string) error
	SetPaused(ctx context.Context, name string, paused bool) error
	SetEnabled(ctx context.Context, name string, on bool) error
	Changes(ctx context.Context) (<-chan daemon.Status, error)
}

// Run shows the TUI until the user quits or ctx is done.
func Run(ctx context.Context, c Client) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	_, err := tea.NewProgram(newModel(ctx, c), tea.WithContext(ctx)).Run()
	return err
}

// callTimeout bounds one request to the daemon, refreshEvery how often the
// whole picture is asked for again.
const (
	callTimeout  = 5 * time.Second
	refreshEvery = 2 * time.Second
)
