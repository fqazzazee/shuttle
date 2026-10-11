// SPDX-License-Identifier: GPL-2.0-or-later

package bus

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/fqazzazee/shuttle/internal/daemon"
	"github.com/fqazzazee/shuttle/internal/dbustest"
	"github.com/fqazzazee/shuttle/internal/engine"
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
	if st.State != daemon.StateIdle || st.Copied != 1 {
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
		"AllowDeletions", "ShuttleChanged", "ConflictRaised", `name="State"`, `name="Paused"`} {
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
