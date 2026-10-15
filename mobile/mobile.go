// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

// Package mobile is the Android app's way into the daemon, built into an
// .aar by `gomobile bind`. gomobile passes only strings, numbers, booleans,
// errors and interfaces, so structures cross as JSON: a Status, an Event or
// an activity entry as the daemon has it, field names and all. It holds no
// logic of its own beyond that; the app is another client of the daemon,
// as the TUI is over D-Bus (DECISIONS 2026-10-14, "Android: Kotlin and
// Compose over the Go core").
//
// It lives outside internal/ because gomobile's generated wrapper imports
// it from outside this module's tree.
package mobile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fqazzazee/shuttle/internal/activity"
	"github.com/fqazzazee/shuttle/internal/config"
	"github.com/fqazzazee/shuttle/internal/daemon"
	"github.com/fqazzazee/shuttle/internal/engine"
	"github.com/fqazzazee/shuttle/internal/fsys"
	"github.com/fqazzazee/shuttle/internal/notice"
	"github.com/fqazzazee/shuttle/internal/present"
	"github.com/fqazzazee/shuttle/internal/secrets"
	"github.com/fqazzazee/shuttle/internal/shares"
)

// Passwords is the app's password store, written in Kotlin over the Android
// Keystore. Each password is keyed by the server it opens, as an account
// like "smb://user@nas.example/home"; Get returns "" when there is none.
type Passwords interface {
	Get(account string) (string, error)
	Set(account, password string) error
	Delete(account string) error
}

// Listener hears from the daemon: a shuttle's status after each change,
// what is worth a notification, worded as the desktop words it (a
// notice.Note: its key, kind, summary, body), and each reload of the config
// (the shuttles it removed, or why it did not load), each as JSON. After a
// reload, the app asks again which shuttles are real-time and what work to
// plan. It is called on the daemon's goroutines and must not block.
type Listener interface {
	OnStatus(statusJSON string)
	OnNotice(noteJSON string)
	OnReload(reloadedJSON string)
}

// Options are what Start needs.
type Options struct {
	ConfigPath string // the app's config.toml
	StateDir   string // state databases, activity logs and daemon.log
	// DeviceName names the phone in conflict copies: the device's name
	// (Settings.Global.DEVICE_NAME), else its model.
	DeviceName string
	Passwords  Passwords
	Listener   Listener // nil: none
	// FilesAccess and LocalNetwork are the permissions the user has given:
	// "All files access", and on Android 17 local network access. Without
	// them, the shuttles that need them wait and say which (SetPermissions).
	FilesAccess, LocalNetwork bool
	// PausedJSON is the shuttles the user paused, as a JSON array of names,
	// and PausedAll whether they paused all: the app keeps them, since its
	// daemons come and go, and every daemon starts with them in place.
	PausedJSON string
	PausedAll  bool
}

// Why a shuttle waits for a permission, as its status says.
const (
	NeedsFilesAccess  = "needs All files access"
	NeedsLocalNetwork = "needs Local network access"
)

// errRunning means this process already runs a daemon: two would fight over
// the state databases (DECISIONS 2026-10-14, "Android: how shuttles run").
// Unexported: gomobile would give the app a setter for it. The app asks
// Running first.
var errRunning = errors.New("a daemon is already running in this process")

var (
	mu      sync.Mutex
	running *Daemon
)

// Daemon is the process's running daemon.
type Daemon struct {
	d          *daemon.Daemon
	passwords  Passwords
	configPath string
	cancel     context.CancelFunc
	done       chan error
	log        *os.File
	access     *access
	browse     browsing
}

// browsing is the share the wizard has open for its listings, one at a time.
type browsing struct {
	mu    sync.Mutex
	share string
	fs    fsys.FS
}

// access is the permissions the app last reported, read by the daemon's
// Waiting under its runners' locks.
type access struct {
	files, network atomic.Bool
}

// waiting is the daemon's Waiting: a shuttle whose folder is in shared
// storage needs All files access, and one whose share is reached over the
// network (not a "path" share) needs local network access, without which
// Android 17 lets connections to the LAN time out.
func (a *access) waiting(cfg *config.Config, s *config.Shuttle) string {
	if !a.files.Load() && localNames(s.Local).Fold != nil {
		return NeedsFilesAccess
	}
	if sh, ok := cfg.Shares[s.Share]; ok && sh.Type != config.SharePath && !a.network.Load() {
		return NeedsLocalNetwork
	}
	return ""
}

// maxLog is how large daemon.log may grow before Start sets it aside as
// daemon.log.1: the phone has no journald to rotate it.
const maxLog = 1 << 20

// Start runs the daemon until Stop, and returns once it has every state
// database open.
func Start(o *Options) (*Daemon, error) {
	mu.Lock()
	defer mu.Unlock()
	if running != nil {
		return nil, errRunning
	}
	if err := os.MkdirAll(o.StateDir, 0o700); err != nil {
		return nil, err
	}
	logf, err := openLog(filepath.Join(o.StateDir, "daemon.log"))
	if err != nil {
		return nil, err
	}
	acc := &access{}
	acc.files.Store(o.FilesAccess)
	acc.network.Store(o.LocalNetwork)
	do, err := daemonOptions(o, acc, slog.New(slog.NewTextHandler(logf, nil)))
	if err != nil {
		logf.Close()
		return nil, err
	}
	d, err := daemon.New(do)
	if err != nil {
		logf.Close()
		return nil, err
	}
	if l := o.Listener; l != nil {
		// Never stopped: it goes with the daemon.
		d.WatchReloads(func(r *daemon.Reloaded) { l.OnReload(toJSON(r)) })
	}
	ctx, cancel := context.WithCancel(context.Background())
	m := &Daemon{d: d, passwords: o.Passwords, configPath: o.ConfigPath, cancel: cancel, done: make(chan error, 1), log: logf, access: acc}
	go func() { m.done <- d.Run(ctx) }()
	select {
	case <-d.Ready():
		running = m
		return m, nil
	case err := <-m.done:
		cancel()
		logf.Close()
		return nil, err
	}
}

// Running is the process's running daemon, or nil: a WorkManager worker uses
// it when the foreground service has one.
func Running() *Daemon {
	mu.Lock()
	defer mu.Unlock()
	return running
}

func daemonOptions(o *Options, acc *access, log *slog.Logger) (*daemon.Options, error) {
	var paused []string
	if o.PausedJSON != "" {
		if err := json.Unmarshal([]byte(o.PausedJSON), &paused); err != nil {
			return nil, fmt.Errorf("paused shuttles: %w", err)
		}
	}
	do := &daemon.Options{
		Paused:     paused,
		PausedAll:  o.PausedAll,
		Paths:      daemon.Paths{Config: o.ConfigPath, StateDir: o.StateDir},
		Store:      store{o.Passwords},
		Log:        log,
		DeviceName: o.DeviceName,
		Here:       "this phone",
		LocalNames: localNames,
		Waiting:    acc.waiting,
	}
	if l := o.Listener; l != nil {
		do.OnChange = func(st *daemon.Status) { l.OnStatus(toJSON(st)) }
		do.OnEvent = func(e *daemon.Event) { l.OnNotice(toJSON(notice.Of(e))) }
	}
	return do, nil
}

// localNames gives a folder in shared storage the phone's rules: the
// internal storage (/storage/emulated/0, also reached as /sdcard) and SD
// cards (/storage/XXXX-XXXX), which are FAT or exFAT. The app's private
// storage keeps Linux's.
func localNames(dir string) fsys.Names {
	dir = path.Clean(dir)
	for _, root := range []string{"/storage", "/sdcard", "/mnt/sdcard"} {
		if dir == root || strings.HasPrefix(dir, root+"/") {
			return fsys.AndroidSharedStorage
		}
	}
	return fsys.Names{}
}

func openLog(name string) (*os.File, error) {
	if info, err := os.Stat(name); err == nil && info.Size() > maxLog {
		if err := os.Rename(name, name+".1"); err != nil {
			return nil, err
		}
	}
	return os.OpenFile(filepath.Clean(name), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
}

// Stop lets a run in progress finish its file, and returns once the state
// is closed. The process can then Start a daemon again.
func (m *Daemon) Stop() error {
	m.EndBrowse()
	m.cancel()
	err := <-m.done
	m.log.Close()
	mu.Lock()
	if running == m {
		running = nil
	}
	mu.Unlock()
	return err
}

// StatusesJSON is every shuttle's status, as a JSON array.
func (m *Daemon) StatusesJSON() string { return toJSON(m.d.Statuses()) }

// State is the shuttles' state taken together, for the service's
// notification: "error", "held", "offline", "running", "paused" or "idle".
func (m *Daemon) State() string { return string(m.d.State()) }

// ConfigError says why the config file did not load, or "".
func (m *Daemon) ConfigError() string { return m.d.ConfigError() }

// entry is a line of an activity log as the app gets it: activity.Entry's
// fields and names, with the shuttle it belongs to, which a log's own lines
// leave out (it is the log's file name).
type entry struct {
	Time    time.Time     `json:"time"`
	Shuttle string        `json:"shuttle"`
	Kind    activity.Kind `json:"kind"`
	Path    string        `json:"path,omitempty"`
	Text    string        `json:"text"`
}

// ActivityJSON is the newest entries of a shuttle's activity log, or of
// every shuttle's when name is "", newest first: at most limit (0: all).
func (m *Daemon) ActivityJSON(name string, limit int) (string, error) {
	es, err := m.d.Activity(name, limit)
	if err != nil {
		return "", err
	}
	out := make([]entry, len(es))
	for i := range es {
		e := &es[i]
		out[i] = entry{Time: e.Time, Shuttle: e.Shuttle, Kind: e.Kind, Path: e.Path, Text: e.Text}
	}
	return toJSON(out), nil
}

// RealTime reports whether an enabled shuttle is real-time: while one is,
// the app's foreground service keeps this daemon running. Ask again after
// Reload.
func (m *Daemon) RealTime() bool { return m.d.Triggered(config.TriggerRealtime) }

// work is what the app asks Android's WorkManager to run for a shuttle that
// is not real-time (DECISIONS 2026-10-14, "Android: how shuttles run"). A
// worker starts the daemon, which runs what is due by itself (a schedule
// slot that passed, a share that is there on its first probe), and stops it
// once all is quiet (WaitQuiet).
type work struct {
	Name string
	// NextRun is when its schedule runs it next; absent without one.
	NextRun time.Time `json:",omitzero"`
	// ShareAvailable: it runs when its share is there. Without the
	// foreground service, the app looks whenever the phone is on a network,
	// at most every 15 minutes.
	ShareAvailable bool
	// Network: its share is reached over a network, so its worker waits
	// for one.
	Network bool
	// Unmetered: its own runs wait for a connection that is not metered.
	Unmetered bool
}

// WorkJSON is the work for every enabled shuttle that has a schedule or the
// share-available trigger and is not real-time (the foreground service runs
// those), as a JSON array. Ask again after each worker and after Reload.
//
// A shuttle paused, or waiting for a permission, has none: its due run
// cannot start, so its schedule stays due, and a job for it would start a
// worker over and over. Resuming it, or giving the permission, happens in
// the app, which plans again.
func (m *Daemon) WorkJSON() string {
	next := m.d.NextRuns()
	cfg := m.d.Config()
	held := map[string]bool{}
	statuses := m.d.Statuses()
	for i := range statuses {
		held[statuses[i].Name] = statuses[i].Paused || statuses[i].Waiting != ""
	}
	out := []work{}
	for i := range cfg.Shuttles {
		s := &cfg.Shuttles[i]
		st := &s.Settings
		if !s.Enabled || held[s.Name] || st.Has(config.TriggerRealtime) ||
			!st.Has(config.TriggerSchedule) && !st.Has(config.TriggerShareAvailable) {
			continue
		}
		out = append(out, work{
			Name:           s.Name,
			NextRun:        next[s.Name],
			ShareAvailable: st.Has(config.TriggerShareAvailable),
			Network:        cfg.Shares[s.Share].Type != config.SharePath,
			Unmetered:      st.Metered == config.MeteredPause,
		})
	}
	return toJSON(out)
}

// WaitQuiet waits up to timeoutMillis for every enabled shuttle to have
// looked for its share and worked out its schedule, and for none of them to
// be running or have a run waiting that could start. A shuttle held back
// (paused, metered, waiting for a permission) counts as quiet.
func (m *Daemon) WaitQuiet(timeoutMillis int64) error {
	deadline := time.Now().Add(time.Duration(timeoutMillis) * time.Millisecond)
	for {
		if quiet(m.d.Statuses(), m.d.Config().Shuttles) {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("the shuttles did not finish in time")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func quiet(statuses []daemon.Status, shuttles []config.Shuttle) bool {
	scheduled := map[string]bool{}
	for i := range shuttles {
		scheduled[shuttles[i].Name] = shuttles[i].Settings.Has(config.TriggerSchedule)
	}
	for i := range statuses {
		st := &statuses[i]
		switch {
		case st.State == daemon.StateDisabled:
		case st.State == daemon.StateRunning,
			st.Pending && st.State != daemon.StatePaused,
			st.Share == "", // not probed yet
			scheduled[st.Name] && st.NextRun.IsZero(): // schedule not worked out yet
			return false
		}
	}
	return true
}

// ProbeNow looks for every share now: the app calls it when the phone joins
// a network.
func (m *Daemon) ProbeNow() { m.d.ProbeNow() }

// shareTimeout bounds TestShare: a server that does not answer is a
// failed test, not a stuck screen.
const shareTimeout = 30 * time.Second

// TestShare tries a share before it is saved: connects, logs in with
// password and looks at its folder. shareJSON is a share's fields as the
// config has them ({"Name": "nas", "Type": "smb", "Host": "nas.example",
// "Share": "home", "User": "user"}). It returns the room there, as JSON
// ({"Free": …, "Total": …}).
func (m *Daemon) TestShare(shareJSON, password string) (string, error) {
	var sh config.Share
	if err := json.Unmarshal([]byte(shareJSON), &sh); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), shareTimeout)
	defer cancel()
	space, err := shares.Test(ctx, &sh, password, store{m.passwords})
	if err != nil {
		return "", err
	}
	return toJSON(space), nil
}

// AddShare adds a share to the config, as TestShare takes it: SaveShare for
// a new one.
func (m *Daemon) AddShare(shareJSON, password string) error {
	_, err := m.SaveShare(shareJSON, password, true)
	return err
}

// SaveShare adds a share to the config (isNew), or changes the one by its
// name, as TestShare takes it; a share is not renamed. Its password goes into
// the app's password store first, as the TUI does: typed, it is stored;
// with none typed, an edit that moves the share to another server, share
// or user takes the stored one along. noPassword reports a share left with
// none, whose first run will need one. A new share by a name already there
// is an error, as is a change to one that is not.
func (m *Daemon) SaveShare(shareJSON, password string, isNew bool) (noPassword bool, err error) {
	var sh config.Share
	if err := json.Unmarshal([]byte(shareJSON), &sh); err != nil {
		return false, err
	}
	cfg, err := m.fileConfig()
	if err != nil {
		return false, err
	}
	old, had := cfg.Shares[sh.Name]
	switch {
	case isNew && had:
		return false, fmt.Errorf("there is a share named %s already", sh.Name)
	case !isNew && !had:
		return false, fmt.Errorf("there is no share named %s", sh.Name)
	}
	var was *config.Share
	if had {
		was = &old
	}
	none, err := shares.StorePassword(context.Background(), store{m.passwords}, was, &sh, password)
	if err != nil {
		return false, err
	}
	return none, m.edit(func(d *config.Doc) error { return d.SetShare(&sh) })
}

// ShareJSON is the named share's fields, as SaveShare takes them, from the
// config file.
func (m *Daemon) ShareJSON(name string) (string, error) {
	cfg, err := m.fileConfig()
	if err != nil {
		return "", err
	}
	sh, ok := cfg.Shares[name]
	if !ok {
		return "", fmt.Errorf("there is no share named %s", name)
	}
	return toJSON(sh), nil
}

// DeleteShare removes the named share from the config. One a shuttle uses
// is refused, with which: change or remove them first, as the TUI says. Its
// password stays in the store, where it harms nothing.
func (m *Daemon) DeleteShare(name string) error {
	cfg, err := m.fileConfig()
	if err != nil {
		return err
	}
	var users []string
	for i := range cfg.Shuttles {
		if cfg.Shuttles[i].Share == name {
			users = append(users, cfg.Shuttles[i].Name)
		}
	}
	if len(users) > 0 {
		return fmt.Errorf("%s is used by %s: change or remove them first", name, strings.Join(users, ", "))
	}
	return m.edit(func(d *config.Doc) error { return d.DeleteShare(name) })
}

// fileConfig is the config as the file has it now, which the daemon may
// not have reloaded yet: an edit just made is in it.
func (m *Daemon) fileConfig() (*config.Config, error) {
	data, err := os.ReadFile(m.configPath)
	if errors.Is(err, fs.ErrNotExist) {
		data = nil
	} else if err != nil {
		return nil, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return config.Parse(data, home)
}

// BrowseJSON lists dir, a slash path from the top of the named share ("/"
// for the top itself), as the wizard shows it: [{"Name": "Photos", "Dir":
// true}, …], folders first (shares.List). The share stays open for the
// next listing, until EndBrowse or a listing of another share.
func (m *Daemon) BrowseJSON(share, dir string) (string, error) {
	var list []shares.Entry
	err := m.browsed(share, func(ctx context.Context, f fsys.FS) (err error) {
		list, err = shares.List(ctx, f, dir)
		return err
	})
	if err != nil {
		return "", err
	}
	return toJSON(list), nil
}

// MakeShareFolder makes the folder name in dir, a slash path from the top
// of the named share, on the share BrowseJSON has open, and returns its
// path: the wizard opens it. A folder by that name already there is not an
// error. A name the share cannot hold is refused before anything is made.
func (m *Daemon) MakeShareFolder(share, dir, name string) (string, error) {
	name, err := folderName(name)
	if err != nil {
		return "", err
	}
	made := path.Join("/", dir, name)
	err = m.browsed(share, func(ctx context.Context, f fsys.FS) error {
		if err := fsys.RefuseName(f, name); err != nil {
			return err
		}
		return f.MkdirAll(ctx, strings.TrimPrefix(made, "/"))
	})
	switch {
	case errors.Is(err, fsys.ErrRefusedName):
		return "", fmt.Errorf("the share cannot hold that name: %s", strings.TrimPrefix(err.Error(), fsys.ErrRefusedName.Error()+": "))
	case err != nil:
		return "", err
	}
	return made, nil
}

// browsed does do on the named share, kept open for the wizard between
// calls until EndBrowse or a call on another share.
func (m *Daemon) browsed(share string, do func(context.Context, fsys.FS) error) error {
	b := &m.browse
	b.mu.Lock()
	defer b.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), shareTimeout)
	defer cancel()
	reused := b.fs != nil && b.share == share
	if !reused {
		if err := m.openBrowse(ctx, share); err != nil {
			return err
		}
	}
	err := do(ctx, b.fs)
	if err != nil && reused && !errors.Is(err, fsys.ErrRefusedName) {
		// The connection may have gone stale while it sat, as the phone
		// slept: once more, connected anew.
		if err := m.openBrowse(ctx, share); err != nil {
			return err
		}
		err = do(ctx, b.fs)
	}
	return err
}

// openBrowse opens the named share for BrowseJSON, as the config file has
// it now, closing what was open. The caller holds m.browse.mu.
func (m *Daemon) openBrowse(ctx context.Context, share string) error {
	b := &m.browse
	if b.fs != nil {
		b.fs.Close()
		b.fs, b.share = nil, ""
	}
	cfg, err := m.fileConfig()
	if err != nil {
		return err
	}
	sh, ok := cfg.Shares[share]
	if !ok {
		return fmt.Errorf("there is no share named %s", share)
	}
	f, err := shares.OpenShare(ctx, &sh, ".", store{m.passwords})
	if err != nil {
		return err
	}
	b.fs, b.share = f, share
	return nil
}

// EndBrowse closes the share BrowseJSON has open, if any: the wizard is done
// with it.
func (m *Daemon) EndBrowse() {
	b := &m.browse
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.fs != nil {
		b.fs.Close()
		b.fs, b.share = nil, ""
	}
}

// newShuttle is what AddShuttle takes: the least a shuttle needs.
type newShuttle struct {
	Name, Share string
	Folder      string // on the share; "" for its top
	Local       string // the folder on the phone
	Mode        config.Mode
	Triggers    []config.Trigger // none: it runs when asked
	Interval    string           // with the schedule trigger, e.g. "1h"
}

// AddShuttle adds a shuttle to the config, with everything else from the
// defaults. shuttleJSON: {"Name": "photos", "Share": "nas", "Folder":
// "Photos", "Local": "/storage/emulated/0/DCIM", "Mode": "push",
// "Triggers": ["realtime"]}. A shuttle by that name already there is an
// error; so is a config that would not load, such as two shuttles on one
// folder.
//
// The folder on the share is made if it is not there: the user named it.
// A run never makes it (a missing folder is more often a share that is not
// what it seems), so without this the shuttle's first run would fail. A
// share that cannot be reached saves nothing, and says why.
func (m *Daemon) AddShuttle(shuttleJSON string) error {
	var s newShuttle
	if err := json.Unmarshal([]byte(shuttleJSON), &s); err != nil {
		return err
	}
	if err := m.makeFolder(s.Share, s.Folder); err != nil {
		return err
	}
	table := []string{"shuttles", s.Name}
	return m.edit(func(d *config.Doc) error {
		if d.HasTable(table) {
			return fmt.Errorf("there is a shuttle named %s already", s.Name)
		}
		set := func(key string, v any) error { return d.Set(table, key, v) }
		if err := set("mode", string(s.Mode)); err != nil {
			return err
		}
		if err := set("local", s.Local); err != nil {
			return err
		}
		if err := set("remote", s.Share+":"+s.Folder); err != nil {
			return err
		}
		if len(s.Triggers) > 0 {
			triggers := make([]string, len(s.Triggers))
			for i, t := range s.Triggers {
				triggers[i] = string(t)
			}
			if err := set("triggers", triggers); err != nil {
				return err
			}
		}
		if s.Interval != "" {
			return set("interval", s.Interval)
		}
		return nil
	})
}

// makeFolder makes folder on the named share, as the config file has it
// now (AddShare may have just written it), if it is not there yet.
func (m *Daemon) makeFolder(share, folder string) error {
	folder = strings.Trim(folder, "/")
	if folder == "" {
		return nil
	}
	cfg, err := m.fileConfig()
	if err != nil {
		return err
	}
	sh, ok := cfg.Shares[share]
	if !ok {
		return fmt.Errorf("there is no share named %s", share)
	}
	ctx, cancel := context.WithTimeout(context.Background(), shareTimeout)
	defer cancel()
	f, err := shares.OpenShare(ctx, &sh, ".", store{m.passwords})
	if err != nil {
		return err
	}
	defer f.Close()
	return f.MkdirAll(ctx, folder)
}

// edit changes the config. The daemon applies it in the background, and the
// app hears when it has (Listener.OnReload), with the new shuttle's status
// after it.
func (m *Daemon) edit(change func(*config.Doc) error) error { return m.d.EditConfig(change) }

// detail is the selected shuttle in full, as the TUI's panel shows it and in
// its words (package present).
type detail struct {
	Name, Mode, Arrow, Profile string
	Local, Remote              string // the folder on the phone; the share's, as "nas:/Photos"
	LocalRoom, RemoteRoom      string // "1.2 TB free of 4.0 TB"; "" until known
	LocalLow, RemoteLow        bool   // under a tenth left
	Syncs, Runs, LastRun, Now  string
	Waiting, Attention         string // why it waits for a permission; what needs the user
	Errors, Refused            []engine.Issue
	Enabled                    bool
}

// DetailJSON is the named shuttle in full, as JSON, for the screen it opens
// to in the app.
func (m *Daemon) DetailJSON(name string) (string, error) {
	var st *daemon.Status
	all := m.d.Statuses()
	for i := range all {
		if all[i].Name == name {
			st = &all[i]
		}
	}
	if st == nil {
		return "", fmt.Errorf("%q: %w", name, daemon.ErrUnknownShuttle)
	}
	d := detail{Name: name, Mode: string(st.Mode), Arrow: present.ModeArrow(st.Mode),
		Now: present.Progress(&st.Progress), Waiting: st.Waiting, Attention: present.Attention(st),
		Errors: st.Errors, Refused: st.Refused, Enabled: st.State != daemon.StateDisabled}
	if st.LocalSpace.Total > 0 {
		d.LocalRoom, d.LocalLow = present.Room(st.LocalSpace), present.Low(st.LocalSpace)
	}
	if st.RemoteSpace.Total > 0 {
		d.RemoteRoom, d.RemoteLow = present.Room(st.RemoteSpace), present.Low(st.RemoteSpace)
	}
	now := time.Now()
	if !st.LastRun.IsZero() {
		d.LastRun = present.Ago(st.LastRun, now) + ": " + present.LastRun(st)
	}
	cfg := m.d.Config()
	for i := range cfg.Shuttles {
		s := &cfg.Shuttles[i]
		if s.Name != name {
			continue
		}
		d.Profile, d.Local = s.Profile, s.Local
		d.Remote = s.Share + ":/" + strings.TrimPrefix(strings.TrimPrefix(s.Remote, "."), "/")
		d.Syncs = present.Selection(&s.Settings)
		d.Runs = present.Runs(&s.Settings)
		if next := present.Next(st, now); next != "" {
			d.Runs += ", " + next
		}
	}
	if st.State == daemon.StateDisabled {
		d.Runs = "never: it is turned off"
	}
	return toJSON(d), nil
}

// setting is one row of a shuttle's settings, as the TUI's editor and
// GNOME's preferences have it (config.RowState), less what JSON cannot hold.
type setting struct {
	Key, Label, Hint string
	Choices          []string // a choice among these; none: typed text
	List, Number     bool     // typed as a comma-separated list; a number
	Value            string   // the shuttle's own; "" when it sets none
	Inherited, From  string   // what applies otherwise, and whence: "profile phone", "default"
	Off              string   // why the row does not apply as things stand; "" when it does
}

// SettingsJSON is every setting of the named shuttle, as a JSON array of
// rows, from the config file as it is on disk.
func (m *Daemon) SettingsJSON(name string) (string, error) {
	rows, err := m.d.ShuttleSettings(name)
	if err != nil {
		return "", err
	}
	return settingsJSON(rows), nil
}

func settingsJSON(rows []config.RowState) string {
	out := make([]setting, len(rows))
	for i := range rows {
		r := &rows[i]
		out[i] = setting{Key: r.Key, Label: r.Label, Hint: r.Hint, Choices: r.Choices, List: r.List, Number: r.Number,
			Value: r.Value, Inherited: r.Inherited, From: r.From, Off: r.Off}
	}
	return toJSON(out)
}

// SetSetting sets one of the named shuttle's settings, the text as its row
// takes it; "" removes the shuttle's own, so its profile's or the default
// applies again. Only that key changes in the config file; one the row
// refuses, or that does not apply, is an error saying why.
func (m *Daemon) SetSetting(name, key, text string) error {
	return m.d.SetShuttleSetting(name, key, text)
}

// Resolve settles a conflict the named shuttle holds at path, as the user
// chose: "keep-local", "keep-remote" or "keep-both". It waits for a run in
// progress to finish its file, so the app calls it off the main thread.
func (m *Daemon) Resolve(name, path, choice string) error {
	ctx, cancel := context.WithTimeout(context.Background(), resolveTimeout)
	defer cancel()
	return m.d.Resolve(ctx, name, path, engine.Choice(choice))
}

// AllowDeletions lets the named shuttle's next run delete what the deletion
// guard has been holding back, and starts that run.
func (m *Daemon) AllowDeletions(name string) error { return m.d.AllowDeletions(name) }

// resolveTimeout bounds Resolve: a run in progress finishes its file first.
const resolveTimeout = 2 * time.Minute

// shareRow is one share as the Shares tab lists it, as the TUI's does.
type shareRow struct {
	Name, Type, Where string
	UsedBy            []string // the shuttles on it
	State             string   // "online" while any of its shuttles reaches it, "offline", or "" before a look
	Room              string   // "1.2 TB free of 4.0 TB" from its shuttles' latest run; "" until known
	Low               bool     // under a tenth left
}

// SharesJSON is every share in the config, by name, as a JSON array.
func (m *Daemon) SharesJSON() string {
	cfg := m.d.Config()
	statuses := map[string]*daemon.Status{}
	all := m.d.Statuses()
	for i := range all {
		statuses[all[i].Name] = &all[i]
	}
	out := []shareRow{}
	for _, name := range slices.Sorted(maps.Keys(cfg.Shares)) {
		sh := cfg.Shares[name]
		r := shareRow{Name: name, Type: string(sh.Type), Where: present.Where(&sh), UsedBy: []string{}}
		var room fsys.Space
		var at time.Time
		for i := range cfg.Shuttles {
			s := &cfg.Shuttles[i]
			if s.Share != name {
				continue
			}
			r.UsedBy = append(r.UsedBy, s.Name)
			st := statuses[s.Name]
			if st == nil {
				continue
			}
			switch {
			case st.Share == daemon.ShareOnline:
				r.State = daemon.ShareOnline
			case st.Share == daemon.ShareOffline && r.State == "":
				r.State = daemon.ShareOffline
			}
			if st.RemoteSpace.Total > 0 && st.LastRun.After(at) {
				room, at = st.RemoteSpace, st.LastRun
			}
		}
		if room.Total > 0 {
			r.Room, r.Low = present.Room(room), present.Low(room)
		}
		out = append(out, r)
	}
	return toJSON(out)
}

// profileRow is one profile as the Profiles tab lists it, as the TUI's does.
type profileRow struct {
	Name, Mode, Triggers string // as the profile sets them; Triggers "manual only" when it sets none
	UsedBy               []string
}

// ProfilesJSON is every profile in the config file, by name, as a JSON array.
func (m *Daemon) ProfilesJSON() (string, error) {
	data, err := os.ReadFile(m.configPath)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	cfg, err := m.fileConfig()
	if err != nil {
		return "", err
	}
	out := []profileRow{}
	for _, name := range cfg.Profiles {
		raw, err := config.RawTable(data, "profiles", name)
		if err != nil {
			return "", err
		}
		r := profileRow{Name: name, Mode: config.RawText(raw["mode"]), Triggers: config.RawText(raw["triggers"]), UsedBy: []string{}}
		if r.Triggers == "" {
			r.Triggers = "manual only"
		}
		for i := range cfg.Shuttles {
			if cfg.Shuttles[i].Profile == name {
				r.UsedBy = append(r.UsedBy, cfg.Shuttles[i].Name)
			}
		}
		out = append(out, r)
	}
	return toJSON(out), nil
}

// ProfileSettingsJSON is every setting of the named profile, as SettingsJSON
// has a shuttle's: where the profile sets nothing, the default applies.
func (m *Daemon) ProfileSettingsJSON(name string) (string, error) {
	rows, err := m.d.ProfileSettings(name)
	if err != nil {
		return "", err
	}
	return settingsJSON(rows), nil
}

// SetProfileSetting sets one of the named profile's settings, as SetSetting
// does a shuttle's; its shuttles follow it, except where one sets the key
// itself.
func (m *Daemon) SetProfileSetting(name, key, text string) error {
	return m.d.SetProfileSetting(name, key, text)
}

// NewProfile adds a profile with its mode: a profile that set nothing would
// not be written, as the TUI says. Its other settings are set after, one by
// one.
func (m *Daemon) NewProfile(name, mode string) error {
	if err := m.freeProfileName(name); err != nil {
		return err
	}
	return m.edit(func(d *config.Doc) error { return d.Set([]string{"profiles", name}, "mode", mode) })
}

// DuplicateProfile copies the profile from, every setting as written, as a
// new profile named to: the TUI's copy. No shuttle uses the copy yet.
func (m *Daemon) DuplicateProfile(from, to string) error {
	if err := m.freeProfileName(to); err != nil {
		return err
	}
	data, err := os.ReadFile(m.configPath)
	if err != nil {
		return err
	}
	raw, err := config.RawTable(data, "profiles", from)
	switch {
	case err != nil:
		return err
	case raw == nil:
		return fmt.Errorf("there is no profile named %s", from)
	case len(raw) == 0:
		return fmt.Errorf("%s sets nothing: there is nothing to copy", from)
	}
	value := func(key string) string { return config.RawText(config.Lookup(raw, key)) }
	return m.edit(func(d *config.Doc) error {
		return config.WriteRows(d, []string{"profiles", to}, value, func(string) string { return "" })
	})
}

// DeleteProfile removes the named profile from the config. One a shuttle
// uses is refused, with which: change them first, as the TUI says.
func (m *Daemon) DeleteProfile(name string) error {
	cfg, err := m.fileConfig()
	if err != nil {
		return err
	}
	if !slices.Contains(cfg.Profiles, name) {
		return fmt.Errorf("there is no profile named %s", name)
	}
	var users []string
	for i := range cfg.Shuttles {
		if cfg.Shuttles[i].Profile == name {
			users = append(users, cfg.Shuttles[i].Name)
		}
	}
	if len(users) > 0 {
		return fmt.Errorf("%s is used by %s: change them first", name, strings.Join(users, ", "))
	}
	return m.edit(func(d *config.Doc) error { return d.DeleteTable([]string{"profiles", name}) })
}

// freeProfileName refuses a new profile's name when it is empty or taken.
func (m *Daemon) freeProfileName(name string) error {
	if name == "" {
		return errors.New("the profile needs a name")
	}
	cfg, err := m.fileConfig()
	if err != nil {
		return err
	}
	if slices.Contains(cfg.Profiles, name) {
		return fmt.Errorf("there is a profile named %s already", name)
	}
	return nil
}

// SyncNow asks for a run of a shuttle.
func (m *Daemon) SyncNow(name string) error { return m.d.SyncNow(name) }

// SetPaused pauses or resumes a shuttle, or every shuttle when name is "".
func (m *Daemon) SetPaused(name string, paused bool) error { return m.d.SetPaused(name, paused) }

// SetEnabled turns a shuttle on or off in the config.
func (m *Daemon) SetEnabled(name string, on bool) error { return m.d.SetEnabled(name, on) }

// DeleteShuttle removes the named shuttle from the config, as the TUI's
// does: its files stay where they are, on both sides. The daemon follows
// the file, and the app forgets its pause with it.
func (m *Daemon) DeleteShuttle(name string) error {
	return m.edit(func(d *config.Doc) error {
		table := []string{"shuttles", name}
		if !d.HasTable(table) {
			return fmt.Errorf("there is no shuttle named %s", name)
		}
		return d.DeleteTable(table)
	})
}

// SetPermissions says which permissions the user has given now, as the app
// finds them each time it comes to the foreground: shuttles waiting for one
// run what was pending, and ones that lost one stop after their file.
func (m *Daemon) SetPermissions(filesAccess, localNetwork bool) {
	m.access.files.Store(filesAccess)
	m.access.network.Store(localNetwork)
	m.d.Recheck()
}

// SetMetered says whether the phone's connection is metered.
func (m *Daemon) SetMetered(metered bool) { m.d.SetMetered(metered) }

// Reload reads the config file again now.
func (m *Daemon) Reload() error { return m.d.Reload() }

// SyncAndWait asks for a run of a shuttle, as SyncNow does, and waits up to
// timeoutMillis for it to end. It returns the shuttle's status after the
// run, as JSON. A WorkManager worker uses it to hold its job open.
func (m *Daemon) SyncAndWait(name string, timeoutMillis int64) (string, error) {
	start := time.Now()
	ended := make(chan string, 1)
	stop := m.d.Watch(func(st *daemon.Status) {
		if st.Name == name && st.State != daemon.StateRunning && st.LastRun.After(start) {
			select {
			case ended <- toJSON(st):
			default: // a later run's end: the first is the one asked for
			}
		}
	})
	defer stop()
	if err := m.d.SyncNow(name); err != nil {
		return "", err
	}
	select {
	case st := <-ended:
		return st, nil
	case <-time.After(time.Duration(timeoutMillis) * time.Millisecond):
		return "", errors.New(name + ": the run did not end in time")
	}
}

func toJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		// Only unsupported types fail, and every type passed here is the
		// daemon's own: a test marshals each.
		panic(err)
	}
	return string(b)
}

// store is the app's Passwords as the daemon's secrets.Store.
type store struct{ p Passwords }

func (s store) Get(_ context.Context, k secrets.Key) (string, error) {
	pw, err := s.p.Get(k.Account())
	if err != nil {
		return "", err
	}
	if pw == "" {
		return "", secrets.ErrNotFound
	}
	return pw, nil
}

func (s store) Set(_ context.Context, k secrets.Key, password string) error {
	return s.p.Set(k.Account(), password)
}

func (s store) Delete(_ context.Context, k secrets.Key) error {
	return s.p.Delete(k.Account())
}
