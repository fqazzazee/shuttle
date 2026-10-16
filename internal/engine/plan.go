// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package engine

import (
	"cmp"
	"maps"
	"slices"
	"strings"

	"github.com/fqazzazee/shuttle/internal/config"
	"github.com/fqazzazee/shuttle/internal/fsys"
	"github.com/fqazzazee/shuttle/internal/state"
)

// Op is one kind of planned step.
type Op uint8

const (
	OpMkdirLocal   Op = iota // create the directory on the local side
	OpMkdirRemote            // create the directory on the remote side
	OpCopyToLocal            // copy the remote file over the local side
	OpCopyToRemote           // copy the local file over the remote side
	OpDeleteLocal            // delete the local file or (empty) directory
	OpDeleteRemote           // delete the remote file or (empty) directory
	OpAdopt                  // both sides already agree: record them as synced
	OpCompare                // same name, no usable history: compare contents
	OpForget                 // gone from both sides: drop the record
	OpConflict               // cannot be decided automatically: hold it
	OpPrune                  // remove versions that retention lets go (never planned)
)

var opNames = [...]string{
	"mkdir-local", "mkdir-remote", "copy-to-local", "copy-to-remote", "delete-local",
	"delete-remote", "adopt", "compare", "forget", "conflict", "prune-versions",
}

func (o Op) String() string { return opNames[o] }

// Action is one planned step for one path.
type Action struct {
	Op   Op
	Path string
	// Local and Remote are the entries as scanned; nil when absent. The
	// executor re-checks them before acting, so a file that changed after the
	// scan is skipped rather than overwritten.
	Local, Remote *fsys.FileInfo
	// OnDiffer is what an OpCompare does if the contents differ: a copy for
	// push and pull, OpConflict for sync.
	OnDiffer Op
	// Reason explains an OpConflict.
	Reason string
	// NoHistory is set when the path has no state record.
	NoHistory bool

	hash       []byte // content hash found by a compare, for an OpAdopt
	skip       bool   // already reported; nothing to execute
	alwaysHold bool   // a conflict no policy can settle (names differing only in case)
	was        string // the record's path before matchCase moved it to Path's spelling
}

// sideStatus is one side of a path compared with its record.
type sideStatus uint8

const (
	absent  sideStatus = iota // not there, and never synced
	added                     // there, never synced
	same                      // there, unchanged since the last sync
	changed                   // there, changed since the last sync
	removed                   // not there, but it was synced
)

func classify(info *fsys.FileInfo, rec *state.Record, side state.Side) sideStatus {
	switch {
	case rec == nil && info == nil:
		return absent
	case rec == nil:
		return added
	case info == nil:
		return removed
	case info.Kind != rec.Kind:
		return changed
	case info.Kind == fsys.KindDir:
		return same
	case info.Size == side.Size && info.ModTime.Equal(side.ModTime):
		return same
	default:
		return changed
	}
}

// Plan works out what to do for every path of a shuttle, given both scans
// and the state records. It does no I/O. Actions come out in execution
// order: directories to create (parents first), then copies and record
// updates, then deletions (children first).
func Plan(mode config.Mode, deletions config.DeletionMode, local, remote map[string]fsys.FileInfo, records map[string]state.Record) []Action {
	paths := map[string]struct{}{}
	for _, m := range []map[string]fsys.FileInfo{local, remote} {
		for p := range m {
			paths[p] = struct{}{}
		}
	}
	for p := range records {
		paths[p] = struct{}{}
	}

	var actions []Action
	var held []string // conflicted paths; nothing below them is touched
	for _, p := range slices.Sorted(maps.Keys(paths)) {
		if under(p, held) {
			continue
		}
		l, r := entry(local, p), entry(remote, p)
		var rec *state.Record
		if x, ok := records[p]; ok {
			rec = &x
		}
		a, ok := decide(mode, deletions, l, r, rec)
		if !ok {
			continue
		}
		a.Path, a.Local, a.Remote, a.NoHistory = p, l, r, rec == nil
		if a.Op == OpConflict {
			held = append(held, p)
		}
		actions = append(actions, a)
	}
	keepNonEmptyDirs(actions, local, remote)
	slices.SortStableFunc(actions, executionOrder)
	return actions
}

func entry(m map[string]fsys.FileInfo, p string) *fsys.FileInfo {
	if x, ok := m[p]; ok {
		return &x
	}
	return nil
}

// under reports whether p lies inside any of dirs.
func under(p string, dirs []string) bool {
	for _, d := range dirs {
		if strings.HasPrefix(p, d+"/") {
			return true
		}
	}
	return false
}

// decide picks the action for one path. ok is false when nothing needs doing.
func decide(mode config.Mode, deletions config.DeletionMode, l, r *fsys.FileInfo, rec *state.Record) (Action, bool) {
	var ls, rs sideStatus
	if rec != nil {
		ls, rs = classify(l, rec, rec.Local), classify(r, rec, rec.Remote)
	} else {
		ls, rs = classify(l, nil, state.Side{}), classify(r, nil, state.Side{})
	}

	if l != nil && r != nil && l.Kind != r.Kind {
		return Action{Op: OpConflict, Reason: "a file on one side and a folder on the other"}, true
	}

	switch mode {
	case config.ModePush:
		return oneWay(deletions, l, r, rec, ls, rs, OpMkdirRemote, OpCopyToRemote, OpDeleteRemote)
	case config.ModePull:
		return oneWay(deletions, r, l, rec, rs, ls, OpMkdirLocal, OpCopyToLocal, OpDeleteLocal)
	default:
		return twoWay(l, r, rec, ls, rs)
	}
}

// needsDirRecord reports whether a directory present on both sides still has
// to be recorded: it never was, or it was recorded as a file.
func needsDirRecord(rec *state.Record) bool {
	return rec == nil || rec.Kind != fsys.KindDir
}

// oneWay decides a push or pull. src is the side that wins; dst follows it.
func oneWay(deletions config.DeletionMode, src, dst *fsys.FileInfo, rec *state.Record,
	ss, ds sideStatus, mkdir, copyOp, del Op) (Action, bool) {
	switch {
	case src == nil && dst == nil:
		return Action{Op: OpForget}, rec != nil
	case src == nil:
		// Gone from the source. Mirror makes the destination match; additive
		// leaves it, and forgets it was ever synced.
		if deletions == config.DeletionsMirror {
			return Action{Op: del}, true
		}
		return Action{Op: OpForget}, rec != nil
	case src.Kind == fsys.KindDir:
		if dst == nil {
			return Action{Op: mkdir}, true
		}
		return Action{Op: OpAdopt}, needsDirRecord(rec)
	case ss == same && ds == same:
		return Action{}, false
	case rec == nil && dst != nil:
		// Both sides have the file and there is no history: copy only if the
		// contents actually differ.
		return Action{Op: OpCompare, OnDiffer: copyOp}, true
	default:
		// The source changed, or the destination was changed or deleted
		// behind our back. The source wins either way.
		return Action{Op: copyOp}, true
	}
}

// twoWay decides a sync: whichever side changed since the last sync wins, an
// edit beats a deletion, and a change on both sides is a conflict unless the
// contents turn out to be the same.
func twoWay(l, r *fsys.FileInfo, rec *state.Record, ls, rs sideStatus) (Action, bool) {
	switch {
	case l == nil && r == nil:
		return Action{Op: OpForget}, rec != nil

	case l != nil && r != nil:
		if l.Kind == fsys.KindDir {
			return Action{Op: OpAdopt}, needsDirRecord(rec)
		}
		switch {
		case ls == same && rs == same:
			return Action{}, false
		case ls == same:
			return Action{Op: OpCopyToLocal}, true
		case rs == same:
			return Action{Op: OpCopyToRemote}, true
		default:
			// Changed on both sides, or present on both with no history
			// (first sync): identical contents are fine, anything else is a
			// conflict.
			return Action{Op: OpCompare, OnDiffer: OpConflict, Reason: "changed on both sides"}, true
		}

	case r == nil: // only local has it
		if rs == removed && ls == same {
			return Action{Op: OpDeleteLocal}, true // deleted remotely, untouched here
		}
		if l.Kind == fsys.KindDir {
			return Action{Op: OpMkdirRemote}, true
		}
		return Action{Op: OpCopyToRemote}, true // new here, or edited here (edit beats delete)

	default: // only remote has it
		if ls == removed && rs == same {
			return Action{Op: OpDeleteRemote}, true
		}
		if r.Kind == fsys.KindDir {
			return Action{Op: OpMkdirLocal}, true
		}
		return Action{Op: OpCopyToLocal}, true
	}
}

// keepNonEmptyDirs turns a planned directory deletion into re-creating the
// directory on the other side when something inside it survives on the side
// being deleted from, e.g. a folder deleted remotely while a new file was
// added to it locally.
func keepNonEmptyDirs(actions []Action, local, remote map[string]fsys.FileInfo) {
	deleted := map[Op]map[string]bool{OpDeleteLocal: {}, OpDeleteRemote: {}}
	for _, a := range actions {
		if m, ok := deleted[a.Op]; ok {
			m[a.Path] = true
		}
	}
	localPaths := slices.Sorted(maps.Keys(local))
	remotePaths := slices.Sorted(maps.Keys(remote))
	for i := range actions {
		a := &actions[i]
		switch {
		case a.Op == OpDeleteLocal && a.Local != nil && a.Local.Kind == fsys.KindDir &&
			survives(localPaths, deleted[OpDeleteLocal], a.Path):
			a.Op = OpMkdirRemote
		case a.Op == OpDeleteRemote && a.Remote != nil && a.Remote.Kind == fsys.KindDir &&
			survives(remotePaths, deleted[OpDeleteRemote], a.Path):
			a.Op = OpMkdirLocal
		}
	}
}

// survives reports whether any path under dir in the sorted paths is not
// being deleted. Everything under "dir/" is contiguous in sorted order.
func survives(sorted []string, deleted map[string]bool, dir string) bool {
	prefix := dir + "/"
	i, _ := slices.BinarySearch(sorted, prefix)
	for ; i < len(sorted) && strings.HasPrefix(sorted[i], prefix); i++ {
		if !deleted[sorted[i]] {
			return true
		}
	}
	return false
}

// executionOrder: mkdirs parents-first, then everything that is not a
// deletion in path order, then deletions children-first.
func executionOrder(a, b Action) int { //nolint:gocritic // slices.SortStableFunc passes values
	phase := func(o Op) int {
		switch o {
		case OpMkdirLocal, OpMkdirRemote:
			return 0
		case OpDeleteLocal, OpDeleteRemote:
			return 2
		default:
			return 1
		}
	}
	pa, pb := phase(a.Op), phase(b.Op)
	if pa != pb {
		return cmp.Compare(pa, pb)
	}
	if pa == 2 {
		return cmp.Compare(b.Path, a.Path) // children (longer, later-sorting) first
	}
	return cmp.Compare(a.Path, b.Path)
}
