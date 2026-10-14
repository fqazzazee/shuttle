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
	"github.com/fqazzazee/shuttle/internal/fsys"
	"github.com/fqazzazee/shuttle/internal/secrets"
)

// Passwords is the app's password store, written in Kotlin over the Android
// Keystore. Each password is keyed by the server it opens, as an account
// like "smb://user@nas.example/home"; Get returns "" when there is none.
type Passwords interface {
	Get(account string) (string, error)
	Set(account, password string) error
	Delete(account string) error
}

// Listener hears from the daemon: a shuttle's status after each change, and
// what is worth a notification, each as JSON. It is called on the daemon's
// goroutines and must not block.
type Listener interface {
	OnStatus(statusJSON string)
	OnEvent(eventJSON string)
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
	d      *daemon.Daemon
	cancel context.CancelFunc
	done   chan error
	log    *os.File
	access *access
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
	d, err := daemon.New(daemonOptions(o, acc, slog.New(slog.NewTextHandler(logf, nil))))
	if err != nil {
		logf.Close()
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	m := &Daemon{d: d, cancel: cancel, done: make(chan error, 1), log: logf, access: acc}
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

func daemonOptions(o *Options, acc *access, log *slog.Logger) *daemon.Options {
	do := &daemon.Options{
		Paths:      daemon.Paths{Config: o.ConfigPath, StateDir: o.StateDir},
		Store:      store{o.Passwords},
		Log:        log,
		DeviceName: o.DeviceName,
		LocalNames: localNames,
		Waiting:    acc.waiting,
	}
	if l := o.Listener; l != nil {
		do.OnChange = func(st *daemon.Status) { l.OnStatus(toJSON(st)) }
		do.OnEvent = func(e *daemon.Event) { l.OnEvent(toJSON(e)) }
	}
	return do
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
