// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package fsys

import (
	"sync"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

// OSNames are the rules of a local folder here: Windows'.
var OSNames = Names{Fold: windowsFold, Refuse: refuseWindows}

// upcase is Windows' uppercase table, from ntdll: the one NTFS compares
// names by, one UTF-16 unit at a time. Measured against NTFS for every
// character with another case (Windows 11): it is "é" to "É" but not "ß" to
// "SS", not "ς" to "σ", and not the case pairs Unicode added after the table
// (Cherokee's small letters, for one). Go's Unicode tables fold those.
var upcase = sync.OnceValue(func() []uint16 {
	proc := windows.NewLazySystemDLL("ntdll.dll").NewProc("RtlUpcaseUnicodeChar")
	t := make([]uint16, 1<<16)
	for c := range t {
		u, _, _ := proc.Call(uintptr(c))
		t[c] = uint16(u) //nolint:gosec // a WCHAR, returned in a register
	}
	return t
})

// windowsFold is name as NTFS compares it: each UTF-16 unit uppercased by
// the table. A character outside the Basic Multilingual Plane is two units,
// neither of which the table changes, and the two ways of spelling "é" stay
// two names.
func windowsFold(name string) string {
	t := upcase()
	units := utf16.Encode([]rune(name))
	for i, u := range units {
		units[i] = t[u]
	}
	return string(utf16.Decode(units))
}
