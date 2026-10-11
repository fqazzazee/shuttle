// SPDX-License-Identifier: GPL-2.0-or-later

package engine

import (
	"context"
	"fmt"
	"io/fs"
	"slices"
	"sync"

	"github.com/fqazzazee/shuttle/internal/fsys"
	"github.com/fqazzazee/shuttle/internal/ignore"
)

// scanWorkers is how many folder listings a scan keeps in flight. A scan of
// a network share is bound by round trips per folder (DECISIONS.md,
// 2026-10-11), so listing several at once is the cheap way to speed it up.
const scanWorkers = 8

// scanned is one side's scan.
type scanned struct {
	entries     map[string]fsys.FileInfo
	unsupported []string        // symlinks and the like, labelled with the side
	temps       []fsys.FileInfo // leftover in-progress writes
}

// scan lists every entry of f that can be synced. In-progress temp files and
// the versions folder are left out; symlinks and other special files are
// left out and returned separately so the run can report them.
// Paths the ignore patterns match are left out too, and an ignored folder is
// not descended into.
func scan(ctx context.Context, f fsys.FS, side string, ignored *ignore.Matcher) (scanned, error) {
	out := scanned{entries: map[string]fsys.FileInfo{}}
	err := fsys.ScanTree(ctx, f, ".", scanWorkers, func(info fsys.FileInfo) error {
		switch {
		case info.Name == VersionsDir:
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
		default:
			out.entries[info.Name] = info
		}
		return nil
	})
	slices.Sort(out.unsupported) // listings finish in any order
	return out, err
}

// scanBoth scans both sides at once; the remote side is usually the slow one.
func scanBoth(ctx context.Context, local, remote fsys.FS, ignored *ignore.Matcher) (l, r scanned, err error) {
	var (
		wg         sync.WaitGroup
		lErr, rErr error
	)
	wg.Go(func() { l, lErr = scan(ctx, local, "local", ignored) })
	wg.Go(func() { r, rErr = scan(ctx, remote, "remote", ignored) })
	wg.Wait()
	if lErr != nil {
		return l, r, fmt.Errorf("scanning local folder: %w", lErr)
	}
	if rErr != nil {
		return l, r, fmt.Errorf("scanning remote folder: %w", rErr)
	}
	return l, r, nil
}
