// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package engine

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/fqazzazee/shuttle/internal/config"
	"github.com/fqazzazee/shuttle/internal/fsys"
)

// versionTimeLayout stamps a version with when it was replaced, in local time.
const versionTimeLayout = "20060102-150405"

// Staggered versioning keeps every version for the first hour, then one per
// hour for a day, one per day for 30 days, and one per week after that.
var staggeredSteps = []struct {
	until, every time.Duration
}{
	{time.Hour, 0},
	{24 * time.Hour, time.Hour},
	{30 * 24 * time.Hour, 24 * time.Hour},
	{1<<63 - 1, 7 * 24 * time.Hour},
}

// versionPath is where the version of p replaced at t is kept:
// ".shuttle-versions/dir/report~20261011-153000.pdf".
func versionPath(p string, t time.Time) string {
	return path.Join(VersionsDir, withSuffix(p, "~"+t.Format(versionTimeLayout)))
}

// keepVersion moves the file at p on f into the versions folder, if the
// shuttle keeps versions. It returns where the file went, or "" when
// versioning is off and the caller should delete or overwrite as usual.
func (e *executor) keepVersion(f fsys.FS, p string) (string, error) {
	if e.o.Settings.Versioning.Kind == config.VersioningNone {
		return "", nil
	}
	base := versionPath(p, e.now())
	name := base
	for n := 2; ; n++ {
		_, err := f.Stat(e.ctx, name)
		if errors.Is(err, fs.ErrNotExist) {
			break
		}
		if err != nil {
			return "", err
		}
		if n > 100 {
			return "", fmt.Errorf("no free version name for %s", p)
		}
		name = withSuffix(base, "-"+strconv.Itoa(n))
	}
	if err := f.MkdirAll(e.ctx, path.Dir(name)); err != nil {
		return "", err
	}
	if err := f.Rename(e.ctx, p, name); err != nil {
		return "", err
	}
	e.res.Versioned++
	return name, nil
}

// version is one kept version found in a versions folder.
type version struct {
	path     string    // where it is, under VersionsDir
	original string    // the path it was a version of
	at       time.Time // when it was replaced
	size     int64
}

// versionName matches a version's base name: the original's stem, "~", the
// time it was replaced, an optional "-N" to tell apart versions from the
// same second, then the original's extension.
var versionName = regexp.MustCompile(`^(.*)~(\d{8}-\d{6})(?:-\d+)?((?:\.[^.]*)?)$`)

// parseVersion reads a path under VersionsDir. ok is false for anything that
// is not a version shuttle wrote; those are never pruned.
func parseVersion(p string, size int64) (version, bool) {
	rel, ok := strings.CutPrefix(p, VersionsDir+"/")
	if !ok {
		return version{}, false
	}
	dir, base := path.Split(rel)
	m := versionName.FindStringSubmatch(base)
	if len(m) != 4 || m[1] == "" {
		return version{}, false
	}
	at, err := time.ParseInLocation(versionTimeLayout, m[2], time.Local)
	if err != nil {
		return version{}, false
	}
	return version{path: p, original: dir + m[1] + m[3], at: at, size: size}, true
}

// listVersions returns every version kept on f.
func listVersions(ctx context.Context, f fsys.FS) ([]version, error) {
	var out []version
	err := fsys.Walk(ctx, f, VersionsDir, func(info fsys.FileInfo) error {
		if info.Kind == fsys.KindFile {
			if v, ok := parseVersion(info.Name, info.Size); ok {
				out = append(out, v)
			}
		}
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil // nothing has been versioned on this side yet
	}
	return out, err
}

// expired decides which versions the retention settings let go, as of now.
// It does no I/O.
func expired(v config.Versioning, versions []version, now time.Time) []version {
	byOriginal := map[string][]version{}
	for _, x := range versions {
		byOriginal[x.original] = append(byOriginal[x.original], x)
	}
	var drop, keep []version
	for _, group := range byOriginal {
		// Newest first.
		slices.SortFunc(group, func(a, b version) int { return b.at.Compare(a.at) })
		seen := map[[2]int64]bool{}
		for i, x := range group {
			age := now.Sub(x.at)
			gone := false
			switch v.Kind {
			case config.VersioningTrash:
				gone = age > v.Keep
			case config.VersioningSimple:
				gone = i >= v.Count
			case config.VersioningStaggered:
				if v.Keep > 0 && age > v.Keep {
					gone = true
					break
				}
				for step, s := range staggeredSteps {
					if age < s.until {
						if s.every > 0 {
							// One version per interval: the newest one in it.
							bucket := [2]int64{int64(step), int64(age / s.every)}
							gone = seen[bucket]
							seen[bucket] = true
						}
						break
					}
				}
			}
			if gone {
				drop = append(drop, x)
			} else {
				keep = append(keep, x)
			}
		}
	}

	if v.SizeCap > 0 {
		var total int64
		for _, x := range keep {
			total += x.size
		}
		// Oldest first, across all files, until under the cap.
		slices.SortFunc(keep, func(a, b version) int { return cmp.Or(a.at.Compare(b.at), cmp.Compare(a.path, b.path)) })
		for len(keep) > 0 && total > v.SizeCap {
			total -= keep[0].size
			drop = append(drop, keep[0])
			keep = keep[1:]
		}
	}
	return drop
}

// prune removes the versions on f that retention lets go, and any folders
// in the versions tree that leaves empty.
func (e *executor) prune(f fsys.FS, side string) {
	v := e.o.Settings.Versioning
	if v.Kind == config.VersioningNone {
		return
	}
	versions, err := listVersions(e.ctx, f)
	if err != nil {
		e.res.Errors = append(e.res.Errors, Issue{Path: VersionsDir, Op: OpPrune, Reason: side + ": " + err.Error()})
		return
	}
	for _, x := range expired(v, versions, e.now()) {
		if err := f.Remove(e.ctx, x.path); err != nil {
			e.res.Errors = append(e.res.Errors, Issue{Path: x.path, Op: OpPrune, Reason: side + ": " + err.Error()})
			continue
		}
		e.res.VersionsPruned++
		// Tidy up folders the removal emptied; Remove fails on a non-empty
		// one, which is where it stops.
		for dir := path.Dir(x.path); dir != VersionsDir && dir != "."; dir = path.Dir(dir) {
			if f.Remove(e.ctx, dir) != nil {
				break
			}
		}
	}
}
