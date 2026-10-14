// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

// Package secrets is how share passwords are kept: a Store, keyed by the
// server a password opens. Never in the config file, logs or errors, and
// never in plaintext as a fallback. Where they are kept is the platform's:
// the desktop's Secret Service (package secretservice), the Android
// Keystore.
//
// It imports nothing desktop-specific, so the daemon and the Android app
// share it (SPEC.md, portability rule).
package secrets

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ErrNotFound means no password is stored for the key.
var ErrNotFound = errors.New("no password stored")

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

// Check refuses a key that cannot name a password: one with no host, share
// or user.
func (k Key) Check() error {
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
