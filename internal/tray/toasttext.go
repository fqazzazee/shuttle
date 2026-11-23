// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package tray

import "strings"

// endings are what would end the CDATA section a toast's text sits in, or
// the PowerShell here-string go-toast falls back to, which also closes on a
// typographic double quote.
var endings = strings.NewReplacer(
	"]]>", "]] >",
	"\"@", "\" @",
	"“@", "“ @",
	"”@", "” @",
	"„@", "„ @",
)

// toastText is s, safe to show in a toast. A toast's text holds file names
// from the share, and when Windows refuses a toast go-toast pastes its XML
// into an expandable PowerShell string and runs it, so a $(...) in a name
// would run as the user. Look-alikes keep the name readable.
func toastText(s string) string {
	// Dropping comes first: it can bring the parts of an ending together.
	return endings.Replace(strings.Map(func(r rune) rune {
		switch {
		case r == '$':
			return '＄'
		case r == '`':
			return 'ˋ'
		case !xmlChar(r):
			return -1
		case r < 0x20 && r != '\t' && r != '\n' && r != '\r', r >= 0x7f && r < 0xa0:
			return -1
		}
		return r
	}, s))
}

// xmlChar reports whether XML 1.0 allows r at all. One it does not makes
// Windows refuse the toast, which is what starts the PowerShell fallback.
func xmlChar(r rune) bool {
	return r == '\t' || r == '\n' || r == '\r' ||
		r >= 0x20 && r <= 0xd7ff || r >= 0xe000 && r <= 0xfffd || r >= 0x10000 && r <= 0x10ffff
}

// toastAttr reports whether s can go in a toast's XML attribute as it is.
// An attribute is a path or an argument, which a look-alike would break.
func toastAttr(s string) bool {
	return s == toastText(s) && !strings.ContainsAny(s, "\"&<>\t\r\n")
}
