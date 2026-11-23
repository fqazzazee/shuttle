// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fqazzazee/shuttle/internal/engine"
)

// resolve calls Resolve and turns a panic into an error: over D-Bus nothing
// recovers it, and it ends the daemon.
func resolve(d *Daemon) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("panic: %v", p)
		}
	}()
	return d.Resolve(context.Background(), "s", "a.txt", engine.KeepLocal)
}

// A runner without its state (not opened yet, or it could not be) has
// nothing to record the choice in.
func TestResolveWithoutStateIsRefused(t *testing.T) {
	f := newFixture(t, "sync", "")
	write(t, f.local, "a.txt", "local")
	write(t, f.mount, "a.txt", "remote")
	f.build()
	if err := resolve(f.d); !errors.Is(err, ErrRestarting) {
		t.Errorf("Resolve = %v, want ErrRestarting", err)
	}
	if b, _ := os.ReadFile(filepath.Join(f.mount, "a.txt")); string(b) != "remote" {
		t.Errorf("share's a.txt = %q, want it untouched", b)
	}
}

// A Resolve waiting for a run can find its runner replaced by a reload once
// the run ends.
func TestResolveWaitingAcrossAReload(t *testing.T) {
	quickReloads(t)
	f := newFixture(t, "sync", "")
	write(t, f.local, "a.txt", "local")
	write(t, f.mount, "a.txt", "remote")
	f.start()
	old := f.runnerOf("s")
	old.runMu.Lock() // as a run in progress holds it
	done := make(chan error, 1)
	go func() { done <- resolve(f.d) }()
	time.Sleep(20 * time.Millisecond) // let it reach runMu

	f.writeConfig(f.shuttleDoc("sync", "deletion_guard = 40"))
	deadline := time.Now().Add(10 * time.Second)
	for {
		old.mu.Lock()
		stopping := old.stopping
		old.mu.Unlock()
		if stopping {
			break
		}
		if time.Now().After(deadline) {
			old.runMu.Unlock()
			t.Fatal("the reload did not stop the old runner")
		}
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond) // let it close what it can
	old.runMu.Unlock()

	select {
	case err := <-done:
		if err != nil && !errors.Is(err, ErrRestarting) {
			t.Errorf("Resolve = %v, want it done or ErrRestarting", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Resolve did not return")
	}
	f.waitFor("s", "the new runner", func(Status) bool { return f.runnerOf("s") != old })
}
