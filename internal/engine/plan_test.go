// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package engine

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/fqazzazee/shuttle/internal/config"
	"github.com/fqazzazee/shuttle/internal/fsys"
	"github.com/fqazzazee/shuttle/internal/state"
)

var (
	t0 = time.Unix(1_700_000_000, 0)
	t1 = t0.Add(time.Hour)
)

func f(p string, size int64, mtime time.Time) fsys.FileInfo {
	return fsys.FileInfo{Name: p, Kind: fsys.KindFile, Size: size, ModTime: mtime}
}

func d(p string) fsys.FileInfo {
	return fsys.FileInfo{Name: p, Kind: fsys.KindDir, ModTime: t0}
}

func tree(entries ...fsys.FileInfo) map[string]fsys.FileInfo {
	m := map[string]fsys.FileInfo{}
	for _, e := range entries {
		m[e.Name] = e
	}
	return m
}

// synced records entries as synced with identical sides.
func synced(entries ...fsys.FileInfo) map[string]state.Record {
	m := map[string]state.Record{}
	for _, e := range entries {
		side := state.Side{Size: e.Size, ModTime: e.ModTime}
		m[e.Name] = state.Record{Path: e.Name, Kind: e.Kind, Local: side, Remote: side}
	}
	return m
}

// summary renders a plan as "op path" lines, e.g. "copy-to-remote a.txt".
func summary(actions []Action) string {
	var b strings.Builder
	for _, a := range actions {
		fmt.Fprintf(&b, "%s %s", a.Op, a.Path)
		if a.Op == OpCompare {
			fmt.Fprintf(&b, " (else %s)", a.OnDiffer)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func TestPlanSync(t *testing.T) {
	file, edited := f("a.txt", 3, t0), f("a.txt", 5, t1)
	tests := []struct {
		name          string
		local, remote map[string]fsys.FileInfo
		records       map[string]state.Record
		want          string
	}{
		{"new local file", tree(file), tree(), nil, "copy-to-remote a.txt\n"},
		{"new remote file", tree(), tree(file), nil, "copy-to-local a.txt\n"},
		{"first sync, on both sides", tree(file), tree(file), nil, "compare a.txt (else conflict)\n"},
		{"in sync", tree(file), tree(file), synced(file), ""},
		{"edited locally", tree(edited), tree(file), synced(file), "copy-to-remote a.txt\n"},
		{"edited remotely", tree(file), tree(edited), synced(file), "copy-to-local a.txt\n"},
		{"edited on both sides", tree(edited), tree(f("a.txt", 9, t1)), synced(file), "compare a.txt (else conflict)\n"},
		{"deleted locally", tree(), tree(file), synced(file), "delete-remote a.txt\n"},
		{"deleted remotely", tree(file), tree(), synced(file), "delete-local a.txt\n"},
		{"deleted locally, edited remotely", tree(), tree(edited), synced(file), "copy-to-local a.txt\n"},
		{"edited locally, deleted remotely", tree(edited), tree(), synced(file), "copy-to-remote a.txt\n"},
		{"deleted on both sides", tree(), tree(), synced(file), "forget a.txt\n"},
		{"new local folder", tree(d("dir"), f("dir/x", 1, t0)), tree(), nil,
			"mkdir-remote dir\ncopy-to-remote dir/x\n"},
		{"folder on both sides, no history", tree(d("dir")), tree(d("dir")), nil, "adopt dir\n"},
		{"folder recorded as a file", tree(d("dir")), tree(d("dir")), synced(f("dir", 1, t0)), "adopt dir\n"},
		{"file here, folder there", tree(f("x", 1, t0)), tree(d("x"), f("x/inner", 1, t0)), nil,
			"conflict x\n"},
		{"folder deleted remotely while a file was added to it locally",
			tree(d("dir"), f("dir/old", 1, t0), f("dir/new", 1, t1)), tree(),
			synced(d("dir"), f("dir/old", 1, t0)),
			// dir/old goes (deleted remotely, untouched here); dir stays because
			// dir/new survives, and is re-created remotely.
			"mkdir-remote dir\ncopy-to-remote dir/new\ndelete-local dir/old\n"},
		{"folder deleted locally, all contents unchanged remotely",
			tree(), tree(d("dir"), f("dir/a", 1, t0), d("dir/sub"), f("dir/sub/b", 1, t0)),
			synced(d("dir"), f("dir/a", 1, t0), d("dir/sub"), f("dir/sub/b", 1, t0)),
			// Children before parents.
			"delete-remote dir/sub/b\ndelete-remote dir/sub\ndelete-remote dir/a\ndelete-remote dir\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := summary(Plan(config.ModeSync, config.DeletionsAdditive, tt.local, tt.remote, tt.records))
			if got != tt.want {
				t.Errorf("plan:\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}

func TestPlanPush(t *testing.T) {
	file, edited := f("a.txt", 3, t0), f("a.txt", 5, t1)
	tests := []struct {
		name          string
		deletions     config.DeletionMode
		local, remote map[string]fsys.FileInfo
		records       map[string]state.Record
		want          string
	}{
		{"new local file", config.DeletionsAdditive, tree(file), tree(), nil, "copy-to-remote a.txt\n"},
		{"in sync", config.DeletionsAdditive, tree(file), tree(file), synced(file), ""},
		{"edited remotely: local wins", config.DeletionsAdditive, tree(file), tree(edited), synced(file), "copy-to-remote a.txt\n"},
		{"deleted remotely: restored", config.DeletionsAdditive, tree(file), tree(), synced(file), "copy-to-remote a.txt\n"},
		{"on both sides, no history", config.DeletionsAdditive, tree(file), tree(edited), nil,
			"compare a.txt (else copy-to-remote)\n"},
		{"remote-only file, additive", config.DeletionsAdditive, tree(), tree(file), nil, ""},
		{"remote-only file, mirror", config.DeletionsMirror, tree(), tree(file), nil, "delete-remote a.txt\n"},
		{"deleted locally, additive", config.DeletionsAdditive, tree(), tree(file), synced(file), "forget a.txt\n"},
		{"deleted locally, mirror", config.DeletionsMirror, tree(), tree(file), synced(file), "delete-remote a.txt\n"},
		{"local edits never pulled back", config.DeletionsAdditive, tree(), tree(edited), nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := summary(Plan(config.ModePush, tt.deletions, tt.local, tt.remote, tt.records))
			if got != tt.want {
				t.Errorf("plan:\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}

// Pull is push with the sides swapped; spot-check that it really is.
func TestPlanPull(t *testing.T) {
	file := f("a.txt", 3, t0)
	got := summary(Plan(config.ModePull, config.DeletionsMirror,
		tree(file, f("extra", 1, t0)), tree(f("a.txt", 4, t1), f("new", 1, t0)), synced(file)))
	want := "copy-to-local a.txt\ncopy-to-local new\ndelete-local extra\n"
	if got != want {
		t.Errorf("plan:\n%s\nwant:\n%s", got, want)
	}
}

func TestPlanMarksNoHistory(t *testing.T) {
	file := f("a", 1, t0)
	for _, tt := range []struct {
		records map[string]state.Record
		want    bool
	}{{nil, true}, {synced(f("a", 2, t1)), false}} {
		actions := Plan(config.ModeSync, config.DeletionsAdditive, tree(file), tree(f("a", 1, t1)), tt.records)
		if len(actions) != 1 || actions[0].NoHistory != tt.want {
			t.Errorf("records %v: NoHistory = %v, want %v", tt.records != nil, actions[0].NoHistory, tt.want)
		}
	}
}
