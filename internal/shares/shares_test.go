// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package shares

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fqazzazee/shuttle/internal/config"
	"github.com/fqazzazee/shuttle/internal/engine"
	"github.com/fqazzazee/shuttle/internal/fsys"
	"github.com/fqazzazee/shuttle/internal/fsys/fsystest"
	"github.com/fqazzazee/shuttle/internal/nfs3/nfs3test"
	"github.com/fqazzazee/shuttle/internal/secrets"
	"github.com/fqazzazee/shuttle/internal/state"
)

// memStore is a Store in memory. A nil map fails the test if it is used.
type memStore struct {
	t  *testing.T
	pw map[string]string
}

func (m memStore) Get(_ context.Context, k secrets.Key) (string, error) {
	if m.pw == nil {
		m.t.Errorf("the keyring was asked for %s", k.Account())
	}
	if pw, ok := m.pw[k.Account()]; ok {
		return pw, nil
	}
	return "", secrets.ErrNotFound
}

func (m memStore) Set(_ context.Context, k secrets.Key, pw string) error {
	m.pw[k.Account()] = pw
	return nil
}

func (m memStore) Delete(_ context.Context, k secrets.Key) error {
	delete(m.pw, k.Account())
	return nil
}

// load parses a config and returns it with its only shuttle.
func load(t *testing.T, doc string) (*config.Config, *config.Shuttle) {
	t.Helper()
	cfg, err := config.Parse([]byte(doc), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Shuttles) != 1 {
		t.Fatalf("want one shuttle, got %d", len(cfg.Shuttles))
	}
	return cfg, &cfg.Shuttles[0]
}

func TestKey(t *testing.T) {
	k, ok := Key(&config.Share{Type: config.ShareSMB, Host: "nas.example", Port: 1445, Share: "home", User: "u", Domain: "WG"})
	if !ok || k.Account() != "smb://WG;u@nas.example:1445/home" {
		t.Errorf("Key = %q, %v", k.Account(), ok)
	}
	for _, s := range []config.Share{
		{Type: config.ShareSMB, Host: "h", Share: "s"}, // guest
		{Type: config.SharePath, Path: "/mnt/x"},
		{Type: config.ShareNFS3, Host: "h", Export: "/e"},
	} {
		if _, ok := Key(&s); ok {
			t.Errorf("Key(%+v) wants a password", s)
		}
	}
}

func TestPathShare(t *testing.T) {
	mount := t.TempDir()
	if err := os.MkdirAll(filepath.Join(mount, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mount, "docs", "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, s := load(t, fmt.Sprintf(`
[shares.m]
type = "path"
path = %q
[shuttles.s]
mode = "pull"
local = "/tmp/never-used"
remote = "m:/docs"
`, mount))
	f, err := OpenRemote(context.Background(), cfg, s, memStore{t: t})
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if info, err := f.Stat(context.Background(), "a.txt"); err != nil || info.Size != 1 {
		t.Errorf("Stat(a.txt) = %+v, %v", info, err)
	}
}

// nfsShare is an NFS share of a folder served in-process, which holds
// docs/a.txt.
func nfsShare(t *testing.T) config.Share {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docs", "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	host, port := nfs3test.Serve(t, dir)
	return config.Share{Name: "n", Type: config.ShareNFS3, Host: host, Port: port, MountPort: port, Export: "/"}
}

func TestNFSShare(t *testing.T) {
	share := nfsShare(t)
	cfg, s := load(t, fmt.Sprintf(`
[shares.n]
type = "nfs3"
host = %q
port = %d
mount_port = %d
export = "/"
[shuttles.s]
mode = "pull"
local = "/tmp/never-used"
remote = "n:/docs"
`, share.Host, share.Port, share.MountPort))
	ctx := context.Background()
	f, err := OpenRemote(ctx, cfg, s, memStore{t: t}) // a nil map: the keyring is never asked
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if info, err := f.Stat(ctx, "a.txt"); err != nil || info.Size != 1 {
		t.Errorf("Stat(a.txt) = %+v, %v", info, err)
	}
	if _, err := OpenShare(ctx, &share, "missing", memStore{t: t}); err == nil || !strings.Contains(err.Error(), "share n:") {
		t.Errorf("a folder that is not there: %v", err)
	}
}

// Unset IDs are the user running shuttle, with its groups; a share that sets
// either is someone else, and gets none of this user's groups.
func TestNFSUser(t *testing.T) {
	me, mine := uint32(os.Getuid()), uint32(os.Getgid()) //nolint:gosec // test IDs
	groups, err := os.Getgroups()
	if err != nil {
		t.Fatal(err)
	}
	uid, gid, got := NFSUser(&config.Share{Type: config.ShareNFS3})
	if uid != me || gid != mine || len(got) != len(groups) {
		t.Errorf("unset: %d, %d, %v; want %d, %d and %d groups", uid, gid, got, me, mine, len(groups))
	}
	uid, gid, got = NFSUser(&config.Share{Type: config.ShareNFS3, UID: ptr[uint32](0)})
	if uid != 0 || gid != mine || got != nil {
		t.Errorf("uid = 0: %d, %d, %v", uid, gid, got)
	}
	uid, gid, got = NFSUser(&config.Share{Type: config.ShareNFS3, GID: ptr[uint32](100)})
	if uid != me || gid != 100 || got != nil {
		t.Errorf("gid = 100: %d, %d, %v", uid, gid, got)
	}
}

func smbConfig(t *testing.T, addr, user, folder string) (*config.Config, *config.Shuttle) {
	t.Helper()
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	return load(t, fmt.Sprintf(`
[shares.nas]
type = "smb"
host = %q
port = %s
share = "test"
user = %q
[shuttles.s]
mode = "push"
local = %q
remote = "nas:/%s"
`, host, port, user, t.TempDir(), folder))
}

func TestMissingPasswordSaysWhereToPutIt(t *testing.T) {
	cfg, s := smbConfig(t, "127.0.0.1:1", "user", "x")
	_, err := OpenRemote(context.Background(), cfg, s, memStore{t: t, pw: map[string]string{}})
	if !errors.Is(err, secrets.ErrNotFound) || !strings.Contains(err.Error(), "smb://user@127.0.0.1:1/test") {
		t.Errorf("OpenRemote = %v", err)
	}
}

func TestGuestShareNeverAsksTheKeyring(t *testing.T) {
	cfg, s := smbConfig(t, "127.0.0.1:1", "", "x") // nothing listens on port 1
	if _, err := OpenRemote(context.Background(), cfg, s, memStore{t: t}); err == nil {
		t.Error("OpenRemote to a closed port succeeded")
	}
}

// smbFolder makes a fresh folder on the test Samba share and returns its
// name, or skips without a test server.
func smbFolder(t *testing.T) (addr, folder string) {
	t.Helper()
	addr = fsystest.Env(t, "SHUTTLE_TEST_SMB_ADDR")
	dir := fsystest.Env(t, "SHUTTLE_TEST_SMB_DIR")
	folder = fsystest.UniqueName("shares")
	if err := os.Mkdir(filepath.Join(dir, folder), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(filepath.Join(dir, folder)) })
	return addr, folder
}

func TestSMBShare(t *testing.T) {
	addr, folder := smbFolder(t)
	cfg, s := smbConfig(t, addr, "shuttle", folder)
	k, _ := Key(ptr(cfg.Shares["nas"]))
	ctx := context.Background()

	f, err := OpenRemote(ctx, cfg, s, memStore{t: t, pw: map[string]string{k.Account(): fsystest.SambaPassword}})
	if err != nil {
		t.Fatal(err)
	}
	f.Close()

	const wrong = "wr0ng-s3cret"
	_, err = OpenRemote(ctx, cfg, s, memStore{t: t, pw: map[string]string{k.Account(): wrong}})
	if err == nil || strings.Contains(err.Error(), wrong) {
		t.Errorf("wrong password: err = %v", err)
	}
}

// TestPasswordComesOnlyFromTheKeyring is Phase 2's exit condition end to
// end: a config with no password in it, the password in a real (throwaway)
// Secret Service, a real push to Samba, and the password nowhere in what the
// run leaves behind.
func TestPasswordComesOnlyFromTheKeyring(t *testing.T) {
	if os.Getenv("SHUTTLE_TEST_KEYRING") != "isolated" {
		t.Skip("needs the throwaway keyring: run `make test-integration`")
	}
	addr, folder := smbFolder(t)
	cfg, s := smbConfig(t, addr, "shuttle", folder)
	// A stuck keyring must fail this test, not hang it.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	store := secrets.SecretService()
	k, _ := Key(ptr(cfg.Shares["nas"]))
	const password = fsystest.SambaPassword
	if err := store.Set(ctx, k, password); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Delete(ctx, k) })

	if err := os.WriteFile(filepath.Join(s.Local, "report.txt"), []byte("pushed over SMB"), 0o644); err != nil {
		t.Fatal(err)
	}
	local, err := fsys.OpenLocal(s.Local)
	if err != nil {
		t.Fatal(err)
	}
	defer local.Close()
	remote, err := OpenRemote(ctx, cfg, s, store)
	if err != nil {
		t.Fatal(err)
	}
	defer remote.Close()
	statePath := filepath.Join(t.TempDir(), "s.db")
	db, err := state.Open(statePath)
	if err != nil {
		t.Fatal(err)
	}
	res, err := engine.Run(ctx, &engine.Options{Local: local, Remote: remote, State: db, Settings: &s.Settings})
	db.Close()
	if err != nil || res.Copied != 1 || len(res.Errors) > 0 {
		t.Fatalf("run: %v %+v", err, res)
	}
	got, err := os.ReadFile(filepath.Join(os.Getenv("SHUTTLE_TEST_SMB_DIR"), folder, "report.txt"))
	if err != nil || string(got) != "pushed over SMB" {
		t.Errorf("file on the share = %q, %v", got, err)
	}

	// The password must not be in anything the run produced or loaded.
	stateBytes, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(stateBytes, []byte(password)) {
		t.Error("the password is in the state database")
	}
	if dump := fmt.Sprintf("%+v %+v %+v", res, cfg, s); strings.Contains(dump, password) {
		t.Errorf("the password reached the run's result or the config: %s", dump)
	}
}

func ptr[T any](v T) *T { return &v }

func TestTestAPathShare(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	if space, err := Test(ctx, &config.Share{Name: "usb", Type: config.SharePath, Path: dir}, "", memStore{t: t}); err != nil ||
		space.Total <= 0 {
		t.Errorf("a mounted folder: %+v, %v", space, err)
	}
	_, err := Test(ctx, &config.Share{Name: "usb", Type: config.SharePath, Path: filepath.Join(dir, "gone")}, "", memStore{t: t})
	if err == nil || !strings.Contains(err.Error(), "is the share mounted?") {
		t.Errorf("a folder that is not there: %v", err)
	}
}

func TestTestAnNFSShare(t *testing.T) {
	ctx := context.Background()
	share := nfsShare(t)
	if space, err := Test(ctx, &share, "", memStore{t: t}); err != nil || space.Total <= 0 {
		t.Errorf("a served export: %+v, %v", space, err)
	}
	share.Port, share.MountPort = 1, 1
	if _, err := Test(ctx, &share, "", memStore{t: t}); err == nil {
		t.Error("a closed port answered")
	}
}

// A password typed in the form is tried as it is: the keyring is not asked.
func TestTestWithATypedPassword(t *testing.T) {
	share := &config.Share{Name: "nas", Type: config.ShareSMB, Host: "127.0.0.1", Port: 1, Share: "test", User: "user"}
	if _, err := Test(context.Background(), share, "typed", memStore{t: t}); err == nil {
		t.Error("a closed port answered")
	}
}

func TestTestAnSMBShare(t *testing.T) {
	addr, _ := smbFolder(t)
	host, port, _ := net.SplitHostPort(addr)
	p, _ := strconv.Atoi(port)
	share := &config.Share{Name: "nas", Type: config.ShareSMB, Host: host, Port: p, Share: "test", User: "shuttle"}
	k, _ := Key(share)
	ctx := context.Background()

	if space, err := Test(ctx, share, fsystest.SambaPassword, memStore{t: t}); err != nil || space.Total <= 0 {
		t.Errorf("the right password, typed: %+v, %v", space, err)
	}
	if _, err := Test(ctx, share, "wr0ng-s3cret", memStore{t: t}); err == nil {
		t.Error("a wrong password, typed, passed")
	}
	stored := memStore{t: t, pw: map[string]string{k.Account(): fsystest.SambaPassword}}
	if _, err := Test(ctx, share, "", stored); err != nil {
		t.Errorf("the stored password: %v", err)
	}
	if _, err := Test(ctx, share, "", memStore{t: t, pw: map[string]string{}}); !errors.Is(err, secrets.ErrNotFound) {
		t.Errorf("no password stored: %v", err)
	}
}
