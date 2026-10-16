// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package engine

import (
	"maps"
	"slices"
	"strings"

	"github.com/fqazzazee/shuttle/internal/fsys"
	"github.com/fqazzazee/shuttle/internal/state"
)

// folding is which sides of a run ignore case, and the key under which
// names are one file on either side that does.
type folding struct {
	local, remote bool
	key           func(string) string
}

// foldingOf is the folding of a run between o's two sides. Where both
// sides ignore case, a name's key is its key on each in turn, so names
// either side takes as one are matched.
func foldingOf(o *Options) folding {
	return folding{
		local:  fsys.FoldsCase(o.Local),
		remote: fsys.FoldsCase(o.Remote),
		key:    func(p string) string { return fsys.FoldKey(o.Local, fsys.FoldKey(o.Remote, p)) },
	}
}

// caseMatched is a scan of both sides lined up for a run where a side
// ignores case.
type caseMatched struct {
	local, remote map[string]fsys.FileInfo
	records       map[string]state.Record
	held          []Action // names a side cannot hold apart
	// moved maps a name to the other spelling its record was under; a
	// change to it moves the record for good. forget drops records left
	// under an old spelling beside one under the new.
	moved  map[string]string
	forget []Action
}

// matchCase lines up the two scans of a run where one side or both ignore
// case, so that planning, which compares paths exactly, sees the right
// pairs. It does no I/O and leaves its inputs alone.
//
//   - An entry whose name differs only in case from exactly one name on the
//     other side is taken to be that file. Both go by the local spelling when
//     the share ignores case, so renaming readme.md to README.md locally is
//     one file keeping its history, not a deletion plus a new file. When only
//     the local side ignores case, both go by the share's spelling, the one a
//     case-sensitive share needs. The state record moves with the name.
//   - Two or more names on one side that differ only in case cannot both live
//     on the other side if it ignores case. They, their counterpart there and
//     everything under them are held as conflicts until the user renames one.
//   - Two names with one key on a side whose other side keeps case are left
//     as they are: that side can hold them apart. A share that ignores case
//     and still lists two such names does not ignore case after all.
func matchCase(local, remote map[string]fsys.FileInfo, records map[string]state.Record, fo folding) caseMatched {
	out := caseMatched{local: maps.Clone(local), remote: maps.Clone(remote), records: maps.Clone(records), moved: map[string]string{}}
	localByKey, remoteByKey := byKey(local, fo.key), byKey(remote, fo.key)

	// Hold every clash, and everything below a clashing folder.
	clashed := map[string]bool{}
	for k, names := range localByKey {
		if fo.remote && len(names) > 1 {
			clashed[k] = true
		}
	}
	for k, names := range remoteByKey {
		if fo.local && len(names) > 1 {
			clashed[k] = true
		}
	}
	clashes := slices.Sorted(maps.Keys(clashed))
	underClash := func(p string) bool {
		k := fo.key(p)
		for _, c := range clashes {
			if strings.HasPrefix(k, c+"/") {
				return true
			}
		}
		return false
	}
	for _, k := range clashes {
		// The side listing the names holds them; the other side's spelling
		// goes with them.
		names, other, otherNames := localByKey[k], out.remote, remoteByKey[k]
		where := "the share sees them as one name, so rename one of them"
		if !fo.remote || len(names) < 2 {
			names, other, otherNames = remoteByKey[k], out.local, localByKey[k]
			where = "the local folder sees them as one name, so rename one of them on the share"
		}
		for _, p := range names {
			others := slices.DeleteFunc(slices.Clone(names), func(o string) bool { return o == p })
			a := Action{Op: OpConflict, Path: p, alwaysHold: true,
				Reason: "differs only in case from " + strings.Join(others, ", ") + "; " + where}
			if info, ok := local[p]; ok {
				a.Local = &info
			}
			if info, ok := remote[p]; ok {
				a.Remote = &info
			}
			out.held = append(out.held, a)
			delete(out.local, p)
			delete(out.remote, p)
			delete(out.records, p)
		}
		for _, p := range otherNames {
			delete(other, p)
			delete(out.records, p)
		}
	}
	for _, m := range []map[string]fsys.FileInfo{out.local, out.remote} {
		for p := range m {
			if underClash(p) {
				delete(m, p)
				delete(out.records, p)
			}
		}
	}

	// Give the two spellings of one file one name, where that is unambiguous.
	for k, rnames := range remoteByKey {
		lnames := localByKey[k]
		if len(rnames) != 1 || len(lnames) != 1 || rnames[0] == lnames[0] {
			continue
		}
		from, to, m := rnames[0], lnames[0], out.remote
		if !fo.remote {
			from, to, m = lnames[0], rnames[0], out.local
		}
		info, ok := m[from]
		if !ok {
			continue // held above
		}
		delete(m, from)
		info.Name = to
		m[to] = info
		if rec, ok := out.records[from]; ok {
			if _, has := out.records[to]; !has {
				rec.Path = to
				out.records[to] = rec
				out.moved[to] = from
			} else {
				out.forget = append(out.forget, Action{Op: OpForget, Path: from})
			}
			delete(out.records, from)
		}
	}
	return out
}

// byKey groups the names in entries by key, each group sorted so that the
// outcome never depends on map order.
func byKey(entries map[string]fsys.FileInfo, key func(string) string) map[string][]string {
	m := map[string][]string{}
	for p := range entries {
		k := key(p)
		m[k] = append(m[k], p)
	}
	for k := range m {
		slices.Sort(m[k])
	}
	return m
}
