// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

// Package secrets keeps share passwords in the desktop's Secret Service
// (GNOME Keyring, KWallet), and nowhere else: not in the config file, not in
// logs, not in errors. If the Secret Service is missing or the keyring stays
// locked, that is an error, never a reason to fall back to plaintext.
//
// This package is desktop-specific. The sync core (engine, config, fsys,
// state, ignore) must not import it; see the portability rule in SPEC.md.
package secrets

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/godbus/dbus/v5"
	"github.com/zalando/go-keyring"
)

// service is the Secret Service "service" attribute of every stored password.
const service = "shuttle"

var (
	// ErrNotFound means no password is stored for the key.
	ErrNotFound = errors.New("no password stored")
	// ErrUnavailable means there is no Secret Service to ask.
	ErrUnavailable = errors.New("no Secret Service on the session bus; is GNOME Keyring (or KWallet) running?")
	// ErrLocked means the keyring is locked and was not unlocked.
	ErrLocked = errors.New("the keyring is locked; unlock it (log in again, or open Passwords and Keys)")
)

// secretsName is the D-Bus name the Secret Service owns.
const secretsName = "org.freedesktop.secrets"

// Key identifies a stored password by the server it opens, not by a share's
// name in the config, so renaming a share in the config keeps its password.
type Key struct {
	Host   string
	Port   int // 0 for the default
	Share  string
	User   string
	Domain string // optional
}

// Account is how the key appears in the keyring, e.g.
// "smb://WORKGROUP;fadi@nas.example:1445/home": readable in Seahorse.
func (k Key) Account() string {
	var b strings.Builder
	b.WriteString("smb://")
	if k.Domain != "" {
		b.WriteString(k.Domain + ";")
	}
	b.WriteString(k.User + "@" + k.Host)
	if k.Port != 0 {
		b.WriteString(":" + strconv.Itoa(k.Port))
	}
	b.WriteString("/" + k.Share)
	return b.String()
}

func (k Key) check() error {
	if k.Host == "" || k.Share == "" || k.User == "" {
		return fmt.Errorf("secrets: key %q needs a host, share and user", k.Account())
	}
	return nil
}

// Store keeps passwords.
type Store interface {
	Get(ctx context.Context, k Key) (string, error)
	Set(ctx context.Context, k Key, password string) error
	Delete(ctx context.Context, k Key) error
}

// SecretService is the Store backed by the Secret Service on the session
// bus. A locked keyring makes the desktop ask the user to unlock it; the
// call waits for that, or for ctx.
func SecretService() Store { return secretService{} }

type secretService struct{}

func (secretService) Get(ctx context.Context, k Key) (string, error) {
	if err := k.check(); err != nil {
		return "", err
	}
	return call(ctx, k, "reading", func() (string, error) { return keyring.Get(service, k.Account()) })
}

func (secretService) Set(ctx context.Context, k Key, password string) error {
	if err := k.check(); err != nil {
		return err
	}
	if password == "" {
		return fmt.Errorf("secrets: refusing to store an empty password for %s", k.Account())
	}
	_, err := call(ctx, k, "storing", func() (string, error) { return "", keyring.Set(service, k.Account(), password) })
	return err
}

func (secretService) Delete(ctx context.Context, k Key) error {
	if err := k.check(); err != nil {
		return err
	}
	_, err := call(ctx, k, "removing", func() (string, error) { return "", keyring.Delete(service, k.Account()) })
	return err
}

// call runs a keyring operation, which takes no context, so that a cancelled
// ctx stops the wait (the D-Bus call itself finishes in the background), and
// gives its errors a shape that never includes the password.
func call(ctx context.Context, k Key, doing string, op func() (string, error)) (string, error) {
	type result struct {
		v   string
		err error
	}
	done := make(chan result, 1)
	go func() {
		// go-keyring reports "not found" when it cannot reach a bus at all, so
		// check first that a Secret Service is there to ask: "no password
		// stored" must only ever mean exactly that.
		if err := available(); err != nil {
			done <- result{"", err}
			return
		}
		v, err := op()
		done <- result{v, err}
	}()
	select {
	case <-ctx.Done():
		return "", fmt.Errorf("secrets: %s the password for %s: %w", doing, k.Account(), ctx.Err())
	case r := <-done:
		switch {
		case r.err == nil:
			return r.v, nil
		case errors.Is(r.err, ErrUnavailable):
			return "", fmt.Errorf("secrets: %s the password for %s: %w", doing, k.Account(), r.err)
		case errors.Is(r.err, keyring.ErrNotFound):
			return "", fmt.Errorf("secrets: %s: %w", k.Account(), ErrNotFound)
		case strings.Contains(r.err.Error(), "failed to unlock"):
			return "", fmt.Errorf("secrets: %s the password for %s: %w", doing, k.Account(), ErrLocked)
		}
		return "", fmt.Errorf("secrets: %s the password for %s: %w", doing, k.Account(), r.err)
	}
}

// available checks that the session bus is reachable and that something
// owns, or can be started for, the Secret Service name.
func available() error {
	conn, err := dbus.SessionBus()
	if err != nil {
		return fmt.Errorf("%w (%w)", ErrUnavailable, err)
	}
	bus := conn.BusObject()
	var owned bool
	if err := bus.Call("org.freedesktop.DBus.NameHasOwner", 0, secretsName).Store(&owned); err != nil {
		return fmt.Errorf("%w (%w)", ErrUnavailable, err)
	}
	if owned {
		return nil
	}
	var activatable []string
	if err := bus.Call("org.freedesktop.DBus.ListActivatableNames", 0).Store(&activatable); err != nil {
		return fmt.Errorf("%w (%w)", ErrUnavailable, err)
	}
	if slices.Contains(activatable, secretsName) {
		return nil
	}
	return ErrUnavailable
}
