package ui

import (
	"bytes"
	"image"

	"github.com/rwcarlsen/goexif/exif"
)

// normalizeOrientation applies a JPEG's EXIF orientation to the decoded
// pixels. Go's decoder ignores the tag, so phone photos otherwise render
// sideways or upside down.
func normalizeOrientation(img image.Image, format string, raw []byte) image.Image {
	if format != "jpeg" {
		return img
	}
	meta, err := exif.Decode(bytes.NewReader(raw))
	if err != nil {
		return img
	}
	tag, err := meta.Get(exif.Orientation)
	if err != nil {
		return img
	}
	orientation, err := tag.Int(0)
	if err != nil {
		return img
	}
	return applyOrientation(img, orientation)
}

func applyOrientation(img image.Image, orientation int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	switch orientation {
	case 2: // mirror horizontal
		return remap(img, w, h, func(x, y int) (int, int) { return w - 1 - x, y })
	case 3: // rotate 180
		return remap(img, w, h, func(x, y int) (int, int) { return w - 1 - x, h - 1 - y })
	case 4: // mirror vertical
		return remap(img, w, h, func(x, y int) (int, int) { return x, h - 1 - y })
	case 5: // transpose
		return remap(img, h, w, func(x, y int) (int, int) { return y, x })
	case 6: // rotate 90 clockwise
		return remap(img, h, w, func(x, y int) (int, int) { return y, h - 1 - x })
	case 7: // transverse
		return remap(img, h, w, func(x, y int) (int, int) { return w - 1 - y, h - 1 - x })
	case 8: // rotate 90 counter-clockwise
		return remap(img, h, w, func(x, y int) (int, int) { return w - 1 - y, x })
	}
	return img
}

func remap(src image.Image, w, h int, at func(x, y int) (srcX, srcY int)) image.Image {
	bounds := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			sx, sy := at(x, y)
			dst.Set(x, y, src.At(bounds.Min.X+sx, bounds.Min.Y+sy))
		}
	}
	return dst
}
