// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package fsys_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fqazzazee/shuttle/internal/fsys"
	"github.com/fqazzazee/shuttle/internal/fsys/fsystest"
	"github.com/fqazzazee/shuttle/internal/nfs3"
	"github.com/fqazzazee/shuttle/internal/nfs3/nfs3test"
)

func TestLocalContract(t *testing.T) {
	fsystest.Run(t, func(t *testing.T) fsys.FS {
		l, err := fsys.OpenLocal(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { l.Close() })
		return l
	})
}

// TestLocalAndroidSharedStorageContract holds a folder that refuses what
// the phone's shared storage refuses to the same contract.
func TestLocalAndroidSharedStorageContract(t *testing.T) {
	fsystest.Run(t, func(t *testing.T) fsys.FS {
		l, err := fsys.OpenLocalWith(t.TempDir(), fsys.AndroidSharedStorage)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { l.Close() })
		return l
	})
}

// smbOptions points at the Samba container from `make samba-up`.
func smbOptions(t *testing.T) *fsys.SMBOptions {
	return &fsys.SMBOptions{
		Addr: fsystest.Env(t, "SHUTTLE_TEST_SMB_ADDR"), Share: "test",
		User: fsystest.SambaUser, Password: fsystest.SambaPassword,
	}
}

// newSMB returns an SMB FS on a fresh folder of the test share.
func newSMB(t *testing.T) *fsys.SMB {
	t.Helper()
	ctx := context.Background()
	root, err := fsys.DialSMB(ctx, smbOptions(t))
	if err != nil {
		t.Fatal(err)
	}
	folder := fsystest.UniqueName("contract")
	if err := root.MkdirAll(ctx, folder); err != nil {
		t.Fatal(err)
	}
	root.Close()
	// Removed through the share's folder on this machine, when the test
	// knows it (make test-integration does): fsys removes only empty folders.
	if dir := os.Getenv("SHUTTLE_TEST_SMB_DIR"); dir != "" {
		t.Cleanup(func() { os.RemoveAll(filepath.Join(dir, folder)) })
	}

	o := smbOptions(t)
	o.Folder = folder
	s, err := fsys.DialSMB(ctx, o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestSMBContract(t *testing.T) {
	fsystest.Run(t, func(t *testing.T) fsys.FS { return newSMB(t) })
}

// A "\" is a separator on SMB but an ordinary character in a Linux file
// name. Sent as-is, a local file named `..\..\x` would climb out of the
// shuttle's folder on the share.
func TestSMBRejectsBackslashNames(t *testing.T) {
	s := newSMB(t)
	ctx := context.Background()
	for _, name := range []string{`..\..\x`, `a\b`, `dir/..\..\x`} {
		if _, err := s.Create(ctx, name); !errors.Is(err, fsys.ErrInvalidPath) {
			t.Errorf("Create(%q) = %v, want ErrInvalidPath", name, err)
		}
		if err := s.Rename(ctx, "anything", name); !errors.Is(err, fsys.ErrInvalidPath) {
			t.Errorf("Rename(_, %q) = %v, want ErrInvalidPath", name, err)
		}
	}
}

func TestSMBDialErrors(t *testing.T) {
	ctx := context.Background()
	const secret = "wr0ng-s3cret-value"
	tests := map[string]func(o *fsys.SMBOptions){
		"wrong password": func(o *fsys.SMBOptions) { o.Password = secret },
		"unknown share":  func(o *fsys.SMBOptions) { o.Share = "nope" },
		"missing folder": func(o *fsys.SMBOptions) { o.Folder = "does/not/exist" },
		"unsafe folder":  func(o *fsys.SMBOptions) { o.Folder = "../escape" },
		"nothing there":  func(o *fsys.SMBOptions) { o.Addr = "127.0.0.1:1" },
	}
	for name, change := range tests {
		t.Run(name, func(t *testing.T) {
			o := smbOptions(t)
			change(o)
			s, err := fsys.DialSMB(ctx, o)
			if err == nil {
				s.Close()
				t.Fatal("DialSMB succeeded")
			}
			if strings.Contains(err.Error(), secret) {
				t.Errorf("the error leaks the password: %v", err)
			}
		})
	}
}

// nfsTarget is the folder an NFS test works in: a temp folder served by
// the in-process server, or, when the environment names a server (the
// Linux kernel server in CI; see nfs3test.Outside), a fresh folder of its
// export. It returns how to reach the folder and its path on this machine.
func nfsTarget(t *testing.T) (*fsys.NFSOptions, string) {
	t.Helper()
	o, root, ok, err := nfs3test.Outside()
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		dir := t.TempDir()
		host, port := nfs3test.Serve(t, dir)
		return &fsys.NFSOptions{
			Options: nfs3.Options{Host: host, Port: port, MountPort: port, Export: "/",
				UID: uint32(os.Getuid()), GID: uint32(os.Getgid())}, //nolint:gosec // test IDs
			Folder: ".",
		}, dir
	}
	folder := fsystest.UniqueName("contract")
	dir := filepath.Join(root, folder)
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return &fsys.NFSOptions{Options: o, Folder: folder}, dir
}

// sub is o for name, a path inside o's folder.
func sub(o *fsys.NFSOptions, name string) *fsys.NFSOptions {
	c := *o
	c.Folder = path.Join(o.Folder, name)
	return &c
}

// newNFS returns an NFS FS on a fresh folder, and that folder's path on
// this machine.
func newNFS(t *testing.T) (*fsys.NFS, string) {
	t.Helper()
	o, dir := nfsTarget(t)
	if err := os.Mkdir(filepath.Join(dir, "folder"), 0o755); err != nil {
		t.Fatal(err)
	}
	n, err := fsys.DialNFS(context.Background(), sub(o, "folder"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { n.Close() })
	return n, filepath.Join(dir, "folder")
}

func TestNFSContract(t *testing.T) {
	fsystest.Run(t, func(t *testing.T) fsys.FS {
		n, _ := newNFS(t)
		return n
	})
}

func TestNFSFolder(t *testing.T) {
	o, dir := nfsTarget(t)
	if err := os.WriteFile(filepath.Join(dir, "file"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	up := *o
	up.Folder = "../up"
	for _, o := range []*fsys.NFSOptions{sub(o, "missing"), sub(o, "file"), &up} {
		if n, err := fsys.DialNFS(ctx, o); err == nil {
			n.Close()
			t.Errorf("DialNFS(%q) worked", o.Folder)
		}
	}
}

// Another client renames a folder shuttle has a handle for, and puts a new
// one in its place: once the handle's few seconds are up, shuttle writes
// into the new folder.
func TestNFSFolderReplacedBehindItsBack(t *testing.T) {
	n, dir := newNFS(t)
	ctx := context.Background()
	if err := n.MkdirAll(ctx, "d"); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(dir, "d"), filepath.Join(dir, "old")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "d"), 0o755); err != nil {
		t.Fatal(err)
	}
	time.Sleep(fsys.NFSDirTTL + 100*time.Millisecond)
	w, err := n.Create(ctx, "d/x")
	if err != nil {
		t.Fatal(err)
	}
	w.Close()
	if _, err := os.Stat(filepath.Join(dir, "d", "x")); err != nil {
		t.Errorf("not in the new folder: %v", err)
	}
}

// Renaming a folder through shuttle drops its handle at once: the old name
// is gone, not an alias of the new one.
func TestNFSRenamedFolder(t *testing.T) {
	n, dir := newNFS(t)
	ctx := context.Background()
	if err := n.MkdirAll(ctx, "a/sub"); err != nil {
		t.Fatal(err)
	}
	if err := n.Rename(ctx, "a", "b"); err != nil {
		t.Fatal(err)
	}
	if _, err := n.Create(ctx, "a/sub/x"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Create under the old name = %v, want fs.ErrNotExist", err)
	}
	if err := n.Remove(ctx, "b/sub"); err != nil {
		t.Fatal(err)
	}
	if err := n.MkdirAll(ctx, "b/sub/again"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "b", "sub", "again")); err != nil {
		t.Error(err)
	}
}

// A symlink on the server is listed as "other" and never followed, not even
// partway through a path.
func TestNFSSymlinks(t *testing.T) {
	n, dir := newNFS(t)
	ctx := context.Background()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	entries, err := n.ReadDir(ctx, ".")
	if err != nil || len(entries) != 1 || entries[0].Kind != fsys.KindOther {
		t.Errorf("ReadDir = %+v, %v; want link as other", entries, err)
	}
	if _, err := n.Stat(ctx, "link/secret"); err == nil {
		t.Error("Stat through the symlink worked")
	}
	if _, err := n.Open(ctx, "link"); err == nil {
		t.Error("Open of the symlink worked")
	}
	if _, err := n.Create(ctx, "link/new"); err == nil {
		t.Error("Create through the symlink worked")
	}
	if err := n.MkdirAll(ctx, "link/new"); err == nil {
		t.Error("MkdirAll through the symlink worked")
	}
	if _, err := os.Stat(filepath.Join(outside, "new")); err == nil {
		t.Error("something was made outside the folder")
	}
}

// Two clients making the same folders at once: the one that loses the race
// takes the other's folder.
func TestNFSMkdirAllRace(t *testing.T) {
	o, _ := nfsTarget(t)
	ctx := context.Background()
	var clients [2]*fsys.NFS
	for i := range clients {
		n, err := fsys.DialNFS(ctx, o)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { n.Close() })
		clients[i] = n
	}
	for i := range 30 {
		p := fmt.Sprintf("r%d/a/b/c", i)
		errs := make(chan error, 2)
		for _, n := range clients {
			go func() { errs <- n.MkdirAll(ctx, p) }()
		}
		for range clients {
			if err := <-errs; err != nil {
				t.Fatalf("MkdirAll(%s): %v", p, err)
			}
		}
	}
}

// A file larger than one WRITE and one READ comes back whole, written in
// odd-sized pieces.
func TestNFSBigFile(t *testing.T) {
	n, dir := newNFS(t)
	ctx := context.Background()
	want := make([]byte, 3<<20+7)
	rand.Read(want)
	w, err := n.Create(ctx, "big")
	if err != nil {
		t.Fatal(err)
	}
	for p := want; len(p) > 0; {
		k := min(len(p), 100_003)
		if _, err := w.Write(p[:k]); err != nil {
			t.Fatal(err)
		}
		p = p[k:]
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "big")); !bytes.Equal(got, want) {
		t.Fatalf("the server holds %d bytes, differing from the %d written", len(got), len(want))
	}
	r, err := n.Open(ctx, "big")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if got, err := io.ReadAll(r); err != nil || !bytes.Equal(got, want) {
		t.Fatalf("read back %d bytes (%v), differing from the %d written", len(got), err, len(want))
	}
}
