// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package daemon

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fqazzazee/shuttle/internal/config"
	"github.com/fqazzazee/shuttle/internal/state"
)

func quickReloads(t *testing.T) {
	old := configDebounce
	configDebounce = 20 * time.Millisecond
	t.Cleanup(func() { configDebounce = old })
}

// shuttleDoc is a config with shuttle s as newFixture writes it, plus more.
func (f *fixture) shuttleDoc(mode, extra string, more ...string) string {
	return fmt.Sprintf("[shares.mnt]\ntype = \"path\"\npath = %q\n[shuttles.s]\nmode = %q\nlocal = %q\nremote = \"mnt:\"\n%s\n%s",
		f.mount, mode, f.local, extra, strings.Join(more, "\n"))
}

// other is a second shuttle t, between its own folders on a share of its
// own: s has all of mnt.
func (f *fixture) other() (doc, local, remote string) {
	local, remote = f.t.TempDir(), f.t.TempDir()
	return fmt.Sprintf("[shares.mnt2]\ntype = \"path\"\npath = %q\n[shuttles.t]\nmode = \"push\"\nlocal = %q\nremote = \"mnt2:\"\n",
		remote, local), local, remote
}

// runnerOf is the named shuttle's current runner.
func (f *fixture) runnerOf(name string) *runner {
	f.d.runnersMu.RLock()
	defer f.d.runnersMu.RUnlock()
	return f.d.runners[name]
}

func (f *fixture) has(name string) bool {
	return slices.ContainsFunc(f.d.Statuses(), func(st Status) bool { return st.Name == name })
}

func TestANewShuttleStarts(t *testing.T) {
	quickReloads(t)
	f := newFixture(t, "push", "")
	f.start()
	doc, local, remote := f.other()
	write(t, local, "a.txt", "a")
	f.writeConfig(f.shuttleDoc("push", "", doc))
	f.waitFor("t", "the new shuttle", func(Status) bool { return true })
	if err := f.d.SyncNow("t"); err != nil {
		t.Fatal(err)
	}
	f.waitFor("t", "its sync", func(Status) bool { return exists(remote, "a.txt") })
}

// A shuttle the edit does not touch carries on: its run is not interrupted.
func TestAnUnchangedShuttleCarriesOn(t *testing.T) {
	quickReloads(t)
	f := newFixture(t, "push", slow)
	slowFiles(t, f.local, 3)
	f.start()
	before := f.runnerOf("s")
	f.d.SyncNow("s")
	f.waitCopying(f.mount)
	doc, _, _ := f.other()
	f.writeConfig(f.shuttleDoc("push", slow, doc))
	f.waitFor("t", "the reload", func(Status) bool { return true })
	st := f.settled("s")
	if f.runnerOf("s") != before || f.runCount() != 1 || st.Copied != 3 {
		t.Errorf("s was restarted or its run cut short: %d runs, status %+v", f.runCount(), st)
	}
}

// A changed shuttle finishes the file it is on, then carries on under the
// new settings, keeping what the user set and the run still owed.
func TestAChangedShuttleRestartsAfterItsFile(t *testing.T) {
	quickReloads(t)
	f := newFixture(t, "push", slow)
	slowFiles(t, f.local, 3)
	write(t, f.local, "skip.tmp", "x")
	f.start()
	f.d.SyncNow("s")
	f.waitCopying(f.mount)
	f.writeConfig(f.shuttleDoc("push", slow+"\nignore = [\"*.tmp\"]"))
	f.waitFor("s", "the new runner's run", func(st Status) bool {
		return f.runnerOf("s").shuttle.Settings.Ignore != nil && st.State == StateIdle && !st.Pending &&
			exists(f.mount, "f2.bin")
	})
	for i := range 3 {
		if !exists(f.mount, fmt.Sprintf("f%d.bin", i)) {
			t.Errorf("f%d.bin missing: the stopped run was not resumed", i)
		}
	}
	entries, _ := os.ReadDir(f.mount)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".shuttle-tmp-") {
			t.Errorf("half-written file left: %s", e.Name())
		}
	}

	f.d.SetPaused("s", true)
	f.writeConfig(f.shuttleDoc("push", slow))
	f.waitFor("s", "the reload", func(Status) bool { return f.runnerOf("s").shuttle.Settings.Ignore == nil })
	if st := f.d.Statuses()[0]; !st.Paused {
		t.Errorf("status = %+v: the pause was lost in the reload", st)
	}
}

func TestARemovedShuttleStops(t *testing.T) {
	quickReloads(t)
	f := newFixture(t, "push", "")
	doc, _, _ := f.other()
	f.writeConfig(f.shuttleDoc("push", "", doc))
	f.start()
	var mu sync.Mutex
	var removed []string
	stop := f.d.WatchReloads(func(r *Reloaded) {
		mu.Lock()
		removed = append(removed, r.Removed...)
		mu.Unlock()
	})
	defer stop()
	f.writeConfig(f.shuttleDoc("push", ""))
	f.waitFor("s", "t gone", func(Status) bool { return !f.has("t") })
	mu.Lock()
	defer mu.Unlock()
	if !slices.Equal(removed, []string{"t"}) {
		t.Errorf("removed = %v, want [t]", removed)
	}
	if err := f.d.SyncNow("t"); !errors.Is(err, ErrUnknownShuttle) {
		t.Errorf("SyncNow(t) = %v, want ErrUnknownShuttle", err)
	}
	// Its state is released: it can be opened again.
	db, err := state.Open(filepath.Join(f.paths.StateDir, "t.db"))
	if err != nil {
		t.Fatalf("t's state is still held: %v", err)
	}
	db.Close()
}

func TestDisablingAndEnablingByEditing(t *testing.T) {
	quickReloads(t)
	f := newFixture(t, "push", "")
	write(t, f.local, "a.txt", "a")
	f.start()
	f.writeConfig(f.shuttleDoc("push", "enabled = false"))
	f.waitFor("s", "disabled", func(st Status) bool { return st.State == StateDisabled })
	if err := f.d.SyncNow("s"); !errors.Is(err, ErrDisabled) {
		t.Errorf("SyncNow on a disabled shuttle = %v", err)
	}
	f.writeConfig(f.shuttleDoc("push", ""))
	f.waitFor("s", "enabled again", func(st Status) bool { return st.State == StateIdle })
	f.syncNow()
	if !exists(f.mount, "a.txt") {
		t.Error("the re-enabled shuttle did not sync")
	}
}

// SetEnabled writes the config file, keeping the rest of it, and applies
// it without waiting for the file watcher.
func TestSetEnabled(t *testing.T) {
	old := configDebounce
	configDebounce = time.Hour // only SetEnabled's own reload can apply it
	t.Cleanup(func() { configDebounce = old })
	f := newFixture(t, "push", "")
	doc := "# mine\n" + f.shuttleDoc("push", "")
	f.writeConfig(doc)
	write(t, f.local, "a.txt", "a")
	f.start()

	if err := f.d.SetEnabled("s", false); err != nil {
		t.Fatal(err)
	}
	f.waitFor("s", "disabled", func(st Status) bool { return st.State == StateDisabled })
	want := strings.Replace(doc, "remote = \"mnt:\"\n", "remote = \"mnt:\"\nenabled = false\n", 1)
	if got, _ := os.ReadFile(f.paths.Config); string(got) != want {
		t.Errorf("config after disabling:\n%s\nwant:\n%s", got, want)
	}
	if err := f.d.SyncNow("s"); !errors.Is(err, ErrDisabled) {
		t.Errorf("SyncNow on a disabled shuttle = %v", err)
	}

	if err := f.d.SetEnabled("s", true); err != nil {
		t.Fatal(err)
	}
	f.waitFor("s", "enabled again", func(st Status) bool { return st.State == StateIdle })
	if got, _ := os.ReadFile(f.paths.Config); string(got) != doc {
		t.Errorf("config after enabling:\n%s\nwant it as it was", got)
	}
	f.syncNow()
	if !exists(f.mount, "a.txt") {
		t.Error("the re-enabled shuttle did not sync")
	}

	if err := f.d.SetEnabled("nope", false); !errors.Is(err, ErrUnknownShuttle) {
		t.Errorf("SetEnabled(nope) = %v, want ErrUnknownShuttle", err)
	}
}

// While the config file is broken, SetEnabled leaves it alone.
func TestSetEnabledWithABrokenConfig(t *testing.T) {
	quickReloads(t)
	f := newFixture(t, "push", "")
	f.start()
	broken := f.shuttleDoc("both", "")
	f.writeConfig(broken)
	f.waitFor("s", "the config error", func(Status) bool { return f.d.ConfigError() != "" })
	if err := f.d.SetEnabled("s", false); !errors.Is(err, ErrConfig) || !strings.Contains(err.Error(), `"both"`) {
		t.Errorf("SetEnabled with a broken config = %v, want ErrConfig and why", err)
	}
	if got, _ := os.ReadFile(f.paths.Config); string(got) != broken {
		t.Error("SetEnabled changed a broken config file")
	}
}

// A broken edit keeps the running config, says why once, and clears once
// fixed.
func TestABrokenEditKeepsTheOldConfig(t *testing.T) {
	quickReloads(t)
	f := newFixture(t, "push", "")
	write(t, f.local, "a.txt", "a")
	f.start()
	f.writeConfig(f.shuttleDoc("both", ""))
	f.waitFor("s", "the config error", func(Status) bool { return f.d.ConfigError() != "" })
	if !strings.Contains(f.d.ConfigError(), `"both" must be one of`) || f.d.State() != StateError {
		t.Errorf("config error %q, state %s", f.d.ConfigError(), f.d.State())
	}
	f.syncNow() // still runs, with the old config
	if !exists(f.mount, "a.txt") {
		t.Error("the shuttle stopped working after a broken edit")
	}
	f.writeConfig(f.shuttleDoc("both", "")) // the same again: not news
	time.Sleep(10 * configDebounce)
	if got := f.eventsOf(EventConfigError); len(got) != 1 {
		t.Errorf("config error events = %+v, want one", got)
	}
	if err := f.d.Reload(); !errors.Is(err, ErrConfig) {
		t.Errorf("Reload with a broken config = %v, want ErrConfig", err)
	}
	f.writeConfig(f.shuttleDoc("push", ""))
	f.waitFor("s", "the fix", func(Status) bool { return f.d.ConfigError() == "" })
	if f.d.State() == StateError {
		t.Error("still in error after the fix")
	}
}

// A config that is a symlink, as from a dotfiles repository, is followed.
func TestASymlinkedConfigIsFollowed(t *testing.T) {
	quickReloads(t)
	f := newFixture(t, "push", "")
	real := filepath.Join(t.TempDir(), "dotfiles.toml")
	if err := os.Rename(f.paths.Config, real); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, f.paths.Config); err != nil {
		t.Fatal(err)
	}
	f.start()
	doc, _, _ := f.other()
	if err := os.WriteFile(real, []byte(f.shuttleDoc("push", "", doc)), 0o600); err != nil {
		t.Fatal(err)
	}
	f.waitFor("s", "the reload", func(Status) bool { return f.has("t") })
}

// Reload applies a valid config without waiting for the file watcher.
func TestReloadAsked(t *testing.T) {
	// Before start, so it is restored after the daemon stops.
	old := configDebounce
	configDebounce = time.Hour // the watcher would never get to it
	t.Cleanup(func() { configDebounce = old })
	f := newFixture(t, "push", "")
	f.start()
	doc, _, _ := f.other()
	f.writeConfig(f.shuttleDoc("push", "", doc))
	if err := f.d.Reload(); err != nil {
		t.Fatal(err)
	}
	f.waitFor("s", "the reload", func(Status) bool { return f.has("t") })
}

// The global bandwidth cap applies to the shuttles once the config sets
// it, without restarting them.
func TestTheGlobalCapFollowsReloads(t *testing.T) {
	if testing.Short() {
		t.Skip("timed")
	}
	quickReloads(t)
	f := newFixture(t, "push", "")
	f.start()
	before := f.runnerOf("s")
	f.writeConfig("[global]\nbandwidth = { up = \"512KiB/s\" }\n" + f.shuttleDoc("push", ""))
	f.waitFor("s", "the cap", func(Status) bool { return f.d.global.Limits().Up != nil })
	if f.runnerOf("s") != before {
		t.Error("a change to the global cap restarted the shuttle")
	}
	write(t, f.local, "big.bin", strings.Repeat("x", 256<<10))
	start := time.Now()
	f.syncNow()
	// 512 KiB/s, after the first 64 KiB burst.
	if took, want := time.Since(start), 300*time.Millisecond; took < want {
		t.Errorf("256 KiB under a 512 KiB/s cap took %v, want at least %v", took, want)
	}
	f.writeConfig(f.shuttleDoc("push", ""))
	f.waitFor("s", "the cap lifted", func(Status) bool { return f.d.global.Limits().Up == nil })
}

// The global cap is one cap for every shuttle together, not one each.
func TestTheGlobalCapIsShared(t *testing.T) {
	if testing.Short() {
		t.Skip("timed")
	}
	f := newFixture(t, "push", "")
	doc, local, remote := f.other()
	f.writeConfig("[global]\nbandwidth = { up = \"512KiB/s\" }\n" + f.shuttleDoc("push", "", doc))
	big := strings.Repeat("x", 256<<10)
	write(t, f.local, "s.bin", big)
	write(t, local, "t.bin", big)
	f.start()
	start := time.Now()
	for _, name := range []string{"s", "t"} {
		if err := f.d.SyncNow(name); err != nil {
			t.Fatal(err)
		}
	}
	f.waitFor("s", "both copied", func(Status) bool { return exists(f.mount, "s.bin") && exists(remote, "t.bin") })
	// 512 KiB at 512 KiB/s together, after the first 64 KiB burst; alone,
	// each would take under half a second.
	if took, want := time.Since(start), 800*time.Millisecond; took < want {
		t.Errorf("two 256 KiB pushes under a shared 512 KiB/s cap took %v, want at least %v", took, want)
	}
}

// The status names the shuttle's mode, and follows a reload that changes it.
func TestStatusHasTheMode(t *testing.T) {
	quickReloads(t)
	f := newFixture(t, "push", "")
	f.start()
	if st := f.d.Statuses()[0]; st.Mode != config.ModePush {
		t.Errorf("mode = %q, want push", st.Mode)
	}
	f.writeConfig(f.shuttleDoc("sync", ""))
	f.waitFor("s", "the new mode", func(st Status) bool { return st.Mode == config.ModeSync })
}

// EditConfig changes the file through the editor, keeping what it does not
// touch, applies it as a reload, and leaves the file alone when the change
// fails or would not load.
func TestEditConfig(t *testing.T) {
	f := newFixture(t, "push", "")
	doc := "# mine\n" + f.shuttleDoc("push", "")
	f.writeConfig(doc)
	f.start()
	_, local, remote := f.other()

	if err := f.d.EditConfig(func(d *config.Doc) error {
		return d.Set([]string{"shuttles", "t"}, "local", local)
	}); err == nil {
		t.Error("a shuttle with no mode or remote taken")
	}
	if err := f.d.EditConfig(func(*config.Doc) error { return errors.New("no") }); err == nil || err.Error() != "no" {
		t.Errorf("a failing change: %v", err)
	}
	if got, _ := os.ReadFile(f.paths.Config); string(got) != doc {
		t.Fatalf("config after refused edits:\n%s", got)
	}

	if err := f.d.EditConfig(func(d *config.Doc) error {
		if err := d.SetShare(&config.Share{Name: "mnt2", Type: config.SharePath, Path: remote}); err != nil {
			return err
		}
		for k, v := range map[string]string{"mode": "pull", "local": local, "remote": "mnt2:"} {
			if err := d.Set([]string{"shuttles", "t"}, k, v); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	f.waitFor("t", "the new shuttle", func(Status) bool { return true })
	// What was there is kept, line for line and in order, the comment too;
	// the editor puts the new share beside the old one.
	got, _ := os.ReadFile(f.paths.Config)
	rest := string(got)
	for line := range strings.SplitSeq(doc, "\n") {
		i := strings.Index(rest, line)
		if i < 0 {
			t.Fatalf("config after the edit lost %q:\n%s", line, got)
		}
		rest = rest[i+len(line):]
	}
}
