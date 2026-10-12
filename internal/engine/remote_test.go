// SPDX-License-Identifier: GPL-2.0-or-later

package engine

import (
	"cmp"
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/fqazzazee/shuttle/internal/fsys"
	"github.com/fqazzazee/shuttle/internal/fsys/fsystest"
	"github.com/fqazzazee/shuttle/internal/nfs3"
	"github.com/fqazzazee/shuttle/internal/nfs3/nfs3test"
)

// The engine tests run the remote side of every shuttle as a local temp
// folder, or, when these say so, as a fresh folder of a share:
//
//   - SMB: the test Samba server (`make samba-up`, `make test-integration`).
//   - NFS: SHUTTLE_TEST_NFS=inprocess serves a temp folder from inside the
//     test (`make test-nfs`); SHUTTLE_TEST_NFS=host or host:port uses that
//     server's export SHUTTLE_TEST_NFS_EXPORT, whose folder on this machine
//     is SHUTTLE_TEST_NFS_DIR. A port means both services listen on it;
//     without one, the server's portmapper says where.
//
// The share is a folder on this machine too, so the tests set up and
// inspect the remote side directly either way.
const (
	envSMBAddr   = "SHUTTLE_TEST_SMB_ADDR"
	envSMBDir    = "SHUTTLE_TEST_SMB_DIR" // the share's folder on this machine
	envNFS       = "SHUTTLE_TEST_NFS"
	envNFSExport = "SHUTTLE_TEST_NFS_EXPORT"
	envNFSDir    = "SHUTTLE_TEST_NFS_DIR"
)

func overSMB() bool {
	return os.Getenv(envSMBAddr) != "" && os.Getenv(envSMBDir) != ""
}

func overNFS() bool { return os.Getenv(envNFS) != "" }

// remoteRoot is the folder on this machine the remote side's share serves,
// or "" when the tests run locally.
func remoteRoot() (string, error) {
	switch {
	case overSMB() && overNFS():
		return "", fmt.Errorf("both %s and %s are set: pick one", envSMBAddr, envNFS)
	case overSMB():
		return os.Getenv(envSMBDir), nil
	case overNFS():
		target, err := nfsServer()
		return target.dir, err
	}
	return "", nil
}

// nfsTarget is the NFS server the tests use.
type nfsTarget struct {
	opts nfs3.Options
	dir  string
}

var inprocess struct {
	once sync.Once
	stop func()
	err  error
}

// stopNFS stops the in-process server, if one was started, and removes its
// folder.
func stopNFS() {
	if inprocess.stop != nil {
		inprocess.stop()
	}
}

func nfsServer() (nfsTarget, error) {
	if os.Getenv(envNFS) == "inprocess" {
		// One server for the whole test binary, on a folder of its own. It
		// then counts as an outside server, so child processes (the crash
		// tests) reach the same one.
		inprocess.once.Do(func() {
			dir, err := os.MkdirTemp("", "shuttle-nfs-")
			if err != nil {
				inprocess.err = err
				return
			}
			l, err := nfs3test.Listen(dir)
			if err != nil {
				inprocess.err = err
				return
			}
			inprocess.stop = func() {
				l.Close()
				os.RemoveAll(dir)
			}
			os.Setenv(envNFS, l.Addr().String())
			os.Setenv(envNFSDir, dir)
			os.Unsetenv(envNFSExport)
		})
		if inprocess.err != nil {
			return nfsTarget{}, inprocess.err
		}
	}
	addr := os.Getenv(envNFS)
	host, port := addr, 0
	if h, p, err := net.SplitHostPort(addr); err == nil {
		host = h
		if port, err = strconv.Atoi(p); err != nil {
			return nfsTarget{}, fmt.Errorf("%s=%q: %w", envNFS, addr, err)
		}
	}
	dir := os.Getenv(envNFSDir)
	if dir == "" {
		return nfsTarget{}, fmt.Errorf("%s is set but %s is not", envNFS, envNFSDir)
	}
	return nfsTarget{
		opts: nfs3.Options{
			Host: host, Port: port, MountPort: port, Export: cmp.Or(os.Getenv(envNFSExport), "/"),
			UID: uint32(os.Getuid()), GID: uint32(os.Getgid()), //nolint:gosec // this user's IDs
		},
		dir: dir,
	}, nil
}

// newRemoteDir returns the folder for a test's remote side.
func newRemoteDir(t *testing.T) string {
	t.Helper()
	root, err := remoteRoot()
	if err != nil {
		t.Fatal(err)
	}
	if root == "" {
		return t.TempDir()
	}
	dir := filepath.Join(root, fsystest.UniqueName("engine"))
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// openRemote opens a folder from newRemoteDir the way the run should reach
// it: over SMB or NFS when the tests run against a server, directly
// otherwise.
func openRemote(dir string) (fsys.FS, error) {
	root, err := remoteRoot()
	if err != nil {
		return nil, err
	}
	folder, err := filepath.Rel(root, dir)
	if root == "" || err != nil || strings.HasPrefix(folder, "..") {
		return fsys.OpenLocal(dir) // a folder outside the share: a local test
	}
	if overNFS() {
		target, err := nfsServer()
		if err != nil {
			return nil, err
		}
		return fsys.DialNFS(context.Background(), &fsys.NFSOptions{Options: target.opts, Folder: filepath.ToSlash(folder)})
	}
	return fsys.DialSMB(context.Background(), &fsys.SMBOptions{
		Addr: os.Getenv(envSMBAddr), Share: "test", Folder: filepath.ToSlash(folder),
		User: fsystest.SambaUser, Password: fsystest.SambaPassword,
	})
}

// TestRemoteSide makes it visible in the test output which kind of remote
// side this run of the suite used.
func TestRemoteSide(t *testing.T) {
	dir := newRemoteDir(t)
	f, err := openRemote(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	t.Logf("remote side: %T", f)
	if _, ok := f.(*fsys.SMB); overSMB() && !ok {
		t.Errorf("SMB is configured but the remote side is %T", f)
	}
	if _, ok := f.(*fsys.NFS); overNFS() && !ok {
		t.Errorf("NFS is configured but the remote side is %T", f)
	}
}
