// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package engine

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/fqazzazee/shuttle/internal/config"
	"github.com/fqazzazee/shuttle/internal/fsys"
	"github.com/fqazzazee/shuttle/internal/fsys/fsystest"
	"github.com/fqazzazee/shuttle/internal/state"
)

// harness is a shuttle between two temp folders with its own state DB.
type harness struct {
	t         *testing.T
	localDir  string
	remoteDir string
	opts      Options
	settings  config.Settings
}

func newHarness(t *testing.T, mode config.Mode, deletions config.DeletionMode) *harness {
	t.Helper()
	h := &harness{t: t, localDir: t.TempDir(), remoteDir: newRemoteDir(t)}
	local, err := fsys.OpenLocal(h.localDir)
	if err != nil {
		t.Fatal(err)
	}
	remote, err := openRemote(h.remoteDir)
	if err != nil {
		t.Fatal(err)
	}
	db, err := state.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { local.Close(); remote.Close(); db.Close() })
	h.settings = config.Defaults()
	h.settings.Mode, h.settings.Deletions = mode, deletions
	h.opts = Options{Local: local, Remote: remote, State: db, Settings: &h.settings}
	return h
}

func (h *harness) run() *Result {
	h.t.Helper()
	res, err := Run(context.Background(), &h.opts)
	if err != nil {
		h.t.Fatalf("Run: %v", err)
	}
	if len(res.Errors) > 0 {
		h.t.Fatalf("Run reported errors: %+v", res.Errors)
	}
	return res
}

// write creates a file with content and a fixed mtime under dir.
func write(t *testing.T, dir, p, content string, mtime time.Time) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(p))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(full, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, dir, p string) (string, bool) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(p)))
	if errors.Is(err, fs.ErrNotExist) {
		return "", false
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(b), true
}

// listing renders every synced file under dir as "path=content", in walk
// order. The versions folder is left out; see versionsListing.
func listing(t *testing.T, dir string) string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(p string, e fs.DirEntry, err error) error {
		if err != nil || p == dir {
			return err
		}
		if e.IsDir() && e.Name() == VersionsDir && filepath.Dir(p) == dir {
			return fs.SkipDir
		}
		rel, _ := filepath.Rel(dir, p)
		rel = filepath.ToSlash(rel)
		if e.IsDir() {
			out = append(out, rel+"/")
			return nil
		}
		b, err := os.ReadFile(p)
		out = append(out, rel+"="+string(b))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return strings.Join(out, " ")
}

func mtimeOf(t *testing.T, dir, p string) time.Time {
	t.Helper()
	fi, err := os.Stat(filepath.Join(dir, filepath.FromSlash(p)))
	if err != nil {
		t.Fatal(err)
	}
	return fi.ModTime()
}

func TestPushEndToEnd(t *testing.T) {
	h := newHarness(t, config.ModePush, config.DeletionsMirror)
	write(t, h.localDir, "a.txt", "alpha", t0)
	write(t, h.localDir, "docs/b.txt", "bravo", t0)
	write(t, h.remoteDir, "stale.txt", "only on the share", t0)

	res := h.run()
	if got, want := listing(t, h.remoteDir), "a.txt=alpha docs/ docs/b.txt=bravo"; got != want {
		t.Errorf("remote = %s\nwant     %s", got, want)
	}
	if res.Copied != 2 || res.Created != 1 || res.Deleted != 1 || res.BytesToRemote != 10 {
		t.Errorf("result = %+v", res)
	}
	if !mtimeOf(t, h.remoteDir, "docs/b.txt").Equal(t0) {
		t.Error("modification time not carried over")
	}

	// Nothing changed, so nothing to do.
	if res := h.run(); res.Planned != 0 {
		t.Errorf("second run planned %d actions, want 0", res.Planned)
	}

	// An edit on the share is overwritten by the source of truth.
	write(t, h.remoteDir, "a.txt", "edited on the share", t1)
	h.run()
	if got, _ := read(t, h.remoteDir, "a.txt"); got != "alpha" {
		t.Errorf("remote a.txt = %q, want the local version", got)
	}
}

func TestPullAdditiveKeepsLocalExtras(t *testing.T) {
	h := newHarness(t, config.ModePull, config.DeletionsAdditive)
	write(t, h.remoteDir, "from-share.txt", "share", t0)
	write(t, h.localDir, "mine.txt", "local only", t0)

	h.run()
	if got, want := listing(t, h.localDir), "from-share.txt=share mine.txt=local only"; got != want {
		t.Errorf("local = %s\nwant    %s", got, want)
	}
	if _, ok := read(t, h.remoteDir, "mine.txt"); ok {
		t.Error("pull wrote to the share")
	}

	// Deleted on the share: additive keeps the local copy.
	os.Remove(filepath.Join(h.remoteDir, "from-share.txt"))
	h.run()
	if _, ok := read(t, h.localDir, "from-share.txt"); !ok {
		t.Error("additive pull deleted a local file")
	}
}

func TestSyncEndToEnd(t *testing.T) {
	h := newHarness(t, config.ModeSync, config.DeletionsAdditive)
	write(t, h.localDir, "local.txt", "L", t0)
	write(t, h.localDir, "dir/doomed.txt", "D", t0)
	write(t, h.remoteDir, "remote.txt", "R", t0)
	h.run()
	want := "dir/ dir/doomed.txt=D local.txt=L remote.txt=R"
	if got := listing(t, h.localDir); got != want {
		t.Fatalf("local after first sync = %s", got)
	}
	if got := listing(t, h.remoteDir); got != want {
		t.Fatalf("remote after first sync = %s", got)
	}

	// Edit on each side, delete a folder on one.
	write(t, h.localDir, "local.txt", "L2", t1)
	write(t, h.remoteDir, "remote.txt", "R2", t1)
	if err := os.RemoveAll(filepath.Join(h.localDir, "dir")); err != nil {
		t.Fatal(err)
	}
	h.run()
	want = "local.txt=L2 remote.txt=R2"
	if got := listing(t, h.localDir); got != want {
		t.Errorf("local = %s\nwant    %s", got, want)
	}
	if got := listing(t, h.remoteDir); got != want {
		t.Errorf("remote = %s\nwant     %s", got, want)
	}
	if res := h.run(); res.Planned != 0 {
		t.Errorf("third run planned %d actions, want 0", res.Planned)
	}
}

func TestSyncHoldsConflicts(t *testing.T) {
	h := newHarness(t, config.ModeSync, config.DeletionsAdditive)
	h.settings.Conflicts = config.ConflictAsk
	write(t, h.localDir, "a.txt", "base", t0)
	h.run()

	write(t, h.localDir, "a.txt", "local edit", t1)
	write(t, h.remoteDir, "a.txt", "remote edit!", t1)
	for range 2 { // and it stays held, rather than flipping on a later run
		res := h.run()
		if len(res.Conflicts) != 1 || res.Conflicts[0].Path != "a.txt" {
			t.Fatalf("conflicts = %+v", res.Conflicts)
		}
		if got, _ := read(t, h.localDir, "a.txt"); got != "local edit" {
			t.Errorf("local a.txt = %q", got)
		}
		if got, _ := read(t, h.remoteDir, "a.txt"); got != "remote edit!" {
			t.Errorf("remote a.txt = %q", got)
		}
	}
}

func TestSyncSameEditOnBothSidesIsNotAConflict(t *testing.T) {
	h := newHarness(t, config.ModeSync, config.DeletionsAdditive)
	write(t, h.localDir, "a.txt", "base", t0)
	h.run()

	// Same new content, written at different times: compared by content.
	write(t, h.localDir, "a.txt", "agreed", t1)
	write(t, h.remoteDir, "a.txt", "agreed", t1.Add(time.Hour))
	res := h.run()
	if len(res.Conflicts) != 0 || res.Adopted != 1 || res.Copied != 0 {
		t.Errorf("result = %+v, want the file adopted without copying", res)
	}
}

// readSizesFS records how much each Read of an opened file asks for.
type readSizesFS struct {
	fsys.FS
	sizes *[]int
}

func (r readSizesFS) Open(ctx context.Context, name string) (io.ReadCloser, error) {
	f, err := r.FS.Open(ctx, name)
	if err != nil {
		return nil, err
	}
	return readSizesFile{f, r.sizes}, nil
}

type readSizesFile struct {
	io.ReadCloser
	sizes *[]int
}

func (f readSizesFile) Read(p []byte) (int, error) {
	*f.sizes = append(*f.sizes, len(p))
	return f.ReadCloser.Read(p)
}

// Comparing contents reads the share's copy, a round trip per read: not
// io.Copy's 32 KB at a time.
func TestCompareReadsShareInLargeChunks(t *testing.T) {
	localDir, remoteDir := t.TempDir(), newRemoteDir(t)
	big := strings.Repeat("0123456789abcdef", 3<<20/16)
	write(t, localDir, "big.bin", big, t0)
	write(t, remoteDir, "big.bin", big, t1)

	var sizes []int
	res, err := runSync(localDir, remoteDir, filepath.Join(t.TempDir(), "state.db"), nil,
		func(f fsys.FS) fsys.FS { return readSizesFS{f, &sizes} }, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Adopted != 1 || res.Copied != 0 {
		t.Fatalf("result = %+v, want big.bin compared and adopted", res)
	}
	if len(sizes) == 0 || slices.Max(sizes) < 1<<20 {
		t.Errorf("reads of the share asked for %v bytes, want 1 MB", slices.Compact(sizes))
	}
}

func TestFirstSyncOfExistingCopies(t *testing.T) {
	h := newHarness(t, config.ModeSync, config.DeletionsAdditive)
	h.settings.Conflicts = config.ConflictAsk
	// The same tree on both sides, timestamps rounded on the share.
	write(t, h.localDir, "same.txt", "identical", t0.Add(400*time.Millisecond))
	write(t, h.remoteDir, "same.txt", "identical", t0)
	// Same size, far-apart times, different bytes.
	write(t, h.localDir, "clash.txt", "aaaa", t0)
	write(t, h.remoteDir, "clash.txt", "bbbb", t1)

	res := h.run()
	if res.Copied != 0 || res.Adopted != 1 {
		t.Errorf("result = %+v, want same.txt adopted without copying", res)
	}
	if len(res.Conflicts) != 1 || res.Conflicts[0].Path != "clash.txt" {
		t.Errorf("conflicts = %+v, want clash.txt (first sync never overwrites)", res.Conflicts)
	}
}

// A file edited between the scan and its copy must not be overwritten or
// recorded; it is picked up by the next run.
func TestFileChangedDuringRunIsSkipped(t *testing.T) {
	h := newHarness(t, config.ModeSync, config.DeletionsAdditive)
	write(t, h.localDir, "c.txt", "c", t0)
	write(t, h.localDir, "d.txt", "d", t0)
	h.opts.OnAction = func(a *Action, _ error) {
		if a.Path == "c.txt" {
			write(t, h.localDir, "d.txt", "d, edited mid-run", t1)
		}
	}
	res := h.run()
	if len(res.Skipped) != 1 || res.Skipped[0].Path != "d.txt" {
		t.Fatalf("skipped = %+v, want d.txt", res.Skipped)
	}
	if _, ok := read(t, h.remoteDir, "d.txt"); ok {
		t.Error("d.txt was copied although it changed after the scan")
	}

	h.opts.OnAction = nil
	h.run()
	if got, _ := read(t, h.remoteDir, "d.txt"); got != "d, edited mid-run" {
		t.Errorf("next run copied %q, want the edited version", got)
	}
}

// editedWhileRead is a file system whose file at path is rewritten once it
// has been opened, as if saved again while being copied; or deleted, when
// content is "".
type editedWhileRead struct {
	fsys.FS
	t         *testing.T
	dir, path string
	content   string
	mtime     time.Time
}

func (e *editedWhileRead) Open(ctx context.Context, name string) (io.ReadCloser, error) {
	r, err := e.FS.Open(ctx, name)
	if err == nil && name == e.path && e.content == "" {
		if err := os.Remove(filepath.Join(e.dir, e.path)); err != nil {
			e.t.Fatal(err)
		}
		e.path = ""
	}
	if err == nil && name == e.path {
		// Saved as editors do, as a new file renamed over the old one: the
		// copy reads the old content to the end. Windows replaces no file
		// that is open, so there the old one is moved away first, as Office
		// saves.
		write(e.t, e.dir, e.path+".new", e.content, e.mtime)
		old := filepath.Join(e.dir, e.path)
		if runtime.GOOS == "windows" {
			if err := os.Rename(old, old+".bak"); err != nil {
				e.t.Fatal(err)
			}
			defer os.Remove(old + ".bak")
		}
		if err := os.Rename(old+".new", old); err != nil {
			e.t.Fatal(err)
		}
		e.path = "" // once
	}
	return r, err
}

// A file that changes while it is being copied leaves a copy that matches
// neither version for sure. The next run copies it again; it must not take
// the copy it left behind for an edit of its own, as a conflict.
func TestFileChangedWhileCopiedIsCopiedAgain(t *testing.T) {
	for _, mode := range []config.Mode{config.ModeSync, config.ModePush} {
		t.Run(string(mode), func(t *testing.T) {
			h := newHarness(t, mode, config.DeletionsAdditive)
			write(t, h.localDir, "a.txt", "half", t0)
			h.opts.Local = &editedWhileRead{FS: h.opts.Local, t: t, dir: h.localDir, path: "a.txt",
				content: "the whole file", mtime: t1}
			if res := h.run(); len(res.Skipped) != 1 {
				t.Fatalf("skipped = %+v, want a.txt", res.Skipped)
			}
			res := h.run()
			if len(res.Conflicts) != 0 || res.Copied != 1 {
				t.Errorf("next run: conflicts %+v, copied %d; want a.txt copied again", res.Conflicts, res.Copied)
			}
			if got, _ := read(t, h.remoteDir, "a.txt"); got != "the whole file" {
				t.Errorf("remote a.txt = %q", got)
			}
			if got := listing(t, h.localDir); got != "a.txt=the whole file" {
				t.Errorf("local = %s", got)
			}
		})
	}
}

// A file deleted while it is being copied stays deleted: the copy it left
// is not brought back as a file of the other side's.
func TestFileDeletedWhileCopiedStaysDeleted(t *testing.T) {
	h := newHarness(t, config.ModeSync, config.DeletionsMirror)
	write(t, h.localDir, "a.txt", "a", t0)
	h.opts.Local = &editedWhileRead{FS: h.opts.Local, t: t, dir: h.localDir, path: "a.txt"}
	h.run()
	h.run()
	if got := listing(t, h.localDir); got != "" {
		t.Errorf("local = %s, want a.txt still deleted", got)
	}
	if _, ok := read(t, h.remoteDir, "a.txt"); ok {
		t.Error("the copy of a deleted file stayed on the share")
	}
}

// The other direction: an edit on the destination after the scan must not be
// overwritten by the copy that was planned before it.
func TestDestinationEditedDuringRunIsNotOverwritten(t *testing.T) {
	h := newHarness(t, config.ModeSync, config.DeletionsAdditive)
	write(t, h.localDir, "a.txt", "a", t0)
	write(t, h.localDir, "b.txt", "b", t0)
	h.run()

	write(t, h.localDir, "b.txt", "b, local edit", t1)
	// Plan copies b.txt to the remote; edit the remote b.txt just before.
	write(t, h.localDir, "a.txt", "a2", t1)
	h.opts.OnAction = func(a *Action, _ error) {
		if a.Path == "a.txt" {
			write(t, h.remoteDir, "b.txt", "b, remote edit", t1.Add(time.Minute))
		}
	}
	res := h.run()
	if len(res.Skipped) != 1 || res.Skipped[0].Path != "b.txt" {
		t.Fatalf("skipped = %+v, want b.txt", res.Skipped)
	}
	if got, _ := read(t, h.remoteDir, "b.txt"); got != "b, remote edit" {
		t.Errorf("remote b.txt = %q: a concurrent edit was overwritten", got)
	}

	// Next run: both sides changed since the last sync, so it is a conflict.
	h.opts.OnAction = nil
	if res := h.run(); len(res.Conflicts) != 1 {
		t.Errorf("conflicts = %+v, want b.txt", res.Conflicts)
	}
}

// With history, two same-size edits made close together are still compared
// by content: the timestamp shortcut is only for files with no history.
func TestCloseTimestampsDoNotHideAConflict(t *testing.T) {
	h := newHarness(t, config.ModeSync, config.DeletionsAdditive)
	write(t, h.localDir, "a.txt", "base", t0)
	h.run()

	write(t, h.localDir, "a.txt", "mine", t1)
	write(t, h.remoteDir, "a.txt", "your", t1.Add(time.Second))
	if res := h.run(); len(res.Conflicts) != 1 || res.Adopted != 0 {
		t.Errorf("result = %+v, want a conflict", res)
	}
}

// If part of the local tree cannot be read, the run must stop: carrying on
// would read the unreadable files as deleted and delete them remotely.
func TestUnreadableFolderStopsTheRun(t *testing.T) {
	h := newHarness(t, config.ModeSync, config.DeletionsAdditive)
	write(t, h.localDir, "private/notes.txt", "n", t0)
	h.run()

	fsystest.Unreadable(t, filepath.Join(h.localDir, "private"))

	res, err := Run(context.Background(), &h.opts)
	if err == nil {
		t.Fatal("Run succeeded with an unreadable folder")
	}
	if res.Planned != 0 {
		t.Errorf("planned %d actions after a failed scan", res.Planned)
	}
	if _, ok := read(t, h.remoteDir, "private/notes.txt"); !ok {
		t.Error("the remote copy was deleted")
	}
}

func TestSkipsSymlinksTempFilesAndVersions(t *testing.T) {
	h := newHarness(t, config.ModePush, config.DeletionsAdditive)
	write(t, h.localDir, "real.txt", "r", t0)
	write(t, h.localDir, fsys.TempPrefix+"abc", "half-written", t0)
	write(t, h.localDir, VersionsDir+"/old.txt", "kept version", t0)
	if err := os.Symlink("/etc/passwd", filepath.Join(h.localDir, "link")); err != nil {
		t.Fatal(err)
	}

	res := h.run()
	if got := listing(t, h.remoteDir); got != "real.txt=r" {
		t.Errorf("remote = %s, want only real.txt", got)
	}
	if !slices.Equal(res.Unsupported, []string{"local: link"}) {
		t.Errorf("unsupported = %v", res.Unsupported)
	}
}

func TestCancelledRun(t *testing.T) {
	h := newHarness(t, config.ModePush, config.DeletionsAdditive)
	write(t, h.localDir, "a.txt", "a", t0)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Run(ctx, &h.opts); !errors.Is(err, context.Canceled) {
		t.Errorf("Run error = %v, want context.Canceled", err)
	}
	if got := listing(t, h.remoteDir); got != "" {
		t.Errorf("remote = %s, want nothing", got)
	}
}

func TestPreviewChangesNothing(t *testing.T) {
	h := newHarness(t, config.ModeSync, config.DeletionsAdditive)
	write(t, h.localDir, "a.txt", "a", t0)
	actions, err := Preview(context.Background(), &h.opts)
	if err != nil {
		t.Fatal(err)
	}
	if summary(actions) != "copy-to-remote a.txt\n" {
		t.Errorf("preview = %q", summary(actions))
	}
	if got := listing(t, h.remoteDir); got != "" {
		t.Errorf("remote = %s after a preview", got)
	}
}

// Stop is asked between actions: the copy in progress finishes, nothing
// more starts, what was done is recorded, and the next run does the rest.
func TestStopFinishesTheCurrentAction(t *testing.T) {
	h := newHarness(t, config.ModePush, config.DeletionsAdditive)
	for _, p := range []string{"a", "b", "c"} {
		write(t, h.localDir, p, p, t0)
	}
	done := 0
	h.opts.OnAction = func(*Action, error) { done++ }
	h.opts.Stop = func() bool { return done >= 1 }

	res, err := Run(context.Background(), &h.opts)
	if !errors.Is(err, ErrStopped) {
		t.Fatalf("Run error = %v, want ErrStopped", err)
	}
	if res.Copied != 1 {
		t.Errorf("copied %d before stopping, want exactly 1", res.Copied)
	}
	if got := listing(t, h.remoteDir); got != "a=a" {
		t.Errorf("remote = %s, want only the finished copy", got)
	}

	h.opts.Stop, h.opts.OnAction = nil, nil
	res = h.run()
	if res.Planned != 2 || res.Copied != 2 {
		t.Errorf("next run planned %d, copied %d; want the remaining 2 (a was recorded)", res.Planned, res.Copied)
	}
}

// A file that cannot be read is an error of its own, not a failed scan: the
// rest of the run goes on, and the file is copied once it can be read.
func TestUnreadableFileIsItsOwnError(t *testing.T) {
	h := newHarness(t, config.ModePush, config.DeletionsAdditive)
	write(t, h.localDir, "locked.txt", "l", t0)
	write(t, h.localDir, "ok.txt", "ok", t0)
	fsystest.Unreadable(t, filepath.Join(h.localDir, "locked.txt"))

	res, err := Run(context.Background(), &h.opts)
	if err != nil {
		t.Fatalf("the run failed: %v", err)
	}
	if len(res.Errors) != 1 || res.Errors[0].Path != "locked.txt" || res.Copied != 1 {
		t.Errorf("errors %+v, copied %d; want locked.txt's error and ok.txt copied", res.Errors, res.Copied)
	}
	if got, _ := read(t, h.remoteDir, "ok.txt"); got != "ok" {
		t.Errorf("ok.txt on the share = %q", got)
	}
}
