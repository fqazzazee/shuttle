// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package mobile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fqazzazee/shuttle/internal/activity"
	"github.com/fqazzazee/shuttle/internal/config"
	"github.com/fqazzazee/shuttle/internal/daemon"
	"github.com/fqazzazee/shuttle/internal/engine"
	"github.com/fqazzazee/shuttle/internal/fsys"
	"github.com/fqazzazee/shuttle/internal/secrets"
)

// passwords is the app's store, in memory.
type passwords struct {
	mu sync.Mutex
	m  map[string]string
}

func (p *passwords) Get(account string) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.m[account], nil
}

func (p *passwords) Set(account, password string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.m[account] = password
	return nil
}

func (p *passwords) Delete(account string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.m, account)
	return nil
}

// listener records what the daemon tells the app.
type listener struct {
	mu       sync.Mutex
	statuses []string
	events   []string
}

func (l *listener) OnStatus(s string) { l.mu.Lock(); l.statuses = append(l.statuses, s); l.mu.Unlock() }
func (l *listener) OnEvent(e string)  { l.mu.Lock(); l.events = append(l.events, e); l.mu.Unlock() }

func (l *listener) heard() (statuses, events []string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.statuses), slices.Clone(l.events)
}

type fixture struct {
	local, share string
	opts         Options
	listener     *listener
}

// newFixture writes a config with one shuttle, s, from a local folder to a
// "path" share, both temp folders.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	f := &fixture{local: t.TempDir(), share: t.TempDir(), listener: &listener{}}
	f.opts = Options{
		ConfigPath: filepath.Join(dir, "config.toml"),
		StateDir:   filepath.Join(dir, "state"),
		DeviceName: "Test Phone",
		Passwords:  &passwords{m: map[string]string{}},
		Listener:   f.listener,
		// A "path" share in a temp folder needs neither permission; given
		// anyway, as the app does once the user has.
		FilesAccess:  true,
		LocalNetwork: true,
	}
	doc := fmt.Sprintf("[shares.mnt]\ntype = \"path\"\npath = %q\n\n[shuttles.s]\nmode = \"push\"\nlocal = %q\nremote = \"mnt:\"\n",
		f.share, f.local)
	if err := os.WriteFile(f.opts.ConfigPath, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *fixture) start(t *testing.T) *Daemon {
	t.Helper()
	m, err := Start(&f.opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if Running() == m {
			m.Stop()
		}
	})
	return m
}

func TestSyncAndWait(t *testing.T) {
	f := newFixture(t)
	if err := os.WriteFile(filepath.Join(f.local, "a.txt"), []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := f.start(t)

	out, err := m.SyncAndWait("s", 10_000)
	if err != nil {
		t.Fatal(err)
	}
	var st daemon.Status
	if err := json.Unmarshal([]byte(out), &st); err != nil {
		t.Fatal(err)
	}
	if st.Name != "s" || st.State != daemon.StateIdle || st.Copied != 1 {
		t.Errorf("status = %+v", st)
	}
	if b, err := os.ReadFile(filepath.Join(f.share, "a.txt")); err != nil || string(b) != "a" {
		t.Errorf("share's a.txt = %q, %v", b, err)
	}

	// What the app heard, and can read back.
	statuses, events := f.listener.heard()
	if len(statuses) == 0 || !strings.Contains(statuses[len(statuses)-1], `"Name":"s"`) {
		t.Errorf("statuses heard: %q", statuses)
	}
	if !slices.ContainsFunc(events, func(e string) bool { return strings.Contains(e, `"Kind":"completed"`) }) {
		t.Errorf("events heard: %q, want the run's completion", events)
	}
	var all []daemon.Status
	if err := json.Unmarshal([]byte(m.StatusesJSON()), &all); err != nil || len(all) != 1 || all[0].Copied != 1 {
		t.Errorf("StatusesJSON = %s, %v", m.StatusesJSON(), err)
	}
	if m.RealTime() {
		t.Error("RealTime for a shuttle with no triggers")
	}
	if m.State() != "idle" || m.ConfigError() != "" {
		t.Errorf("State = %q, ConfigError = %q", m.State(), m.ConfigError())
	}
	act, err := m.ActivityJSON("s", 0)
	if err != nil {
		t.Fatal(err)
	}
	var es []activity.Entry
	if err := json.Unmarshal([]byte(act), &es); err != nil || len(es) == 0 || es[0].Kind != activity.KindRun {
		t.Errorf("ActivityJSON = %s, %v", act, err)
	}
}

func TestSyncAndWaitGivesUp(t *testing.T) {
	m := newFixture(t).start(t)
	if err := m.SetPaused("s", true); err != nil {
		t.Fatal(err)
	}
	// A paused shuttle runs when resumed: the wait ends first.
	if _, err := m.SyncAndWait("s", 200); err == nil || !strings.Contains(err.Error(), "did not end in time") {
		t.Errorf("err = %v", err)
	}
	if _, err := m.SyncAndWait("nope", 200); !errors.Is(err, daemon.ErrUnknownShuttle) {
		t.Errorf("unknown shuttle: err = %v", err)
	}
}

func TestOneDaemonPerProcess(t *testing.T) {
	f := newFixture(t)
	if Running() != nil {
		t.Fatal("a daemon runs before Start")
	}
	m := f.start(t)
	if Running() != m {
		t.Error("Running is not the started daemon")
	}
	if _, err := Start(&f.opts); !errors.Is(err, errRunning) {
		t.Errorf("second Start: err = %v, want errRunning", err)
	}
	if err := m.Stop(); err != nil {
		t.Fatal(err)
	}
	if Running() != nil {
		t.Error("Running after Stop")
	}
	f.start(t) // and again, as after the service restarts
}

func TestStartFails(t *testing.T) {
	f := newFixture(t)
	if err := os.WriteFile(f.opts.ConfigPath, []byte("[shuttles.s]\nmode = 3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Start(&f.opts); !errors.Is(err, daemon.ErrConfig) {
		t.Errorf("err = %v, want daemon.ErrConfig", err)
	}
	if Running() != nil {
		t.Error("a failed Start left a running daemon")
	}
}

func TestDaemonOptions(t *testing.T) {
	f := newFixture(t)
	do := daemonOptions(&f.opts, &access{}, nil)
	if do.DeviceName != "Test Phone" || do.Paths.Config != f.opts.ConfigPath || do.Paths.StateDir != f.opts.StateDir {
		t.Errorf("options = %+v", do)
	}
	if do.LocalNames == nil || do.Waiting == nil || do.OnChange == nil || do.OnEvent == nil {
		t.Error("LocalNames, Waiting, OnChange or OnEvent not set")
	}
	f.opts.Listener = nil
	if do := daemonOptions(&f.opts, &access{}, nil); do.OnChange != nil || do.OnEvent != nil {
		t.Error("callbacks set without a listener")
	}
}

func TestLocalNames(t *testing.T) {
	for dir, shared := range map[string]bool{
		"/storage/emulated/0/Music":           true,
		"/storage/emulated/0":                 true,
		"/storage/1A2B-3C4D/DCIM":             true, // an SD card
		"/sdcard/Documents":                   true,
		"/sdcard":                             true,
		"/storage/emulated/0/../0/x":          true,
		"/data/user/0/io.example.app/files/x": false,
		"/storagebox/x":                       false,
		"/data/../storage/emulated/0":         true,
	} {
		got := localNames(dir)
		if (got.Fold != nil) != shared || (got.Refuse != nil) != shared {
			t.Errorf("localNames(%q): shared storage %v, want %v", dir, got.Fold != nil, shared)
		}
	}
}

func TestPasswords(t *testing.T) {
	p := &passwords{m: map[string]string{}}
	s := store{p}
	ctx := context.Background()
	k := secrets.Key{Host: "nas.example", Share: "home", User: "user"}
	if _, err := s.Get(ctx, k); !errors.Is(err, secrets.ErrNotFound) {
		t.Errorf("Get of nothing: err = %v, want ErrNotFound", err)
	}
	if err := s.Set(ctx, k, "fake-pw"); err != nil {
		t.Fatal(err)
	}
	if p.m["smb://user@nas.example/home"] != "fake-pw" {
		t.Errorf("app's store = %v, want the password under the key's account", p.m)
	}
	if pw, err := s.Get(ctx, k); err != nil || pw != "fake-pw" {
		t.Errorf("Get = %q, %v", pw, err)
	}
	if err := s.Delete(ctx, k); err != nil || len(p.m) != 0 {
		t.Errorf("Delete: %v, store %v", err, p.m)
	}
	failing := store{failingPasswords{}}
	if _, err := failing.Get(ctx, k); err == nil || errors.Is(err, secrets.ErrNotFound) {
		t.Errorf("a failing store: err = %v, want its own error", err)
	}
}

type failingPasswords struct{}

func (failingPasswords) Get(string) (string, error) { return "", errors.New("keystore locked") }
func (failingPasswords) Set(string, string) error   { return errors.New("keystore locked") }
func (failingPasswords) Delete(string) error        { return errors.New("keystore locked") }

func TestLogIsSetAside(t *testing.T) {
	f := newFixture(t)
	if err := os.MkdirAll(f.opts.StateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(f.opts.StateDir, "daemon.log")
	if err := os.WriteFile(name, make([]byte, maxLog+1), 0o600); err != nil {
		t.Fatal(err)
	}
	f.start(t).Stop()
	if info, err := os.Stat(name + ".1"); err != nil || info.Size() != maxLog+1 {
		t.Errorf("daemon.log.1: %v, %v", info, err)
	}
	if info, err := os.Stat(name); err != nil || info.Size() == 0 || info.Size() > maxLog {
		t.Errorf("daemon.log: %v, %v; want a fresh log", info, err)
	}
}

// The JSON the app parses: every type crosses with its fields, including
// the nested ones, so a change to them shows up here.
func TestJSON(t *testing.T) {
	info := &fsys.FileInfo{Name: "a.txt", Kind: fsys.KindFile, Size: 1, ModTime: time.Unix(0, 0).UTC()}
	issue := engine.Issue{Path: "a.txt", Op: engine.OpConflict, Reason: "r", Outcome: "held", Local: info, Remote: info}
	for _, tt := range []struct {
		v    any
		keys []string
	}{
		{daemon.Status{Conflicts: []engine.Issue{issue}},
			[]string{`"Name"`, `"State"`, `"Paused"`, `"Metered"`, `"Waiting"`, `"LastRun"`, `"Copied"`, `"Conflicts":[{"Path":"a.txt","Op":9,`,
				`"Local":{"Name":"a.txt","Kind":0,"Size":1,"ModTime":"1970-01-01T00:00:00Z"}`, `"Refused"`, `"Progress"`}},
		{daemon.Event{Kind: daemon.EventCompleted, Shuttle: "s"}, []string{`"Kind":"completed"`, `"Shuttle":"s"`}},
		{[]activity.Entry{{Kind: activity.KindLeftOut, Path: "a:b", Text: "t"}}, []string{`"kind":"left-out"`, `"path":"a:b"`, `"time"`}},
	} {
		got := toJSON(tt.v)
		for _, k := range tt.keys {
			if !strings.Contains(got, k) {
				t.Errorf("%T as JSON lacks %s:\n%s", tt.v, k, got)
			}
		}
	}
}

func TestWaiting(t *testing.T) {
	cfg := &config.Config{Shares: map[string]config.Share{
		"nas":  {Type: config.ShareSMB, Host: "nas.example"},
		"nfs":  {Type: config.ShareNFS3, Host: "nas.example"},
		"card": {Type: config.SharePath, Path: "/storage/1A2B-3C4D"},
	}}
	tests := []struct {
		local, share   string
		files, network bool
		want           string
	}{
		{"/storage/emulated/0/Music", "nas", true, true, ""},
		{"/storage/emulated/0/Music", "nas", false, true, NeedsFilesAccess},
		{"/storage/emulated/0/Music", "nas", false, false, NeedsFilesAccess},
		{"/storage/emulated/0/Music", "nas", true, false, NeedsLocalNetwork},
		{"/storage/emulated/0/Music", "nfs", true, false, NeedsLocalNetwork},
		{"/storage/emulated/0/Music", "card", true, false, ""},      // a path share is not on the network
		{"/data/user/0/io.example/files/x", "nas", false, true, ""}, // private storage needs no permission
	}
	for _, tt := range tests {
		a := &access{}
		a.files.Store(tt.files)
		a.network.Store(tt.network)
		if got := a.waiting(cfg, &config.Shuttle{Local: tt.local, Share: tt.share}); got != tt.want {
			t.Errorf("%s on %s, files %v, network %v: %q, want %q", tt.local, tt.share, tt.files, tt.network, got, tt.want)
		}
	}
}

// Through the binding: a shuttle waits for a permission, says so, and runs
// what was asked once the app reports it given.
func TestSetPermissions(t *testing.T) {
	f := newFixture(t)
	if err := os.WriteFile(filepath.Join(f.local, "a.txt"), []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The fixture's share is a "path" one: make it one on the network, at a
	// name that never resolves (RFC 2606), so the run that follows reaches
	// no server.
	doc, err := os.ReadFile(f.opts.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	doc = []byte(strings.Replace(string(doc), `type = "path"`+"\npath = ", `type = "nfs3"`+"\nhost = \"nas.invalid\"\nexport = ", 1))
	if err := os.WriteFile(f.opts.ConfigPath, doc, 0o600); err != nil {
		t.Fatal(err)
	}
	f.opts.LocalNetwork = false
	m := f.start(t)
	if err := m.SyncNow("s"); err != nil {
		t.Fatal(err)
	}
	var all []daemon.Status
	if err := json.Unmarshal([]byte(m.StatusesJSON()), &all); err != nil || all[0].Waiting != NeedsLocalNetwork || all[0].State != daemon.StatePaused {
		t.Errorf("statuses = %s, want s waiting for local network access", m.StatusesJSON())
	}
	m.SetPermissions(true, true)
	if err := json.Unmarshal([]byte(m.StatusesJSON()), &all); err != nil || all[0].Waiting != "" {
		t.Errorf("after SetPermissions: %s", m.StatusesJSON())
	}
}
