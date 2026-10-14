// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package engine

// Crash and fault tests. The engine promises that however a run ends — the
// process killed, the share dropping mid-run or mid-scan — nothing is lost,
// and the next run converges to exactly the state an uninterrupted run would
// have reached. These tests try every interruption point to check it.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/fqazzazee/shuttle/internal/config"
	"github.com/fqazzazee/shuttle/internal/fsys"
	"github.com/fqazzazee/shuttle/internal/state"
)

// crashEnv marks the test binary as the child process of a crash test.
const crashEnv = "SHUTTLE_CRASH_CHILD"

func TestMain(m *testing.M) {
	if os.Getenv(crashEnv) != "" {
		crashChild()
		os.Exit(0)
	}
	code := m.Run()
	stopNFS()
	os.Exit(code)
}

// crashChild runs one sync and kills itself, SIGKILL and all, at the Nth
// file system operation: a real crash, with nothing flushed or closed.
func crashChild() {
	at, err := strconv.ParseInt(os.Getenv("CRASH_AT"), 10, 64)
	if err != nil {
		panic(err)
	}
	if flushEvery, err = strconv.Atoi(os.Getenv("CRASH_FLUSH")); err != nil {
		panic(err)
	}
	ops := &atomic.Int64{}
	wrap := func(f fsys.FS) fsys.FS { return &faultFS{FS: f, ops: ops, at: at, kill: true} }
	if _, err := runSync(os.Getenv("CRASH_LOCAL"), os.Getenv("CRASH_REMOTE"), os.Getenv("CRASH_STATE"), wrap, wrap, nil); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(3)
	}
}

// faultFS counts every operation that changes a side, across both sides, and
// from the at-th one on either kills the process or fails, like a dropped
// connection.
type faultFS struct {
	fsys.FS
	ops  *atomic.Int64
	at   int64 // 0: never
	kill bool
}

func (f *faultFS) tick() error {
	if n := f.ops.Add(1); f.at == 0 || n < f.at {
		return nil
	}
	if f.kill {
		syscall.Kill(syscall.Getpid(), syscall.SIGKILL)
		select {} // never returns
	}
	return errInjected
}

func (f *faultFS) Create(ctx context.Context, name string) (io.WriteCloser, error) {
	if err := f.tick(); err != nil {
		return nil, err
	}
	w, err := f.FS.Create(ctx, name)
	if err != nil {
		return nil, err
	}
	return &faultWriter{w: w, f: f}, nil
}

func (f *faultFS) Rename(ctx context.Context, oldname, newname string) error {
	if err := f.tick(); err != nil {
		return err
	}
	return f.FS.Rename(ctx, oldname, newname)
}

func (f *faultFS) Remove(ctx context.Context, name string) error {
	if err := f.tick(); err != nil {
		return err
	}
	return f.FS.Remove(ctx, name)
}

func (f *faultFS) MkdirAll(ctx context.Context, name string) error {
	if err := f.tick(); err != nil {
		return err
	}
	return f.FS.MkdirAll(ctx, name)
}

func (f *faultFS) Chtimes(ctx context.Context, name string, mtime time.Time) error {
	if err := f.tick(); err != nil {
		return err
	}
	return f.FS.Chtimes(ctx, name, mtime)
}

// faultWriter counts each Write, so a crash can land halfway through a file.
type faultWriter struct {
	w io.WriteCloser
	f *faultFS
}

func (w *faultWriter) Write(p []byte) (int, error) {
	if err := w.f.tick(); err != nil {
		return 0, err
	}
	return w.w.Write(p)
}

func (w *faultWriter) Close() error { return w.w.Close() }

// runSync runs one sync between two folders, opening the state DB at
// statePath for the run only (a crash child needs it unlocked). wrapLocal and
// wrapRemote, when set, wrap each side; now, when set, is the run's clock.
func runSync(localDir, remoteDir, statePath string, wrapLocal, wrapRemote func(fsys.FS) fsys.FS, now func() time.Time) (*Result, error) {
	local, err := fsys.OpenLocal(localDir)
	if err != nil {
		return nil, err
	}
	defer local.Close()
	remote, err := openRemote(remoteDir)
	if err != nil {
		return nil, err
	}
	defer remote.Close()
	db, err := state.Open(statePath)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	var l, r fsys.FS = local, remote
	if wrapLocal != nil {
		l = wrapLocal(l)
	}
	if wrapRemote != nil {
		r = wrapRemote(r)
	}
	settings := config.Defaults()
	settings.Mode = config.ModeSync
	return Run(context.Background(), &Options{Local: l, Remote: r, State: db, Settings: &settings, Now: now})
}

// crashScenario lays out a synced pair of folders, then changes both sides
// the way a day of use would: edits, deletions and new files on each, a
// deleted folder, and a file big enough to take several writes.
func crashScenario(t *testing.T) (localDir, remoteDir, statePath string) {
	t.Helper()
	localDir, remoteDir = t.TempDir(), newRemoteDir(t)
	statePath = filepath.Join(t.TempDir(), "state.db")
	for _, p := range []string{"a", "b", "c", "d", "e", "dir/f", "dir/g"} {
		write(t, localDir, p, p, t0)
	}
	if res, err := runSync(localDir, remoteDir, statePath, nil, nil, nil); err != nil || len(res.Errors) > 0 {
		t.Fatalf("base sync: %v %+v", err, res)
	}

	write(t, localDir, "a", "a, edited locally", t1)
	write(t, remoteDir, "b", "b, edited remotely", t1)
	os.Remove(filepath.Join(localDir, "c"))
	os.Remove(filepath.Join(remoteDir, "d"))
	write(t, localDir, "new-local", "n1", t1)
	write(t, remoteDir, "sub/new-remote", "n2", t1)
	os.RemoveAll(filepath.Join(localDir, "dir"))
	write(t, localDir, "big.bin", strings.Repeat("0123456789abcdef", 8<<10), t1) // 128 KiB
	return localDir, remoteDir, statePath
}

// recoverAndCheck runs the recovery sync and checks the invariants: no
// errors or conflicts, both sides exactly as an uninterrupted run leaves
// them, no leftover temp files, and nothing left to do.
func recoverAndCheck(t *testing.T, label, localDir, remoteDir, statePath, want string) {
	t.Helper()
	later := func() time.Time { return time.Now().Add(2 * staleTemp) }
	res, err := runSync(localDir, remoteDir, statePath, nil, nil, later)
	if err != nil {
		t.Fatalf("%s: recovery run failed: %v", label, err)
	}
	if len(res.Errors) > 0 || len(res.Conflicts) > 0 || len(res.HeldDeletions) > 0 {
		t.Errorf("%s: recovery errors %+v, conflicts %+v, held %+v", label, res.Errors, res.Conflicts, res.HeldDeletions)
	}
	for side, dir := range map[string]string{"local": localDir, "remote": remoteDir} {
		if got := listing(t, dir); got != want {
			t.Errorf("%s: %s after recovery =\n  %s\nwant\n  %s", label, side, got, want)
		}
		filepath.WalkDir(dir, func(p string, e fs.DirEntry, err error) error {
			if err == nil && fsys.IsTemp(e.Name()) {
				t.Errorf("%s: temp file left behind: %s", label, p)
			}
			return nil
		})
	}
	if res, err := runSync(localDir, remoteDir, statePath, nil, nil, later); err != nil || res.Planned != 0 {
		t.Errorf("%s: the run after recovery planned %d actions (err %v), want 0", label, res.Planned, err)
	}
}

// reference is the state an uninterrupted run of the scenario reaches.
func reference(t *testing.T) string {
	t.Helper()
	localDir, remoteDir, statePath := crashScenario(t)
	res, err := runSync(localDir, remoteDir, statePath, nil, nil, nil)
	if err != nil || len(res.Errors) > 0 || len(res.Conflicts) > 0 {
		t.Fatalf("uninterrupted run: %v %+v", err, res)
	}
	want := listing(t, localDir)
	if got := listing(t, remoteDir); got != want {
		t.Fatalf("uninterrupted run did not converge:\nlocal  %s\nremote %s", want, got)
	}
	return want
}

// TestCrashAtEveryPoint kills a real sync process at each file system
// operation in turn, then checks that a normal run recovers completely. It
// runs twice: with records committed in batches (so a crash loses all of
// this run's records) and with a commit after every change.
func TestCrashAtEveryPoint(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a process per crash point")
	}
	want := reference(t)
	for _, flush := range []int{1000, 1} {
		crashes := 0
		for at := 1; ; at++ {
			if at > 500 {
				t.Fatal("the run never finished; is the operation count unbounded?")
			}
			localDir, remoteDir, statePath := crashScenario(t)
			cmd := exec.Command(os.Args[0], "-test.run=^$")
			cmd.Env = append(os.Environ(), crashEnv+"=1",
				"CRASH_LOCAL="+localDir, "CRASH_REMOTE="+remoteDir, "CRASH_STATE="+statePath,
				"CRASH_AT="+strconv.Itoa(at), "CRASH_FLUSH="+strconv.Itoa(flush))
			out, err := cmd.CombinedOutput()
			finished := err == nil
			var exit *exec.ExitError
			if !finished {
				if !errors.As(err, &exit) || !exit.Sys().(syscall.WaitStatus).Signaled() {
					t.Fatalf("crash child at %d: %v\n%s", at, err, out)
				}
				crashes++
			}
			recoverAndCheck(t, fmt.Sprintf("flush %d, crash at op %d", flush, at), localDir, remoteDir, statePath, want)
			if finished {
				break
			}
		}
		t.Logf("flush every %d: recovered from %d crash points", flush, crashes)
		if crashes < 20 {
			t.Errorf("only %d crash points; the scenario should exercise more", crashes)
		}
	}
}

// TestConnectionDropAtEveryPoint fails every remote operation from the Nth
// on, as when the share drops mid-run, then reconnects and recovers.
func TestConnectionDropAtEveryPoint(t *testing.T) {
	want := reference(t)
	for at := int64(1); ; at++ {
		if at > 500 {
			t.Fatal("the run never finished")
		}
		localDir, remoteDir, statePath := crashScenario(t)
		ops := &atomic.Int64{}
		drop := func(f fsys.FS) fsys.FS { return &faultFS{FS: f, ops: ops, at: at} }
		res, err := runSync(localDir, remoteDir, statePath, nil, drop, nil)
		if err != nil {
			t.Fatalf("drop at %d: run failed outright: %v", at, err)
		}
		recoverAndCheck(t, fmt.Sprintf("drop at op %d", at), localDir, remoteDir, statePath, want)
		if len(res.Errors) == 0 {
			break // the drop came after the last operation
		}
	}
}

// scanFaultFS fails listings after the first few, as when the share drops
// partway through a scan.
type scanFaultFS struct {
	fsys.FS
	left int
}

func (f *scanFaultFS) ReadDir(ctx context.Context, name string) ([]fsys.FileInfo, error) {
	if f.left == 0 {
		return nil, errInjected
	}
	f.left--
	return f.FS.ReadDir(ctx, name)
}

func TestShareDropMidScanChangesNothing(t *testing.T) {
	localDir, remoteDir, statePath := crashScenario(t)
	before := listing(t, localDir) + " | " + listing(t, remoteDir)
	drop := func(f fsys.FS) fsys.FS { return &scanFaultFS{FS: f, left: 1} }

	if _, err := runSync(localDir, remoteDir, statePath, nil, drop, nil); !errors.Is(err, errInjected) {
		t.Fatalf("run error = %v, want the scan failure", err)
	}
	if after := listing(t, localDir) + " | " + listing(t, remoteDir); after != before {
		t.Errorf("a failed scan changed files:\nbefore %s\nafter  %s", before, after)
	}
}

// Temp files from another machine's write in progress must be left alone;
// only old ones are taken to be leftovers of a crash.
func TestFreshTempFilesAreLeftAlone(t *testing.T) {
	h := newHarness(t, config.ModeSync, config.DeletionsAdditive)
	write(t, h.remoteDir, fsys.TempPrefix+"inflight", "someone else's write", time.Now())
	write(t, h.remoteDir, fsys.TempPrefix+"stale", "a crash's leftover", time.Now().Add(-2*staleTemp))
	if res := h.run(); res.TempsRemoved != 1 {
		t.Errorf("TempsRemoved = %d, want 1", res.TempsRemoved)
	}
	if _, ok := read(t, h.remoteDir, fsys.TempPrefix+"inflight"); !ok {
		t.Error("a fresh temp file was removed")
	}
}
