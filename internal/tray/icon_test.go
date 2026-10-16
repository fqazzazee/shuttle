// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package tray

import (
	"bytes"
	"encoding/binary"
	"image/color"
	"image/png"
	"testing"
)

// Each look is an .ico holding the three sizes as PNGs, where its header says.
func TestIconFor(t *testing.T) {
	for _, l := range []look{lookIdle, lookSyncing, lookPaused, lookError, lookStopped} {
		b, err := iconFor(l)
		if err != nil {
			t.Fatal(err)
		}
		var head [3]uint16
		if err := binary.Read(bytes.NewReader(b), binary.LittleEndian, &head); err != nil || head != [3]uint16{0, 1, 3} {
			t.Fatalf("look %d: header %v, %v", l, head, err)
		}
		for i, want := range sizes {
			e := b[6+16*i:]
			size, offset := binary.LittleEndian.Uint32(e[8:]), binary.LittleEndian.Uint32(e[12:])
			img, err := png.Decode(bytes.NewReader(b[offset : offset+size]))
			if err != nil || int(e[0]) != want || img.Bounds().Dx() != want {
				t.Errorf("look %d, entry %d: %d px, %v; want %d", l, i, e[0], err, want)
			}
		}
	}
}

// The looks differ where they should: the badge's corner, the whole icon
// greyed when stopped.
func TestLooks(t *testing.T) {
	f, err := icons.Open("icons/shuttle-64.png")
	if err != nil {
		t.Fatal(err)
	}
	base, err := png.Decode(f)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	corner := func(l look) color.NRGBA {
		return color.NRGBAModel.Convert(drawLook(base, l).At(58, 50)).(color.NRGBA)
	}
	if c := corner(lookSyncing); c != green {
		t.Errorf("syncing's corner = %v, want green", c)
	}
	if c := corner(lookError); c != red && c != white {
		t.Errorf("error's corner = %v, want red", c)
	}
	if idle, paused := corner(lookIdle), corner(lookPaused); idle == paused {
		t.Errorf("paused looks as idle at the corner: %v", idle)
	}
	stopped := drawLook(base, lookStopped)
	for _, p := range [][2]int{{32, 20}, {20, 40}} {
		c := color.NRGBAModel.Convert(stopped.At(p[0], p[1])).(color.NRGBA)
		if c.R != c.G || c.G != c.B {
			t.Errorf("stopped at %v = %v, want grey", p, c)
		}
	}
}
