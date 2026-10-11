// SPDX-License-Identifier: GPL-2.0-or-later

package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fqazzazee/shuttle/internal/fsys"
	"github.com/fqazzazee/shuttle/internal/fsys/fsystest"
)

// The engine tests run the remote side of every shuttle either as a local
// temp folder, or, when these point at the test Samba server (`make
// samba-up`, `make test-integration`), as a fresh folder of its share reached over
// SMB. The share is a folder on this machine too, so the tests set up and
// inspect the remote side directly either way.
const (
	envSMBAddr = "SHUTTLE_TEST_SMB_ADDR"
	envSMBDir  = "SHUTTLE_TEST_SMB_DIR" // the share's folder on this machine
)

func overSMB() bool {
	return os.Getenv(envSMBAddr) != "" && os.Getenv(envSMBDir) != ""
}

// newRemoteDir returns the folder for a test's remote side.
func newRemoteDir(t *testing.T) string {
	t.Helper()
	if !overSMB() {
		return t.TempDir()
	}
	dir := filepath.Join(os.Getenv(envSMBDir), fsystest.UniqueName("engine"))
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// openRemote opens a folder from newRemoteDir the way the run should reach
// it: over SMB when the tests run against Samba, directly otherwise.
func openRemote(dir string) (fsys.FS, error) {
	if !overSMB() {
		return fsys.OpenLocal(dir)
	}
	folder, err := filepath.Rel(os.Getenv(envSMBDir), dir)
	if err != nil || strings.HasPrefix(folder, "..") {
		return fsys.OpenLocal(dir) // a folder outside the share: a local test
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
}
