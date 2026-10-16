// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/fqazzazee/shuttle/internal/config"
)

// Paths longer than Windows' old limit of 260 characters sync both ways,
// and keep their versions and conflict copies, as short ones do.
func TestLongPaths(t *testing.T) {
	deep := strings.Repeat("a-folder-with-a-forty-character-name-xx/", 8)
	h := newHarness(t, config.ModeSync, config.DeletionsAdditive)
	write(t, h.localDir, deep+"from-here.txt", "here", t0)
	write(t, h.remoteDir, deep+"from-there.txt", "there", t0)
	if res := h.run(); res.Copied != 2 {
		t.Fatalf("copied %d, want both", res.Copied)
	}
	for _, dir := range []string{h.localDir, h.remoteDir} {
		for p, want := range map[string]string{"from-here.txt": "here", "from-there.txt": "there"} {
			if got, _ := read(t, dir, deep+p); got != want {
				t.Errorf("%s in %s = %q, want %q", p, dir, got, want)
			}
		}
	}

	// Edited here: copied there, the old one kept as a version. Edited on
	// both sides: a conflict, and a copy of the losing side.
	write(t, h.localDir, deep+"from-here.txt", "here, edited", t1)
	write(t, h.localDir, deep+"from-there.txt", "there, edited here", t1)
	write(t, h.remoteDir, deep+"from-there.txt", "there, edited there", t1.Add(time.Hour))
	res := h.run()
	if got, _ := read(t, h.remoteDir, deep+"from-here.txt"); got != "here, edited" {
		t.Errorf("from-here.txt there = %q", got)
	}
	if res.Versioned == 0 || len(res.Conflicts) != 1 {
		t.Errorf("versioned %d, conflicts %+v; want a version and one conflict", res.Versioned, res.Conflicts)
	}
	t.Logf("%d characters below the folders", len(deep)+len("from-there.txt"))
}
