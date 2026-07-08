package ui

import (
	"fmt"
	"image"
	"image/color"
	"strings"

	"golang.org/x/image/draw"
)

// ansiMosaic renders an image as half-block characters: each cell shows two
// vertical pixels via ▀ with truecolor fore/background. Plain styled text,
// so it scrolls and reflows like any other viewport content.
func ansiMosaic(src image.Image, maxCellsW, maxCellsH int, dark bool) []string {
	bounds := src.Bounds()
	srcW, srcH := bounds.Dx(), bounds.Dy()
	if srcW == 0 || srcH == 0 || maxCellsW < 1 || maxCellsH < 1 {
		return nil
	}

	cellsW := min(maxCellsW, srcW)
	pxH := srcH * cellsW / srcW
	cellsH := max((pxH+1)/2, 1)
	if cellsH > maxCellsH {
		cellsH = maxCellsH
		cellsW = min(cellsW, max(srcW*cellsH*2/srcH, 1))
	}

	dst := image.NewRGBA(image.Rect(0, 0, cellsW, cellsH*2))
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, bounds, draw.Src, nil)

	bg := uint8(28)
	if !dark {
		bg = 245
	}

	lines := make([]string, 0, cellsH)
	for y := 0; y < cellsH; y++ {
		var b strings.Builder
		for x := 0; x < cellsW; x++ {
			tr, tg, tb := flatten(dst.RGBAAt(x, 2*y), bg)
			br, bgr, bb := flatten(dst.RGBAAt(x, 2*y+1), bg)
			fmt.Fprintf(&b, "\x1b[38;2;%d;%d;%dm\x1b[48;2;%d;%d;%dm▀", tr, tg, tb, br, bgr, bb)
		}
		b.WriteString("\x1b[0m")
		lines = append(lines, b.String())
	}
	return lines
}

// flatten composites a premultiplied-alpha pixel over a solid background.
func flatten(c color.RGBA, bg uint8) (uint8, uint8, uint8) {
	inv := uint16(255 - c.A)
	r := uint8(uint16(c.R) + uint16(bg)*inv/255)
	g := uint8(uint16(c.G) + uint16(bg)*inv/255)
	b := uint8(uint16(c.B) + uint16(bg)*inv/255)
	return r, g, b
}
