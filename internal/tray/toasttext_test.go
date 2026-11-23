// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package tray

import (
	"encoding/xml"
	"regexp"
	"strings"
	"testing"
)

// A line that would close go-toast's PowerShell here-string: the tokenizer
// takes the typographic double quotes for a plain one.
var closesHereString = regexp.MustCompile(`(?m)(^|\r)["\x{201C}\x{201D}\x{201E}]@`)

// A file name comes from the share, and go-toast can hand a toast's text to
// PowerShell inside a CDATA section inside an expandable here-string: nothing
// in it may run, end the section or end the string.
func TestToastText(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"plain text stays", "docs/report 1.txt: copied", "docs/report 1.txt: copied"},
		{"lines stay", "one\ntwo\tthree", "one\ntwo\tthree"},
		{"accents stay", "Résumé/日本語.txt", "Résumé/日本語.txt"},
		{"a subexpression", "$(calc).txt", "＄(calc).txt"},
		{"a variable", "a$env:PATH", "a＄env:PATH"},
		{"a backtick escape", "a`nb", "aˋnb"},
		{"not an XML character", "\uffff$(calc).txt", "＄(calc).txt"},
		{"a control character", "a\x1b]52;c;eA==\x07b", "a]52;c;eA==b"},
		{"a bad UTF-8 byte", "a\xffb", "a\ufffdb"},
		{"the end of CDATA", "a]]>b", "a]] >b"},
		{"the end of CDATA, once a character is dropped", "a]\uffff]>b", "a]] >b"},
		{"the end of the here-string", "a\n\"@\n$(calc)", "a\n\" @\n＄(calc)"},
		{"the end of the here-string, after a return", "a\r\"@", "a\r\" @"},
		{"the end of the here-string, typographic", "a\n\u201d@", "a\n\u201d @"},
		{"the end of the here-string, first line", "\u201c@b", "\u201c @b"},
		{"the end of the here-string, low quote", "a\n\u201e@", "a\n\u201e @"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := toastText(tc.in)
			if got != tc.want {
				t.Errorf("toastText(%q) = %q, want %q", tc.in, got, tc.want)
			}
			if strings.ContainsAny(got, "$`") || closesHereString.MatchString(got) {
				t.Errorf("toastText(%q) = %q can still act in a here-string", tc.in, got)
			}
			var parsed struct {
				Text string `xml:",chardata"`
			}
			if err := xml.Unmarshal([]byte("<text><![CDATA["+got+"]]></text>"), &parsed); err != nil {
				t.Errorf("toastText(%q) = %q does not parse in CDATA: %v", tc.in, got, err)
			}
		})
	}
}

// An icon's path and a button's arguments sit in XML attributes, where
// showing a look-alike would break them: they go in whole or not at all.
func TestToastAttr(t *testing.T) {
	for in, want := range map[string]bool{
		"":                                  true,
		"open:conflicts":                    true,
		`C:\Users\user\AppData\shuttle.png`: true,
		`C:\Users\a$(calc)\shuttle.png`:     false,
		"a`b":                               false,
		`a"b`:                               false,
		"a&b":                               false,
		"a<b":                               false,
		"a>b":                               false,
		"a\nb":                              false,
		"a\rb":                              false,
		"a\uffffb":                          false,
		"a\xffb":                            false,
	} {
		if got := toastAttr(in); got != want {
			t.Errorf("toastAttr(%q) = %v, want %v", in, got, want)
		}
	}
}
