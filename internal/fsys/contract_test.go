// SPDX-License-Identifier: GPL-2.0-or-later

package fsys_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fqazzazee/shuttle/internal/fsys"
	"github.com/fqazzazee/shuttle/internal/fsys/fsystest"
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
