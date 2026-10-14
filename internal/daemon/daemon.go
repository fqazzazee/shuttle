// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

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
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/fqazzazee/shuttle/internal/activity"
	"github.com/fqazzazee/shuttle/internal/config"
	"github.com/fqazzazee/shuttle/internal/engine"
	"github.com/fqazzazee/shuttle/internal/fsys"
	"github.com/fqazzazee/shuttle/internal/secrets"
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
	// OnEvent, if set, is called with what is worth a desktop notification
	// (see Event). Like OnChange, it must not block or keep the pointer.
	OnEvent func(*Event)
	// DeviceName names this side in conflict copies and in what was done
	// about a conflict. "" means the host name, which Android gives as
	// "localhost": the app passes the device's name.
	DeviceName string
	// Waiting, if set, says why a shuttle cannot run now although nothing is
	// wrong with it, or "" when it can: on Android, a permission the user has
	// not given ("needs All files access"). A waiting shuttle starts no run,
	// not even one the user asks for, and a run in progress stops after its
	// file. It is asked often, under the runner's lock: it must be quick and
	// must not call the daemon. Call Recheck when what it depends on changes.
	Waiting func(cfg *config.Config, s *config.Shuttle) string
	// Paused and PausedAll are the shuttles paused, and whether all are, as
	// the daemon starts: in place before any of them can run. The desktop's
	// pauses last while its daemon runs; the Android app starts daemons
	// often, so it keeps the user's pauses itself and hands them over.
	Paused    []string
	PausedAll bool
	// LocalNames, if set, gives the naming rules of the file system a
	// shuttle's local folder is on, where Linux cannot tell: the Android app
	// says which folders are in the phone's shared storage. nil: Linux's.
	LocalNames func(dir string) fsys.Names
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
	// ErrConfig means the config file is missing or invalid: running again
	// will not help until the user fixes it.
	ErrConfig = errors.New("config")
	// ErrAlreadyRunning means another daemon holds the shuttles' state.
	ErrAlreadyRunning = errors.New("another shuttle daemon is already running")
)

// Daemon runs the shuttles of one config, and follows the config as it
// changes.
type Daemon struct {
	o Options

	ready  chan struct{} // closed once Run has every state database open
	quit   chan struct{} // closed when Run starts shutting down
	reload chan struct{} // asks the config watcher for a reload now

	// runnersMu guards the config and the runners, which a reload replaces.
	// Taken after shareMu and before any runner's mu.
	runnersMu sync.RWMutex
	cfg       *config.Config
	runners   map[string]*runner

	// global is the bandwidth cap across every shuttle. A reload changes
	// its rates in place, so runs in progress pick them up at their next
	// file.
	global *engine.Cap

	reloadMu  sync.Mutex // one reload at a time, and none during shutdown
	editMu    sync.Mutex // one write to the config file at a time
	configErr string     // why the config on disk was not loaded; "" when it was
	errShown  string     // the config error last announced

	listenMu  sync.Mutex
	listeners map[int]func(*Status)
	reloads   map[int]func(*Reloaded)
	nextID    int

	shareMu sync.Mutex        // taken before runnersMu
	shares  map[string]string // share -> ShareOnline or ShareOffline, as last announced

	mu        sync.Mutex
	pausedAll bool
	metered   bool // the connection is metered (SetMetered)
	hostname  string
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
	cfg, err := loadConfig(o.Paths.Config)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrConfig, err)
	}
	if _, err := os.Stat(o.Paths.Config); errors.Is(err, fs.ErrNotExist) {
		o.Log.Info("no config file yet: run shuttle to add a share and a shuttle", "config", o.Paths.Config)
	}
	host := o.DeviceName
	if host == "" {
		if host, err = os.Hostname(); err != nil {
			host = "local"
		}
	}
	d := &Daemon{o: o, cfg: cfg, runners: map[string]*runner{}, hostname: host, shares: map[string]string{},
		ready: make(chan struct{}), quit: make(chan struct{}), reload: make(chan struct{}, 1),
		global: engine.NewCap(cfg.Bandwidth.Up, cfg.Bandwidth.Down), pausedAll: o.PausedAll}
	for i := range cfg.Shuttles {
		s := &cfg.Shuttles[i]
		d.runners[s.Name] = newRunner(d, cfg, s)
	}
	return d, nil
}

// Run opens every enabled shuttle's state and runs the shuttles until ctx is
// done, reloading the config whenever it changes. It then lets each run in
// progress finish the file it is on, starts nothing more, and returns once
// every state database is closed.
func (d *Daemon) Run(ctx context.Context) error {
	if err := os.MkdirAll(d.o.Paths.StateDir, 0o700); err != nil {
		return err
	}
	// Every database first: a second daemon is refused before anything runs.
	var opened []*runner
	for _, r := range d.all() {
		if !r.shuttle.Enabled {
			continue
		}
		if err := r.openState(); err != nil {
			_ = closeAll(opened) // the open error is the one to report
			return err
		}
		opened = append(opened, r)
	}
	for _, r := range opened {
		r.launch()
	}
	d.o.Log.Info("daemon started", "config", d.o.Paths.Config, "shuttles", len(opened))

	reloads := d.watchConfig()
	watching := make(chan struct{})
	go func() {
		defer close(watching)
		reloads()
	}()
	close(d.ready)
	<-ctx.Done()
	close(d.quit)
	<-watching

	d.reloadMu.Lock() // no reload from now on
	defer d.reloadMu.Unlock()
	err := stopAll(d.all())
	d.o.Log.Info("daemon stopped")
	return err
}

// Ready is closed once Run holds every enabled shuttle's state and its
// runners have started: from then on, a second daemon is refused.
func (d *Daemon) Ready() <-chan struct{} { return d.ready }

// SyncNow triggers a run of the named shuttle. A shuttle that is running runs
// once more when it finishes; one that is paused runs when resumed. The user
// asked for it, so its completion is announced even if nothing changed.
func (d *Daemon) SyncNow(name string) error {
	r, err := d.runner(name)
	if err != nil {
		return err
	}
	r.mu.Lock()
	r.requested = true
	r.mu.Unlock()
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
		for _, r := range d.all() {
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

// Watch registers fn to be called with a copy of a shuttle's status after
// every change, besides Options.OnChange, until the returned function is
// called. fn runs on the runner's goroutine and must not block.
func (d *Daemon) Watch(fn func(*Status)) (stop func()) {
	d.listenMu.Lock()
	defer d.listenMu.Unlock()
	if d.listeners == nil {
		d.listeners = map[int]func(*Status){}
	}
	id := d.nextID
	d.nextID++
	d.listeners[id] = fn
	return func() {
		d.listenMu.Lock()
		delete(d.listeners, id)
		d.listenMu.Unlock()
	}
}

func (d *Daemon) notify(st *Status) {
	if d.o.OnChange != nil {
		d.o.OnChange(st)
	}
	d.listenMu.Lock()
	ls := slices.Collect(maps.Values(d.listeners))
	d.listenMu.Unlock()
	for _, fn := range ls {
		cp := *st
		fn(&cp)
	}
}

// loadConfig reads the config file. One that is not there yet is a config
// with nothing in it: after `make install` the service starts before the
// TUI has written one, and picks it up when it does.
func loadConfig(path string) (*config.Config, error) {
	cfg, err := config.Load(path)
	if !errors.Is(err, fs.ErrNotExist) {
		return cfg, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return config.Parse(nil, home)
}

// SetMetered says whether the computer's connection is metered. A shuttle set
// to pause on one (config.MeteredPause, the default) starts no run of its own
// while it is, and one in progress stops after its file; on a connection that
// is not, waiting runs start. Runs the user asks for (SyncNow) still run.
func (d *Daemon) SetMetered(metered bool) {
	d.mu.Lock()
	changed := d.metered != metered
	d.metered = metered
	d.mu.Unlock()
	if !changed {
		return
	}
	d.o.Log.Info("connection", "metered", metered)
	d.runnersMu.RLock()
	defer d.runnersMu.RUnlock()
	for _, r := range d.runners {
		r.pausedChanged()
	}
}

func (d *Daemon) isMetered() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.metered
}

// Recheck asks Options.Waiting again for every shuttle, after what it
// depends on changed: shuttles that no longer wait run what was pending.
func (d *Daemon) Recheck() {
	for _, r := range d.all() {
		r.pausedChanged()
	}
}

// Paused reports whether every shuttle is paused (SetPaused("", true)).
func (d *Daemon) Paused() bool { return d.isPausedAll() }

// State is the state of the shuttles taken together, for one icon; see
// Overall.
func (d *Daemon) State() State {
	return Overall(d.Statuses(), d.isPausedAll(), d.ConfigError())
}

// Overall is the state of shuttles taken together: the most urgent of
// them, in the order error, held, offline, running, paused, idle. A config
// file that did not load counts as an error. Clients with the statuses
// work it out themselves, as the daemon would, without asking.
func Overall(all []Status, pausedAll bool, configErr string) State {
	if configErr != "" {
		return StateError
	}
	rank := map[State]int{StateError: 6, StateHeld: 5, StateOffline: 4, StateRunning: 3, StatePaused: 2, StateIdle: 1}
	best := StateIdle
	for i := range all {
		if s := all[i].State; rank[s] > rank[best] {
			best = s
		}
	}
	if pausedAll && rank[best] < rank[StateRunning] {
		best = StatePaused
	}
	return best
}

// Resolve settles a held conflict of the named shuttle the way the user
// chose, then syncs the shuttle. It waits for a run in progress to end.
func (d *Daemon) Resolve(ctx context.Context, name, path string, choice engine.Choice) error {
	r, err := d.runner(name)
	if err != nil {
		return err
	}
	if err := func() error {
		r.runMu.Lock()
		defer r.runMu.Unlock()
		local, remote, err := r.open(ctx)
		if err != nil {
			return err
		}
		defer local.Close()
		defer remote.Close()
		return engine.Resolve(ctx, r.options(local, remote), path, choice)
	}(); err != nil {
		return err
	}
	d.o.Log.Info("conflict resolved", "shuttle", name, "path", path, "choice", choice)
	r.addActivity(activity.KindConflict, path, "resolved: "+resolvedText[choice])
	r.trigger()
	return nil
}

// AllowDeletions lets the named shuttle's next run delete what the deletion
// guard has been holding back, and starts that run.
func (d *Daemon) AllowDeletions(name string) error {
	r, err := d.runner(name)
	if err != nil {
		return err
	}
	r.mu.Lock()
	r.allowDeletions = true
	r.mu.Unlock()
	d.o.Log.Info("deletions allowed for one run", "shuttle", name)
	r.trigger()
	return nil
}

// Statuses returns every shuttle's status, sorted by name.
func (d *Daemon) Statuses() []Status {
	all := d.all()
	out := make([]Status, 0, len(all))
	for _, r := range all {
		out = append(out, r.status())
	}
	return out
}

func (d *Daemon) runner(name string) (*runner, error) {
	d.runnersMu.RLock()
	r, ok := d.runners[name]
	d.runnersMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%q: %w", name, ErrUnknownShuttle)
	}
	if !r.shuttle.Enabled {
		return nil, fmt.Errorf("%q: %w", name, ErrDisabled)
	}
	return r, nil
}

// all returns the current runners, sorted by name.
// ProbeNow checks every share now rather than at the next probe: the
// Android app calls it when the phone joins a network, where the desktop
// waits up to 30 seconds.
func (d *Daemon) ProbeNow() {
	for _, r := range d.all() {
		select {
		case r.probe <- struct{}{}:
		default: // one is already asked for
		}
	}
}

// Config is the config the daemon runs, which may be a reload behind the
// file. A reload replaces it rather than changing it: read it, never
// change it.
func (d *Daemon) Config() *config.Config {
	d.runnersMu.RLock()
	defer d.runnersMu.RUnlock()
	return d.cfg
}

// NextRuns is when each scheduled shuttle's schedule runs it next, by name,
// worked out now rather than read from its status, which the scheduler
// fills in once it has started. It counts from the last run that ended: a
// slot the scheduler triggered but whose run has not happened (a daemon
// stopped at once, as the Android app's are) is still due, now.
func (d *Daemon) NextRuns() map[string]time.Time {
	now := d.o.Now()
	out := map[string]time.Time{}
	for _, r := range d.all() {
		if next, ok := nextDue(&r.shuttle.Settings, r.lastEnded(), now); ok {
			out[r.shuttle.Name] = next
		}
	}
	return out
}

// Triggered reports whether an enabled shuttle has trigger t. The Android
// app keeps its foreground service running while one is real-time.
func (d *Daemon) Triggered(t config.Trigger) bool {
	for _, r := range d.all() {
		if r.shuttle.Enabled && r.shuttle.Settings.Has(t) {
			return true
		}
	}
	return false
}

func (d *Daemon) all() []*runner {
	d.runnersMu.RLock()
	defer d.runnersMu.RUnlock()
	out := make([]*runner, 0, len(d.runners))
	for _, name := range slices.Sorted(maps.Keys(d.runners)) {
		out = append(out, d.runners[name])
	}
	return out
}

func (d *Daemon) isPausedAll() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.pausedAll
}
