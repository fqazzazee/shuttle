// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

//go:build !windows

package fsystest

import (
	"os"
	"testing"
)

// Unreadable makes the file or folder dir unreadable to this user until the
// test ends: its mode 000, then back to what it was. Root reads it anyway, so the
// test is skipped as root.
func Unreadable(t *testing.T, dir string) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root can read any directory")
	}
	was, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(dir, was.Mode().Perm()); err != nil {
			t.Error(err)
		}
	})
}
