// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package credman

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/fqazzazee/shuttle/internal/fsys/fsystest"
	"github.com/fqazzazee/shuttle/internal/secrets"
)

// Tests that write to Credential Manager run only where SHUTTLE_TEST_CREDMAN
// is set, as on the CI runner: Windows has no throwaway vault, and anywhere
// else they would write to the user's own. Their keys are the run's alone,
// and removed after.
func writes(t *testing.T) secrets.Key {
	t.Helper()
	if os.Getenv("SHUTTLE_TEST_CREDMAN") == "" {
		t.Skip("writes to this user's Credential Manager: set SHUTTLE_TEST_CREDMAN=1 on a throwaway machine")
	}
	k := secrets.Key{Host: "nas.example", Share: fsystest.UniqueName("credman-test"), User: "user"}
	t.Cleanup(func() { New().Delete(context.Background(), k) })
	return k
}

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

func TestRoundTrip(t *testing.T) {
	key := writes(t)
	other := writes(t)
	ctx := context.Background()
	s := New()

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
	if err := s.Delete(ctx, key); !errors.Is(err, secrets.ErrNotFound) {
		t.Errorf("Delete of nothing = %v, want ErrNotFound", err)
	}
	if err := s.Set(ctx, key, ""); err == nil {
		t.Error("an empty password was stored")
	}
}

// The stored credential is an ordinary one, which Windows' own tools list
// by its name: the user can find it in Credential Manager.
func TestVisibleToOtherTools(t *testing.T) {
	key := writes(t)
	if err := New().Set(context.Background(), key, "seen by cmdkey"); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("cmdkey", "/list:shuttle:"+key.Account()).CombinedOutput()
	if err != nil || !strings.Contains(string(out), "shuttle:"+key.Account()) {
		t.Errorf("cmdkey /list = %q, %v", out, err)
	}
}

func TestCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	k := secrets.Key{Host: "nas.example", Share: "never", User: "user"}
	if err := New().Set(ctx, k, "never stored"); !errors.Is(err, context.Canceled) {
		t.Errorf("Set = %v, want context.Canceled", err)
	}
}

// Errors name the key, never the password.
func TestErrorsNeverCarryThePassword(t *testing.T) {
	const secret = "do-not-print-me"
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := New().Set(ctx, secrets.Key{Host: "nas.example", Share: "never", User: "user"}, secret)
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Errorf("error = %v", err)
	}
}
