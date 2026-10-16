// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package engine

import (
	"context"
	"io"
	"maps"
	"os"
	"path"
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

// The foldings of the runs the engine meets: a desktop against an SMB
// share, the phone against an NFS export, the phone against an SMB share.
var (
	smbShare    = folding{remote: true, key: fsys.SimpleFold}
	phoneNFS    = folding{local: true, key: fsys.FullFold}
	phoneSMB    = folding{local: true, remote: true, key: func(p string) string { return fsys.FullFold(fsys.SimpleFold(p)) }}
	nfcE, nfdE  = "\u00e9", "e\u0301" // é, composed and decomposed
	sharpS, ssS = "straße", "STRASSE"
)

func TestFoldingOf(t *testing.T) {
	dir := t.TempDir()
	plain, err := fsys.OpenLocal(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer plain.Close()
	phone, err := fsys.OpenLocalWith(dir, fsys.AndroidSharedStorage)
	if err != nil {
		t.Fatal(err)
	}
	defer phone.Close()
	if fo := foldingOf(&Options{Local: plain, Remote: plain}); fo.local || fo.remote || fo.key("A") != "A" {
		t.Errorf("two case-sensitive sides: %+v", fo)
	}
	fo := foldingOf(&Options{Local: phone, Remote: plain})
	if !fo.local || fo.remote || fo.key(sharpS) != fo.key(ssS) || fo.key(nfcE) != fo.key(nfdE) {
		t.Errorf("the phone's side: %+v, %q is %q", fo, sharpS, fo.key(sharpS))
	}
}

// planFolded is plan() for a run with folding fo.
func planFolded(fo folding, mode config.Mode, local, remote map[string]fsys.FileInfo, records map[string]state.Record) string {
	m := matchCase(local, remote, records, fo)
	actions := slices.Concat(m.held, Plan(mode, config.DeletionsMirror, m.local, m.remote, m.records))
	slices.SortStableFunc(actions, executionOrder)
	return summary(actions)
}

func TestMatchCase(t *testing.T) {
	file, edited := f("readme.md", 3, t0), f("README.md", 5, t1)
	tests := []struct {
		name          string
		fo            folding
		mode          config.Mode
		local, remote map[string]fsys.FileInfo
		records       map[string]state.Record
		want          string
	}{
		{"renamed locally, unchanged: nothing to copy or delete",
			smbShare, config.ModeSync, tree(f("README.md", 3, t0)), tree(file), synced(file), ""},
		{"renamed and edited locally: one copy, no deletion",
			smbShare, config.ModeSync, tree(edited), tree(file), synced(file), "copy-to-remote README.md\n"},
		{"renamed on the share, mirror push: nothing deleted",
			smbShare, config.ModePush, tree(file), tree(f("README.md", 3, t0)), synced(file), ""},
		{"first sync, spelled differently on each side",
			smbShare, config.ModeSync, tree(f("Notes.txt", 1, t0)), tree(f("notes.txt", 1, t0)), nil,
			"compare Notes.txt (else conflict)\n"},
		{"two local names the share cannot hold apart",
			smbShare, config.ModeSync, tree(f("Readme.md", 1, t0), f("README.md", 2, t0)), tree(), nil,
			"conflict README.md\nconflict Readme.md\n"},
		{"clashing folders hold everything below them",
			smbShare, config.ModeSync, tree(d("Docs"), f("Docs/a", 1, t0), d("docs"), f("docs/b", 1, t0), f("other", 1, t0)),
			tree(d("DOCS"), f("DOCS/c", 1, t0)), nil,
			"conflict Docs\nconflict docs\ncopy-to-remote other\n"},
		{"a share listing two spellings is case-sensitive: left alone",
			smbShare, config.ModeSync, tree(f("a", 1, t0)), tree(f("a", 1, t0), f("A", 2, t0)), synced(f("a", 1, t0)),
			"copy-to-local A\n"},
		{"desktop: SMB keeps ß and SS apart, so both go up",
			smbShare, config.ModeSync, tree(f(sharpS, 1, t0), f(ssS, 2, t0)), tree(), nil,
			"copy-to-remote STRASSE\ncopy-to-remote straße\n"},

		{"phone: renamed on the phone, unchanged: nothing to copy or delete",
			phoneNFS, config.ModeSync, tree(f("README.md", 3, t0)), tree(file), synced(file), ""},
		{"phone: renamed and edited on the phone: one copy under the share's name",
			phoneNFS, config.ModeSync, tree(edited), tree(file), synced(file), "copy-to-remote readme.md\n"},
		{"phone: renamed on the share: nothing deleted on the phone",
			phoneNFS, config.ModeSync, tree(file), tree(f("README.md", 3, t0)), synced(file), ""},
		{"phone: first sync, spelled differently on each side",
			phoneNFS, config.ModeSync, tree(f("Notes.txt", 1, t0)), tree(f("notes.txt", 1, t0)), nil,
			"compare notes.txt (else conflict)\n"},
		{"phone: two names on the share the phone cannot hold apart",
			phoneNFS, config.ModeSync, tree(), tree(f("Readme.md", 1, t0), f("README.md", 2, t0)), nil,
			"conflict README.md\nconflict Readme.md\n"},
		{"phone: ß and SS are one name on the phone",
			phoneNFS, config.ModePull, tree(), tree(f(sharpS, 1, t0), f(ssS, 2, t0)), nil,
			"conflict STRASSE\nconflict straße\n"},
		{"phone: so are the two spellings of é",
			phoneNFS, config.ModePull, tree(), tree(f(nfcE, 1, t0), f(nfdE, 2, t0)), nil,
			"conflict " + nfdE + "\nconflict " + nfcE + "\n"},
		{"phone: clashing folders on the share hold everything below them",
			phoneNFS, config.ModeSync, tree(d("DOCS"), f("DOCS/c", 1, t0)),
			tree(d("Docs"), f("Docs/a", 1, t0), d("docs"), f("docs/b", 1, t0), f("other", 1, t0)), nil,
			"conflict Docs\nconflict docs\ncopy-to-local other\n"},
		{"phone: a phone listing two spellings keeps case: left alone",
			phoneNFS, config.ModeSync, tree(f("a", 1, t0), f("A", 2, t0)), tree(f("a", 1, t0)), synced(f("a", 1, t0)),
			"copy-to-remote A\n"},

		{"phone and SMB: a case-only rename on the phone reaches the share",
			phoneSMB, config.ModeSync, tree(f("README.md", 3, t0)), tree(file), synced(file), ""},
		{"phone and SMB: two spellings of ß the share holds, the phone cannot",
			phoneSMB, config.ModeSync, tree(), tree(f(sharpS, 1, t0), f(ssS, 2, t0)), nil,
			"conflict STRASSE\nconflict straße\n"},
		{"phone and SMB: two names neither side holds apart",
			phoneSMB, config.ModeSync, tree(f("a", 1, t0)), tree(f("A", 2, t0)), nil,
			"compare a (else conflict)\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := planFolded(tt.fo, tt.mode, tt.local, tt.remote, tt.records); got != tt.want {
				t.Errorf("plan:\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}

func TestMatchCaseLeavesInputsAlone(t *testing.T) {
	local := tree(f("README.md", 1, t0), f("Readme.md", 1, t0))
	remote := tree(f("readme.md", 1, t0))
	records := synced(f("readme.md", 1, t0))
	l, r, rec := maps.Clone(local), maps.Clone(remote), maps.Clone(records)
	matchCase(local, remote, records, smbShare)
	matchCase(remote, local, records, phoneNFS)
	if !maps.Equal(local, l) || !maps.Equal(remote, r) || len(records) != len(rec) {
		t.Error("matchCase modified its inputs")
	}
}

func TestMatchCaseMovesTheRecord(t *testing.T) {
	m := matchCase(tree(f("README.md", 3, t0)), tree(f("readme.md", 3, t0)), synced(f("readme.md", 3, t0)), smbShare)
	if rec, ok := m.records["README.md"]; !ok || rec.Path != "README.md" {
		t.Errorf("records = %v, want the history under the local spelling", m.records)
	}
	if _, ok := m.records["readme.md"]; ok {
		t.Error("the old spelling's record is still there")
	}
	if m.remote["README.md"].Name != "README.md" {
		t.Errorf("remote entry = %+v, want it renamed to the local spelling", m.remote)
	}
}

func TestMatchCaseMovesTheRecordToTheSharesSpelling(t *testing.T) {
	m := matchCase(tree(f("README.md", 3, t0)), tree(f("readme.md", 3, t0)), synced(f("README.md", 3, t0)), phoneNFS)
	if rec, ok := m.records["readme.md"]; !ok || rec.Path != "readme.md" {
		t.Errorf("records = %v, want the history under the share's spelling", m.records)
	}
	if _, ok := m.records["README.md"]; ok {
		t.Error("the phone's spelling's record is still there")
	}
	if m.local["readme.md"].Name != "readme.md" {
		t.Errorf("local entry = %+v, want it renamed to the share's spelling", m.local)
	}
}

// remoteFiles lists the remote side's files as "name=content". The share's
// folder on this machine is case-sensitive, so tests look files up by
// listing rather than by exact name.
func remoteFiles(t *testing.T, dir string) string {
	t.Helper()
	return listing(t, dir)
}

func smbOnly(t *testing.T) {
	t.Helper()
	if !overSMB() {
		t.Skip("needs a remote side that ignores case: run with `make test-integration`")
	}
}

func TestCaseClashIsHeldOverSMB(t *testing.T) {
	smbOnly(t)
	if runtime.GOOS == "windows" {
		t.Skip("the local folder here is on NTFS, which cannot hold Readme.md beside README.md")
	}
	h := newHarness(t, config.ModeSync, config.DeletionsAdditive)
	write(t, h.localDir, "Readme.md", "lower", t0)
	write(t, h.localDir, "README.md", "UPPER", t1)
	for range 2 { // stable, not a loop of skips
		res := h.run()
		if len(res.Conflicts) != 2 || len(res.Skipped) != 0 || res.Copied != 0 {
			t.Fatalf("result = %+v, want both names held", res)
		}
		if c := res.Conflicts[0]; c.Local == nil || c.Local.Size == 0 || c.Remote != nil {
			t.Errorf("conflict sides: local %+v, remote %+v; want the local copy only", c.Local, c.Remote)
		}
		if !strings.Contains(res.Conflicts[0].Reason, "differs only in case") {
			t.Errorf("reason = %q", res.Conflicts[0].Reason)
		}
	}
	if got := remoteFiles(t, h.remoteDir); got != "" {
		t.Errorf("remote = %s, want nothing written", got)
	}
}

func TestCaseOnlyRenameOverSMB(t *testing.T) {
	smbOnly(t)
	h := newHarness(t, config.ModeSync, config.DeletionsAdditive)
	write(t, h.localDir, "readme.md", "same", t0)
	write(t, h.localDir, "Docs/a.txt", "a", t0)
	h.run()

	if err := os.Rename(filepath.Join(h.localDir, "readme.md"), filepath.Join(h.localDir, "README.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(h.localDir, "Docs"), filepath.Join(h.localDir, "docs")); err != nil {
		t.Fatal(err)
	}
	res := h.run()
	if res.Deleted != 0 || res.Versioned != 0 || len(res.Conflicts) != 0 || len(res.Skipped) != 0 || len(res.Errors) != 0 {
		t.Errorf("result = %+v, want a quiet run: the same files under new spellings", res)
	}
	if got := strings.ToLower(remoteFiles(t, h.remoteDir)); got != "docs/ docs/a.txt=a readme.md=same" {
		t.Errorf("remote = %s", got)
	}
	if res := h.run(); res.Planned != 0 {
		t.Errorf("next run planned %d actions", res.Planned)
	}
}

func TestCaseRenameWithEditOverSMB(t *testing.T) {
	smbOnly(t)
	h := newHarness(t, config.ModeSync, config.DeletionsAdditive)
	write(t, h.localDir, "notes.txt", "v1", t0)
	h.run()
	os.Remove(filepath.Join(h.localDir, "notes.txt"))
	write(t, h.localDir, "Notes.txt", "v2, renamed", t1)

	res := h.run()
	if len(res.Conflicts) != 0 || res.Copied != 1 {
		t.Errorf("result = %+v, want one copy and no conflict", res)
	}
	if got := strings.ToLower(remoteFiles(t, h.remoteDir)); got != "notes.txt=v2, renamed" {
		t.Errorf("remote = %s", got)
	}
}

// phoneFS is a folder that ignores case as the phone's shared storage does,
// for tests on a disk that keeps it: each element of a path goes by the
// spelling already on disk, if there is one, so a rename over a file keeps
// that file's name.
type phoneFS struct{ *fsys.Local }

func (p phoneFS) on(ctx context.Context, name string) string {
	if name == "." || !fsys.ValidPath(name) {
		return name // for Local to refuse
	}
	dir := "."
	for elem := range strings.SplitSeq(name, "/") {
		next := path.Join(dir, elem)
		entries, _ := p.Local.ReadDir(ctx, dir) // a folder that is not there holds no other spelling
		for _, e := range entries {
			if p.FoldKey(e.Name) == p.FoldKey(next) {
				next = e.Name
				break
			}
		}
		dir = next
	}
	return dir
}

func (p phoneFS) Stat(ctx context.Context, name string) (fsys.FileInfo, error) {
	return p.Local.Stat(ctx, p.on(ctx, name))
}

func (p phoneFS) ReadDir(ctx context.Context, name string) ([]fsys.FileInfo, error) {
	return p.Local.ReadDir(ctx, p.on(ctx, name))
}

func (p phoneFS) Open(ctx context.Context, name string) (io.ReadCloser, error) {
	return p.Local.Open(ctx, p.on(ctx, name))
}

func (p phoneFS) Create(ctx context.Context, name string) (io.WriteCloser, error) {
	return p.Local.Create(ctx, p.on(ctx, name))
}

func (p phoneFS) Rename(ctx context.Context, oldname, newname string) error {
	return p.Local.Rename(ctx, p.on(ctx, oldname), p.on(ctx, newname))
}

func (p phoneFS) Remove(ctx context.Context, name string) error {
	return p.Local.Remove(ctx, p.on(ctx, name))
}

func (p phoneFS) MkdirAll(ctx context.Context, name string) error {
	return p.Local.MkdirAll(ctx, p.on(ctx, name))
}

func (p phoneFS) Chtimes(ctx context.Context, name string, mtime time.Time) error {
	return p.Local.Chtimes(ctx, p.on(ctx, name), mtime)
}

func openPhoneFS(t *testing.T, dir string) phoneFS {
	t.Helper()
	l, err := fsys.OpenLocalWith(dir, fsys.AndroidSharedStorage)
	if err != nil {
		t.Fatal(err)
	}
	return phoneFS{l}
}

func TestPhoneFSContract(t *testing.T) {
	fsystest.Run(t, func(t *testing.T) fsys.FS {
		p := openPhoneFS(t, t.TempDir())
		t.Cleanup(func() { p.Close() })
		return p
	}, fsystest.LocalDisk)
}

// shareHoldsWhatNTFSCannot skips a test whose share, a local folder here,
// holds names NTFS cannot (a:b.txt; Readme.md beside README.md): on Windows
// the share cannot be made. In use, such a share is a Linux server's, over
// SMB or NFS.
func shareHoldsWhatNTFSCannot(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the share here is a folder on NTFS, which cannot hold its names")
	}
}

// newPhoneHarness is a harness whose local side is the phone's shared
// storage, and whose remote side keeps case apart, as an NFS export does.
func newPhoneHarness(t *testing.T, mode config.Mode, deletions config.DeletionMode) *harness {
	t.Helper()
	h := &harness{t: t, localDir: t.TempDir(), remoteDir: t.TempDir()}
	local := openPhoneFS(t, h.localDir)
	remote, err := fsys.OpenLocal(h.remoteDir)
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

func TestCaseClashOnTheShareIsHeldOnThePhone(t *testing.T) {
	shareHoldsWhatNTFSCannot(t)
	h := newPhoneHarness(t, config.ModeSync, config.DeletionsMirror)
	write(t, h.remoteDir, "Readme.md", "lower", t0)
	write(t, h.remoteDir, "README.md", "UPPER", t1)
	for range 2 { // stable, not a loop of skips
		res := h.run()
		if len(res.Conflicts) != 2 || len(res.Skipped) != 0 || res.Copied != 0 {
			t.Fatalf("result = %+v, want both names held", res)
		}
		if c := res.Conflicts[0]; c.Local != nil || c.Remote == nil {
			t.Errorf("conflict sides: local %+v, remote %+v; want the share's copy only", c.Local, c.Remote)
		}
		if !strings.Contains(res.Conflicts[0].Reason, "the local folder sees them as one name") {
			t.Errorf("reason = %q", res.Conflicts[0].Reason)
		}
	}
	if got := listing(t, h.localDir); got != "" {
		t.Errorf("phone = %s, want nothing written", got)
	}
	if got := listing(t, h.remoteDir); got != "README.md=UPPER Readme.md=lower" {
		t.Errorf("share = %s", got)
	}
}

func TestRenameOnTheShareKeepsThePhonesFile(t *testing.T) {
	h := newPhoneHarness(t, config.ModeSync, config.DeletionsMirror)
	write(t, h.remoteDir, "readme.md", "same", t0)
	h.run()
	if err := os.Rename(filepath.Join(h.remoteDir, "readme.md"), filepath.Join(h.remoteDir, "README.md")); err != nil {
		t.Fatal(err)
	}

	// Planned by exact names, this is a new README.md and a deleted
	// readme.md: the phone would write one and delete it as the other.
	res := h.run()
	if res.Deleted != 0 || len(res.Conflicts) != 0 || len(res.Errors) != 0 {
		t.Errorf("result = %+v, want nothing deleted", res)
	}
	if got := listing(t, h.localDir); !strings.EqualFold(got, "readme.md=same") {
		t.Errorf("phone = %s", got)
	}
	if got := listing(t, h.remoteDir); got != "README.md=same" {
		t.Errorf("share = %s", got)
	}
	if res := h.run(); res.Planned != 0 {
		t.Errorf("next run planned %d actions", res.Planned)
	}
}

func TestRenameOnThePhoneWithEdit(t *testing.T) {
	h := newPhoneHarness(t, config.ModeSync, config.DeletionsMirror)
	write(t, h.localDir, "notes.txt", "v1", t0)
	h.run()
	if err := os.Rename(filepath.Join(h.localDir, "notes.txt"), filepath.Join(h.localDir, "Notes.txt")); err != nil {
		t.Fatal(err)
	}
	write(t, h.localDir, "Notes.txt", "v2, renamed", t1)

	res := h.run()
	if len(res.Conflicts) != 0 || res.Copied != 1 || res.Deleted != 0 {
		t.Errorf("result = %+v, want one copy, no conflict, no deletion", res)
	}
	if got := listing(t, h.remoteDir); got != "notes.txt=v2, renamed" {
		t.Errorf("share = %s, want the share's spelling kept", got)
	}
	if res := h.run(); res.Planned != 0 {
		t.Errorf("next run planned %d actions", res.Planned)
	}
}
