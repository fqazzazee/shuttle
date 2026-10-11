// SPDX-License-Identifier: GPL-2.0-or-later

package engine

import (
	"fmt"

	"github.com/fqazzazee/shuttle/internal/fsys"
)

// guardMinFiles is the fewest file deletions on one side that can trip the
// guard. Below it, deleting most of a small folder is ordinary use, and
// versioning keeps what was deleted.
const guardMinFiles = 10

// guardDeletions holds back every deletion on a side when they would remove
// more than percent of that side's files at once: the signature of an empty
// mount, a wiped share or a wrong folder, rather than of someone tidying up.
// percent 0 turns the guard off. It returns the actions to run and the
// deletions it held.
func guardDeletions(actions []Action, local, remote map[string]fsys.FileInfo, percent int) ([]Action, []Issue) {
	if percent <= 0 {
		return actions, nil
	}
	trips := func(del Op, side map[string]fsys.FileInfo) bool {
		files, gone := 0, 0
		for _, info := range side {
			if info.Kind == fsys.KindFile {
				files++
			}
		}
		for _, a := range actions {
			if a.Op == del && (a.Local != nil && a.Local.Kind == fsys.KindFile || a.Remote != nil && a.Remote.Kind == fsys.KindFile) {
				gone++
			}
		}
		return gone >= guardMinFiles && gone*100 > percent*files
	}
	holdLocal, holdRemote := trips(OpDeleteLocal, local), trips(OpDeleteRemote, remote)
	if !holdLocal && !holdRemote {
		return actions, nil
	}
	kept := actions[:0:0]
	var held []Issue
	for _, a := range actions {
		if a.Op == OpDeleteLocal && holdLocal || a.Op == OpDeleteRemote && holdRemote {
			side := "local"
			if a.Op == OpDeleteRemote {
				side = "remote"
			}
			held = append(held, Issue{Path: a.Path, Op: a.Op,
				Reason: fmt.Sprintf("more than %d%% of the %s files would be deleted at once", percent, side)})
			continue
		}
		kept = append(kept, a)
	}
	return kept, held
}
