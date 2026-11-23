// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package daemon

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// heldFixture is a sync shuttle whose guard holds the deletion of all 12
// local files: they are gone from the share.
func heldFixture(t *testing.T) *fixture {
	t.Helper()
	f := newFixture(t, "sync", "")
	for i := range 12 {
		write(t, f.local, "f"+strconv.Itoa(i), "x")
	}
	f.start()
	f.d.SyncNow("s")
	f.waitFor("s", "the first sync", func(st Status) bool { return st.State == StateIdle && !st.LastOK.IsZero() })
	for i := range 12 {
		if err := os.Remove(filepath.Join(f.mount, "f"+strconv.Itoa(i))); err != nil {
			t.Fatal(err)
		}
	}
	f.d.SyncNow("s")
	f.waitFor("s", "the deletions held", func(st Status) bool { return st.State == StateHeld && st.HeldDeletions == 12 })
	return f
}

// An approval is for the run it starts. If that run cannot open a side, the
// next one must not inherit it against whatever the sides hold by then.
func TestFailedOpenUsesUpTheApproval(t *testing.T) {
	f := heldFixture(t)
	away := filepath.Join(t.TempDir(), "away")
	if err := os.Rename(f.mount, away); err != nil {
		t.Fatal(err)
	}
	if err := f.d.AllowDeletions("s", 12); err != nil {
		t.Fatal(err)
	}
	f.waitFor("s", "the failed run", func(st Status) bool { return st.State == StateError })
	if err := os.Mkdir(f.mount, 0o700); err != nil { // an empty stand-in
		t.Fatal(err)
	}
	f.d.SyncNow("s")
	st := f.waitFor("s", "the next run", func(st Status) bool { return st.State != StateError && st.State != StateRunning })
	if st.State != StateHeld || st.Deleted != 0 {
		t.Errorf("state = %s, deleted %d; want held, none deleted", st.State, st.Deleted)
	}
	if !exists(f.local, "f0") {
		t.Error("a file was deleted on the strength of a used approval")
	}
}

// The user answered about the number on screen. If the guard holds another
// by then, the answer is not about these deletions.
func TestApprovalOfAnotherCountIsRefused(t *testing.T) {
	f := heldFixture(t)
	for _, count := range []int{0, 3, 13} {
		if err := f.d.AllowDeletions("s", count); !errors.Is(err, ErrHeldChanged) {
			t.Errorf("AllowDeletions(%d) = %v, want ErrHeldChanged", count, err)
		}
	}
	f.d.SyncNow("s")
	st := f.settled("s")
	if st.State != StateHeld || st.Deleted != 0 || !exists(f.local, "f0") {
		t.Errorf("after refused approvals: state %s, deleted %d", st.State, st.Deleted)
	}
}

// A file that disappears after the approval is not part of what was
// confirmed: the run holds them all again.
func TestApprovalDoesNotCoverALargerSet(t *testing.T) {
	f := newFixture(t, "sync", "")
	for i := range 20 {
		write(t, f.local, "f"+strconv.Itoa(i), "x")
	}
	f.start()
	f.d.SyncNow("s")
	f.waitFor("s", "the first sync", func(st Status) bool { return st.State == StateIdle && !st.LastOK.IsZero() })
	for i := range 12 {
		if err := os.Remove(filepath.Join(f.mount, "f"+strconv.Itoa(i))); err != nil {
			t.Fatal(err)
		}
	}
	f.d.SyncNow("s")
	f.waitFor("s", "the deletions held", func(st Status) bool { return st.State == StateHeld && st.HeldDeletions == 12 })
	// Paused, so the approval waits while the share loses one more.
	if err := f.d.SetPaused("s", true); err != nil {
		t.Fatal(err)
	}
	if err := f.d.AllowDeletions("s", 12); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(f.mount, "f12")); err != nil {
		t.Fatal(err)
	}
	if err := f.d.SetPaused("s", false); err != nil {
		t.Fatal(err)
	}
	st := f.waitFor("s", "the deletions held again", func(st Status) bool { return st.State == StateHeld && st.HeldDeletions == 13 })
	if st.Deleted != 0 || !exists(f.local, "f0") {
		t.Errorf("deleted %d past the guard", st.Deleted)
	}
}

// A config change replaces the runner: what was approved for the shuttle as
// it was does not carry over.
func TestReloadDropsTheApproval(t *testing.T) {
	quickReloads(t)
	f := heldFixture(t)
	before := f.settled("s").LastRun
	if err := f.d.SetPaused("s", true); err != nil {
		t.Fatal(err)
	}
	if err := f.d.AllowDeletions("s", 12); err != nil {
		t.Fatal(err)
	}
	old := f.runnerOf("s")
	f.writeConfig(f.shuttleDoc("sync", "deletion_guard = 40"))
	deadline := time.Now().Add(10 * time.Second)
	for f.runnerOf("s") == old {
		if time.Now().After(deadline) {
			t.Fatal("the config change was not applied")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := f.d.SetPaused("s", false); err != nil {
		t.Fatal(err)
	}
	f.d.SyncNow("s")
	st := f.waitFor("s", "the next run", func(st Status) bool {
		return st.State != StateRunning && !st.Pending && st.LastRun.After(before)
	})
	if st.State != StateHeld || st.Deleted != 0 || !exists(f.local, "f0") {
		t.Errorf("state %s, deleted %d on an approval from before the reload", st.State, st.Deleted)
	}
}
