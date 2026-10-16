// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

// Package tray is shuttle's icon in Windows' notification area: what the
// GNOME extension is on Linux. It shows the daemon's state, and from its menu
// syncs, pauses and resumes, and opens the TUI, talking to the daemon over
// its pipe as the CLI does.
//
// This package is desktop-specific; the sync core must not import it.
package tray

import (
	"bytes"
	"embed"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
)

// look is what the icon shows.
type look int

const (
	lookIdle    look = iota // the icon as it is
	lookSyncing             // a green dot
	lookPaused              // an amber dot with two bars
	lookError               // a red dot with an exclamation mark
	lookStopped             // the icon greyed: no daemon to ask
)

//go:embed icons/shuttle-32.png icons/shuttle-48.png icons/shuttle-64.png
var icons embed.FS

var sizes = []int{32, 48, 64}

var (
	green = color.NRGBA{0x2e, 0xc2, 0x7e, 0xff}
	amber = color.NRGBA{0xe5, 0xa5, 0x0a, 0xff}
	red   = color.NRGBA{0xe0, 0x1b, 0x24, 0xff}
	white = color.NRGBA{0xff, 0xff, 0xff, 0xff}
)

// iconFor is the icon for l, as an .ico file: the three sizes, each a PNG.
func iconFor(l look) ([]byte, error) {
	var imgs []image.Image
	for _, n := range sizes {
		f, err := icons.Open(fmt.Sprintf("icons/shuttle-%d.png", n))
		if err != nil {
			return nil, err
		}
		base, err := png.Decode(f)
		f.Close()
		if err != nil {
			return nil, err
		}
		imgs = append(imgs, drawLook(base, l))
	}
	return ico(imgs)
}

// drawLook is base as l shows it.
func drawLook(base image.Image, l look) image.Image {
	b := base.Bounds()
	img := image.NewNRGBA(b)
	draw.Draw(img, b, base, b.Min, draw.Src)
	n := b.Dx()
	switch l {
	case lookStopped:
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				c := img.NRGBAAt(x, y)
				g := uint8((299*uint32(c.R) + 587*uint32(c.G) + 114*uint32(c.B)) / 1000) //nolint:gosec // a weighted mean of bytes is a byte
				img.SetNRGBA(x, y, color.NRGBA{g, g, g, c.A / 2})
			}
		}
	case lookSyncing:
		badge(img, n, green, nil)
	case lookPaused:
		badge(img, n, amber, func(cx, cy, r float64, x, y int) bool {
			dx, dy := float64(x)+0.5-cx, math.Abs(float64(y)+0.5-cy)
			bar := math.Abs(math.Abs(dx)-r*0.32) < r*0.16
			return bar && dy < r*0.5
		})
	case lookError:
		badge(img, n, red, func(cx, cy, r float64, x, y int) bool {
			dx, dy := math.Abs(float64(x)+0.5-cx), float64(y)+0.5-cy
			return dx < r*0.16 && (dy > -r*0.6 && dy < r*0.15 || dy > r*0.35 && dy < r*0.62)
		})
	}
	return img
}

// badge draws a dot of colour c in the icon's bottom right corner, ringed in
// white so it stands off the icon, with mark's pixels white inside it.
func badge(img *image.NRGBA, n int, c color.NRGBA, mark func(cx, cy, r float64, x, y int) bool) {
	r := float64(n) * 0.24
	cx, cy := float64(n)-r-0.5, float64(n)-r-0.5
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			d := math.Hypot(float64(x)+0.5-cx, float64(y)+0.5-cy)
			switch {
			case d <= r && mark != nil && mark(cx, cy, r, x, y):
				img.SetNRGBA(x, y, white)
			case d <= r:
				img.SetNRGBA(x, y, c)
			case d <= r+float64(n)/24:
				img.SetNRGBA(x, y, white)
			}
		}
	}
}

// ico is an .ico file of imgs, each stored as a PNG, as Windows reads them
// since Vista.
func ico(imgs []image.Image) ([]byte, error) {
	var pngs [][]byte
	for _, img := range imgs {
		var b bytes.Buffer
		if err := png.Encode(&b, img); err != nil {
			return nil, err
		}
		pngs = append(pngs, b.Bytes())
	}
	var out bytes.Buffer
	le := binary.LittleEndian
	_ = binary.Write(&out, le, [3]uint16{0, 1, uint16(len(imgs))}) //nolint:gosec // reserved, type icon, count: three
	offset := 6 + 16*len(imgs)
	for i, img := range imgs {
		w, h := img.Bounds().Dx(), img.Bounds().Dy()
		if w > 256 || h > 256 {
			return nil, fmt.Errorf("an icon %dx%d is larger than .ico holds", w, h)
		}
		out.WriteByte(byte(w % 256))                     //nolint:gosec // 0 is 256
		out.WriteByte(byte(h % 256))                     //nolint:gosec // 0 is 256
		out.Write([]byte{0, 0})                          // no palette, reserved
		_ = binary.Write(&out, le, [2]uint16{1, 32})     // planes, bits per pixel
		_ = binary.Write(&out, le, uint32(len(pngs[i]))) //nolint:gosec // a PNG of at most 256x256
		_ = binary.Write(&out, le, uint32(offset))       //nolint:gosec // as small as the PNGs
		offset += len(pngs[i])
	}
	for _, p := range pngs {
		out.Write(p)
	}
	return out.Bytes(), nil
}
