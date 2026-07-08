package ui

import (
	"image"
	"image/color"
	"strings"
	"testing"

	"github.com/jomei/notionapi"

	"github.com/justinm35/lazynotion/internal/convert"
	"github.com/justinm35/lazynotion/internal/notion"
)

func testImage(w, h int, c color.RGBA) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

func TestAnsiMosaic(t *testing.T) {
	lines := ansiMosaic(testImage(8, 8, color.RGBA{R: 255, A: 255}), 8, 10, true)

	if len(lines) != 4 {
		t.Fatalf("8px tall image should be 4 cell rows, got %d", len(lines))
	}
	if !strings.Contains(lines[0], "▀") {
		t.Error("mosaic missing half-block glyph")
	}
	if !strings.Contains(lines[0], "38;2;255;0;0") {
		t.Errorf("mosaic missing red foreground: %q", lines[0])
	}
	if !strings.HasSuffix(lines[0], "\x1b[0m") {
		t.Error("mosaic line missing reset")
	}
}

func TestAnsiMosaicRespectsBounds(t *testing.T) {
	lines := ansiMosaic(testImage(500, 400, color.RGBA{B: 255, A: 255}), 40, 10, true)
	if len(lines) > 10 {
		t.Errorf("mosaic exceeds max height: %d lines", len(lines))
	}
	cells := strings.Count(lines[0], "▀")
	if cells > 40 {
		t.Errorf("mosaic exceeds max width: %d cells", cells)
	}
}

func TestPageViewRendersDownloadedImage(t *testing.T) {
	block := &notionapi.ImageBlock{
		BasicBlock: notionapi.BasicBlock{ID: "img-1", Type: "image"},
		Image: notionapi.Image{
			External: &notionapi.FileObject{URL: "https://img.example/x.png"},
			Caption:  []notionapi.RichText{{PlainText: "the caption"}},
		},
	}
	units := convert.Flatten([]notion.BlockNode{{Block: block}})

	var pv pageView
	pv.setUnits("p", units, 60, nil)
	if !strings.Contains(strings.Join(pv.rendered[0], "\n"), "img.example") {
		t.Error("undownloaded image should render as link placeholder")
	}

	images := map[string]image.Image{"img-1": testImage(10, 4, color.RGBA{G: 255, A: 255})}
	pv.setUnits("p", units, 60, images)
	joined := strings.Join(pv.rendered[0], "\n")
	if !strings.Contains(joined, "▀") {
		t.Error("downloaded image should render as mosaic")
	}
	if !strings.Contains(joined, "the caption") {
		t.Error("mosaic missing caption line")
	}
}
