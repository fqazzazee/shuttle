// SPDX-License-Identifier: GPL-2.0-or-later

package fsys

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"path"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fqazzazee/shuttle/internal/nfs3"
)

// NFSOptions say how to reach a folder on an NFSv3 export.
type NFSOptions struct {
	nfs3.Options
	Folder string // folder inside the export, slash-separated; "." for its top
}

// NFS is an FS on a folder of an NFSv3 export, reached directly. NFS has no
// paths, only file handles, so each path is looked up one name at a time
// from the folder's handle; folder handles are kept for a few seconds.
type NFS struct {
	c    *nfs3.Client
	root nfs3.FH // the folder's handle

	mu   sync.Mutex
	dirs map[string]dirHandle // folder path -> handle
}

type dirHandle struct {
	fh   nfs3.FH
	seen time.Time
}

// dirTTL is how long a folder's handle is used without looking it up again.
// Another client could rename the folder meanwhile and put a new one in its
// place; this bounds how long shuttle would keep writing into the old one.
const dirTTL = 2 * time.Second

// newFileMode and newDirMode are the modes new files and folders get,
// before the server applies its own rules (anonuid, umask, ACLs).
const (
	newFileMode = 0o644
	newDirMode  = 0o755
)

var _ FS = (*NFS)(nil)

// DialNFS mounts the export and opens the folder. The folder must already
// exist: a mistyped folder should fail, not be created.
func DialNFS(ctx context.Context, o *NFSOptions) (*NFS, error) {
	folder := o.Folder
	if folder == "" {
		folder = "."
	}
	if !ValidPath(folder) {
		return nil, fmt.Errorf("nfs folder %q: %w", folder, ErrInvalidPath)
	}
	c, err := nfs3.Dial(ctx, &o.Options)
	if err != nil {
		return nil, fmt.Errorf("export %q on %s: %w", o.Export, o.Host, err)
	}
	n := &NFS{c: c, root: c.Root(), dirs: map[string]dirHandle{}}
	if folder != "." {
		fh, err := n.dir(ctx, "open", folder)
		if err != nil {
			c.Close()
			return nil, fmt.Errorf("folder %q on export %q: %w", folder, o.Export, err)
		}
		n.root = fh
		n.dirs = map[string]dirHandle{}
	}
	return n, nil
}

// dir returns the handle of folder name, following no symlinks.
func (n *NFS) dir(ctx context.Context, op, name string) (nfs3.FH, error) {
	if name == "." {
		return n.root, nil
	}
	n.mu.Lock()
	h, ok := n.dirs[name]
	n.mu.Unlock()
	if ok && time.Since(h.seen) < dirTTL {
		return h.fh, nil
	}
	parent, base := n.split(name)
	pfh, err := n.dir(ctx, op, parent)
	if err != nil {
		return nil, err
	}
	fh, a, err := n.c.Lookup(ctx, pfh, base)
	if err != nil {
		return nil, &fs.PathError{Op: op, Path: name, Err: err}
	}
	if a.Type != nfs3.TypeDir {
		return nil, &fs.PathError{Op: op, Path: name, Err: syscall.ENOTDIR}
	}
	n.remember(name, fh)
	return fh, nil
}

func (n *NFS) split(name string) (parent, base string) {
	parent, base = path.Split(name)
	if parent == "" {
		return ".", base
	}
	return strings.TrimSuffix(parent, "/"), base
}

func (n *NFS) remember(name string, fh nfs3.FH) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.dirs[name] = dirHandle{fh: fh, seen: time.Now()}
}

// forget drops the handles of name and everything under it, after a
// change that may have moved or removed them.
func (n *NFS) forget(name string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	for p := range n.dirs {
		if p == name || strings.HasPrefix(p, name+"/") {
			delete(n.dirs, p)
		}
	}
}

// at checks name and returns the handle of the folder it is in, plus its
// last element. A stale cached handle is dropped and looked up again once.
func (n *NFS) at(ctx context.Context, op, name string) (nfs3.FH, string, error) {
	if err := begin(ctx, op, name); err != nil {
		return nil, "", err
	}
	if name == "." {
		return nil, "", &fs.PathError{Op: op, Path: name, Err: ErrInvalidPath}
	}
	parent, base := n.split(name)
	fh, err := n.dir(ctx, op, parent)
	return fh, base, err
}

// retry runs f, and once more with fresh handles if a cached one was stale.
func (n *NFS) retry(f func() error) error {
	err := f()
	var ne *nfs3.Error
	if errors.As(err, &ne) && (ne.Status == nfs3.StatusStale || ne.Status == nfs3.StatusBadHandle) {
		n.mu.Lock()
		clear(n.dirs)
		n.mu.Unlock()
		err = f()
	}
	return err
}

func (n *NFS) lookup(ctx context.Context, op, name string) (nfs3.FH, nfs3.Attr, error) {
	if err := begin(ctx, op, name); err != nil {
		return nil, nfs3.Attr{}, err
	}
	if name == "." {
		a, err := n.c.GetAttr(ctx, n.root)
		return n.root, a, pathErr(op, name, err)
	}
	var fh nfs3.FH
	var a nfs3.Attr
	err := n.retry(func() error {
		pfh, base, err := n.at(ctx, op, name)
		if err != nil {
			return err
		}
		fh, a, err = n.c.Lookup(ctx, pfh, base)
		return err
	})
	if err != nil {
		return nil, nfs3.Attr{}, pathErr(op, name, err)
	}
	if a.Type == nfs3.TypeDir {
		n.remember(name, fh)
	}
	return fh, a, nil
}

// pathErr adds the path to an error from the server; errors that already
// name it pass through.
func pathErr(op, name string, err error) error {
	var pe *fs.PathError
	if err == nil || errors.As(err, &pe) {
		return err
	}
	return &fs.PathError{Op: op, Path: name, Err: err}
}

func (n *NFS) Stat(ctx context.Context, name string) (FileInfo, error) {
	_, a, err := n.lookup(ctx, "stat", name)
	if err != nil {
		return FileInfo{}, err
	}
	return nfsInfo(name, a), nil
}

func (n *NFS) ReadDir(ctx context.Context, name string) ([]FileInfo, error) {
	if err := begin(ctx, "readdir", name); err != nil {
		return nil, err
	}
	var out []FileInfo
	err := n.retry(func() error {
		out = out[:0]
		fh, err := n.dir(ctx, "readdir", name)
		if err != nil {
			return err
		}
		return n.c.ReadDir(ctx, fh, func(e nfs3.Entry) error {
			// A server is untrusted: never pass on a name that is not a
			// single, plain path element.
			if e.Name == "" || strings.ContainsAny(e.Name, "/\x00") {
				return fmt.Errorf("server sent an unsafe name %q: %w", e.Name, ErrInvalidPath)
			}
			p := path.Join(name, e.Name)
			if !e.HasAttr || e.FH == nil {
				var err error
				if e.FH, e.Attr, err = n.c.Lookup(ctx, fh, e.Name); err != nil {
					if errors.Is(err, fs.ErrNotExist) {
						return nil // removed since the listing
					}
					return err
				}
			}
			if e.Attr.Type == nfs3.TypeDir {
				n.remember(p, e.FH)
			}
			out = append(out, nfsInfo(p, e.Attr))
			return nil
		})
	})
	if err != nil {
		return nil, pathErr("readdir", name, err)
	}
	slices.SortFunc(out, func(a, b FileInfo) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

func (n *NFS) Open(ctx context.Context, name string) (io.ReadCloser, error) {
	fh, a, err := n.lookup(ctx, "open", name)
	if err != nil {
		return nil, err
	}
	if a.Type != nfs3.TypeReg {
		return nil, &fs.PathError{Op: "open", Path: name, Err: errors.New("not a regular file")}
	}
	return &nfsReader{ctx: ctx, c: n.c, fh: fh, name: name}, nil
}

func (n *NFS) Create(ctx context.Context, name string) (io.WriteCloser, error) {
	var fh nfs3.FH
	err := n.retry(func() error {
		pfh, base, err := n.at(ctx, "create", name)
		if err != nil {
			return err
		}
		fh, err = n.c.Create(ctx, pfh, base, newFileMode)
		return err
	})
	if err != nil {
		return nil, pathErr("create", name, err)
	}
	return &nfsWriter{ctx: ctx, c: n.c, fh: fh, name: name, buf: make([]byte, 0, n.c.WriteSize())}, nil
}

func (n *NFS) Rename(ctx context.Context, oldname, newname string) error {
	if err := begin(ctx, "rename", newname); err != nil {
		return err
	}
	err := n.retry(func() error {
		ofh, obase, err := n.at(ctx, "rename", oldname)
		if err != nil {
			return err
		}
		nfh, nbase, err := n.at(ctx, "rename", newname)
		if err != nil {
			return err
		}
		return n.c.Rename(ctx, ofh, obase, nfh, nbase)
	})
	n.forget(oldname)
	n.forget(newname)
	return pathErr("rename", oldname, err)
}

func (n *NFS) Remove(ctx context.Context, name string) error {
	_, a, err := n.lookup(ctx, "remove", name)
	if err != nil {
		return err
	}
	err = n.retry(func() error {
		pfh, base, err := n.at(ctx, "remove", name)
		if err != nil {
			return err
		}
		if a.Type == nfs3.TypeDir {
			return n.c.Rmdir(ctx, pfh, base)
		}
		return n.c.Remove(ctx, pfh, base)
	})
	n.forget(name)
	return pathErr("remove", name, err)
}

func (n *NFS) MkdirAll(ctx context.Context, name string) error {
	if err := begin(ctx, "mkdir", name); err != nil {
		return err
	}
	if name == "." {
		return nil // the folder itself, which DialNFS checked exists
	}
	return pathErr("mkdir", name, n.retry(func() error {
		fh := n.root
		elems := strings.Split(name, "/")
		for i, elem := range elems {
			p := path.Join(elems[:i+1]...)
			next, err := n.dir(ctx, "mkdir", p)
			if errors.Is(err, fs.ErrNotExist) {
				next, err = n.c.Mkdir(ctx, fh, elem, newDirMode)
				if errors.Is(err, fs.ErrExist) { // made meanwhile, by someone else
					next, err = n.dir(ctx, "mkdir", p)
				} else if err == nil {
					n.remember(p, next)
				}
			}
			if err != nil {
				return err
			}
			fh = next
		}
		return nil
	}))
}

func (n *NFS) Chtimes(ctx context.Context, name string, mtime time.Time) error {
	fh, _, err := n.lookup(ctx, "chtimes", name)
	if err != nil {
		return err
	}
	return pathErr("chtimes", name, n.c.SetMtime(ctx, fh, mtime))
}

// Space asks the server how much room the export's file system has for
// this user.
func (n *NFS) Space(ctx context.Context) (Space, error) {
	if err := ctx.Err(); err != nil {
		return Space{}, err
	}
	st, err := n.c.FSStat(ctx, n.root)
	if err != nil {
		return Space{}, pathErr("statfs", ".", err)
	}
	return Space{Free: int64(min(st.Avail, math.MaxInt64)), Total: int64(min(st.Total, math.MaxInt64))}, nil //nolint:gosec // clamped on this line
}

func (n *NFS) Close() error { return n.c.Close() }

func nfsInfo(name string, a nfs3.Attr) FileInfo {
	info := FileInfo{Name: name, ModTime: a.Mtime}
	switch a.Type {
	case nfs3.TypeReg:
		info.Kind = KindFile
		info.Size = int64(min(a.Size, 1<<63-1)) //nolint:gosec // clamped on this line
	case nfs3.TypeDir:
		info.Kind = KindDir
	default:
		info.Kind = KindOther
	}
	return info
}

// nfsReader reads a file a READ's worth at a time.
type nfsReader struct {
	ctx  context.Context
	c    *nfs3.Client
	fh   nfs3.FH
	name string
	off  uint64
	buf  []byte // read but not yet returned
	eof  bool
}

func (r *nfsReader) Read(p []byte) (int, error) {
	if len(r.buf) == 0 {
		if r.eof {
			return 0, io.EOF
		}
		data, eof, err := r.c.Read(r.ctx, r.fh, r.off, r.c.ReadSize())
		if err != nil {
			return 0, &fs.PathError{Op: "read", Path: r.name, Err: err}
		}
		r.off += uint64(len(data))
		r.buf, r.eof = data, eof
		if len(data) == 0 {
			return 0, io.EOF
		}
	}
	n := copy(p, r.buf)
	r.buf = r.buf[n:]
	return n, nil
}

func (r *nfsReader) Close() error { return nil }

// nfsWriter writes a file a WRITE's worth at a time, unstably, and commits
// it on Close.
type nfsWriter struct {
	ctx    context.Context
	c      *nfs3.Client
	fh     nfs3.FH
	name   string
	off    uint64
	buf    []byte
	verf   nfs3.Verifier
	wrote  bool
	err    error
	closed bool
}

func (w *nfsWriter) Write(p []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	n := 0
	for len(p) > 0 {
		k := copy(w.buf[len(w.buf):cap(w.buf)], p)
		w.buf = w.buf[:len(w.buf)+k]
		p, n = p[k:], n+k
		if len(w.buf) == cap(w.buf) {
			if err := w.flush(); err != nil {
				return n, err
			}
		}
	}
	return n, nil
}

func (w *nfsWriter) flush() error {
	for len(w.buf) > 0 {
		k, _, v, err := w.c.Write(w.ctx, w.fh, w.off, w.buf, nfs3.Unstable)
		if err == nil && k == 0 {
			err = errors.New("the server took none of the data")
		}
		if err == nil && w.wrote && v != w.verf {
			err = errRestarted
		}
		if err != nil {
			w.err = &fs.PathError{Op: "write", Path: w.name, Err: err}
			return w.err
		}
		w.verf, w.wrote = v, true
		w.off += uint64(k) //nolint:gosec // nfs3.Client.Write returns 0 <= k <= len(w.buf)
		w.buf = w.buf[:copy(w.buf, w.buf[k:])]
	}
	return nil
}

// errRestarted is a write the server may have lost: it restarted between
// taking the data and making it durable.
var errRestarted = errors.New("the NFS server restarted during the write; the file will be copied again")

// Close writes what is left and commits the file. Any error means the file
// on the server cannot be trusted.
func (w *nfsWriter) Close() error {
	if w.closed || w.err != nil {
		w.closed = true
		return w.err
	}
	w.closed = true
	if err := w.flush(); err != nil {
		return err
	}
	if !w.wrote {
		return nil // an empty file: nothing to commit
	}
	v, err := w.c.Commit(w.ctx, w.fh)
	if err == nil && v != w.verf {
		err = errRestarted
	}
	if err != nil {
		w.err = &fs.PathError{Op: "close", Path: w.name, Err: err}
	}
	return w.err
}
