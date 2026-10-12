// SPDX-License-Identifier: GPL-2.0-or-later

// Package nfs3test serves a folder over NFSv3, in-process, for tests: the
// willscott/go-nfs server, which answers MOUNT and NFS on one port and runs
// no portmapper.
package nfs3test

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/osfs"
	nfs "github.com/willscott/go-nfs"
	"github.com/willscott/go-nfs/helpers"
)

func init() {
	nfs.Log.SetLevel(nfs.PanicLevel) // its "connection closed" noise would fill test logs
}

// Serve serves dir until the test ends, and returns the address it listens
// on, 127.0.0.1 and a free port, for both the mount and the NFS service.
func Serve(t testing.TB, dir string) (host string, port int) {
	t.Helper()
	l, err := Listen(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	a := l.Addr().(*net.TCPAddr)
	return a.IP.String(), a.Port
}

// Listen serves dir on a free port of 127.0.0.1 until the listener is
// closed.
func Listen(dir string) (net.Listener, error) {
	return ListenOn("127.0.0.1:0", dir)
}

// ListenOn serves dir on addr until the listener is closed.
func ListenOn(addr, dir string) (net.Listener, error) {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	fsys := changeFS{osfs.New(dir, osfs.WithBoundOS()), dir}
	h := helpers.NewCachingHandler(helpers.NewNullAuthHandler(fsys), 4096)
	go func() { _ = nfs.Serve(l, h) }() // returns once the listener is closed
	return l, nil
}

// changeFS lets the server set times and modes, which billy's osfs cannot:
// without it, go-nfs answers SETATTR with NOTSUPP.
type changeFS struct {
	billy.Filesystem
	root string
}

var _ billy.Change = changeFS{}

// path turns the server's name for a file into one inside root.
func (c changeFS) path(name string) string {
	return filepath.Join(c.root, filepath.Clean("/"+name))
}

func (c changeFS) Chmod(name string, mode os.FileMode) error { return os.Chmod(c.path(name), mode) }

func (c changeFS) Lchown(name string, uid, gid int) error { return os.Lchown(c.path(name), uid, gid) }

func (c changeFS) Chown(name string, uid, gid int) error { return os.Chown(c.path(name), uid, gid) }

func (c changeFS) Chtimes(name string, atime, mtime time.Time) error {
	return os.Chtimes(c.path(name), atime, mtime)
}
