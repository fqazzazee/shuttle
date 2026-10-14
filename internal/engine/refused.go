// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package engine

import (
	"path"
	"slices"
	"strings"

	"github.com/fqazzazee/shuttle/internal/fsys"
)

// leaveOutRefused takes out of each scan the names the other side cannot
// hold: Android's shared storage refuses ":" in a name, for one. A run no
// longer sees such an entry, so it is never copied, and never deleted
// either: a push that mirrors the phone leaves the share's "a:b" alone.
// Below a refused folder, only the folder is reported.
func leaveOutRefused(o *Options, local, remote *scanned) []Issue {
	var out []Issue
	take := func(s *scanned, to fsys.FS, where string, file, dir Op) {
		for p, info := range s.entries {
			err := fsys.RefuseName(to, p)
			if err == nil {
				continue
			}
			delete(s.entries, p)
			if parent := path.Dir(p); parent != "." && fsys.RefuseName(to, parent) != nil {
				continue
			}
			op := file
			if info.Kind == fsys.KindDir {
				op = dir
			}
			why := strings.TrimPrefix(err.Error(), fsys.ErrRefusedName.Error()+": ")
			out = append(out, Issue{Path: p, Op: op, Reason: where + " cannot hold this name: " + why})
		}
	}
	take(remote, o.Local, "the local folder", OpCopyToLocal, OpMkdirLocal)
	take(local, o.Remote, "the share", OpCopyToRemote, OpMkdirRemote)
	slices.SortFunc(out, func(a, b Issue) int { return strings.Compare(a.Path, b.Path) })
	return out
}
