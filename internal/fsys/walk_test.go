// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package fsys_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/fqazzazee/shuttle/internal/fsys"
	"github.com/fqazzazee/shuttle/internal/fsys/fsystest"
)

// A directory that cannot be listed must fail the walk, not be skipped:
// treating a partial scan as complete would turn unreadable files into
// deletions on the other side.
func TestWalkFailsOnUnreadableDir(t *testing.T) {
	dir := t.TempDir()
	locked := filepath.Join(dir, "locked")
	if err := os.MkdirAll(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(locked, "f"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	l, err := fsys.OpenLocal(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	fsystest.Unreadable(t, locked)

	err = fsys.Walk(context.Background(), l, ".", func(fsys.FileInfo) error { return nil })
	if !errors.Is(err, fs.ErrPermission) {
		t.Errorf("error = %v, want fs.ErrPermission", err)
	}
}
