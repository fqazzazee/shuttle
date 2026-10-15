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
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

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

// ActivityJSON is the newest entries of a shuttle's activity log, or of
// every shuttle's when name is "", newest first: at most limit (0: all).
func (m *Daemon) ActivityJSON(name string, limit int) (string, error) {
	es, err := m.d.Activity(name, limit)
	if err != nil {
		return "", err
	}
	return toJSON(es), nil
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

// AddShare adds a share to the config, as TestShare takes it, after storing
// its password in the app's password store when one is given: a share with
// no password would fail its first run, a password with no share harms
// nothing. A share by that name already there is an error.
func (m *Daemon) AddShare(shareJSON, password string) error {
	var sh config.Share
	if err := json.Unmarshal([]byte(shareJSON), &sh); err != nil {
		return err
	}
	if k, needs := shares.Key(&sh); needs && password != "" {
		if err := (store{m.passwords}).Set(context.Background(), k, password); err != nil {
			return err
		}
	}
	return m.edit(func(d *config.Doc) error {
		if d.HasTable([]string{"shares", sh.Name}) {
			return fmt.Errorf("there is a share named %s already", sh.Name)
		}
		return d.SetShare(&sh)
	})
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
	data, err := os.ReadFile(m.configPath)
	if err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	cfg, err := config.Parse(data, home)
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
		Errors: st.Errors, Refused: st.Refused}
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

// SyncNow asks for a run of a shuttle.
func (m *Daemon) SyncNow(name string) error { return m.d.SyncNow(name) }

// SetPaused pauses or resumes a shuttle, or every shuttle when name is "".
func (m *Daemon) SetPaused(name string, paused bool) error { return m.d.SetPaused(name, paused) }

// SetEnabled turns a shuttle on or off in the config.
func (m *Daemon) SetEnabled(name string, on bool) error { return m.d.SetEnabled(name, on) }

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
