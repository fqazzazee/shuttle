// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package fsys

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"path"
	"strings"
	"time"
)

// TempPrefix starts the base name of every in-progress write. Scans skip these
// names, so a half-written file is never synced or mistaken for user data.
const TempPrefix = ".shuttle-tmp-"

// IsTemp reports whether name is an in-progress write left by WriteFile.
func IsTemp(name string) bool {
	return strings.HasPrefix(path.Base(name), TempPrefix)
}

// WriteFile writes r to name without ever exposing a partial file: it writes a
// temp file in the same directory, sets its modification time, and renames it
// over name. On failure the temp file is removed and name is left as it was.
// It returns the number of bytes written.
func WriteFile(ctx context.Context, fsys FS, name string, r io.Reader, mtime time.Time) (n int64, err error) {
	if err := checkPath("write", name); err != nil {
		return 0, err
	}
	tmp, err := tempName(name)
	if err != nil {
		return 0, err
	}
	w, err := fsys.Create(ctx, tmp)
	if err != nil {
		return 0, err
	}
	defer func() {
		if err != nil {
			// Best effort: the write already failed, and a leftover temp file is
			// skipped by scans and overwritten by nothing.
			_ = fsys.Remove(context.WithoutCancel(ctx), tmp)
		}
	}()

	n, err = io.Copy(w, ctxReader{ctx, r})
	if cerr := w.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return n, err
	}
	// err is the named result the deferred cleanup reads, so assign, never :=.
	err = fsys.Chtimes(ctx, tmp, mtime)
	if err != nil {
		return n, err
	}
	return n, fsys.Rename(ctx, tmp, name)
}

func tempName(name string) (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return path.Join(path.Dir(name), TempPrefix+hex.EncodeToString(b[:])), nil
}

// ctxReader stops a long copy once the context is cancelled, between reads.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

// WalkFunc is called for every entry under the walk root, parents before
// children. As with fs.WalkDir, returning fs.SkipDir from a directory skips
// its contents, and from a file skips the rest of that file's directory. Any
// other error stops the walk and is returned by Walk.
type WalkFunc func(info FileInfo) error

// Walk visits every entry under dir ("." for the whole FS) in lexical order,
// depth first. dir itself is not passed to fn. A listing error stops the walk:
// the engine must never treat an incomplete scan as a complete one.
func Walk(ctx context.Context, fsys FS, dir string, fn WalkFunc) error {
	entries, err := fsys.ReadDir(ctx, dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		err := fn(e)
		if errors.Is(err, fs.SkipDir) {
			if e.Kind == KindDir {
				continue
			}
			return nil
		}
		if err == nil && e.Kind == KindDir {
			err = Walk(ctx, fsys, e.Name, fn)
		}
		if err != nil {
			return err
		}
	}
	return nil
}
