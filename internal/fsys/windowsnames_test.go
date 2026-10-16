// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package fsys

import (
	"errors"
	"testing"
)

func TestRefuseWindows(t *testing.T) {
	for _, name := range []string{"a:b", "a?b", "a*b", `a"b`, "a<b", "a>b", "a|b", `a\b`, "a\x01b", "a\x1fb",
		"trail.", "trail ", "a.b.", "CON", "con", "CON.txt", "con .txt", "NUL", "nul.tar.gz", "AUX", "PRN",
		"COM0", "COM1", "COM9", "LPT1", "COM¹", "lpt³", "CONIN$", "CONOUT$"} {
		if err := refuseWindows(name); !errors.Is(err, ErrRefusedName) {
			t.Errorf("refuseWindows(%q) = %v, want ErrRefusedName", name, err)
		}
	}
	for _, name := range []string{"", ".", "ok.txt", " lead", ".lead", "a\x7fb", "CONSOLE", "COM10", "COM", "LPT", "conf.d",
		"x.CON", "CLOCK$", "straße"} {
		if err := refuseWindows(name); err != nil {
			t.Errorf("refuseWindows(%q) = %v, want nil", name, err)
		}
	}
}
