// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package engine

import (
	"context"
	"fmt"
	"io/fs"
	"maps"
	"path"
	"slices"
	"sync"

	"github.com/fqazzazee/shuttle/internal/fsys"
	"github.com/fqazzazee/shuttle/internal/ignore"
	"github.com/fqazzazee/shuttle/internal/state"
)

// scanWorkers is how many folder listings a scan keeps in flight. A scan of
// a network share is bound by round trips per folder (DECISIONS.md,
// 2026-10-11), so listing several at once is the cheap way to speed it up.
const scanWorkers = 8

// scanned is one side's scan.
type scanned struct {
	entries     map[string]fsys.FileInfo
	unsupported []string        // symlinks and the like, labelled with the side
	special     []string        // the same paths, as they are on this side
	temps       []fsys.FileInfo // leftover in-progress writes
}

// scan lists every entry of f that can be synced. In-progress temp files and
// the versions folder are left out; symlinks and other special files are
// left out and returned separately so the run can report and hold them.
// Paths the ignore patterns match are left out too, and an ignored folder is
// not descended into.
func scan(ctx context.Context, f fsys.FS, side string, ignored *ignore.Matcher, t *Tracker) (scanned, error) {
	out := scanned{entries: map[string]fsys.FileInfo{}}
	err := fsys.ScanTree(ctx, f, ".", scanWorkers, func(info fsys.FileInfo) error {
		switch {
		case info.Name == VersionsDir:
			if info.Kind != fsys.KindDir {
				// SkipDir from a non-folder would drop the rest of the root
				// listing, and what it dropped would read as deleted.
				return fmt.Errorf("%s is not a folder; move it out of the way", VersionsDir)
			}
			return fs.SkipDir
		case fsys.IsTemp(info.Name):
			if info.Kind == fsys.KindFile {
				out.temps = append(out.temps, info)
			}
		case ignored.Match(info.Name, info.Kind == fsys.KindDir):
			if info.Kind == fsys.KindDir {
				return fs.SkipDir
			}
		case info.Kind == fsys.KindOther:
			out.unsupported = append(out.unsupported, side+": "+info.Name)
			out.special = append(out.special, info.Name)
		default:
			out.entries[info.Name] = info
			t.update(func(p *Progress) { p.Done++ })
		}
		return nil
	})
	slices.Sort(out.unsupported) // listings finish in any order
	return out, err
}

// leaveOutSpecial holds every path that is a symlink or special file on
// either side: that path and everything at or below it leave both scans and
// the records of this run, so nothing there is copied or deleted, and the
// records stand for when the path is a file or folder again. It returns the
// keys of the folders around the held paths, which must not be deleted.
func leaveOutSpecial(o *Options, local, remote *scanned, records map[string]state.Record) map[string]bool {
	key := foldingOf(o).key
	held, parents := map[string]bool{}, map[string]bool{}
	for _, p := range slices.Concat(local.special, remote.special) {
		held[key(p)] = true
		for dir := path.Dir(p); dir != "."; dir = path.Dir(dir) {
			parents[key(dir)] = true
		}
	}
	if len(held) == 0 {
		return nil
	}
	isHeld := func(p string) bool {
		for ; p != "."; p = path.Dir(p) {
			if held[key(p)] {
				return true
			}
		}
		return false
	}
	maps.DeleteFunc(local.entries, func(p string, _ fsys.FileInfo) bool { return isHeld(p) })
	maps.DeleteFunc(remote.entries, func(p string, _ fsys.FileInfo) bool { return isHeld(p) })
	maps.DeleteFunc(records, func(p string, _ state.Record) bool { return isHeld(p) })
	return parents
}

// keepParents drops planned deletions of the folders around held paths: such
// a folder is not empty, whatever the scans now say.
func keepParents(o *Options, actions []Action, parents map[string]bool) []Action {
	if len(parents) == 0 {
		return actions
	}
	key := foldingOf(o).key
	return slices.DeleteFunc(actions, func(a Action) bool { //nolint:gocritic // slices.DeleteFunc passes values
		return (a.Op == OpDeleteLocal || a.Op == OpDeleteRemote) && parents[key(a.Path)]
	})
}

// scanBoth scans both sides at once; the remote side is usually the slow one.
func scanBoth(ctx context.Context, local, remote fsys.FS, ignored *ignore.Matcher, t *Tracker) (l, r scanned, err error) {
	var (
		wg         sync.WaitGroup
		lErr, rErr error
	)
	t.phase(PhaseScanning, 0, 0)
	wg.Go(func() { l, lErr = scan(ctx, local, "local", ignored, t) })
	wg.Go(func() { r, rErr = scan(ctx, remote, "remote", ignored, t) })
	wg.Wait()
	if lErr != nil {
		return l, r, fmt.Errorf("scanning local folder: %w", lErr)
	}
	if rErr != nil {
		return l, r, fmt.Errorf("scanning remote folder: %w", rErr)
	}
	return l, r, nil
}
