// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/fqazzazee/shuttle/internal/activity"
	"github.com/fqazzazee/shuttle/internal/engine"
	"github.com/fqazzazee/shuttle/internal/fsys"
)

// texts is a log as "kind path: text" lines, newest first.
func texts(es []activity.Entry) string {
	var b strings.Builder
	for _, e := range es {
		b.WriteString(string(e.Kind) + " " + e.Path + ": " + e.Text + "\n")
	}
	return b.String()
}

func (f *fixture) activity(name string) []activity.Entry {
	f.t.Helper()
	es, err := f.d.Activity(name, 0)
	if err != nil {
		f.t.Fatal(err)
	}
	return es
}

func TestActivityOfARun(t *testing.T) {
	f := newFixture(t, "push", "")
	write(t, f.local, "a.txt", "a")
	if err := os.Mkdir(filepath.Join(f.local, "d"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, f.local, "d/b.txt", "b")
	f.start()
	f.syncNow()
	want := `run : 2 copied, 1 folder created
done d/b.txt: copied to the share
done a.txt: copied to the share
done d: folder created on the share
`
	if got := texts(f.activity("s")); got != want {
		t.Errorf("activity:\n%s\nwant:\n%s", got, want)
	}
	for _, e := range f.activity("s") {
		if e.Shuttle != "s" || time.Since(e.Time) > time.Minute {
			t.Errorf("entry %+v", e)
		}
	}

	// A run with nothing to do adds nothing.
	f.syncNow()
	if got := len(f.activity("s")); got != 4 {
		t.Errorf("%d entries after a quiet run, want still 4", got)
	}
	if info, err := os.Stat(filepath.Join(f.paths.StateDir, "s.activity")); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("log file: %v, %v", info, err)
	}
}

func TestActivityOfProblems(t *testing.T) {
	f := newFixture(t, "sync", `conflicts = "ask"`)
	write(t, f.local, "clash.txt", "local")
	write(t, f.mount, "clash.txt", "remote, longer")
	write(t, f.local, "locked.txt", "l")
	if err := os.Chmod(filepath.Join(f.local, "locked.txt"), 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(filepath.Join(f.local, "locked.txt"), 0o600) })
	f.start()
	f.syncNow()
	got := texts(f.activity("s"))
	for _, want := range []string{
		"run : 1 conflict, 1 error\n",
		"conflict clash.txt: changed on both sides; waiting for you to choose\n",
		"error locked.txt: could not copy to the share: ",
		"permission denied",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("activity lacks %q:\n%s", want, got)
		}
	}

	// Resolving starts a run, whose entries come after: the locked file's
	// error again. Wait for it, so the log is whole when it is read and the
	// next run is the test's own.
	f.ranAfter(func() {
		if err := f.d.Resolve(context.Background(), "s", "clash.txt", engine.KeepLocal); err != nil {
			t.Fatal(err)
		}
	})
	es := f.activity("s")
	if i := slices.IndexFunc(es, func(e activity.Entry) bool { return e.Path == "clash.txt" }); i < 0 ||
		es[i].Kind != activity.KindConflict || es[i].Text != "resolved: kept this computer's copy" {
		t.Errorf("activity after resolving:\n%s", texts(es))
	}

	// A run that fails as a whole says why.
	if err := os.Rename(f.local, f.local+".gone"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Rename(f.local+".gone", f.local) })
	f.syncNow()
	if es := f.activity("s"); es[0].Kind != activity.KindError || !strings.Contains(es[0].Text, "the run failed: local folder") {
		t.Errorf("newest entry = %+v, want the failed run", es[0])
	}
}

// A local folder that refuses what the phone's shared storage refuses: the
// run leaves those names out, the status lists them, and the log says so
// once.
func TestActivityOfRefusedNames(t *testing.T) {
	f := newFixture(t, "sync", "")
	write(t, f.mount, "ok.txt", "ok")
	write(t, f.mount, "a:b.txt", "colon")
	f.build()
	f.d.o.LocalNames = func(dir string) fsys.Names {
		if dir != f.local {
			t.Errorf("LocalNames(%q), want the shuttle's folder", dir)
		}
		return fsys.Names{Refuse: fsys.AndroidSharedStorage.Refuse}
	}
	f.run()
	st := f.syncNow()
	if st.State != StateIdle || len(st.Refused) != 1 || st.Refused[0].Path != "a:b.txt" {
		t.Errorf("status = %s, refused %+v; want idle with a:b.txt left out", st.State, st.Refused)
	}
	want := "left-out a:b.txt: the local folder cannot hold this name: it contains ':'; left out, and not deleted on either side\n"
	if got := texts(f.activity("s")); !strings.Contains(got, want) {
		t.Errorf("activity lacks %q:\n%s", want, got)
	}

	// Still left out by the next run, which logs it no more.
	write(t, f.mount, "new.txt", "new")
	if st := f.syncNow(); len(st.Refused) != 1 {
		t.Errorf("refused = %+v after another run", st.Refused)
	}
	if n := strings.Count(texts(f.activity("s")), "left-out"); n != 1 {
		t.Errorf("logged %d times, want once:\n%s", n, texts(f.activity("s")))
	}
	if _, err := os.Stat(filepath.Join(f.mount, "a:b.txt")); err != nil {
		t.Errorf("the share's a:b.txt: %v", err)
	}
	if _, err := os.Stat(filepath.Join(f.local, "a:b.txt")); err == nil {
		t.Error("a:b.txt was written to the local folder")
	}
}

// The app names the phone by the device's name: conflict copies carry it,
// not the "localhost" Android gives as the host name.
func TestDeviceNameInConflictCopies(t *testing.T) {
	f := newFixture(t, "sync", `conflicts = "keep-both"`)
	f.deviceName = "Téléphone de Zoé"
	write(t, f.local, "clash.txt", "local")
	write(t, f.mount, "clash.txt", "remote, newer")
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(filepath.Join(f.local, "clash.txt"), old, old); err != nil {
		t.Fatal(err)
	}
	f.start()
	st := f.syncNow()
	if len(st.Conflicts) != 1 || !strings.Contains(st.Conflicts[0].Outcome, "the Téléphone-de-Zoé copy is now clash.sync-conflict-") {
		t.Fatalf("conflicts = %+v", st.Conflicts)
	}
	saved, err := filepath.Glob(filepath.Join(f.local, "clash.sync-conflict-*-Téléphone-de-Zoé.txt"))
	if err != nil || len(saved) != 1 {
		t.Errorf("conflict copy: %v, %v", saved, err)
	}
}

func TestActivityOfEveryShuttle(t *testing.T) {
	quickReloads(t)
	f := newFixture(t, "push", "")
	doc, local, _ := f.other()
	f.writeConfig(f.shuttleDoc("push", "", doc))
	write(t, f.local, "s.txt", "s")
	write(t, local, "t.txt", "t")
	f.start()
	f.syncNow()
	if err := f.d.SyncNow("t"); err != nil {
		t.Fatal(err)
	}
	f.waitFor("t", "t's run", func(st Status) bool { return !st.LastRun.IsZero() && st.State == StateIdle })

	all, err := f.d.Activity("", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 4 || all[0].Shuttle != "t" || all[0].Kind != activity.KindRun || all[3].Path != "s.txt" {
		t.Errorf("every shuttle's activity:\n%+v", all)
	}
	if two, _ := f.d.Activity("", 2); len(two) != 2 || two[0].Shuttle != "t" {
		t.Errorf("limit 2: %+v", two)
	}
	if _, err := f.d.Activity("nope", 0); !errors.Is(err, ErrUnknownShuttle) {
		t.Errorf("Activity(nope) = %v, want ErrUnknownShuttle", err)
	}

	// A disabled shuttle's log can still be read.
	f.writeConfig(f.shuttleDoc("push", "enabled = false", doc))
	f.waitFor("s", "disabled", func(st Status) bool { return st.State == StateDisabled })
	if got := f.activity("s"); len(got) != 2 {
		t.Errorf("a disabled shuttle's activity: %+v", got)
	}
}

// A run stopped by a pause says so, and what it did before stopping.
func TestActivityOfAStoppedRun(t *testing.T) {
	f := newFixture(t, "push", slow)
	slowFiles(t, f.local, 3)
	f.start()
	if err := f.d.SyncNow("s"); err != nil {
		t.Fatal(err)
	}
	f.waitCopying(f.mount)
	if err := f.d.SetPaused("s", true); err != nil {
		t.Fatal(err)
	}
	f.waitFor("s", "the run stopped", func(st Status) bool { return !st.LastRun.IsZero() && st.State != StateRunning })
	es := f.activity("s")
	if len(es) == 0 || es[0].Kind != activity.KindRun ||
		es[0].Text != "stopped by a pause or shutdown after 1 copied; the rest is left for the next run" {
		t.Errorf("activity:\n%s", texts(es))
	}
}

// A conflict kept on this side says which side that is, by the name the
// daemon is given.
func TestResolvedText(t *testing.T) {
	for _, c := range []struct {
		choice engine.Choice
		here   string
		want   string
	}{
		{engine.KeepLocal, "", "kept this computer's copy"},
		{engine.KeepLocal, "this phone", "kept this phone's copy"},
		{engine.KeepRemote, "this phone", "kept the share's copy"},
		{engine.KeepBoth, "this phone", "kept both"},
	} {
		if got := resolvedText(c.choice, c.here); got != c.want {
			t.Errorf("resolvedText(%s, %q) = %q; want %q", c.choice, c.here, got, c.want)
		}
	}
}
