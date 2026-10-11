// SPDX-License-Identifier: GPL-2.0-or-later

package engine

import (
	"context"
	"fmt"
	"io/fs"
	"sync"

	"github.com/fqazzazee/shuttle/internal/fsys"
	"github.com/fqazzazee/shuttle/internal/ignore"
)

// scan lists every entry of f that can be synced. In-progress temp files and
// the versions folder are left out; symlinks and other special files are
// left out and returned separately so the run can report them.
// Paths the ignore patterns match are left out too, and an ignored folder is
// not descended into.
func scan(ctx context.Context, f fsys.FS, side string, ignored *ignore.Matcher) (map[string]fsys.FileInfo, []string, error) {
	entries := map[string]fsys.FileInfo{}
	var unsupported []string
	err := fsys.Walk(ctx, f, ".", func(info fsys.FileInfo) error {
		switch {
		case info.Name == VersionsDir:
			return fs.SkipDir
		case fsys.IsTemp(info.Name):
		case ignored.Match(info.Name, info.Kind == fsys.KindDir):
			if info.Kind == fsys.KindDir {
				return fs.SkipDir
			}
		case info.Kind == fsys.KindOther:
			unsupported = append(unsupported, side+": "+info.Name)
		default:
			entries[info.Name] = info
		}
		return nil
	})
	return entries, unsupported, err
}

// scanBoth scans both sides at once; the remote side is usually the slow one.
func scanBoth(ctx context.Context, local, remote fsys.FS, ignored *ignore.Matcher) (l, r map[string]fsys.FileInfo, unsupported []string, err error) {
	var (
		wg             sync.WaitGroup
		lUnsup, rUnsup []string
		lErr, rErr     error
	)
	wg.Go(func() { l, lUnsup, lErr = scan(ctx, local, "local", ignored) })
	wg.Go(func() { r, rUnsup, rErr = scan(ctx, remote, "remote", ignored) })
	wg.Wait()
	if lErr != nil {
		return nil, nil, nil, fmt.Errorf("scanning local folder: %w", lErr)
	}
	if rErr != nil {
		return nil, nil, nil, fmt.Errorf("scanning remote folder: %w", rErr)
	}
	return l, r, append(lUnsup, rUnsup...), nil
}
