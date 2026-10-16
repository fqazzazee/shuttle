// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package fsys

import (
	"fmt"
	"strings"
)

// windowsDevices are the names Windows keeps for devices, with or without an
// extension. Windows 11 lets most of them be files again, but Windows 10 does
// not, and programs on either may not open them.
var windowsDevices = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true, "CONIN$": true, "CONOUT$": true,
	"COM¹": true, "COM²": true, "COM³": true, "LPT¹": true, "LPT²": true, "LPT³": true,
}

func init() {
	for i := range 10 {
		windowsDevices[fmt.Sprint("COM", i)] = true
		windowsDevices[fmt.Sprint("LPT", i)] = true
	}
}

// refuseWindows refuses what Windows cannot hold as a name, or holds as
// another, as measured on NTFS (Windows 11, 2026-10-16): the characters it
// refuses (":" would name a stream of the file before it), a trailing dot or
// space (dropped from the name), and devices.
func refuseWindows(elem string) error {
	if elem == "" || elem == "." {
		return nil // the folder itself
	}
	for _, r := range elem {
		if r < 0x20 || strings.ContainsRune(`"*:<>?\|`, r) {
			return fmt.Errorf("%w: it contains %q", ErrRefusedName, r)
		}
	}
	if last := elem[len(elem)-1:]; last == "." || last == " " {
		return fmt.Errorf("%w: it ends in %q", ErrRefusedName, last)
	}
	base, _, _ := strings.Cut(elem, ".")
	if windowsDevices[strings.ToUpper(strings.TrimRight(base, " "))] {
		return fmt.Errorf("%w: %s is a device on Windows", ErrRefusedName, base)
	}
	return nil
}
