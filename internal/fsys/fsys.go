// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

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
	"unicode"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
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

// CaseInsensitive is implemented by file systems that may treat names
// differing only in case as the same file. FoldKey says which: two names
// with the same key are one file there.
type CaseInsensitive interface {
	FoldsCase() bool
	FoldKey(name string) string
}

// FoldsCase reports whether f treats names differing only in case as one.
func FoldsCase(f FS) bool {
	c, ok := f.(CaseInsensitive)
	return ok && c.FoldsCase()
}

// FoldKey is how f sees name: names with the same key are one file there.
// On a file system that keeps case apart, it is name itself.
func FoldKey(f FS, name string) string {
	c, ok := f.(CaseInsensitive)
	if !ok || !c.FoldsCase() {
		return name
	}
	return c.FoldKey(name)
}

// SimpleFold is how SMB servers compare names: each rune upper- then
// lower-cased, which also folds the odd ones out, like "ſ" (long s) and "s".
// It keeps "straße" and "STRASSE" apart, as Samba and Windows do.
func SimpleFold(name string) string {
	return strings.Map(func(r rune) rune { return unicode.ToLower(unicode.ToUpper(r)) }, name)
}

// FullFold is how Linux's case-insensitive file systems compare names,
// Android's shared storage among them: Unicode's full case folding ("ß" is
// "ss"), then the decomposed form, so that the two ways of spelling "é" are
// one name.
func FullFold(name string) string {
	// A Caser keeps state between calls, so each call gets its own.
	return norm.NFD.String(cases.Fold().String(name))
}

// ErrRefusedName is returned for a valid path whose name the file system
// cannot hold: Android's shared storage refuses ":" in a name, for one.
var ErrRefusedName = errors.New("name refused")

// NameRefuser is implemented by file systems that cannot hold every name
// Linux can.
type NameRefuser interface {
	// RefuseName says why name, a valid path, cannot be created here, as an
	// error wrapping ErrRefusedName, or nil if it can.
	RefuseName(name string) error
}

// RefuseName says why f cannot hold name, or nil if it can.
func RefuseName(f FS, name string) error {
	r, ok := f.(NameRefuser)
	if !ok {
		return nil
	}
	return r.RefuseName(name)
}

// Space is how much room a file system has, in bytes: Free for the user
// writing to it, out of Total. Zero is not known.
type Space struct {
	Free, Total int64
}

// SpaceReporter is implemented by file systems that can tell how much
// space is left where they write.
type SpaceReporter interface {
	Space(ctx context.Context) (Space, error)
}

// SpaceOf is how much room f has; zero, with no error, when f cannot tell.
func SpaceOf(ctx context.Context, f FS) (Space, error) {
	r, ok := f.(SpaceReporter)
	if !ok {
		return Space{}, nil
	}
	return r.Space(ctx)
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
