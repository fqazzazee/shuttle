// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package secretservice

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/fqazzazee/shuttle/internal/secrets"
)

// Tests that talk to a Secret Service only run inside the throwaway keyring
// of testdata/with-test-keyring.sh (`make test-keyring`). Anywhere else they
// would read and write the user's own keyring.
func isolated(t *testing.T) {
	t.Helper()
	if os.Getenv("SHUTTLE_TEST_KEYRING") != "isolated" {
		t.Skip("needs the throwaway keyring: run `make test-keyring`")
	}
}

var key = secrets.Key{Host: "nas.example", Share: "home", User: "user"}

func TestIncompleteKeysAreRefused(t *testing.T) {
	ctx := context.Background()
	s := New()
	for _, k := range []secrets.Key{{Share: "home", User: "u"}, {Host: "h", User: "u"}, {Host: "h", Share: "s"}} {
		if _, err := s.Get(ctx, k); err == nil {
			t.Errorf("Get(%+v) succeeded", k)
		}
		if err := s.Set(ctx, k, "pw"); err == nil {
			t.Errorf("Set(%+v) succeeded", k)
		}
	}
}

// With no bus to reach, the answer must be "unavailable": go-keyring on its
// own would say "not found", and the daemon would then ask the user for a
// password it cannot store.
func TestNoBusIsUnavailableNotNotFound(t *testing.T) {
	if os.Getenv("SHUTTLE_TEST_KEYRING") == "isolated" {
		t.Skip("the isolated run already holds a working bus connection")
	}
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path=/nonexistent/shuttle-test-bus")
	_, err := New().Get(context.Background(), key)
	if !errors.Is(err, ErrUnavailable) || errors.Is(err, secrets.ErrNotFound) {
		t.Errorf("Get = %v, want ErrUnavailable", err)
	}
}

// keyringCtx bounds a test's keyring calls, so a stuck unlock prompt fails
// the test quickly instead of hanging it.
func keyringCtx(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestRoundTrip(t *testing.T) {
	isolated(t)
	ctx := keyringCtx(t)
	s := New()
	other := secrets.Key{Host: "nas.example", Share: "photos", User: "user"}

	if _, err := s.Get(ctx, key); !errors.Is(err, secrets.ErrNotFound) {
		t.Fatalf("Get before Set = %v, want ErrNotFound", err)
	}
	if err := s.Set(ctx, key, "first password"); err != nil {
		t.Fatal(err)
	}
	if err := s.Set(ctx, other, "other password"); err != nil {
		t.Fatal(err)
	}
	if err := s.Set(ctx, key, "second password"); err != nil { // replaces, not adds
		t.Fatal(err)
	}
	if got, err := s.Get(ctx, key); err != nil || got != "second password" {
		t.Errorf("Get = %q, %v", got, err)
	}
	if got, err := s.Get(ctx, other); err != nil || got != "other password" {
		t.Errorf("Get(other) = %q, %v: keys must not share a password", got, err)
	}
	if err := s.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, key); !errors.Is(err, secrets.ErrNotFound) {
		t.Errorf("Get after Delete = %v, want ErrNotFound", err)
	}
	if err := s.Set(ctx, key, ""); err == nil {
		t.Error("an empty password was stored")
	}
}

// The stored item is an ordinary keyring entry other tools can see, labelled
// so the user can tell what it is in Passwords and Keys.
func TestVisibleToOtherTools(t *testing.T) {
	isolated(t)
	tool, err := exec.LookPath("secret-tool")
	if err != nil {
		t.Skip("secret-tool not installed")
	}
	if err := New().Set(keyringCtx(t), key, "seen by secret-tool"); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(tool, "lookup", "service", "shuttle", "username", key.Account()).Output()
	if err != nil || string(out) != "seen by secret-tool" {
		t.Errorf("secret-tool lookup = %q, %v", out, err)
	}
}

func TestCancelledContext(t *testing.T) {
	isolated(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := New().Set(ctx, key, "never stored"); !errors.Is(err, context.Canceled) {
		t.Errorf("Set = %v, want context.Canceled", err)
	}
}

// Errors name the key, never the password.
func TestErrorsNeverCarryThePassword(t *testing.T) {
	isolated(t)
	const secret = "do-not-print-me"
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := New().Set(ctx, key, secret)
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Errorf("error = %v", err)
	}
}

// TestLockedKeyring locks a keyring, which nothing can unlock again without
// a display. So it runs in a throwaway keyring of its own, in a child
// process: locking the shared one would hang every later test that uses it.
func TestLockedKeyring(t *testing.T) {
	isolated(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "testdata/with-test-keyring.sh", os.Args[0], "-test.run=^TestLockedKeyringChild$", "-test.v")
	cmd.Env = append(os.Environ(), "SHUTTLE_TEST_LOCK_CHILD=1")
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "--- PASS: TestLockedKeyringChild") {
		t.Errorf("locked-keyring child: %v\n%s", err, out)
	}
}

func TestLockedKeyringChild(t *testing.T) {
	if os.Getenv("SHUTTLE_TEST_LOCK_CHILD") == "" {
		t.Skip("run by TestLockedKeyring in a keyring of its own")
	}
	isolated(t)
	conn, err := dbus.SessionBus()
	if err != nil {
		t.Fatal(err)
	}
	var locked []dbus.ObjectPath
	var prompt dbus.ObjectPath
	err = conn.Object(secretsName, "/org/freedesktop/secrets").
		Call("org.freedesktop.Secret.Service.Lock", 0, []dbus.ObjectPath{"/org/freedesktop/secrets/collection/login"}).
		Store(&locked, &prompt)
	if err != nil || len(locked) != 1 {
		t.Fatalf("locking the test keyring: %v %v", locked, err)
	}
	// With no display the unlock prompt cannot appear; go-keyring then either
	// fails or waits for a prompt that never comes, so give it a deadline.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, err = New().Get(ctx, key)
	if !errors.Is(err, ErrLocked) && !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Get on a locked keyring = %v, want ErrLocked or the deadline", err)
	}
}
