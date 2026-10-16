// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package engine

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"time"

	"github.com/fqazzazee/shuttle/internal/fsys"
	"github.com/fqazzazee/shuttle/internal/state"
)

// Records are committed in batches (see package state for why that is safe):
// whichever comes first of this many changes or this much time. A variable so
// the crash tests can also commit after every change.
var flushEvery = 1000

const flushInterval = time.Second

type executor struct {
	ctx       context.Context
	o         *Options
	res       *Result
	now       func() time.Time
	pending   []func(*state.Tx) error
	lastFlush time.Time
	own       Limits // this shuttle's bandwidth limits
}

func (e *executor) run(actions []Action) error {
	e.lastFlush = e.now()
	for i := range actions {
		if err := e.ctx.Err(); err != nil {
			// Keep the records of everything already done.
			return errors.Join(err, e.flush())
		}
		a := &actions[i]
		if a.skip {
			continue
		}
		if e.o.Stop != nil && e.o.Stop() {
			return errors.Join(ErrStopped, e.flush())
		}
		track := counts(a)
		if track {
			e.o.Tracker.update(func(p *Progress) { p.Path = a.Path })
		}
		err := e.do(a)
		if err == nil && a.was != "" {
			// The record now stands under the new spelling: the old one goes.
			was := a.was
			e.pending = append(e.pending, func(tx *state.Tx) error { return tx.Delete(was) })
		}
		if track {
			e.o.Tracker.update(func(p *Progress) { p.Done, p.Path = p.Done+1, "" })
		}
		e.report(a, err)
		if len(e.pending) >= flushEvery || e.now().Sub(e.lastFlush) >= flushInterval {
			if err := e.flush(); err != nil {
				return err
			}
		}
	}
	return e.flush()
}

func (e *executor) report(a *Action, err error) {
	switch {
	case err == nil:
	case errors.Is(err, ErrChanged):
		e.res.Skipped = append(e.res.Skipped, Issue{Path: a.Path, Op: a.Op, Reason: err.Error()})
	default:
		e.res.Errors = append(e.res.Errors, Issue{Path: a.Path, Op: a.Op, Reason: err.Error()})
	}
	if e.o.OnAction != nil {
		e.o.OnAction(a, err)
	}
}

func (e *executor) do(a *Action) error {
	switch a.Op {
	case OpMkdirLocal, OpMkdirRemote:
		return e.mkdir(a, a.Op == OpMkdirRemote)
	case OpCopyToLocal, OpCopyToRemote:
		return e.copy(a, a.Op == OpCopyToRemote)
	case OpDeleteLocal, OpDeleteRemote:
		return e.delete(a, a.Op == OpDeleteRemote)
	case OpAdopt:
		e.record(a.Path, a.Local, a.Remote, a.hash)
		e.res.Adopted++
		return nil
	case OpForget:
		e.pending = append(e.pending, func(tx *state.Tx) error { return tx.Delete(a.Path) })
		return nil
	case OpConflict:
		return e.conflict(a)
	}
	return fmt.Errorf("unknown op %v", a.Op)
}

// sides returns the destination FS and the scanned source and destination
// entries, for an action that writes to the remote side when toRemote is set.
func (e *executor) sides(a *Action, toRemote bool) (dst fsys.FS, srcInfo, dstInfo *fsys.FileInfo) {
	if toRemote {
		return e.o.Remote, a.Local, a.Remote
	}
	return e.o.Local, a.Remote, a.Local
}

func (e *executor) mkdir(a *Action, toRemote bool) error {
	dst, srcInfo, dstInfo := e.sides(a, toRemote)
	if _, err := e.unchanged(dst, a.Path, dstInfo); err != nil {
		return err
	}
	if err := dst.MkdirAll(e.ctx, a.Path); err != nil {
		return err
	}
	made, err := dst.Stat(e.ctx, a.Path)
	if err != nil {
		return err
	}
	if toRemote {
		e.record(a.Path, srcInfo, &made, nil)
	} else {
		e.record(a.Path, &made, srcInfo, nil)
	}
	e.res.Created++
	return nil
}

func (e *executor) copy(a *Action, toRemote bool) error {
	_, srcInfo, dstInfo := e.sides(a, toRemote)
	return e.copyFile(a.Path, toRemote, srcInfo, dstInfo, true)
}

// copyFile copies p from one side to the other. srcInfo and dstInfo are what
// the two sides are expected to hold (dstInfo nil: nothing); if either side
// no longer matches, nothing is written. A planned copy counts towards the
// run's progress; a conflict's copies do not.
func (e *executor) copyFile(p string, toRemote bool, srcInfo, dstInfo *fsys.FileInfo, planned bool) error {
	src, dst := e.o.Remote, e.o.Local
	if toRemote {
		src, dst = e.o.Local, e.o.Remote
	}
	if _, err := e.unchanged(src, p, srcInfo); err != nil {
		return err
	}
	if _, err := e.unchanged(dst, p, dstInfo); err != nil {
		return err
	}
	if dir := path.Dir(p); dir != "." {
		if err := dst.MkdirAll(e.ctx, dir); err != nil {
			return err
		}
	}

	r, err := src.Open(e.ctx, p)
	if err != nil {
		return err
	}
	// The file about to be replaced goes to the versions folder first. If the
	// new copy then fails, it is put back, so a failed copy loses nothing.
	var kept string
	if dstInfo != nil && dstInfo.Kind == fsys.KindFile {
		if kept, err = e.keepVersion(dst, p); err != nil {
			r.Close()
			return err
		}
	}
	h := sha256.New()
	n, err := fsys.WriteFile(e.ctx, dst, p, io.TeeReader(e.limit(r, toRemote, planned), h), srcInfo.ModTime)
	r.Close()
	if err != nil {
		if kept != "" {
			if rerr := dst.Rename(context.WithoutCancel(e.ctx), kept, p); rerr != nil {
				return fmt.Errorf("%w; the replaced file is kept at %s", err, kept)
			}
			e.res.Versioned--
		}
		return err
	}
	if toRemote {
		e.res.BytesToRemote += n
	} else {
		e.res.BytesToLocal += n
	}
	e.res.Copied++

	written, err := dst.Stat(e.ctx, p)
	if err != nil {
		return err
	}
	// If the source changed while it was being read, the copy may mix old and
	// new content. Record it against the source as scanned: the next run sees
	// only the source changed, and copies it again. Unrecorded, the copy left
	// here would look like an edit of its own, and a sync would call it a
	// conflict.
	after, changed := e.unchanged(src, p, srcInfo)
	if changed != nil {
		after = *srcInfo
	}
	if toRemote {
		e.record(p, &after, &written, h.Sum(nil))
	} else {
		e.record(p, &written, &after, h.Sum(nil))
	}
	if changed != nil {
		return fmt.Errorf("source %w", changed)
	}
	return nil
}

func (e *executor) delete(a *Action, onRemote bool) error {
	target, info := e.o.Local, a.Local
	if onRemote {
		target, info = e.o.Remote, a.Remote
	}
	if _, err := e.unchanged(target, a.Path, info); err != nil {
		return err
	}
	kept := ""
	if info.Kind == fsys.KindFile {
		var err error
		if kept, err = e.keepVersion(target, a.Path); err != nil {
			return err
		}
	}
	if kept == "" {
		if err := target.Remove(e.ctx, a.Path); err != nil {
			return err
		}
	}
	e.pending = append(e.pending, func(tx *state.Tx) error { return tx.DeleteTree(a.Path) })
	e.res.Deleted++
	return nil
}

// compareAll settles every OpCompare before anything is written: identical
// contents become OpAdopt, anything else the action's OnDiffer. Doing this
// up front, read-only, means every conflict is known before the first write,
// which is what lets an "ask" conflict hold the whole shuttle.
func (e *executor) compareAll(actions []Action) error {
	for i := range actions {
		a := &actions[i]
		if a.Op != OpCompare {
			continue
		}
		if err := e.ctx.Err(); err != nil {
			return err
		}
		e.o.Tracker.update(func(p *Progress) { p.Path = a.Path })
		err := e.compare(a)
		e.o.Tracker.update(func(p *Progress) { p.Done, p.Path = p.Done+1, "" })
		if err != nil {
			// Unreadable or changed since the scan: leave it for the next run.
			e.report(a, err)
			a.Op, a.skip = OpForget, true
		}
	}
	return nil
}

func (e *executor) compare(a *Action) error {
	if _, err := e.unchanged(e.o.Local, a.Path, a.Local); err != nil {
		return err
	}
	if _, err := e.unchanged(e.o.Remote, a.Path, a.Remote); err != nil {
		return err
	}
	same, sum, err := e.sameContent(a)
	if err != nil {
		return err
	}
	if same {
		a.Op, a.hash = OpAdopt, sum
	} else {
		a.Op = a.OnDiffer
	}
	return nil
}

// sameContent decides whether both copies of a.Path hold the same bytes. A
// file with no history whose size matches and whose times are within
// ModTimeWindow is taken as the same without reading it, so a first sync of
// an existing copy does not read every file on both sides.
func (e *executor) sameContent(a *Action) (bool, []byte, error) {
	l, r := a.Local, a.Remote
	if l.Size != r.Size {
		return false, nil, nil
	}
	if a.NoHistory {
		d := l.ModTime.Sub(r.ModTime)
		if d.Abs() <= ModTimeWindow {
			return true, nil, nil
		}
	}
	lsum, err := e.hash(e.o.Local, a.Path)
	if err != nil {
		return false, nil, err
	}
	rsum, err := e.hash(e.o.Remote, a.Path)
	if err != nil {
		return false, nil, err
	}
	return bytes.Equal(lsum, rsum), lsum, nil
}

func (e *executor) hash(f fsys.FS, p string) ([]byte, error) {
	r, err := f.Open(e.ctx, p)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	h := sha256.New()
	var src io.Reader = r
	if f == e.o.Remote {
		src = e.limit(r, false, false) // reading the share counts as download
	}
	if _, err := fsys.Copy(h, src); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}

// unchanged re-stats p and checks it still looks as scanned (want nil means
// it was absent). It returns the fresh entry, or an error wrapping
// ErrChanged when it differs.
func (e *executor) unchanged(f fsys.FS, p string, want *fsys.FileInfo) (fsys.FileInfo, error) {
	got, err := f.Stat(e.ctx, p)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if want == nil {
			return fsys.FileInfo{}, nil
		}
		return fsys.FileInfo{}, fmt.Errorf("%s was deleted: %w", p, ErrChanged)
	case err != nil:
		return fsys.FileInfo{}, err
	case want == nil:
		return got, fmt.Errorf("%s appeared: %w", p, ErrChanged)
	case got.Kind != want.Kind,
		got.Kind == fsys.KindFile && (got.Size != want.Size || !got.ModTime.Equal(want.ModTime)):
		return got, fmt.Errorf("%s was modified: %w", p, ErrChanged)
	}
	return got, nil
}

// record queues the state record for a path both sides now agree on.
func (e *executor) record(p string, l, r *fsys.FileInfo, sum []byte) {
	rec := &state.Record{
		Path:   p,
		Kind:   l.Kind,
		Local:  state.Side{Size: l.Size, ModTime: l.ModTime},
		Remote: state.Side{Size: r.Size, ModTime: r.ModTime},
		Hash:   sum,
		Synced: e.now(),
	}
	e.pending = append(e.pending, func(tx *state.Tx) error { return tx.Put(rec) })
}

func (e *executor) flush() error {
	e.lastFlush = e.now()
	if len(e.pending) == 0 {
		return nil
	}
	pending := e.pending
	e.pending = nil
	return e.o.State.Update(func(tx *state.Tx) error {
		for _, f := range pending {
			if err := f(tx); err != nil {
				return err
			}
		}
		return nil
	})
}
