package ui

import (
	"image"
	"image/color"
	"testing"
)

// a 2x1 image: red pixel left, blue pixel right
func twoPixel() image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 2, 1))
	img.SetRGBA(0, 0, color.RGBA{255, 0, 0, 255})
	img.SetRGBA(1, 0, color.RGBA{0, 0, 255, 255})
	return img
}

func redAt(t *testing.T, img image.Image, x, y int) bool {
	t.Helper()
	r, _, b, _ := img.At(x, y).RGBA()
	return r > b
}

func TestApplyOrientation(t *testing.T) {
	src := twoPixel()

	// 1: untouched
	if got := applyOrientation(src, 1); !redAt(t, got, 0, 0) {
		t.Error("orientation 1 must not change pixels")
	}
	// 2: mirror horizontal → blue left
	if got := applyOrientation(src, 2); redAt(t, got, 0, 0) {
		t.Error("orientation 2 should mirror horizontally")
	}
	// 3: rotate 180 → blue left
	if got := applyOrientation(src, 3); redAt(t, got, 0, 0) {
		t.Error("orientation 3 should rotate 180")
	}
	// 6: rotate 90 CW → 1x2, red at bottom... red was left → after CW red is top
	got := applyOrientation(src, 6)
	if got.Bounds().Dx() != 1 || got.Bounds().Dy() != 2 {
		t.Fatalf("orientation 6 should swap dimensions, got %v", got.Bounds())
	}
	if !redAt(t, got, 0, 0) {
		t.Error("orientation 6: left pixel should rotate to the top")
	}
	// 8: rotate 90 CCW → red at bottom
	got = applyOrientation(src, 8)
	if got.Bounds().Dx() != 1 || got.Bounds().Dy() != 2 {
		t.Fatalf("orientation 8 should swap dimensions, got %v", got.Bounds())
	}
	if redAt(t, got, 0, 0) {
		t.Error("orientation 8: left pixel should rotate to the bottom")
	}
}
