// SPDX-License-Identifier: GPL-2.0-or-later

package engine

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/fqazzazee/shuttle/internal/config"
	"github.com/fqazzazee/shuttle/internal/fsys"
	"github.com/fqazzazee/shuttle/internal/state"
)

func TestFoldKey(t *testing.T) {
	for _, pair := range [][2]string{
		{"README.md", "readme.md"},
		{"Docs/Report.PDF", "docs/report.pdf"},
		{"ſ", "s"},     // long s folds with s, as SMB servers treat it
		{"ÉTÉ", "été"}, // non-ASCII letters fold too
	} {
		if foldKey(pair[0]) != foldKey(pair[1]) {
			t.Errorf("%q and %q should fold together", pair[0], pair[1])
		}
	}
	if foldKey("a.txt") == foldKey("b.txt") {
		t.Error("different names folded together")
	}
}

// planFolded is plan() for a remote side that ignores case.
func planFolded(mode config.Mode, local, remote map[string]fsys.FileInfo, records map[string]state.Record) string {
	m := matchCase(local, remote, records)
	actions := slices.Concat(m.held, Plan(mode, config.DeletionsMirror, m.local, m.remote, m.records))
	slices.SortStableFunc(actions, executionOrder)
	return summary(actions)
}

func TestMatchCase(t *testing.T) {
	file, edited := f("readme.md", 3, t0), f("README.md", 5, t1)
	tests := []struct {
		name          string
		mode          config.Mode
		local, remote map[string]fsys.FileInfo
		records       map[string]state.Record
		want          string
	}{
		{"renamed locally, unchanged: nothing to copy or delete",
			config.ModeSync, tree(f("README.md", 3, t0)), tree(file), synced(file), ""},
		{"renamed and edited locally: one copy, no deletion",
			config.ModeSync, tree(edited), tree(file), synced(file), "copy-to-remote README.md\n"},
		{"renamed on the share, mirror push: nothing deleted",
			config.ModePush, tree(file), tree(f("README.md", 3, t0)), synced(file), ""},
		{"first sync, spelled differently on each side",
			config.ModeSync, tree(f("Notes.txt", 1, t0)), tree(f("notes.txt", 1, t0)), nil,
			"compare Notes.txt (else conflict)\n"},
		{"two local names the share cannot hold apart",
			config.ModeSync, tree(f("Readme.md", 1, t0), f("README.md", 2, t0)), tree(), nil,
			"conflict README.md\nconflict Readme.md\n"},
		{"clashing folders hold everything below them",
			config.ModeSync, tree(d("Docs"), f("Docs/a", 1, t0), d("docs"), f("docs/b", 1, t0), f("other", 1, t0)),
			tree(d("DOCS"), f("DOCS/c", 1, t0)), nil,
			"conflict Docs\nconflict docs\ncopy-to-remote other\n"},
		{"a share listing two spellings is case-sensitive: left alone",
			config.ModeSync, tree(f("a", 1, t0)), tree(f("a", 1, t0), f("A", 2, t0)), synced(f("a", 1, t0)),
			"copy-to-local A\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := planFolded(tt.mode, tt.local, tt.remote, tt.records); got != tt.want {
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
	matchCase(local, remote, records)
	if !maps.Equal(local, l) || !maps.Equal(remote, r) || len(records) != len(rec) {
		t.Error("matchCase modified its inputs")
	}
}

func TestMatchCaseMovesTheRecord(t *testing.T) {
	m := matchCase(tree(f("README.md", 3, t0)), tree(f("readme.md", 3, t0)), synced(f("readme.md", 3, t0)))
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
