// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package credman

import (
	"context"
	"errors"
	"fmt"

	"github.com/zalando/go-keyring"

	"github.com/fqazzazee/shuttle/internal/secrets"
)

// service is the first part of every stored credential's name, as it is the
// Secret Service "service" attribute on Linux.
const service = "shuttle"

// New is the Store backed by this user's Credential Manager.
func New() secrets.Store { return credMan{} }

type credMan struct{}

func (credMan) Get(ctx context.Context, k secrets.Key) (string, error) {
	if err := k.Check(); err != nil {
		return "", err
	}
	return call(ctx, k, "reading", func() (string, error) { return keyring.Get(service, k.Account()) })
}

func (credMan) Set(ctx context.Context, k secrets.Key, password string) error {
	if err := k.Check(); err != nil {
		return err
	}
	if password == "" {
		return fmt.Errorf("secrets: refusing to store an empty password for %s", k.Account())
	}
	_, err := call(ctx, k, "storing", func() (string, error) { return "", keyring.Set(service, k.Account(), password) })
	return err
}

func (credMan) Delete(ctx context.Context, k secrets.Key) error {
	if err := k.Check(); err != nil {
		return err
	}
	_, err := call(ctx, k, "removing", func() (string, error) { return "", keyring.Delete(service, k.Account()) })
	return err
}

// call runs a Credential Manager operation, which takes no context, so that
// a cancelled ctx stops the wait, and gives its errors a shape that never
// includes the password.
func call(ctx context.Context, k secrets.Key, doing string, op func() (string, error)) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("secrets: %s the password for %s: %w", doing, k.Account(), err)
	}
	type result struct {
		v   string
		err error
	}
	done := make(chan result, 1)
	go func() {
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
		case errors.Is(r.err, keyring.ErrNotFound):
			return "", fmt.Errorf("secrets: %s: %w", k.Account(), secrets.ErrNotFound)
		}
		return "", fmt.Errorf("secrets: %s the password for %s: %w", doing, k.Account(), r.err)
	}
}
