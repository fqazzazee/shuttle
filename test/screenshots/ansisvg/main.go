// SPDX-License-Identifier: GPL-2.0-or-later

// Command ansisvg draws a terminal screen, as tmux capture-pane -e prints
// it, as an SVG image: for the README's screenshots. Every character has a
// cell of its own, so a glyph a font lacks cannot shift the rest of its
// line, and block elements are drawn as rectangles, which join where
// glyphs leave seams. It reads SGR colors and bold, faint and reverse;
// every character takes one column, as shuttle's do.
//
//	ansisvg -bg '#22252d' -fg '#c9ced8' < screen.ans > screen.svg
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// The size of a cell and of the text in it, in pixels, and the padding
// around the screen.
const (
	cellWidth  = 8.4
	cellHeight = 18
	fontSize   = 14
	baseline   = 13
	padding    = 16
)

// font is the font stack the text asks for: a monospace font the viewer
// has, as none is embedded.
const font = `'JetBrains Mono', 'DejaVu Sans Mono', Menlo, Consolas, monospace`

// cell is a character on the screen and how it is drawn.
type cell struct {
	r           rune
	fg, bg      string // "" for the screen's own
	bold, faint bool
	reverse     bool
}

func main() {
	bg := flag.String("bg", "#22252d", "the screen's background")
	fg := flag.String("fg", "#c9ced8", "the screen's text")
	flag.Parse()
	grid, err := parse(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	w := bufio.NewWriter(os.Stdout)
	render(w, grid, *bg, *fg)
	if err := w.Flush(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// parse reads a screen into rows of cells, following its SGR sequences
// and skipping any other escape sequence.
func parse(r io.Reader) ([][]cell, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	var grid [][]cell
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		var row []cell
		cur := cell{} // a line's style starts afresh, as capture-pane writes it
		rs := []rune(line)
		for i := 0; i < len(rs); i++ {
			if rs[i] != '\x1b' {
				c := cur
				c.r = rs[i]
				row = append(row, c)
				continue
			}
			if i+1 < len(rs) && rs[i+1] == '[' {
				j := i + 2
				for j < len(rs) && (rs[j] < 0x40 || rs[j] > 0x7e) {
					j++
				}
				if j < len(rs) && rs[j] == 'm' {
					sgr(&cur, string(rs[i+2:j]))
				}
				i = j
				continue
			}
			i++ // a two-character escape
		}
		grid = append(grid, row)
	}
	return grid, nil
}

// sgr applies the parameters of an SGR sequence to c.
func sgr(c *cell, params string) {
	if params == "" {
		params = "0"
	}
	ps := strings.FieldsFunc(params, func(r rune) bool { return r == ';' || r == ':' })
	n := func(i int) int {
		if i >= len(ps) {
			return 0
		}
		v, _ := strconv.Atoi(ps[i])
		return v
	}
	for i := 0; i < len(ps); i++ {
		switch p := n(i); {
		case p == 0:
			*c = cell{}
		case p == 1:
			c.bold = true
		case p == 2:
			c.faint = true
		case p == 22:
			c.bold, c.faint = false, false
		case p == 7:
			c.reverse = true
		case p == 27:
			c.reverse = false
		case p >= 30 && p <= 37:
			c.fg = ansi16[p-30]
		case p >= 90 && p <= 97:
			c.fg = ansi16[p-90+8]
		case p == 39:
			c.fg = ""
		case p >= 40 && p <= 47:
			c.bg = ansi16[p-40]
		case p >= 100 && p <= 107:
			c.bg = ansi16[p-100+8]
		case p == 49:
			c.bg = ""
		case p == 38 || p == 48:
			var color string
			switch n(i + 1) {
			case 5:
				color = ansi256(n(i + 2))
				i += 2
			case 2:
				color = fmt.Sprintf("#%02x%02x%02x", n(i+2), n(i+3), n(i+4))
				i += 4
			}
			if p == 38 {
				c.fg = color
			} else {
				c.bg = color
			}
		}
	}
}

// ansi16 are the 16 colors, as a One Dark terminal has them.
var ansi16 = [16]string{
	"#1b1e26", "#e06c75", "#98c379", "#e5c07b", "#61afef", "#c678dd", "#56b6c2", "#abb2bf",
	"#5c6370", "#ef7a83", "#a8d389", "#f5d08b", "#71bfff", "#d688ed", "#66c6d2", "#ffffff",
}

// ansi256 is a color of the 256.
func ansi256(n int) string {
	switch {
	case n < 16:
		return ansi16[max(n, 0)]
	case n < 232:
		n -= 16
		v := [6]int{0, 95, 135, 175, 215, 255}
		return fmt.Sprintf("#%02x%02x%02x", v[n/36], v[n/6%6], v[n%6])
	}
	g := 8 + (min(n, 255)-232)*10
	return fmt.Sprintf("#%02x%02x%02x", g, g, g)
}

// block is the part of its cell a block element fills, as fractions of
// the cell from its left and top, and how opaque; ok is false for any
// other character.
func block(r rune) (x0, y0, x1, y1, opacity float64, ok bool) {
	switch {
	case r == '█':
		return 0, 0, 1, 1, 1, true
	case r == '▀':
		return 0, 0, 1, 0.5, 1, true
	case r == '▐':
		return 0.5, 0, 1, 1, 1, true
	case r == '░':
		return 0, 0, 1, 1, 0.25, true
	case r >= '▁' && r <= '▇': // lower eighths, ▄ among them
		return 0, 1 - float64(r-'▁'+1)/8, 1, 1, 1, true
	case r >= '▉' && r <= '▏': // left eighths, from seven to one, ▌ among them
		return 0, 0, float64('▏'-r+1) / 8, 1, 1, true
	}
	return 0, 0, 0, 0, 0, false
}

// stroke is a line of a box-drawing character: a path in its cell, in
// fractions of the cell, and whether it is heavy.
type stroke struct {
	d     string
	heavy bool
}

// lines are the box-drawing characters shuttle draws, as lines across
// their cells, which join where a font's glyphs fall short of a cell's
// height. Corners are round, as ╭ is.
var lines = map[rune][]stroke{
	'─': {{"M0 .5H1", false}},
	'━': {{"M0 .5H1", true}},
	'│': {{"M.5 0V1", false}},
	'═': {{"M0 .35H1M0 .65H1", false}},
	'┥': {{"M.5 0V1", false}, {"M0 .5H.5", true}},
	'┝': {{"M.5 0V1", false}, {"M.5 .5H1", true}},
	'╭': {{"M1 .5Q.5 .5 .5 1", false}},
	'╮': {{"M0 .5Q.5 .5 .5 1", false}},
	'╰': {{"M.5 0Q.5 .5 1 .5", false}},
	'╯': {{"M.5 0Q.5 .5 0 .5", false}},
}

// render writes grid as an SVG image on a rounded panel of bg, its text in
// fg where a cell does not say.
func render(w io.Writer, grid [][]cell, bg, fg string) {
	cols := 0
	for _, row := range grid {
		cols = max(cols, len(row))
	}
	width, height := float64(cols)*cellWidth+2*padding, float64(len(grid))*cellHeight+2*padding
	fmt.Fprintf(w, `<svg xmlns="http://www.w3.org/2000/svg" width="%.0f" height="%d" viewBox="0 0 %.0f %d">`+"\n",
		width, int(height), width, int(height))
	fmt.Fprintf(w, `<rect width="100%%" height="100%%" rx="10" fill="%s"/>`+"\n", bg)
	// Edges crisp, so blocks side by side join without a seam.
	fmt.Fprintf(w, `<g font-family="%s" font-size="%d" xml:space="preserve" shape-rendering="crispEdges">`+"\n", font, fontSize)
	for y, row := range grid {
		top := padding + float64(y*cellHeight)
		// Backgrounds, a rectangle a run of one color.
		for x := 0; x < len(row); {
			_, back := colors(&row[x], bg, fg)
			end := x + 1
			for end < len(row) {
				if _, b := colors(&row[end], bg, fg); b != back {
					break
				}
				end++
			}
			if back != bg {
				fmt.Fprintf(w, `<rect x="%.1f" y="%.0f" width="%.1f" height="%d" fill="%s"/>`+"\n",
					padding+float64(x)*cellWidth, top, float64(end-x)*cellWidth, cellHeight, back)
			}
			x = end
		}
		// Blocks as rectangles; the rest as text, a run of one style, each
		// character at its own cell.
		for x := 0; x < len(row); {
			c := &row[x]
			fore, _ := colors(c, bg, fg)
			if x0, y0, x1, y1, op, ok := block(c.r); ok {
				fmt.Fprintf(w, `<rect x="%.2f" y="%.2f" width="%.2f" height="%.2f" fill="%s"%s/>`+"\n",
					padding+(float64(x)+x0)*cellWidth, top+y0*cellHeight, (x1-x0)*cellWidth, (y1-y0)*cellHeight,
					fore, opacityAttr(op, c.faint))
				x++
				continue
			}
			if strokes, ok := lines[c.r]; ok {
				// A run of one horizontal line goes as one.
				run := 1
				for c.r == '─' || c.r == '━' {
					if x+run >= len(row) || row[x+run] != *c {
						break
					}
					run++
				}
				for _, st := range strokes {
					width := 1.2
					if st.heavy {
						width = 2.6
					}
					// The path in a cell's fractions, scaled to the cell;
					// the stroke's width kept in pixels.
					fmt.Fprintf(w, `<path transform="translate(%.2f %.0f) scale(%.1f %d)" d="%s" fill="none" stroke="%s"`+
						` stroke-width="%.1f" vector-effect="non-scaling-stroke" shape-rendering="geometricPrecision"%s/>`+"\n",
						padding+float64(x)*cellWidth, top, float64(run)*cellWidth, cellHeight, st.d, fore, width, opacityAttr(1, c.faint))
				}
				x += run
				continue
			}
			if c.r == ' ' {
				x++
				continue
			}
			end := x + 1
			for end < len(row) && sameText(&row[end], c, bg, fg) {
				end++
			}
			var xs []string
			var text strings.Builder
			for i := x; i < end; i++ {
				xs = append(xs, strconv.FormatFloat(padding+float64(i)*cellWidth, 'f', 1, 64))
				text.WriteString(escape(row[i].r))
			}
			weight := ""
			if c.bold {
				weight = ` font-weight="bold"`
			}
			fmt.Fprintf(w, `<text x="%s" y="%.0f" fill="%s"%s%s>%s</text>`+"\n",
				strings.Join(xs, " "), top+baseline, fore, weight, opacityAttr(1, c.faint), text.String())
			x = end
		}
	}
	fmt.Fprintln(w, "</g>\n</svg>")
}

// colors is the foreground and background a cell is drawn in.
func colors(c *cell, bg, fg string) (fore, back string) {
	fore, back = fg, bg
	if c.fg != "" {
		fore = c.fg
	}
	if c.bg != "" {
		back = c.bg
	}
	if c.reverse {
		fore, back = back, fore
	}
	return fore, back
}

// sameText says whether d goes on in the run of text c starts: drawn as
// text, in the same style.
func sameText(d, c *cell, bg, fg string) bool {
	if _, _, _, _, _, ok := block(d.r); ok || d.r == ' ' {
		return false
	}
	if _, ok := lines[d.r]; ok {
		return false
	}
	df, _ := colors(d, bg, fg)
	cf, _ := colors(c, bg, fg)
	return df == cf && d.bold == c.bold && d.faint == c.faint
}

func opacityAttr(op float64, faint bool) string {
	if faint {
		op *= 0.6
	}
	if op == 1 {
		return ""
	}
	return fmt.Sprintf(` opacity="%.2f"`, op)
}

func escape(r rune) string {
	switch r {
	case '&':
		return "&amp;"
	case '<':
		return "&lt;"
	case '>':
		return "&gt;"
	}
	return string(r)
}
