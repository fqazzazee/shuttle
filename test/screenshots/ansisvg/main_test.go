// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	grid, err := parse(strings.NewReader(
		"\x1b[1;38;2;245;196;81mab\x1b[0m c\x1b[7m\x1b[38;5;196md\x1b[m\x1b[2Ke\n\x1b[48;2;1;2;3mx\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := [][]cell{
		{
			{r: 'a', fg: "#f5c451", bold: true}, {r: 'b', fg: "#f5c451", bold: true},
			{r: ' '}, {r: 'c'}, {r: 'd', fg: "#ff0000", reverse: true}, {r: 'e'}, // the CSI K skipped
		},
		{{r: 'x', bg: "#010203"}},
	}
	if len(grid) != len(want) {
		t.Fatalf("%d rows, want %d", len(grid), len(want))
	}
	for y := range want {
		if len(grid[y]) != len(want[y]) {
			t.Fatalf("row %d: %d cells, want %d: %+v", y, len(grid[y]), len(want[y]), grid[y])
		}
		for x := range want[y] {
			if grid[y][x] != want[y][x] {
				t.Errorf("cell %d,%d = %+v, want %+v", x, y, grid[y][x], want[y][x])
			}
		}
	}
}

func TestBlock(t *testing.T) {
	for _, tc := range []struct {
		r                  rune
		x0, y0, x1, y1, op float64
	}{
		{'█', 0, 0, 1, 1, 1},
		{'▀', 0, 0, 1, 0.5, 1},
		{'▄', 0, 0.5, 1, 1, 1},
		{'▁', 0, 0.875, 1, 1, 1},
		{'▇', 0, 0.125, 1, 1, 1},
		{'▏', 0, 0, 0.125, 1, 1},
		{'▌', 0, 0, 0.5, 1, 1},
		{'▉', 0, 0, 0.875, 1, 1},
		{'▐', 0.5, 0, 1, 1, 1},
		{'░', 0, 0, 1, 1, 0.25},
	} {
		x0, y0, x1, y1, op, ok := block(tc.r)
		if !ok || x0 != tc.x0 || y0 != tc.y0 || x1 != tc.x1 || y1 != tc.y1 || op != tc.op {
			t.Errorf("block(%q) = %v %v %v %v %v %v", tc.r, x0, y0, x1, y1, op, ok)
		}
	}
	for _, r := range "a─│╭━⇄ " {
		if _, _, _, _, _, ok := block(r); ok {
			t.Errorf("%q drawn as a block", r)
		}
	}
}

// Text goes a character a cell, escaped; blocks go as rectangles; a run
// of a background as one.
func TestRender(t *testing.T) {
	grid := [][]cell{{
		{r: '<'}, {r: '&'}, {r: 'b'}, {r: ' '},
		{r: '█', fg: "#61afef"}, {r: 'x', bg: "#3a4252"}, {r: 'y', bg: "#3a4252"},
	}}
	var b strings.Builder
	render(&b, grid, "#000000", "#ffffff")
	s := b.String()
	for _, want := range []string{
		`<text x="16.0 24.4 32.8" y="29" fill="#ffffff">&lt;&amp;b</text>`,
		`<rect x="49.60" y="16.00" width="8.40" height="18.00" fill="#61afef"/>`,
		`<rect x="58.0" y="16" width="16.8" height="18" fill="#3a4252"/>`,
		`<text x="58.0 66.4" y="29" fill="#ffffff">xy</text>`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("no %s in:\n%s", want, s)
		}
	}
	if strings.Contains(s, "█") {
		t.Error("a block drawn as text")
	}
}

// Box-drawing characters go as lines across their cells, which join; a
// heavy one is thicker; a run of a horizontal one, one line.
func TestRenderLines(t *testing.T) {
	var b strings.Builder
	render(&b, [][]cell{{{r: '│'}, {r: '━', fg: "#61afef"}, {r: '━', fg: "#61afef"}, {r: '╭'}}}, "#000000", "#ffffff")
	s := b.String()
	for _, want := range []string{
		`translate(16.00 16) scale(8.4 18)" d="M.5 0V1" fill="none" stroke="#ffffff" stroke-width="1.2"`,
		`translate(24.40 16) scale(16.8 18)" d="M0 .5H1" fill="none" stroke="#61afef" stroke-width="2.6"`, // the run as one
		`d="M1 .5Q.5 .5 .5 1"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("no %s in:\n%s", want, s)
		}
	}
	if strings.ContainsAny(s, "│━╭") {
		t.Error("a box-drawing character drawn as text")
	}
}
