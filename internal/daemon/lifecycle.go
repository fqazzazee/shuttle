// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package daemon

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/fqazzazee/shuttle/internal/config"
	"github.com/fqazzazee/shuttle/internal/state"
)

// openState opens the runner's state database and ties it to the folders
// the shuttle syncs.
func (r *runner) openState() error {
	name := r.shuttle.Name
	db, err := state.Open(filepath.Join(r.d.o.Paths.StateDir, name+".db"))
	if errors.Is(err, state.ErrLocked) {
		return fmt.Errorf("%w (shuttle %s's state is in use)", ErrAlreadyRunning, name)
	}
	if err != nil {
		return err
	}
	reset, err := db.Bind(pairOf(r.cfg, r.shuttle))
	if err != nil {
		db.Close()
		return err
	}
	if reset {
		r.d.o.Log.Warn("the shuttle's folders changed: starting afresh, as a first sync (nothing is deleted; files that differ are conflicts)",
			"shuttle", name)
	}
	// The schedule counts from the last complete run, so a slot missed
	// while the machine was off runs at start.
	if last, err := db.LastComplete(); err == nil {
		r.mu.Lock()
		r.completed = last
		r.mu.Unlock()
		if last.After(r.lastAttempt()) {
			r.markAttempt(last)
		}
	}
	r.db = db
	r.countSynced()
	r.openActivity()
	return nil
}

// launch starts the runner's loop and its triggers. openState comes first.
func (r *runner) launch() {
	r.loopDone = make(chan struct{})
	go func() {
		defer close(r.loopDone)
		r.loop()
	}()
	r.bg.Go(func() { r.watchShare(r.quit) })
	if r.shuttle.Settings.Has(config.TriggerSchedule) {
		r.bg.Go(func() { r.schedule(r.quit) })
	}
	if hasRealtime(&r.shuttle.Settings) {
		r.bg.Go(func() { r.realtime(r.quit) })
	}
	// A run asked for before the runner existed (carried over by a
	// reload) starts now.
	r.wake()
}

// stopAll stops runners together: their triggers end, each run in progress
// finishes the file it is on, and their databases close. Runners that never
// opened their state are skipped.
func stopAll(rs []*runner) error {
	var live []*runner
	for _, r := range rs {
		if r.db != nil {
			live = append(live, r)
		}
	}
	for _, r := range live {
		close(r.quit)
	}
	for _, r := range live {
		r.bg.Wait()
		r.mu.Lock()
		r.stopping = true
		r.mu.Unlock()
		r.wake()
	}
	for _, r := range live {
		if r.loopDone != nil {
			<-r.loopDone
		}
	}
	return closeAll(live)
}

func closeAll(rs []*runner) error {
	var errs []error
	for _, r := range rs {
		errs = append(errs, r.db.Close())
		r.db = nil
		if r.activity != nil {
			errs = append(errs, r.activity.Close())
			r.activity = nil
		}
	}
	return errors.Join(errs...)
}

// inherit carries what the user and the last runs left on a shuttle over
// to its replacement: pause, a run waiting, the last run's results, what
// was already announced. A disabled shuttle keeps none of it.
func (r *runner) inherit(old *runner) {
	if !r.shuttle.Enabled {
		return
	}
	old.mu.Lock()
	st, paused, pending, requested, allow := old.st, old.paused, old.pending, old.requested, old.allowDeletions
	attempted := old.attempted
	old.mu.Unlock()

	r.mu.Lock()
	defer r.mu.Unlock()
	if st.State == StateDisabled || st.State == StateRunning {
		st.State = StateIdle // a stopped run is pending again
	}
	st.Name, st.Mode, st.Note = r.shuttle.Name, r.shuttle.Settings.Mode, ""
	r.st = st
	r.paused, r.pending, r.requested, r.allowDeletions = paused, pending, requested, allow
	r.attempted = attempted
	r.announced = old.announced // old has stopped: nothing else touches it
}

// sameShuttle reports whether a shuttle is unchanged between two configs,
// share included: its runner can carry on.
func sameShuttle(oldCfg *config.Config, old *config.Shuttle, newCfg *config.Config, s *config.Shuttle) bool {
	return reflect.DeepEqual(old, s) && reflect.DeepEqual(oldCfg.Shares[old.Share], newCfg.Shares[s.Share])
}

// configDebounce is how long the config file must be quiet before it is
// reloaded: an editor's save can take several writes.
var configDebounce = 300 * time.Millisecond

// watchConfig starts watching the config file. Once it returns, a change is
// seen; the returned function reloads the config on each change, or when
// asked, until quit.
func (d *Daemon) watchConfig() (loop func()) {
	var events <-chan fsnotify.Event
	var errs <-chan error
	names := map[string]bool{filepath.Clean(d.o.Paths.Config): true}
	w, err := fsnotify.NewWatcher()
	if err == nil {
		// Watch the folder, not the file: editors save by writing a new
		// file and renaming it over the old one. A symlinked config (from
		// a dotfiles repo) is followed to the file it points to.
		dirs := map[string]bool{filepath.Dir(d.o.Paths.Config): true}
		// Before the first config there may be no folder to watch: the
		// TUI would make it, but only once the watch had failed.
		if err = os.MkdirAll(filepath.Dir(d.o.Paths.Config), 0o700); err != nil {
			dirs = nil
		}
		if target, err := filepath.EvalSymlinks(d.o.Paths.Config); err == nil {
			names[target] = true
			dirs[filepath.Dir(target)] = true
		}
		for dir := range dirs {
			if err = w.Add(dir); err != nil {
				break
			}
		}
	}
	if err != nil {
		d.o.Log.Warn("not watching the config file: changes apply when the daemon restarts", "err", err)
		if w != nil {
			w.Close()
			w = nil
		}
	} else {
		events, errs = w.Events, w.Errors
	}
	return func() {
		if w != nil {
			defer w.Close()
		}
		d.reloadLoop(names, events, errs)
	}
}

func (d *Daemon) reloadLoop(names map[string]bool, events <-chan fsnotify.Event, errs <-chan error) {
	var settle <-chan time.Time
	for {
		select {
		case <-d.quit:
			return
		case <-d.reload:
			settle = nil
			d.apply()
		case ev, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			if names[filepath.Clean(ev.Name)] {
				settle = time.After(configDebounce)
			}
		case err, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			d.o.Log.Warn("watching the config file", "err", err)
		case <-settle:
			settle = nil
			d.apply()
		}
	}
}

// Reloaded is what a reload did, for the bus.
type Reloaded struct {
	Removed     []string // shuttles no longer in the config
	ConfigError string   // why the config was not loaded; "" when it was
}

// WatchReloads registers fn to be called after every reload, until the
// returned function is called.
func (d *Daemon) WatchReloads(fn func(*Reloaded)) (stop func()) {
	d.listenMu.Lock()
	defer d.listenMu.Unlock()
	if d.reloads == nil {
		d.reloads = map[int]func(*Reloaded){}
	}
	id := d.nextID
	d.nextID++
	d.reloads[id] = fn
	return func() {
		d.listenMu.Lock()
		delete(d.reloads, id)
		d.listenMu.Unlock()
	}
}

// Reload checks the config file now and, if it is valid, applies it in the
// background, as a change to the file would. It returns the config error,
// if any; the running config stays in use then.
func (d *Daemon) Reload() error {
	if _, err := loadConfig(d.o.Paths.Config); err != nil {
		return fmt.Errorf("%w: %w", ErrConfig, err)
	}
	d.askReload()
	return nil
}

// SetEnabled turns a shuttle on or off in the config file, changing only
// its enabled key, and applies it in the background as a reload: a run in
// progress on a shuttle turned off finishes the file it is on. A broken
// config file is left alone.
func (d *Daemon) SetEnabled(name string, on bool) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	d.editMu.Lock()
	_, err = config.SetEnabled(d.o.Paths.Config, home, name, on)
	d.editMu.Unlock()
	switch {
	case errors.Is(err, config.ErrNoShuttle):
		return fmt.Errorf("%q: %w", name, ErrUnknownShuttle)
	case err != nil:
		return fmt.Errorf("%w: %w", ErrConfig, err)
	}
	// Even when the file already said so: the daemon may not have
	// caught up with it yet.
	d.askReload()
	return nil
}

// EditConfig changes the config file through its editor (config.Doc), which
// keeps the rest of the file as written, and applies the result as a reload.
// The change must leave a config that loads: if not, the file is left alone.
// A file another editor saved meanwhile is read again and the change made
// to it. A missing file is an empty one, as on a phone before its first
// shuttle.
func (d *Daemon) EditConfig(change func(*config.Doc) error) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	d.editMu.Lock()
	defer d.editMu.Unlock()
	for range 3 {
		before, err := os.ReadFile(d.o.Paths.Config)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			before = nil
		case err != nil:
			return err
		}
		doc, err := config.ParseDoc(before)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrConfig, err)
		}
		if err := change(doc); err != nil {
			return err
		}
		err = config.Save(d.o.Paths.Config, before, doc.Bytes(), home)
		if errors.Is(err, config.ErrChanged) {
			continue
		}
		if err != nil {
			return err
		}
		d.askReload()
		return nil
	}
	return config.ErrChanged
}

// ShuttleSettings are the named shuttle's settings as rows, as its editor in
// the TUI shows them: its own value of each, what applies otherwise and
// from where, and whether the row applies. From the config file as it is
// on disk, which may be ahead of what the daemon runs by a reload.
func (d *Daemon) ShuttleSettings(name string) ([]config.RowState, error) {
	data, err := os.ReadFile(d.o.Paths.Config)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrConfig, err)
	}
	rows, err := config.ShuttleRows(data, name)
	if errors.Is(err, config.ErrNoShuttle) {
		return nil, fmt.Errorf("%q: %w", name, ErrUnknownShuttle)
	}
	return rows, err
}

// SetShuttleSetting sets one of the named shuttle's settings in the config
// file, the text as its row has it; "" removes the shuttle's own, so its
// profile's or the default applies again. Only that key changes in the
// file (config.SetShuttleRow), and the daemon reloads at once.
func (d *Daemon) SetShuttleSetting(name, key, text string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	d.editMu.Lock()
	changed, err := config.SetShuttleRow(d.o.Paths.Config, home, name, key, text)
	d.editMu.Unlock()
	switch {
	case errors.Is(err, config.ErrNoShuttle):
		return fmt.Errorf("%q: %w", name, ErrUnknownShuttle)
	case err != nil:
		return err
	}
	if changed {
		d.o.Log.Info("setting changed", "shuttle", name, "key", key)
		d.askReload()
	}
	return nil
}

func (d *Daemon) askReload() {
	select {
	case d.reload <- struct{}{}:
	default: // one is already asked for
	}
}

// Terminal is the terminal the config names for "Open shuttle"; nil when
// it names none.
func (d *Daemon) Terminal() []string {
	d.runnersMu.RLock()
	defer d.runnersMu.RUnlock()
	return d.cfg.Terminal
}

// ConfigError is why the config file was last not loaded, or "": the
// daemon then still runs the config it last loaded.
func (d *Daemon) ConfigError() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.configErr
}

// apply loads the config file and brings the runners in line with it.
// Unchanged shuttles carry on undisturbed. A changed one finishes the file
// it is on, then its replacement starts with what it left (see inherit);
// a removed one finishes its file and stops; a new one starts.
func (d *Daemon) apply() {
	d.reloadMu.Lock()
	defer d.reloadMu.Unlock()
	log := d.o.Log
	cfg, err := loadConfig(d.o.Paths.Config)
	if err != nil {
		log.Warn("config not reloaded; the previous one stays in use", "err", err)
		d.configFailed(err.Error())
		return
	}
	d.mu.Lock()
	d.configErr, d.errShown = "", ""
	d.mu.Unlock()
	d.global.Set(cfg.Bandwidth.Up, cfg.Bandwidth.Down)

	d.runnersMu.RLock()
	oldCfg, old := d.cfg, maps.Clone(d.runners)
	d.runnersMu.RUnlock()

	next := map[string]*runner{}
	var replaced, fresh []*runner // old runners going, and their successors
	for i := range cfg.Shuttles {
		s := &cfg.Shuttles[i]
		o := old[s.Name]
		if o != nil && sameShuttle(oldCfg, o.shuttle, cfg, s) {
			next[s.Name] = o
			continue
		}
		n := newRunner(d, cfg, s)
		next[s.Name] = n
		fresh = append(fresh, n)
		if o != nil {
			replaced = append(replaced, o)
		}
	}
	var removed []string
	for _, name := range slices.Sorted(maps.Keys(old)) {
		if _, ok := next[name]; !ok {
			removed = append(removed, name)
			replaced = append(replaced, old[name])
		}
	}
	if len(fresh) == 0 && len(removed) == 0 {
		d.runnersMu.Lock()
		d.cfg = cfg
		d.runnersMu.Unlock()
		d.reloaded(&Reloaded{})
		return
	}

	// Until the swap, the old runners stay reachable: a sync asked for
	// meanwhile is carried over.
	if err := stopAll(replaced); err != nil {
		log.Warn("closing the state of a changed shuttle", "err", err)
	}
	for _, n := range fresh {
		if o := old[n.shuttle.Name]; o != nil {
			n.inherit(o)
		}
	}
	d.runnersMu.Lock()
	d.cfg, d.runners = cfg, next
	d.runnersMu.Unlock()

	started := 0
	for _, n := range fresh {
		if !n.shuttle.Enabled {
			n.changed()
			continue
		}
		if err := n.openState(); err != nil {
			n.mu.Lock()
			n.st.State, n.st.Error = StateError, err.Error()
			n.mu.Unlock()
			log.Warn("starting a shuttle after a reload", "shuttle", n.shuttle.Name, "err", err)
		} else {
			n.launch()
			started++
		}
		n.changed()
	}
	log.Info("config reloaded", "started", started, "disabled", len(fresh)-started, "removed", len(removed))
	d.reloaded(&Reloaded{Removed: removed})
}

// configFailed records a config that did not load, and announces it once.
func (d *Daemon) configFailed(msg string) {
	d.mu.Lock()
	d.configErr = msg
	news := msg != d.errShown
	d.errShown = msg
	d.mu.Unlock()
	if news {
		d.event(&Event{Kind: EventConfigError, Error: msg})
	}
	d.reloaded(&Reloaded{ConfigError: msg})
}

func (d *Daemon) reloaded(r *Reloaded) {
	r.ConfigError = d.ConfigError()
	d.listenMu.Lock()
	ls := slices.Collect(maps.Values(d.reloads))
	d.listenMu.Unlock()
	for _, fn := range ls {
		fn(r)
	}
}
