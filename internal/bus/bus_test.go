// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package bus

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/fqazzazee/shuttle/internal/activity"
	"github.com/fqazzazee/shuttle/internal/config"
	"github.com/fqazzazee/shuttle/internal/daemon"
	"github.com/fqazzazee/shuttle/internal/dbustest"
	"github.com/fqazzazee/shuttle/internal/engine"
	"github.com/fqazzazee/shuttle/internal/fsys"
	"github.com/fqazzazee/shuttle/internal/secrets"
)

type noKeyring struct{}

func (noKeyring) Get(context.Context, secrets.Key) (string, error) { return "", secrets.ErrNotFound }
func (noKeyring) Set(context.Context, secrets.Key, string) error   { return nil }
func (noKeyring) Delete(context.Context, secrets.Key) error        { return nil }

// fixture is a daemon serving a "path" shuttle s, plus a client.
type fixture struct {
	t            *testing.T
	local, mount string
	d            *daemon.Daemon
	c            *Client
	connect      func() *dbus.Conn
	serveConn    *dbus.Conn // the daemon's own connection
	config       string     // the config file
}

func newFixture(t *testing.T, mode, extra string) *fixture {
	t.Helper()
	f := &fixture{t: t, local: t.TempDir(), mount: t.TempDir(), connect: dbustest.Bus(t)}
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.toml")
	doc := fmt.Sprintf("[shares.mnt]\ntype = \"path\"\npath = %q\n[shuttles.s]\nmode = %q\nlocal = %q\nremote = \"mnt:\"\n%s\n",
		f.mount, mode, f.local, extra)
	if err := os.WriteFile(cfg, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	f.config = cfg
	d, err := daemon.New(&daemon.Options{Paths: daemon.Paths{Config: cfg, StateDir: filepath.Join(dir, "state")}, Store: noKeyring{}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	runDone, serveDone := make(chan error, 1), make(chan error, 1)
	go func() { runDone <- d.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		<-serveDone
		<-runDone
	})
	<-d.Ready()
	f.serveConn = f.connect()
	go func() { serveDone <- Serve(ctx, f.serveConn, d) }()
	f.d, f.c = d, NewClient(f.connect())
	f.waitForName()
	return f
}

func (f *fixture) waitForName() {
	f.t.Helper()
	conn := f.connect()
	for range 200 {
		var has bool
		if conn.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, Name).Store(&has) == nil && has {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	f.t.Fatal("the daemon never took its bus name")
}

// waitFor reads the client's status stream until ok holds.
func (f *fixture) waitFor(changes <-chan daemon.Status, what string, ok func(*daemon.Status) bool) daemon.Status {
	f.t.Helper()
	timeout := time.After(10 * time.Second)
	for {
		select {
		case st := <-changes:
			if ok(&st) {
				return st
			}
		case <-timeout:
			f.t.Fatalf("timed out waiting for %s", what)
		}
	}
}

func ctx(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	return c
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func ran(st *daemon.Status) bool {
	return !st.LastRun.IsZero() && st.State != daemon.StateRunning && !st.Pending
}

func TestListAndSyncOverTheBus(t *testing.T) {
	f := newFixture(t, "push", "")
	write(t, f.local, "a.txt", "a")
	all, err := f.c.Shuttles(ctx(t))
	if err != nil || len(all) != 1 || all[0].Name != "s" || all[0].State != daemon.StateIdle {
		t.Fatalf("Shuttles = %+v, %v", all, err)
	}

	changes, err := f.c.Changes(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.c.SyncNow(ctx(t), "s"); err != nil {
		t.Fatal(err)
	}
	st := f.waitFor(changes, "the run's result", ran)
	if st.State != daemon.StateIdle || st.Copied != 1 || st.Files != 1 || st.Size != 1 {
		t.Errorf("status over the bus = %+v", st)
	}
	if _, err := os.Stat(filepath.Join(f.mount, "a.txt")); err != nil {
		t.Error("the run did not push")
	}
}

func TestErrorsCrossTheBus(t *testing.T) {
	f := newFixture(t, "push", "")
	if err := f.c.SyncNow(ctx(t), "nope"); !errors.Is(err, daemon.ErrUnknownShuttle) {
		t.Errorf("SyncNow(nope) = %v, want ErrUnknownShuttle", err)
	}
	if err := f.c.Resolve(ctx(t), "s", "a.txt", "keep-neither"); err == nil || !strings.Contains(err.Error(), "keep-local") {
		t.Errorf("Resolve with a bad choice = %v", err)
	}
	g := newFixture(t, "push", "enabled = false")
	if err := g.c.SyncNow(ctx(t), "s"); !errors.Is(err, daemon.ErrDisabled) {
		t.Errorf("SyncNow(disabled) = %v, want ErrDisabled", err)
	}
}

func TestPauseAndOverallState(t *testing.T) {
	f := newFixture(t, "push", "")
	if err := f.c.SetPaused(ctx(t), "", true); err != nil {
		t.Fatal(err)
	}
	state, paused, err := f.c.State(ctx(t))
	if err != nil || state != daemon.StatePaused || !paused {
		t.Errorf("State = %q, paused %v, %v", state, paused, err)
	}
	f.c.SetPaused(ctx(t), "", false)
	if state, paused, _ := f.c.State(ctx(t)); state != daemon.StateIdle || paused {
		t.Errorf("after resume: %q, paused %v", state, paused)
	}
}

// A held conflict is announced once with ConflictRaised, and resolving it
// over the bus settles it.
func TestConflictRaisedOnceThenResolved(t *testing.T) {
	f := newFixture(t, "sync", `conflicts = "ask"`)
	write(t, f.local, "a.txt", "base")
	changes, _ := f.c.Changes(ctx(t))
	f.c.SyncNow(ctx(t), "s")
	f.waitFor(changes, "the first sync", ran)

	raised := make(chan []any, 8)
	conn := f.connect()
	conn.AddMatchSignal(dbus.WithMatchInterface(Interface), dbus.WithMatchMember("ConflictRaised"))
	sigs := make(chan *dbus.Signal, 8)
	conn.Signal(sigs)
	go func() {
		for s := range sigs {
			if s.Name == Interface+".ConflictRaised" {
				raised <- s.Body
			}
		}
	}()

	write(t, f.local, "a.txt", "local edit")
	write(t, f.mount, "a.txt", "remote edit!")
	later := time.Now().Add(time.Minute)
	os.Chtimes(filepath.Join(f.mount, "a.txt"), later, later)
	for range 2 { // the second run holds the same conflict: no second announcement
		f.c.SyncNow(ctx(t), "s")
		f.waitFor(changes, "held", func(st *daemon.Status) bool { return ran(st) && st.State == daemon.StateHeld })
	}
	select {
	case body := <-raised:
		if body[0] != "s" || body[1] != "a.txt" {
			t.Errorf("ConflictRaised%v", body)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no ConflictRaised signal")
	}
	select {
	case body := <-raised:
		t.Errorf("ConflictRaised again for an old conflict: %v", body)
	case <-time.After(200 * time.Millisecond):
	}

	// Both copies are described, for the user to choose between.
	all, err := f.c.Shuttles(ctx(t))
	if err != nil || len(all) != 1 || len(all[0].Conflicts) != 1 {
		t.Fatalf("Shuttles = %+v, %v", all, err)
	}
	if c := all[0].Conflicts[0]; c.Local == nil || c.Local.Size != int64(len("local edit")) ||
		c.Remote == nil || c.Remote.Size != int64(len("remote edit!")) || !c.Remote.ModTime.Equal(later.Truncate(time.Second)) {
		t.Errorf("conflict copies: local %+v, remote %+v", c.Local, c.Remote)
	}

	// The overall state, the one icon in the top bar, shows it too.
	if state, _, _ := f.c.State(ctx(t)); state != daemon.StateHeld {
		t.Errorf("overall state = %q while a conflict is held, want held", state)
	}

	if err := f.c.Resolve(ctx(t), "s", "a.txt", engine.KeepLocal); err != nil {
		t.Fatal(err)
	}
	f.waitFor(changes, "the conflict gone", func(st *daemon.Status) bool { return ran(st) && st.State == daemon.StateIdle })
	if b, _ := os.ReadFile(filepath.Join(f.mount, "a.txt")); string(b) != "local edit" {
		t.Errorf("remote a.txt = %q after keep-local", b)
	}
	if state, _, _ := f.c.State(ctx(t)); state != daemon.StateIdle {
		t.Errorf("overall state = %q after resolving, want idle", state)
	}
}

func TestAllowDeletionsOverTheBus(t *testing.T) {
	f := newFixture(t, "sync", "")
	for i := range 12 {
		write(t, f.local, "f"+strconv.Itoa(i), "x")
	}
	changes, _ := f.c.Changes(ctx(t))
	f.c.SyncNow(ctx(t), "s")
	f.waitFor(changes, "the first sync", ran)
	for i := range 12 {
		os.Remove(filepath.Join(f.local, "f"+strconv.Itoa(i)))
	}
	f.c.SyncNow(ctx(t), "s")
	st := f.waitFor(changes, "deletions held", func(st *daemon.Status) bool { return ran(st) && st.HeldDeletions == 12 })
	if st.State != daemon.StateHeld {
		t.Errorf("state = %q, want held", st.State)
	}
	if err := f.c.AllowDeletions(ctx(t), "s"); err != nil {
		t.Fatal(err)
	}
	st = f.waitFor(changes, "the deletions done", func(st *daemon.Status) bool { return ran(st) && st.Deleted == 12 })
	if st.State != daemon.StateIdle {
		t.Errorf("state after allowing = %q", st.State)
	}
}

func TestSecondDaemonCannotTakeTheName(t *testing.T) {
	f := newFixture(t, "push", "")
	if err := Serve(ctx(t), f.connect(), f.d); !errors.Is(err, ErrNameTaken) {
		t.Errorf("second Serve = %v, want ErrNameTaken", err)
	}
}

func TestClientWithoutADaemon(t *testing.T) {
	c := NewClient(dbustest.Bus(t)())
	if _, err := c.Shuttles(ctx(t)); !errors.Is(err, ErrNotRunning) {
		t.Errorf("Shuttles = %v, want ErrNotRunning", err)
	}
}

func TestIntrospection(t *testing.T) {
	f := newFixture(t, "push", "")
	var xml string
	err := f.connect().Object(Name, Path).Call("org.freedesktop.DBus.Introspectable.Introspect", 0).Store(&xml)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{Interface, "ListShuttles", "SyncNow", "SetPaused", "ResolveConflict",
		"AllowDeletions", "SetEnabled", "ReloadConfig", "Activity", "ShuttleChanged", "ConflictRaised", "ShuttleRemoved",
		`name="State"`, `name="Paused"`, `name="ConfigError"`, `name="RateUp"`, `name="RateDown"`} {
		if !strings.Contains(xml, want) {
			t.Errorf("introspection lacks %s", want)
		}
	}
}

// If the daemon's bus connection goes away (the bus restarts, say), status
// changes must not panic: syncing has to carry on without the bus.
func TestClosedBusDoesNotStopTheDaemon(t *testing.T) {
	f := newFixture(t, "push", "")
	write(t, f.local, "a.txt", "a")
	f.serveConn.Close()
	if err := f.d.SyncNow("s"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if st := f.d.Statuses()[0]; ran(&st) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the run did not finish")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := os.Stat(filepath.Join(f.mount, "a.txt")); err != nil {
		t.Error("the run did not push")
	}
}

// Config edits reach the bus: a broken one as ConfigError and a refused
// ReloadConfig, a removed shuttle as ShuttleRemoved.
func TestReloadOverTheBus(t *testing.T) {
	f := newFixture(t, "push", "")
	conn := f.connect()
	if err := conn.AddMatchSignal(dbus.WithMatchObjectPath(Path), dbus.WithMatchMember("ShuttleRemoved")); err != nil {
		t.Fatal(err)
	}
	signals := make(chan *dbus.Signal, 8)
	conn.Signal(signals)

	broken := strings.Replace(mustRead(t, f.config), `mode = "push"`, `mode = "both"`, 1)
	if err := os.WriteFile(f.config, []byte(broken), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := f.c.Reload(ctx(t)); !errors.Is(err, daemon.ErrConfig) || !strings.Contains(err.Error(), `"both"`) {
		t.Errorf("Reload with a broken config = %v, want the config error", err)
	}
	waitUntil(t, "ConfigError over the bus", func() bool {
		msg, err := f.c.ConfigError(ctx(t))
		return err == nil && strings.Contains(msg, `"both" must be one of`)
	})
	if state, _, _ := f.c.State(ctx(t)); state != daemon.StateError {
		t.Errorf("overall state = %s, want error while the config is broken", state)
	}

	other := t.TempDir()
	fixed := fmt.Sprintf("[shares.mnt]\ntype = \"path\"\npath = %q\n[shuttles.t]\nmode = \"push\"\nlocal = %q\nremote = \"mnt:\"\n", f.mount, other)
	if err := os.WriteFile(f.config, []byte(fixed), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := f.c.Reload(ctx(t)); err != nil {
		t.Fatal(err)
	}
	select {
	case sig := <-signals:
		if len(sig.Body) != 1 || sig.Body[0] != "s" {
			t.Errorf("signal %s %v, want ShuttleRemoved(s)", sig.Name, sig.Body)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no ShuttleRemoved")
	}
	waitUntil(t, "the error cleared", func() bool {
		msg, err := f.c.ConfigError(ctx(t))
		return err == nil && msg == ""
	})
	all, err := f.c.Shuttles(ctx(t))
	if err != nil || len(all) != 1 || all[0].Name != "t" {
		t.Errorf("Shuttles = %+v, %v; want only t", all, err)
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func waitUntil(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestSetEnabledOverTheBus(t *testing.T) {
	f := newFixture(t, "push", "")
	if err := f.c.SetEnabled(ctx(t), "s", false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mustRead(t, f.config), "enabled = false") {
		t.Errorf("config after disabling:\n%s", mustRead(t, f.config))
	}
	waitUntil(t, "s disabled", func() bool {
		all, err := f.c.Shuttles(ctx(t))
		return err == nil && len(all) == 1 && all[0].State == daemon.StateDisabled
	})
	if err := f.c.SetEnabled(ctx(t), "s", true); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "s enabled again", func() bool {
		all, err := f.c.Shuttles(ctx(t))
		return err == nil && len(all) == 1 && all[0].State == daemon.StateIdle
	})
	if err := f.c.SetEnabled(ctx(t), "nope", true); !errors.Is(err, daemon.ErrUnknownShuttle) {
		t.Errorf("SetEnabled(nope) = %v, want ErrUnknownShuttle", err)
	}
}

func TestStatusRoundTrip(t *testing.T) {
	st := daemon.Status{
		Name: "s", Mode: config.ModeSync, State: daemon.StateRunning, Paused: true, Metered: true, Pending: true, Share: daemon.ShareOnline,
		Note: "n", Error: "e", LastRun: time.Unix(1000, 0), LastOK: time.Unix(900, 0),
		NextRun: time.Unix(4600, 0), Copied: 3, Deleted: 2, HeldDeletions: 1,
		Conflicts: []engine.Issue{
			{Path: "c", Reason: "r", Outcome: "held", Op: engine.OpConflict,
				Local:  &fsys.FileInfo{Name: "c", Kind: fsys.KindFile, Size: 10, ModTime: time.Unix(2000, 0)},
				Remote: &fsys.FileInfo{Name: "c", Kind: fsys.KindDir, ModTime: time.Unix(3000, 0)}},
			{Path: "Readme", Reason: "differs only in case", Outcome: "held", Op: engine.OpConflict,
				Local: &fsys.FileInfo{Name: "Readme", Kind: fsys.KindFile, Size: 1 << 40, ModTime: time.Unix(4000, 0)}},
		},
		Errors: []engine.Issue{{Path: "x", Reason: "denied"}},
		Files:  31_204, Size: 48 << 30,
		LocalSpace: fsys.Space{Free: 120 << 30, Total: 512 << 30}, RemoteSpace: fsys.Space{Free: 1 << 40, Total: 4 << 40},
		Progress: daemon.Progress{Phase: engine.PhaseSyncing, Done: 4, Total: 9, Bytes: 5 << 30,
			TotalBytes: 7 << 30, Path: "big.iso", RateUp: 2_100_000, RateDown: 1},
	}
	if got := decode("s", encode(&st)); !reflect.DeepEqual(got, st) {
		t.Errorf("round trip:\n got %+v\nwant %+v", got, st)
	}
}

// While a shuttle copies, its progress and the overall rates are on the
// bus; once it is done, they are back to zero.
func TestProgressOverTheBus(t *testing.T) {
	if testing.Short() {
		t.Skip("timed")
	}
	f := newFixture(t, "push", `bandwidth = { up = "256KiB/s" }`)
	write(t, f.local, "big.bin", strings.Repeat("x", 768<<10))
	changes, _ := f.c.Changes(ctx(t))
	if err := f.c.SyncNow(ctx(t), "s"); err != nil {
		t.Fatal(err)
	}
	st := f.waitFor(changes, "progress", func(st *daemon.Status) bool {
		return st.Progress.Path == "big.bin" && st.Progress.RateUp > 0
	})
	if st.Progress.Phase != engine.PhaseSyncing || st.Progress.TotalBytes != 768<<10 {
		t.Errorf("progress = %+v", st.Progress)
	}
	if up, down, err := f.c.Rates(ctx(t)); err != nil || up <= 0 || down != 0 {
		t.Errorf("Rates while copying = %d, %d, %v", up, down, err)
	}
	f.waitFor(changes, "the run's end", ran)
	waitUntil(t, "the rates back to zero", func() bool {
		up, down, err := f.c.Rates(ctx(t))
		return err == nil && up == 0 && down == 0
	})
}

func TestActivityOverTheBus(t *testing.T) {
	f := newFixture(t, "push", "")
	write(t, f.local, "a.txt", "a")
	changes, _ := f.c.Changes(ctx(t))
	if err := f.c.SyncNow(ctx(t), "s"); err != nil {
		t.Fatal(err)
	}
	f.waitFor(changes, "the run", ran)
	es, err := f.c.Activity(ctx(t), "s", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(es) != 2 || es[0].Kind != activity.KindRun || es[0].Text != "1 copied" || es[0].Shuttle != "s" ||
		es[1].Path != "a.txt" || es[1].Text != "copied to the share" || time.Since(es[1].Time) > time.Minute {
		t.Errorf("activity = %+v", es)
	}
	if all, err := f.c.Activity(ctx(t), "", 1); err != nil || len(all) != 1 || all[0].Kind != activity.KindRun {
		t.Errorf("every shuttle's, limit 1: %+v, %v", all, err)
	}
	if _, err := f.c.Activity(ctx(t), "nope", 0); !errors.Is(err, daemon.ErrUnknownShuttle) {
		t.Errorf("Activity(nope) = %v, want ErrUnknownShuttle", err)
	}
}

// An editor reads a shuttle's settings as rows over the bus, sets one, and
// hears ConfigReloaded once the daemon runs with it.
func TestShuttleSettingsOverTheBus(t *testing.T) {
	f := newFixture(t, "push", "")
	conn := f.connect()
	obj := conn.Object(Name, Path)

	var xml string
	if err := conn.Object(Name, Path).Call("org.freedesktop.DBus.Introspectable.Introspect", 0).Store(&xml); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`<method name="ShuttleSettings">`, `type="a(ssssasssss)"`, `<method name="SetShuttleSetting">`, `<signal name="ConfigReloaded">`} {
		if !strings.Contains(xml, want) {
			t.Errorf("introspection lacks %s", want)
		}
	}

	rows := func() map[string]setting {
		var out []setting
		if err := obj.Call(Interface+".ShuttleSettings", 0, "s").Store(&out); err != nil {
			t.Fatal(err)
		}
		m := map[string]setting{}
		for _, r := range out {
			m[r.Key] = r
		}
		return m
	}
	if r := rows()["metered"]; r.Kind != "choice" || strings.Join(r.Choices, ",") != "pause,run" || r.Value != "" ||
		r.Inherited != "pause" || r.From != "default" {
		t.Errorf("metered row %+v", r)
	}
	if r := rows()["triggers"]; r.Kind != "list" {
		t.Errorf("triggers row %+v, want a list", r)
	}

	if err := conn.AddMatchSignal(dbus.WithMatchInterface(Interface), dbus.WithMatchMember("ConfigReloaded")); err != nil {
		t.Fatal(err)
	}
	sigs := make(chan *dbus.Signal, 8)
	conn.Signal(sigs)
	if err := obj.Call(Interface+".SetShuttleSetting", 0, "s", "metered", "run").Err; err != nil {
		t.Fatal(err)
	}
	select {
	case <-sigs:
	case <-time.After(10 * time.Second):
		t.Fatal("no ConfigReloaded after a setting changed")
	}
	if r := rows()["metered"]; r.Value != "run" {
		t.Errorf("after setting it: %+v", r)
	}

	var dbusErr dbus.Error
	err := obj.Call(Interface+".SetShuttleSetting", 0, "nope", "metered", "run").Err
	if !errors.As(err, &dbusErr) || dbusErr.Name != errUnknownShuttle {
		t.Errorf("an unknown shuttle: %v", err)
	}
	err = obj.Call(Interface+".SetShuttleSetting", 0, "s", "metered", "sometimes").Err
	if err == nil || !strings.Contains(err.Error(), "is none of") {
		t.Errorf("a value none of the choices: %v", err)
	}
}
