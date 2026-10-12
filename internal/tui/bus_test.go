// SPDX-License-Identifier: GPL-2.0-or-later

package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/fqazzazee/shuttle/internal/present"
)

// The bus lines up only if every character takes one column: a wide one (☰,
// say) pushes the rest of its line out in most terminals.
func TestBusIsOneColumnACharacter(t *testing.T) {
	for _, l := range present.Bus {
		if w, n := ansi.StringWidth(l), len([]rune(l)); w != n {
			t.Errorf("%q takes %d columns for %d characters", l, w, n)
		}
	}
}

// With no shuttles yet, the first screen shows the bus and what to do.
func TestNoShuttlesYet(t *testing.T) {
	f := newFake()
	f.shuttles = nil
	m := start(t, f, 100, 30)
	s := screen(m)
	for _, want := range append(present.Bus, "No shuttles yet.", "Press n to add one.") {
		if !strings.Contains(s, want) {
			t.Errorf("no %q on the screen:\n%s", want, s)
		}
	}
}
