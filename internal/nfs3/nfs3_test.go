// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package nfs3_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/fqazzazee/shuttle/internal/nfs3"
	"github.com/fqazzazee/shuttle/internal/nfs3/nfs3test"
)

// dial mounts a fresh folder served by the in-process server, and returns
// the client and the folder, which the test can also reach directly.
func dial(t *testing.T) (*nfs3.Client, string) {
	t.Helper()
	dir := t.TempDir()
	host, port := nfs3test.Serve(t, dir)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := nfs3.Dial(ctx, &nfs3.Options{
		Host: host, Port: port, MountPort: port, Export: "/",
		UID: uint32(os.Getuid()), GID: uint32(os.Getgid()), Machine: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c, dir
}

func TestFiles(t *testing.T) {
	c, dir := dial(t)
	ctx := t.Context()
	root := c.Root()

	fh, err := c.Create(ctx, root, "a.bin", 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Create(ctx, root, "a.bin", 0o644); !errors.Is(err, fs.ErrExist) {
		t.Errorf("second Create = %v, want fs.ErrExist", err)
	}

	// More than one write's worth, so the data crosses several calls.
	want := make([]byte, 3*c.WriteSize()+123)
	rand.Read(want)
	var verf nfs3.Verifier
	for off := 0; off < len(want); {
		n, _, v, err := c.Write(ctx, fh, uint64(off), want[off:min(off+c.WriteSize(), len(want))], nfs3.Unstable)
		if err != nil {
			t.Fatal(err)
		}
		off += n
		verf = v
	}
	if v, err := c.Commit(ctx, fh); err != nil || v != verf {
		t.Fatalf("Commit = %x, %v; the writes' verifier was %x", v, err, verf)
	}
	if got, err := os.ReadFile(filepath.Join(dir, "a.bin")); err != nil || !bytes.Equal(got, want) {
		t.Fatalf("the server's file differs (%d bytes, %v)", len(got), err)
	}

	var got []byte
	for eof := false; !eof; {
		var data []byte
		data, eof, err = c.Read(ctx, fh, uint64(len(got)), c.ReadSize())
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, data...)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("read back %d bytes, differing from the %d written", len(got), len(want))
	}

	mtime := time.Date(2024, 5, 6, 7, 8, 9, 0, time.UTC)
	if err := c.SetMtime(ctx, fh, mtime); err != nil {
		t.Fatal(err)
	}
	lfh, a, err := c.Lookup(ctx, root, "a.bin")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(lfh, fh) || a.Type != nfs3.TypeReg || a.Size != uint64(len(want)) || !a.Mtime.Equal(mtime) {
		t.Errorf("Lookup = %x %+v", lfh, a)
	}
	if err := c.SetMtime(ctx, fh, time.Date(1960, 1, 1, 0, 0, 0, 0, time.UTC)); err == nil {
		t.Error("SetMtime before 1970 worked; NFSv3 cannot carry it")
	}

	if _, _, err := c.Lookup(ctx, root, "missing"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Lookup(missing) = %v, want fs.ErrNotExist", err)
	}
}

func TestFolders(t *testing.T) {
	c, dir := dial(t)
	ctx := t.Context()
	root := c.Root()

	sub, err := c.Mkdir(ctx, root, "sub", 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Create(ctx, sub, "f", 0o644); err != nil {
		t.Fatal(err)
	}
	if err := c.Rmdir(ctx, root, "sub"); err == nil {
		t.Error("Rmdir of a folder with a file in it worked")
	}
	if err := c.Rename(ctx, sub, "f", root, "g"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "g")); err != nil {
		t.Errorf("after Rename: %v", err)
	}
	if err := c.Rmdir(ctx, root, "sub"); err != nil {
		t.Fatal(err)
	}
	if err := c.Remove(ctx, root, "g"); err != nil {
		t.Fatal(err)
	}
	if err := c.Remove(ctx, root, "g"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("second Remove = %v, want fs.ErrNotExist", err)
	}
}

// A listing longer than one READDIRPLUS reply comes back whole, once each.
func TestReadDirPages(t *testing.T) {
	c, dir := dial(t)
	var want []string
	for i := range 2500 {
		name := fmt.Sprintf("file-with-a-longish-name-%04d", i)
		want = append(want, name)
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "z-dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	want = append(want, "z-dir")

	var got []string
	err := c.ReadDir(t.Context(), c.Root(), func(e nfs3.Entry) error {
		got = append(got, e.Name)
		if e.Name == "z-dir" && (!e.HasAttr || e.Attr.Type != nfs3.TypeDir || e.FH == nil) {
			t.Errorf("z-dir: %+v", e)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("listed %d names, want %d", len(got), len(want))
	}
}

func TestMountErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	// A port nothing listens on: refused straight away.
	_, port := nfs3test.Serve(t, t.TempDir())
	o := &nfs3.Options{Host: "127.0.0.1", Port: port, MountPort: 1, Export: "/"}
	if _, err := nfs3.Dial(ctx, o); err == nil {
		t.Error("Dial to a closed mount port worked")
	}
}

// A server that speaks only NFSv4 has no NFSv3 mount service. Dial says
// so, and what to do instead: the README promises it.
func TestNFSv4OnlyServer(t *testing.T) {
	host := os.Getenv(nfs3test.EnvV4Only)
	if host == "" {
		t.Skipf("%s not set: CI's kernel server job sets it", nfs3test.EnvV4Only)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c, err := nfs3.Dial(ctx, &nfs3.Options{Host: host, Export: "/"})
	if err == nil {
		c.Close()
		t.Fatal("an NFSv4-only server mounted over NFSv3")
	}
	if !strings.Contains(err.Error(), "NFSv4-only") || !strings.Contains(err.Error(), "mount it") {
		t.Errorf("Dial = %v; want it to say the server is NFSv4-only and to mount it", err)
	}
}

// FSSTAT says how big the served folder's disk is, as statfs does here.
func TestFSStat(t *testing.T) {
	c, dir := dial(t)
	got, err := c.FSStat(t.Context(), c.Root())
	if err != nil {
		t.Fatal(err)
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		t.Fatal(err)
	}
	if want := st.Blocks * uint64(st.Bsize); got.Total != want || got.Avail == 0 || got.Avail > got.Free ||
		got.Free > got.Total {
		t.Errorf("FSStat = %+v, want a total of %d and some room", got, want)
	}
}
