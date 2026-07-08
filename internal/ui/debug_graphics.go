package ui

import (
	"fmt"
	"image"
	"image/color"
	"os"
)

// DebugGraphics exercises the sharp-image pipeline outside the TUI: prints
// the detection inputs, transmits a test pattern through the exact same
// code path the app uses, and renders both the placeholder block and the
// mosaic so the two are visually comparable in place.
func DebugGraphics() {
	fmt.Println("── environment ──")
	for _, env := range []string{"TERM", "TERM_PROGRAM", "TMUX", "KITTY_WINDOW_ID", "GHOSTTY_RESOURCES_DIR", "COLORTERM"} {
		fmt.Printf("  %-24s %q\n", env, os.Getenv(env))
	}
	if inTmux() {
		fmt.Printf("  %-24s %v\n", "tmux passthrough", tmuxPassthroughEnabled())
	}
	fmt.Printf("  %-24s %v\n", "underlying terminal ok", underlyingKittyTerminal())
	fmt.Printf("  %-24s %v\n", "kitty graphics detected", supportsKittyGraphics())
	if hint := kittySupportHint(); hint != "" {
		fmt.Printf("  %-24s %s\n", "action needed", hint)
	}
	cw, ch, ok := cellPixelSize()
	fmt.Printf("  %-24s %dx%d px (reported: %v)\n", "cell size", cw, ch, ok)

	img := testPattern(320, 160)
	bounds := img.Bounds()
	cols, rows := kittyCellDims(bounds.Dx(), bounds.Dy(), 60, 20)
	placement := kittyPlacement{id: 191, cols: cols, rows: rows}
	fmt.Printf("  %-24s %dx%d px → %d cols × %d rows\n", "test image", bounds.Dx(), bounds.Dy(), cols, rows)

	fmt.Println("\n── kitty graphics protocol ──")
	msg := transmitKittyImage("", "debug", img, placement)()
	if km, isKitty := msg.(kittyMsg); isKitty && km.err != nil {
		fmt.Printf("  TRANSMIT FAILED: %v\n", km.err)
	} else {
		fmt.Println("  transmitted OK — the block below should be a SHARP test")
		fmt.Println("  pattern (thin diagonals, crisp circle). Garbled glyphs or")
		fmt.Println("  emptiness mean the terminal did not honor the placement:")
		fmt.Println()
		for _, line := range kittyPlaceholderLines(placement, 999) {
			fmt.Println("  " + line)
		}
	}

	fmt.Println("\n── mosaic fallback (for comparison) ──")
	initTheme()
	for _, line := range ansiMosaic(img, 60, 20, true) {
		fmt.Println("  " + line)
	}
	fmt.Println("\nIf both blocks look equally chunky, the sharp path isn't rendering;")
	fmt.Println("send this whole output back.")
}

// testPattern draws content that clearly separates pixel-perfect rendering
// from cell-resolution rendering: 2px diagonal stripes and a thin circle.
func testPattern(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.RGBA{30, 30, 40, 255}
			if (x+y)%8 < 2 {
				c = color.RGBA{120, 200, 255, 255}
			}
			cx, cy := float64(x-w/2), float64(y-h/2)
			r := cx*cx/2.6 + cy*cy
			radius := float64(h*h) / 6.5
			if r > radius*0.92 && r < radius*1.08 {
				c = color.RGBA{255, 120, 120, 255}
			}
			img.SetRGBA(x, y, c)
		}
	}
	return img
}
