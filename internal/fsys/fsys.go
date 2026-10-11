// SPDX-License-Identifier: GPL-2.0-or-later

// Package fsys is the filesystem layer the sync engine works through. Each
// backend (local disk, SMB, NFS) exposes one shuttle's folder as an FS whose
// paths are relative, slash-separated and confined to that folder.
//
// This package must stay free of desktop-specific imports; see the
// portability rule in SPEC.md.
package fsys

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"strings"
	"time"
)

// Kind is what sort of entry a path names.
type Kind uint8

const (
	KindFile  Kind = iota
	KindDir        // directory
	KindOther      // symlink, device, socket, pipe: never synced
)

func (k Kind) String() string {
	switch k {
	case KindFile:
		return "file"
	case KindDir:
		return "dir"
	default:
		return "other"
	}
}

// FileInfo describes one entry of an FS.
type FileInfo struct {
	// Name is the path relative to the FS root, slash-separated, e.g. "a/b.txt".
	Name    string
	Kind    Kind
	Size    int64
	ModTime time.Time
}

// FS is one shuttle's folder on one side of a sync. Every path argument must
// pass ValidPath; "." names the root. Errors wrap fs.ErrNotExist, fs.ErrExist
// and ErrInvalidPath so callers can test them with errors.Is.
type FS interface {
	// Stat describes name without following a final symlink.
	Stat(ctx context.Context, name string) (FileInfo, error)
	// ReadDir lists directory name, sorted by name, without following symlinks.
	ReadDir(ctx context.Context, name string) ([]FileInfo, error)
	// Open opens a file for reading.
	Open(ctx context.Context, name string) (io.ReadCloser, error)
	// Create creates a new file for writing. It fails if name already exists.
	Create(ctx context.Context, name string) (io.WriteCloser, error)
	// Rename moves oldname to newname, replacing newname if it is a file.
	Rename(ctx context.Context, oldname, newname string) error
	// Remove deletes a file or an empty directory.
	Remove(ctx context.Context, name string) error
	// MkdirAll creates directory name and any missing parents.
	MkdirAll(ctx context.Context, name string) error
	// Chtimes sets the modification time of name.
	Chtimes(ctx context.Context, name string, mtime time.Time) error
	// Close releases the connection or handle behind the FS.
	Close() error
}

// ErrInvalidPath is returned for a path that is absolute, contains "..", empty
// or "." elements, a trailing slash, or a NUL byte.
var ErrInvalidPath = errors.New("invalid path")

// ValidPath reports whether name is a clean, relative, slash-separated path
// that cannot leave the FS root. Remote listings are untrusted, so every
// backend checks this before touching a path.
func ValidPath(name string) bool {
	return fs.ValidPath(name) && !strings.ContainsRune(name, 0)
}

func checkPath(op, name string) error {
	if !ValidPath(name) {
		return &fs.PathError{Op: op, Path: name, Err: ErrInvalidPath}
	}
	return nil
}
