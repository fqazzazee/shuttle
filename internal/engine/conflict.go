// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package engine

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/fqazzazee/shuttle/internal/config"
	"github.com/fqazzazee/shuttle/internal/fsys"
)

// conflict applies the shuttle's conflict policy to a path that changed on
// both sides, or is a file on one side and a folder on the other.
func (e *executor) conflict(a *Action) error {
	policy := e.o.Settings.Conflicts
	if policy == config.ConflictAsk || a.alwaysHold {
		e.hold(a)
		return nil
	}
	if a.Local == nil || a.Remote == nil {
		return fmt.Errorf("conflict on %s without both sides", a.Path)
	}
	clash := a.Local.Kind != a.Remote.Kind
	if policy == config.ConflictNewestWins && !clash && !a.Local.ModTime.Equal(a.Remote.ModTime) {
		return e.newestWins(a)
	}
	// keep-both, plus the cases newest-wins cannot decide: a tie, or a file
	// against a folder (there is nothing to overwrite a folder with).
	return e.keepBoth(a)
}

func (e *executor) hold(a *Action) {
	e.settled(a, "held")
}

// settled reports a conflict and what was done about it.
func (e *executor) settled(a *Action, outcome string) {
	e.res.Conflicts = append(e.res.Conflicts, Issue{Path: a.Path, Op: OpConflict, Reason: a.Reason,
		Outcome: outcome, Local: a.Local, Remote: a.Remote})
}

// newestWins copies the more recently modified copy over the other.
func (e *executor) newestWins(a *Action) error {
	toRemote := a.Local.ModTime.After(a.Remote.ModTime)
	src, dst := a.Remote, a.Local
	winner := e.o.RemoteName
	if toRemote {
		src, dst = a.Local, a.Remote
		winner = e.o.LocalName
	}
	if err := e.copyFile(a.Path, toRemote, src, dst, false); err != nil {
		return err
	}
	e.settled(a, "newest wins: kept the "+label(winner)+" copy")
	return nil
}

// keepBoth keeps both versions. For two files, the newer one keeps the name
// and the older one is saved beside it under a conflict name, on both sides.
// For a file against a folder, the file is renamed and the folder keeps the
// name; the next run then syncs both normally.
func (e *executor) keepBoth(a *Action) error {
	// Which side gives up the name: the older file, or the file in a
	// file/folder clash. Ties go to the remote side, so the local copy keeps
	// the name the user is looking at.
	loserRemote := !a.Remote.ModTime.After(a.Local.ModTime)
	if a.Local.Kind != a.Remote.Kind {
		loserRemote = a.Remote.Kind == fsys.KindFile
	}
	loserFS, loserName, winnerInfo := e.o.Local, e.o.LocalName, a.Remote
	if loserRemote {
		loserFS, loserName, winnerInfo = e.o.Remote, e.o.RemoteName, a.Local
	}

	if _, err := e.unchanged(e.o.Local, a.Path, a.Local); err != nil {
		return err
	}
	if _, err := e.unchanged(e.o.Remote, a.Path, a.Remote); err != nil {
		return err
	}
	saved, err := e.freeConflictName(a.Path, loserName)
	if err != nil {
		return err
	}
	if err := loserFS.Rename(e.ctx, a.Path, saved); err != nil {
		return err
	}
	e.settled(a, "kept both: the "+label(loserName)+" copy is now "+saved)

	if a.Local.Kind != a.Remote.Kind {
		return nil // the folder and the renamed file sync on the next run
	}
	// Give the loser side the winning file, and the winner side the saved one.
	if err := e.copyFile(a.Path, loserRemote, winnerInfo, nil, false); err != nil {
		return err
	}
	savedInfo, err := loserFS.Stat(e.ctx, saved)
	if err != nil {
		return err
	}
	return e.copyFile(saved, !loserRemote, &savedInfo, nil, false)
}

// freeConflictName returns a conflict name for p that exists on neither side.
func (e *executor) freeConflictName(p, side string) (string, error) {
	base := conflictName(p, e.now(), side)
	for n := 1; n < 100; n++ {
		name := base
		if n > 1 {
			name = withSuffix(base, "-"+strconv.Itoa(n))
		}
		free := true
		for _, f := range []fsys.FS{e.o.Local, e.o.Remote} {
			_, err := f.Stat(e.ctx, name)
			if err == nil {
				free = false
				break
			}
			if !errors.Is(err, fs.ErrNotExist) {
				return "", err
			}
		}
		if free {
			return name, nil
		}
	}
	return "", fmt.Errorf("no free conflict name for %s", p)
}

var unsafeLabel = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// conflictName is where the losing copy of p goes:
// "dir/report.sync-conflict-20261011-153000-laptop.pdf".
func conflictName(p string, at time.Time, side string) string {
	return withSuffix(p, ".sync-conflict-"+at.Format("20060102-150405")+"-"+label(side))
}

// withSuffix inserts s before p's extension. A dotfile like ".bashrc" has no
// extension, only a name.
func withSuffix(p, s string) string {
	dir, base := path.Split(p)
	ext := path.Ext(base)
	if ext == base {
		ext = ""
	}
	return dir + strings.TrimSuffix(base, ext) + s + ext
}

func label(s string) string {
	s = strings.Trim(unsafeLabel.ReplaceAllString(s, "-"), "-.")
	if s == "" {
		return "unknown"
	}
	return s
}

// Choice is how the user settles a held conflict.
type Choice string

const (
	KeepLocal  Choice = "keep-local"
	KeepRemote Choice = "keep-remote"
	KeepBoth   Choice = "keep-both"
)

// Resolve settles one held conflict at p the way the user chose. Both sides
// must still be there; whatever is replaced is replaced as in a normal run.
func Resolve(ctx context.Context, o *Options, p string, choice Choice) error {
	if !fsys.ValidPath(p) || p == "." {
		return fmt.Errorf("resolve %q: %w", p, fsys.ErrInvalidPath)
	}
	e := newExecutor(ctx, o, &Result{})
	l, err := o.Local.Stat(ctx, p)
	if err != nil {
		return err
	}
	r, err := o.Remote.Stat(ctx, p)
	if err != nil {
		return err
	}
	a := &Action{Op: OpConflict, Path: p, Local: &l, Remote: &r, Reason: "resolved by the user"}
	switch choice {
	case KeepLocal:
		err = e.overwrite(a, true)
	case KeepRemote:
		err = e.overwrite(a, false)
	case KeepBoth:
		err = e.keepBoth(a)
	default:
		return fmt.Errorf("resolve %s: unknown choice %q", p, choice)
	}
	if err != nil {
		return err
	}
	return e.flush()
}

// overwrite makes one side's copy win. Only files can be overwritten; a file
// against a folder can only be kept both ways.
func (e *executor) overwrite(a *Action, toRemote bool) error {
	if a.Local.Kind != fsys.KindFile || a.Remote.Kind != fsys.KindFile {
		return fmt.Errorf("resolve %s: a file and a folder can only be kept both", a.Path)
	}
	if toRemote {
		return e.copyFile(a.Path, true, a.Local, a.Remote, false)
	}
	return e.copyFile(a.Path, false, a.Remote, a.Local, false)
}
