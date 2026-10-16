// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package fsys

import (
	"os"
	"path/filepath"
	"testing"
	"unicode"
	"unicode/utf8"
)

// The pairs NTFS was first measured on (Windows 11): the same name or two.
func TestWindowsFold(t *testing.T) {
	for _, tt := range []struct {
		a, b string
		same bool
	}{
		{"readme.md", "README.MD", true},
		{"é", "É", true},
		{"ǆ", "Ǆ", true},
		{"ⓐ", "Ⓐ", true},
		{"ａ", "Ａ", true}, // fullwidth
		{"straße", "STRAßE", true},
		{"straße", "STRASSE", false},
		{"straße", "STRAẞE", false},  // capital sharp s
		{"\u00e9", "e\u0301", false}, // NFC, NFD
		{"σ", "ς", false},
		{"i", "ı", false},
		{"i", "İ", false},
		{"k", "\u212a", false}, // Kelvin sign
		{"ǆ", "ǅ", false},      // title case
		{"ﬀ", "FF", false},
		{"𐐨", "𐐀", false}, // outside the BMP
		{"ꮧ", "Ꮧ", false}, // Cherokee: Unicode 8, after the table
	} {
		if same := windowsFold(tt.a) == windowsFold(tt.b); same != tt.same {
			t.Errorf("%q and %q: same = %v, want %v", tt.a, tt.b, same, tt.same)
		}
	}
}

// windowsFold says what this machine's NTFS says, for every character in the
// Basic Multilingual Plane with another case: whether the file made with it
// opens by its other cases.
func TestWindowsFoldIsNTFSs(t *testing.T) {
	dir := t.TempDir()
	checked := 0
	for r := rune(0x20); r <= 0xffff; r++ {
		if r >= 0xd800 && r <= 0xdfff || refuseWindows(string(r)) != nil {
			continue // surrogate halves; names Windows refuses
		}
		others := map[rune]bool{}
		for _, o := range []rune{unicode.ToUpper(r), unicode.ToLower(r), unicode.ToTitle(r), first(windowsFold(string(r)))} {
			if o != r && o <= 0xffff {
				others[o] = true
			}
		}
		if len(others) == 0 {
			continue
		}
		name := "f" + string(r)
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o600); err != nil {
			t.Fatalf("%U: %v", r, err)
		}
		for o := range others {
			_, err := os.Stat(filepath.Join(dir, "f"+string(o)))
			ntfs := err == nil
			if ours := windowsFold(name) == windowsFold("f"+string(o)); ours != ntfs {
				t.Errorf("%U %q and %U %q: NTFS says same = %v, windowsFold %v", r, r, o, o, ntfs, ours)
			}
		}
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
		checked++
	}
	t.Logf("checked %d characters", checked)
}

// first is the first character of s.
func first(s string) rune {
	r, _ := utf8.DecodeRuneInString(s)
	return r
}
