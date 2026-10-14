// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package engine

import (
	"maps"
	"slices"
	"strings"
	"unicode"

	"github.com/fqazzazee/shuttle/internal/fsys"
	"github.com/fqazzazee/shuttle/internal/state"
)

// foldKey is how a case-insensitive file system sees a name: two paths with
// the same key are the same file there. Upper- then lower-casing each rune
// also folds the odd ones out, like "ſ" (long s) and "s".
func foldKey(p string) string {
	return strings.Map(func(r rune) rune { return unicode.ToLower(unicode.ToUpper(r)) }, p)
}

// caseMatched is a scan of both sides lined up for a remote side that
// ignores case.
type caseMatched struct {
	local, remote map[string]fsys.FileInfo
	records       map[string]state.Record
	held          []Action // names the share cannot hold apart
}

// matchCase lines up a case-insensitive remote side with the local names, so
// that planning, which compares paths exactly, sees the right pairs. It does
// no I/O and leaves its inputs alone.
//
//   - A remote entry whose name differs only in case from exactly one local
//     name is taken to be that file, under the local name. Its state record
//     moves with it, so renaming readme.md to README.md locally is one file
//     keeping its history, not a deletion plus a new file.
//   - Two or more local names that differ only in case cannot both live on
//     the share. They, their counterpart on the share and everything under
//     them are held as conflicts until the user renames one.
//   - If the share itself lists two names with the same key, it does not
//     ignore case after all; those names are left as they are.
func matchCase(local, remote map[string]fsys.FileInfo, records map[string]state.Record) caseMatched {
	out := caseMatched{local: maps.Clone(local), remote: maps.Clone(remote), records: maps.Clone(records)}

	localByKey := map[string][]string{}
	for p := range local {
		k := foldKey(p)
		localByKey[k] = append(localByKey[k], p)
	}
	remoteByKey := map[string][]string{}
	for p := range remote {
		k := foldKey(p)
		remoteByKey[k] = append(remoteByKey[k], p)
	}
	// Sorted, so the outcome never depends on map order.
	for _, m := range []map[string][]string{localByKey, remoteByKey} {
		for k := range m {
			slices.Sort(m[k])
		}
	}

	// Hold every clash, and everything below a clashing folder.
	var clashes []string // keys
	for k, names := range localByKey {
		if len(names) > 1 {
			clashes = append(clashes, k)
		}
	}
	slices.Sort(clashes)
	underClash := func(p string) bool {
		k := foldKey(p)
		for _, c := range clashes {
			if strings.HasPrefix(k, c+"/") {
				return true
			}
		}
		return false
	}
	for _, k := range clashes {
		names := localByKey[k]
		for _, p := range names {
			others := slices.DeleteFunc(slices.Clone(names), func(o string) bool { return o == p })
			info := local[p]
			a := Action{Op: OpConflict, Path: p, Local: &info, alwaysHold: true,
				Reason: "differs only in case from " + strings.Join(others, ", ") +
					"; the share sees them as one name, so rename one of them"}
			if r, ok := remote[p]; ok {
				a.Remote = &r
			}
			out.held = append(out.held, a)
			delete(out.local, p)
			delete(out.records, p)
		}
		for _, r := range remoteByKey[k] {
			delete(out.remote, r)
			delete(out.records, r)
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

	// Rename remote entries to the local spelling where that is unambiguous.
	for k, rnames := range remoteByKey {
		lnames := localByKey[k]
		if len(rnames) != 1 || len(lnames) != 1 || rnames[0] == lnames[0] {
			continue
		}
		r, l := rnames[0], lnames[0]
		info, ok := out.remote[r]
		if !ok {
			continue // held above
		}
		delete(out.remote, r)
		info.Name = l
		out.remote[l] = info
		if rec, ok := out.records[r]; ok {
			if _, has := out.records[l]; !has {
				rec.Path = l
				out.records[l] = rec
			}
			delete(out.records, r)
		}
	}
	return out
}
