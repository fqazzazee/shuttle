// SPDX-License-Identifier: GPL-2.0-or-later

package tui

import (
	"image/color"
	"math"
	"regexp"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
)

// Every color stays readable on the backgrounds terminals commonly have:
// WCAG AA (4.5:1) for text, 4:1 for dim text beside it.
func TestPaletteContrast(t *testing.T) {
	dark := []string{"#000000", "#1d1d20", "#1e1e2e", "#241f31", "#282c34", "#2e3440", "#303446"}
	light := []string{"#ffffff", "#fafafa", "#f6f5f4", "#eff1f5", "#fdf6e3"}
	for _, tc := range []struct {
		name string
		p    *palette
		bgs  []string
	}{
		{"dark", &darkPalette, dark},
		{"light", &lightPalette, light},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := tc.p
			text := map[string]color.Color{
				"amber": p.amber, "sky": p.sky, "coral": p.coral, "mint": p.mint, "teal": p.teal, "orange": p.orange,
			}
			for _, bg := range tc.bgs {
				for name, c := range text {
					if r := contrast(c, lipgloss.Color(bg)); r < 4.5 {
						t.Errorf("%s on %s: %.1f:1, want 4.5", name, bg, r)
					}
				}
				if r := contrast(p.dim, lipgloss.Color(bg)); r < 4 {
					t.Errorf("dim on %s: %.1f:1, want 4", bg, r)
				}
			}
			if r := contrast(p.bright, p.surface); r < 4.5 {
				t.Errorf("the selection: %.1f:1, want 4.5", r)
			}
			if r := contrast(p.ink, p.yellow); r < 4.5 {
				t.Errorf("the chosen tab: %.1f:1, want 4.5", r)
			}
			if r := contrast(p.yellow, p.sign); r < 4.5 {
				t.Errorf("the bus's sign: %.1f:1, want 4.5", r)
			}
			for _, bg := range tc.bgs {
				if r := contrast(p.glass, lipgloss.Color(bg)); r < 3 {
					t.Errorf("the bus's windscreen on %s: %.1f:1, want 3, as for graphics", bg, r)
				}
			}
		})
	}
}

// contrast is the WCAG 2 contrast ratio of two colors.
func contrast(a, b color.Color) float64 {
	la, lb := luminance(a), luminance(b)
	return (max(la, lb) + 0.05) / (min(la, lb) + 0.05)
}

func luminance(c color.Color) float64 {
	r, g, b, _ := c.RGBA()
	lin := func(v uint32) float64 {
		x := float64(v) / 0xffff
		if x <= 0.03928 {
			return x / 12.92
		}
		return math.Pow((x+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(r) + 0.7152*lin(g) + 0.0722*lin(b)
}

// The palette is for terminals with 256 colors or more; the others keep
// their own 16, and are not asked for their background.
func TestThemeFor(t *testing.T) {
	for _, tc := range []struct {
		profile colorprofile.Profile
		dark    bool
		want    *palette
		asks    bool
	}{
		{colorprofile.NoTTY, true, nil, false},
		{colorprofile.ASCII, true, nil, false},
		{colorprofile.ANSI, false, nil, false},
		{colorprofile.ANSI256, true, &darkPalette, true},
		{colorprofile.TrueColor, true, &darkPalette, true},
		{colorprofile.TrueColor, false, &lightPalette, true},
	} {
		asked := false
		got := themeFor(tc.profile, func() bool { asked = true; return tc.dark })
		if got != tc.want || asked != tc.asks {
			t.Errorf("%v, dark %v: %p, asked %v; want %p, asked %v", tc.profile, tc.dark, got, asked, tc.want, tc.asks)
		}
	}
}

// reverse matches an SGR sequence that turns reverse on.
var reverse = regexp.MustCompile(`\x1b\[(\d+;)*7(;\d+)*m`)

// Without colors (NO_COLOR), the selected row, the chosen tab and the
// bus's sign still show: in reverse.
func TestSelectionShowsWithoutColor(t *testing.T) {
	t.Cleanup(func() { useTheme(&darkPalette) })
	useTheme(nil)
	for name, s := range map[string]lipgloss.Style{"selected": selected, "activeTab": activeTab, "signStyle": signStyle} {
		var b strings.Builder
		w := &colorprofile.Writer{Forward: &b, Profile: colorprofile.ASCII}
		if _, err := w.WriteString(s.Render("row")); err != nil {
			t.Fatal(err)
		}
		if !reverse.MatchString(b.String()) {
			t.Errorf("%s without colors: %q, not in reverse", name, b.String())
		}
	}
}
