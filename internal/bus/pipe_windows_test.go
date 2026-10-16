// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package bus

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fqazzazee/shuttle/internal/activity"
	"github.com/fqazzazee/shuttle/internal/daemon"
	"github.com/fqazzazee/shuttle/internal/fsys/fsystest"
	"github.com/fqazzazee/shuttle/internal/secrets"
)

type noKeyring struct{}

func (noKeyring) Get(context.Context, secrets.Key) (string, error) { return "", secrets.ErrNotFound }
func (noKeyring) Set(context.Context, secrets.Key, string) error   { return nil }
func (noKeyring) Delete(context.Context, secrets.Key) error        { return nil }

// fixture is a daemon serving a "path" shuttle s on a pipe of the test's
// own, plus a client.
type fixture struct {
	t            *testing.T
	local, mount string
	pipe         string
	d            *daemon.Daemon
	c            *Client
	config       string
}

func newFixture(t *testing.T, mode, extra string) *fixture {
	t.Helper()
	f := &fixture{t: t, local: t.TempDir(), mount: t.TempDir(), pipe: `\\.\pipe\` + fsystest.UniqueName("shuttle-test")}
	dir := t.TempDir()
	f.config = filepath.Join(dir, "config.toml")
	doc := fmt.Sprintf("[shares.mnt]\ntype = \"path\"\npath = %q\n[shuttles.s]\nmode = %q\nlocal = %q\nremote = \"mnt:\"\n%s\n",
		f.mount, mode, f.local, extra)
	if err := os.WriteFile(f.config, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	d, err := daemon.New(&daemon.Options{Paths: daemon.Paths{Config: f.config, StateDir: filepath.Join(dir, "state")}, Store: noKeyring{}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	runDone, serveDone := make(chan error, 1), make(chan error, 1)
	go func() { runDone <- d.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		if err := <-serveDone; err != nil {
			t.Errorf("serve: %v", err)
		}
		<-runDone
	})
	<-d.Ready()
	go func() { serveDone <- serve(ctx, d, f.pipe) }()
	f.d = d
	for range 200 { // until the pipe is there
		if f.c, err = connect(f.pipe); err == nil {
			t.Cleanup(func() { f.c.Close() })
			return f
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the daemon never served its pipe: %v", err)
	return nil
}

// waitFor reads the client's status stream until ok holds.
func (f *fixture) waitFor(changes <-chan daemon.Status, what string, ok func(*daemon.Status) bool) daemon.Status {
	f.t.Helper()
	timeout := time.After(10 * time.Second)
	for {
		select {
		case st, open := <-changes:
			if !open {
				f.t.Fatalf("the stream ended waiting for %s", what)
			}
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

func TestListAndSyncOverThePipe(t *testing.T) {
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
		t.Errorf("status over the pipe = %+v", st)
	}
	if _, err := os.Stat(filepath.Join(f.mount, "a.txt")); err != nil {
		t.Error("the run did not push")
	}
	if up, down, err := f.c.Rates(ctx(t)); err != nil || up < 0 || down < 0 {
		t.Errorf("Rates = %d, %d, %v", up, down, err)
	}
}

func TestErrorsCrossThePipe(t *testing.T) {
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

func TestPauseAndOverallStateOverThePipe(t *testing.T) {
	f := newFixture(t, "push", "")
	if err := f.c.SetPaused(ctx(t), "", true); err != nil {
		t.Fatal(err)
	}
	state, paused, err := f.c.State(ctx(t))
	if err != nil || state != daemon.StatePaused || !paused {
		t.Errorf("State = %q, paused %v, %v", state, paused, err)
	}
	if err := f.c.SetPaused(ctx(t), "", false); err != nil {
		t.Fatal(err)
	}
	if state, paused, _ := f.c.State(ctx(t)); state != daemon.StateIdle || paused {
		t.Errorf("after resume: %q, paused %v", state, paused)
	}
}

// One daemon per user: a second cannot serve the pipe the first serves.
func TestSecondDaemonCannotTakeThePipe(t *testing.T) {
	f := newFixture(t, "push", "")
	if err := serve(ctx(t), f.d, f.pipe); !errors.Is(err, ErrNameTaken) {
		t.Errorf("a second serve = %v, want ErrNameTaken", err)
	}
	if _, err := f.c.Shuttles(ctx(t)); err != nil {
		t.Errorf("the first daemon stopped answering: %v", err)
	}
}

func TestClientWithoutADaemon(t *testing.T) {
	if _, err := connect(`\\.\pipe\` + fsystest.UniqueName("shuttle-none")); !errors.Is(err, ErrNotRunning) {
		t.Errorf("connect = %v, want ErrNotRunning", err)
	}
}

// A call cut short by its context says so; the client then dials again.
func TestCancelledCallThenAnother(t *testing.T) {
	f := newFixture(t, "push", "")
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.c.Shuttles(cancelled); !errors.Is(err, context.Canceled) {
		t.Errorf("a cancelled call = %v", err)
	}
	if all, err := f.c.Shuttles(ctx(t)); err != nil || len(all) != 1 {
		t.Errorf("the next call = %+v, %v", all, err)
	}
}

func TestReloadAndConfigErrorOverThePipe(t *testing.T) {
	f := newFixture(t, "push", "")
	if msg, err := f.c.ConfigError(ctx(t)); err != nil || msg != "" {
		t.Errorf("ConfigError = %q, %v", msg, err)
	}
	if err := os.WriteFile(f.config, []byte("[shuttles.s\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := f.c.Reload(ctx(t)); !errors.Is(err, daemon.ErrConfig) {
		t.Errorf("Reload of a broken config = %v, want ErrConfig", err)
	}
	for i := 0; ; i++ { // the daemon takes the error in after answering
		msg, err := f.c.ConfigError(ctx(t))
		if err == nil && msg != "" {
			break
		}
		if i == 200 {
			t.Fatalf("ConfigError after = %q, %v", msg, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestActivityAndEnableOverThePipe(t *testing.T) {
	f := newFixture(t, "push", "")
	write(t, f.local, "a.txt", "a")
	changes, err := f.c.Changes(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.c.SyncNow(ctx(t), "s"); err != nil {
		t.Fatal(err)
	}
	f.waitFor(changes, "the run's result", ran)
	es, err := f.c.Activity(ctx(t), "s", 0)
	if err != nil || len(es) == 0 || es[0].Shuttle != "s" || es[0].Kind != activity.KindRun {
		t.Errorf("Activity = %+v, %v", es, err)
	}
	if err := f.c.SetEnabled(ctx(t), "s", false); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(f.config); !strings.Contains(string(data), "enabled = false") {
		t.Errorf("config after disabling:\n%s", data)
	}
}

// A stream's context ends it: its channel closes.
func TestChangesEndWithTheirContext(t *testing.T) {
	f := newFixture(t, "push", "")
	c, cancel := context.WithCancel(context.Background())
	changes, err := f.c.Changes(c)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case _, open := <-changes:
		for open {
			_, open = <-changes
		}
	case <-time.After(5 * time.Second):
		t.Error("the stream did not end with its context")
	}
}
