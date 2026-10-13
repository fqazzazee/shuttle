// SPDX-License-Identifier: GPL-2.0-or-later

// Package nfs3test serves a folder over NFSv3, in-process, for tests: the
// willscott/go-nfs server, which answers MOUNT and NFS on one port and runs
// no portmapper. It also finds an outside server, when the environment
// names one.
package nfs3test

import (
	"cmp"
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/osfs"
	nfs "github.com/willscott/go-nfs"
	"github.com/willscott/go-nfs/helpers"

	"github.com/fqazzazee/shuttle/internal/nfs3"
)

// The environment that names an outside NFSv3 server, such as the Linux
// kernel server in CI.
const (
	// EnvServer is the server: a host, whose portmapper says where its
	// services are, or host:port, where both listen. The engine tests also
	// take "inprocess", which is theirs to handle.
	EnvServer = "SHUTTLE_TEST_NFS"
	EnvExport = "SHUTTLE_TEST_NFS_EXPORT" // the export; "/" if unset
	EnvDir    = "SHUTTLE_TEST_NFS_DIR"    // the export's folder on this machine
	// EnvV4Only is the host of a server that speaks only NFSv4.
	EnvV4Only = "SHUTTLE_TEST_NFS4_ONLY"
)

// Outside returns the server the environment names, reached as this user,
// and the export's folder on this machine. ok is false if it names none.
func Outside() (o nfs3.Options, dir string, ok bool, err error) {
	addr := os.Getenv(EnvServer)
	if addr == "" || addr == "inprocess" {
		return o, "", false, nil
	}
	host, port := addr, 0
	if h, p, err := net.SplitHostPort(addr); err == nil {
		host = h
		if port, err = strconv.Atoi(p); err != nil {
			return o, "", false, fmt.Errorf("%s=%q: %w", EnvServer, addr, err)
		}
	}
	if dir = os.Getenv(EnvDir); dir == "" {
		return o, "", false, fmt.Errorf("%s is set but %s is not", EnvServer, EnvDir)
	}
	return nfs3.Options{
		Host: host, Port: port, MountPort: port, Export: cmp.Or(os.Getenv(EnvExport), "/"),
		UID: uint32(os.Getuid()), GID: uint32(os.Getgid()), //nolint:gosec // this user's IDs
	}, dir, true, nil
}

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
	h := statfsHandler{helpers.NewCachingHandler(helpers.NewNullAuthHandler(fsys), 1<<16), dir}
	go func() { _ = nfs.Serve(l, h) }() // returns once the listener is closed
	return l, nil
}

// statfsHandler answers FSSTAT with the room of the served folder's disk,
// as a real server does: go-nfs on its own says 4 EiB of everything.
type statfsHandler struct {
	nfs.Handler
	dir string
}

func (h statfsHandler) FSStat(_ context.Context, _ billy.Filesystem, s *nfs.FSStat) error {
	var st syscall.Statfs_t
	if err := syscall.Statfs(h.dir, &st); err != nil {
		return err
	}
	bs := uint64(st.Bsize) //nolint:gosec // a block size is positive
	s.TotalSize, s.FreeSize, s.AvailableSize = st.Blocks*bs, st.Bfree*bs, st.Bavail*bs
	s.TotalFiles, s.FreeFiles, s.AvailableFiles = st.Files, st.Ffree, st.Ffree
	return nil
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
