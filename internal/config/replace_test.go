// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package config

import (
	"os"
	"path/filepath"
	"testing"
)

// A backup restored: the file is the backup's, whatever it held; one that
// would not load leaves it as it was.
func TestReplace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := Replace(path, "/home/user", []byte(rowsConfig)); err != nil {
		t.Fatalf("no file yet: %v", err)
	}
	if got, _ := os.ReadFile(path); string(got) != rowsConfig { //nolint:gosec // the test's own file
		t.Errorf("restored:\n%s", got)
	}
	if err := Replace(path, "/home/user", []byte(minimalShare)); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != minimalShare { //nolint:gosec // the test's own file
		t.Errorf("replaced:\n%s", got)
	}
	if err := Replace(path, "/home/user", []byte("[shuttles.x]\nmode = \"sideways\"\n")); err == nil {
		t.Error("a backup that does not load restored")
	}
	if got, _ := os.ReadFile(path); string(got) != minimalShare { //nolint:gosec // the test's own file
		t.Errorf("after a refused one:\n%s", got)
	}
}
