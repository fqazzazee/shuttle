// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package fsys_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"

	"github.com/fqazzazee/shuttle/internal/fsys"
)

// A file another program holds open sharing nothing (an Outlook .pst, a
// virtual machine's disk) is still listed: it is copied once it is let go.
func TestWalkListsAFileHeldExclusively(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "held")
	if err := os.WriteFile(p, []byte("busy"), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateFile(windows.StringToUTF16Ptr(p), windows.GENERIC_READ, 0, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(h)
	l, err := fsys.OpenLocal(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	var got []fsys.FileInfo
	if err := fsys.Walk(context.Background(), l, ".", func(fi fsys.FileInfo) error { got = append(got, fi); return nil }); err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(got) != 1 || got[0].Name != "held" || got[0].Size != 4 {
		t.Errorf("walked %+v, want held, 4 bytes", got)
	}
}
