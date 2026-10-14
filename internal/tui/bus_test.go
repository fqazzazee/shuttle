// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

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

// The bus in color is the bus: the CLI prints present.Bus, and the two
// must not drift apart.
func TestBigBusIsTheBus(t *testing.T) {
	got := bigBus()
	if len(got) != len(present.Bus) {
		t.Fatalf("%d rows, want %d", len(got), len(present.Bus))
	}
	for i, l := range got {
		if s := ansi.Strip(l); s != present.Bus[i] {
			t.Errorf("row %d %q, want %q", i, s, present.Bus[i])
		}
	}
}

// The first screen shows the bus in its colors: the sign, the windscreen,
// the lights.
func TestNoShuttlesYetInColor(t *testing.T) {
	f := newFake()
	f.shuttles = nil
	s := start(t, f, 100, 30).View().Content
	for _, want := range []string{
		signStyle.Render("[ ⇄  ]"),
		windowStyle.Render("      "),
		lightStyle.Render("o") + "  " + faint.Render("==") + "  " + lightStyle.Render("o"),
	} {
		if !strings.Contains(s, want) {
			t.Errorf("no %q on the screen", want)
		}
	}
}
