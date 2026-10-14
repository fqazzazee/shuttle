// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package fsys

import (
	"context"
	"io"
	"io/fs"
	"math"
	"os"
	"path"
	"slices"
	"strings"
	"syscall"
	"time"
)

// Local is an FS backed by a directory on local disk. All access goes through
// an os.Root, so neither ".." nor a symlink inside the tree can reach a file
// outside it.
type Local struct {
	root *os.Root
}

var _ FS = (*Local)(nil)

// OpenLocal opens dir as an FS. dir must exist and be a directory.
func OpenLocal(dir string) (*Local, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	return &Local{root: root}, nil
}

// Space is what statfs says of the folder's file system.
func (l *Local) Space(ctx context.Context) (Space, error) {
	if err := ctx.Err(); err != nil {
		return Space{}, err
	}
	return LocalSpace(l.root.Name())
}

// LocalSpace is how much room the file system dir is on has: what a user
// can write, without root's reserve, out of the whole.
func LocalSpace(dir string) (Space, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return Space{}, &fs.PathError{Op: "statfs", Path: dir, Err: err}
	}
	return Space{Free: clampInt64(st.Bavail, st.Bsize), Total: clampInt64(st.Blocks, st.Bsize)}, nil
}

// clampInt64 is blocks of size bytes, at most math.MaxInt64.
func clampInt64(blocks uint64, size int64) int64 {
	if size <= 0 || blocks > uint64(math.MaxInt64/size) { //nolint:gosec // size > 0, so the quotient is positive
		return math.MaxInt64
	}
	return int64(blocks) * size //nolint:gosec // checked above that it fits
}

func (l *Local) Stat(ctx context.Context, name string) (FileInfo, error) {
	if err := begin(ctx, "stat", name); err != nil {
		return FileInfo{}, err
	}
	fi, err := l.root.Lstat(name)
	if err != nil {
		return FileInfo{}, err
	}
	return localInfo(name, fi), nil
}

func (l *Local) ReadDir(ctx context.Context, name string) ([]FileInfo, error) {
	if err := begin(ctx, "readdir", name); err != nil {
		return nil, err
	}
	f, err := l.root.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	entries, err := f.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	out := make([]FileInfo, 0, len(entries))
	for _, e := range entries {
		// Lstat through the root rather than DirEntry.Info, which resolves the
		// entry by path string outside the root's protection.
		child := path.Join(name, e.Name())
		fi, err := l.root.Lstat(child)
		if err != nil {
			return nil, err
		}
		out = append(out, localInfo(child, fi))
	}
	// File.ReadDir returns directory order, unlike os.ReadDir.
	slices.SortFunc(out, func(a, b FileInfo) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

func (l *Local) Open(ctx context.Context, name string) (io.ReadCloser, error) {
	if err := begin(ctx, "open", name); err != nil {
		return nil, err
	}
	return l.root.Open(name)
}

func (l *Local) Create(ctx context.Context, name string) (io.WriteCloser, error) {
	if err := begin(ctx, "create", name); err != nil {
		return nil, err
	}
	return l.root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o666)
}

func (l *Local) Rename(ctx context.Context, oldname, newname string) error {
	if err := begin(ctx, "rename", oldname); err != nil {
		return err
	}
	if err := checkPath("rename", newname); err != nil {
		return err
	}
	return l.root.Rename(oldname, newname)
}

func (l *Local) Remove(ctx context.Context, name string) error {
	if err := begin(ctx, "remove", name); err != nil {
		return err
	}
	return l.root.Remove(name)
}

func (l *Local) MkdirAll(ctx context.Context, name string) error {
	if err := begin(ctx, "mkdir", name); err != nil {
		return err
	}
	return l.root.MkdirAll(name, 0o777)
}

func (l *Local) Chtimes(ctx context.Context, name string, mtime time.Time) error {
	if err := begin(ctx, "chtimes", name); err != nil {
		return err
	}
	// A zero atime leaves the access time unchanged.
	return l.root.Chtimes(name, time.Time{}, mtime)
}

func (l *Local) Close() error {
	return l.root.Close()
}

// begin is the common preamble: give up early on a cancelled context, and
// reject paths that could leave the root before the OS ever sees them.
func begin(ctx context.Context, op, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return checkPath(op, name)
}

func localInfo(name string, fi fs.FileInfo) FileInfo {
	info := FileInfo{Name: name, ModTime: fi.ModTime()}
	switch {
	case fi.Mode().IsRegular():
		info.Kind = KindFile
		info.Size = fi.Size()
	case fi.IsDir():
		info.Kind = KindDir
	default:
		info.Kind = KindOther
	}
	return info
}
