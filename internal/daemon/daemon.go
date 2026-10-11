// SPDX-License-Identifier: GPL-2.0-or-later

// Package daemon runs every shuttle in the config on its own: one runner per
// shuttle, each owning its state database for as long as the daemon runs,
// starting a run whenever it is triggered and reporting its status.
//
// This package is desktop-specific; the sync core must not import it.
package daemon

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/fqazzazee/shuttle/internal/config"
	"github.com/fqazzazee/shuttle/internal/secrets"
	"github.com/fqazzazee/shuttle/internal/state"
)

// Paths are where the daemon finds its config and keeps its state.
type Paths struct {
	Config   string // the config file
	StateDir string // one state database per shuttle
}

// DefaultPaths follows the XDG base directories:
// $XDG_CONFIG_HOME/shuttle/config.toml and $XDG_STATE_HOME/shuttle.
func DefaultPaths() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, err
	}
	cfgHome := cmp.Or(os.Getenv("XDG_CONFIG_HOME"), filepath.Join(home, ".config"))
	stateHome := cmp.Or(os.Getenv("XDG_STATE_HOME"), filepath.Join(home, ".local", "state"))
	return Paths{
		Config:   filepath.Join(cfgHome, "shuttle", "config.toml"),
		StateDir: filepath.Join(stateHome, "shuttle"),
	}, nil
}

// Options configure a Daemon.
type Options struct {
	Paths Paths
	Store secrets.Store
	Log   *slog.Logger // nil: discard
	// OnChange, if set, is called with a copy of a shuttle's status after it
	// changes. It runs on the runner's goroutine and must not block.
	OnChange func(*Status)
	// KeyringTimeout bounds waiting for the keyring, including an unlock
	// prompt the user has not answered. Zero means two minutes.
	KeyringTimeout time.Duration
	// Now returns the current time; nil means time.Now.
	Now func() time.Time
}

var (
	// ErrUnknownShuttle means no shuttle has that name.
	ErrUnknownShuttle = errors.New("no shuttle by that name")
	// ErrDisabled means the shuttle is disabled in the config.
	ErrDisabled = errors.New("shuttle is disabled in the config")
	// ErrAlreadyRunning means another daemon holds the shuttles' state.
	ErrAlreadyRunning = errors.New("another shuttle daemon is already running")
)

// Daemon runs the shuttles of one config.
type Daemon struct {
	o       Options
	cfg     *config.Config
	runners map[string]*runner

	ready chan struct{}  // closed once Run has every state database open
	quit  chan struct{}  // closed when Run starts shutting down
	bg    sync.WaitGroup // schedulers and share watchers

	mu          sync.Mutex
	pausedAll   bool
	stopping    bool
	hostname    string
	runnersDone sync.WaitGroup
}

// New loads the config. Nothing runs until Run.
func New(opts *Options) (*Daemon, error) {
	o := *opts
	if o.Log == nil {
		o.Log = slog.New(slog.DiscardHandler)
	}
	if o.KeyringTimeout == 0 {
		o.KeyringTimeout = 2 * time.Minute
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	cfg, err := config.Load(o.Paths.Config)
	if err != nil {
		return nil, err
	}
	host, err := os.Hostname()
	if err != nil {
		host = "local"
	}
	d := &Daemon{o: o, cfg: cfg, runners: map[string]*runner{}, hostname: host,
		ready: make(chan struct{}), quit: make(chan struct{})}
	for i := range cfg.Shuttles {
		s := &cfg.Shuttles[i]
		d.runners[s.Name] = newRunner(d, s)
	}
	return d, nil
}

// Run opens every enabled shuttle's state and runs the shuttles until ctx is
// done. It then lets each run in progress finish the file it is on, starts
// nothing more, and returns once every state database is closed.
func (d *Daemon) Run(ctx context.Context) error {
	if err := os.MkdirAll(d.o.Paths.StateDir, 0o700); err != nil {
		return err
	}
	var opened []*runner
	for _, name := range d.names() {
		r := d.runners[name]
		if !r.shuttle.Enabled {
			continue
		}
		db, err := state.Open(filepath.Join(d.o.Paths.StateDir, name+".db"))
		if err != nil {
			for _, o := range opened {
				o.db.Close()
			}
			if errors.Is(err, state.ErrLocked) {
				return fmt.Errorf("%w (shuttle %s's state is in use)", ErrAlreadyRunning, name)
			}
			return err
		}
		r.db = db
		// The schedule counts from the last complete run, so a slot missed
		// while the machine was off runs at start.
		if last, err := db.LastComplete(); err == nil {
			r.markAttempt(last)
		}
		opened = append(opened, r)
	}
	d.o.Log.Info("daemon started", "config", d.o.Paths.Config, "shuttles", len(opened))

	for _, r := range opened {
		d.runnersDone.Go(r.loop)
		d.bg.Go(func() { r.watchShare(d.quit) })
		if r.shuttle.Settings.Has(config.TriggerSchedule) {
			d.bg.Go(func() { r.schedule(d.quit) })
		}
	}
	close(d.ready)
	<-ctx.Done()
	close(d.quit)
	d.bg.Wait()

	d.mu.Lock()
	d.stopping = true
	d.mu.Unlock()
	for _, r := range opened {
		r.wake()
	}
	d.runnersDone.Wait()
	var errs []error
	for _, r := range opened {
		errs = append(errs, r.db.Close())
	}
	d.o.Log.Info("daemon stopped")
	return errors.Join(errs...)
}

// Ready is closed once Run holds every enabled shuttle's state and its
// runners have started: from then on, a second daemon is refused.
func (d *Daemon) Ready() <-chan struct{} { return d.ready }

// SyncNow triggers a run of the named shuttle. A shuttle that is running runs
// once more when it finishes; one that is paused runs when resumed.
func (d *Daemon) SyncNow(name string) error {
	r, err := d.runner(name)
	if err != nil {
		return err
	}
	r.trigger()
	return nil
}

// SetPaused pauses or resumes the named shuttle, or every shuttle when name
// is empty. A run in progress finishes the file it is on and stops.
func (d *Daemon) SetPaused(name string, paused bool) error {
	if name == "" {
		d.mu.Lock()
		d.pausedAll = paused
		d.mu.Unlock()
		for _, r := range d.runners {
			r.pausedChanged()
		}
		return nil
	}
	r, err := d.runner(name)
	if err != nil {
		return err
	}
	r.setPaused(paused)
	return nil
}

// Statuses returns every shuttle's status, sorted by name.
func (d *Daemon) Statuses() []Status {
	out := make([]Status, 0, len(d.runners))
	for _, name := range d.names() {
		out = append(out, d.runners[name].status())
	}
	return out
}

func (d *Daemon) runner(name string) (*runner, error) {
	r, ok := d.runners[name]
	if !ok {
		return nil, fmt.Errorf("%q: %w", name, ErrUnknownShuttle)
	}
	if !r.shuttle.Enabled {
		return nil, fmt.Errorf("%q: %w", name, ErrDisabled)
	}
	return r, nil
}

func (d *Daemon) names() []string {
	names := make([]string, 0, len(d.runners))
	for n := range d.runners {
		names = append(names, n)
	}
	slices.Sort(names)
	return names
}

func (d *Daemon) isStopping() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.stopping
}

func (d *Daemon) isPausedAll() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.pausedAll
}
