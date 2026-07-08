package ui

import (
	"image"
	"image/color"
	"strings"
	"sync"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/jomei/notionapi"

	"github.com/justinm35/lazynotion/internal/notion"
)

func TestDiacriticsTableComplete(t *testing.T) {
	if len(rowColDiacritics) != 297 {
		t.Fatalf("diacritics table has %d entries, canonical file has 297", len(rowColDiacritics))
	}
	if rowColDiacritics[0] != 0x0305 || rowColDiacritics[1] != 0x030D {
		t.Error("table does not start with the canonical entries")
	}
	seen := map[rune]bool{}
	for _, r := range rowColDiacritics {
		if seen[r] {
			t.Fatalf("duplicate diacritic %U", r)
		}
		seen[r] = true
	}
}

func TestPlaceholderLineGeometry(t *testing.T) {
	p := kittyPlacement{id: 42, cols: 10, rows: 3}
	lines := kittyPlaceholderLines(p, 80)
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3", len(lines))
	}
	for i, l := range lines {
		// each placeholder cell must occupy exactly one column, or every
		// layout calculation (gutter, pane border, scrolling) breaks
		if w := lipgloss.Width(l); w != 10 {
			t.Errorf("line %d width = %d, want 10", i, w)
		}
		if !strings.Contains(l, "\x1b[38;5;42m") {
			t.Errorf("line %d missing image-id foreground color", i)
		}
		if strings.Count(l, "\U0010EEEE") != 10 {
			t.Errorf("line %d has %d placeholder runes, want 10", i, strings.Count(l, "\U0010EEEE"))
		}
	}

	// narrow pane crops columns instead of overflowing
	cropped := kittyPlaceholderLines(p, 4)
	if w := lipgloss.Width(cropped[0]); w != 4 {
		t.Errorf("cropped width = %d, want 4", w)
	}
}

func TestImageCellDims(t *testing.T) {
	// 640x480 capped at 22 rows (44 half-block pixels tall) shrinks the
	// width to preserve aspect: 640/480*44 ≈ 58 columns
	cols, rows := imageCellDims(640, 480, 72, 22)
	if rows != 22 {
		t.Errorf("rows = %d, want 22 (height-capped)", rows)
	}
	if cols < 56 || cols > 60 {
		t.Errorf("cols = %d, want ≈58 (aspect-preserving)", cols)
	}
	cols, rows = imageCellDims(100, 2000, 72, 22)
	if rows != 22 {
		t.Errorf("tall image rows = %d, want capped 22", rows)
	}
	if cols >= 100 {
		t.Errorf("tall image cols = %d, should shrink to preserve aspect", cols)
	}
	if c, r := imageCellDims(0, 0, 72, 22); c != 0 || r != 0 {
		t.Errorf("degenerate image dims = %d,%d", c, r)
	}
}

func TestSupportsKittyGraphics(t *testing.T) {
	for _, env := range []string{"TERM", "TMUX", "KITTY_WINDOW_ID", "GHOSTTY_RESOURCES_DIR"} {
		t.Setenv(env, "")
	}
	if supportsKittyGraphics() {
		t.Error("bare env should not detect kitty graphics")
	}
	t.Setenv("TERM", "xterm-ghostty")
	if !supportsKittyGraphics() {
		t.Error("ghostty TERM should detect")
	}

	// inside tmux, support depends on the live allow-passthrough option;
	// detection must see the emulator through tmux's rewritten TERM
	t.Setenv("TERM", "tmux-256color")
	t.Setenv("TMUX", "/tmp/tmux-1000/default,123,0")
	t.Setenv("GHOSTTY_RESOURCES_DIR", "/Applications/Ghostty.app/x")
	old := tmuxAllowsPassthrough
	defer func() { tmuxAllowsPassthrough = old }()

	tmuxAllowsPassthrough = func() bool { return false }
	tmuxPassOnce = sync.Once{}
	if supportsKittyGraphics() {
		t.Error("tmux without passthrough should fall back to the mosaic")
	}
	if kittySupportHint() == "" {
		t.Error("capable terminal behind closed tmux should produce a hint")
	}

	tmuxAllowsPassthrough = func() bool { return true }
	tmuxPassOnce = sync.Once{}
	if !supportsKittyGraphics() {
		t.Error("tmux with passthrough enabled should support sharp images")
	}
}

func TestWrapForTmux(t *testing.T) {
	t.Setenv("TMUX", "")
	if got := wrapForTmux("\x1b_Gtest\x1b\\"); got != "\x1b_Gtest\x1b\\" {
		t.Errorf("outside tmux the sequence must pass through unchanged")
	}
	t.Setenv("TMUX", "/tmp/tmux/default,1,0")
	got := wrapForTmux("\x1b_Gi=1;AAAA\x1b\\")
	want := "\x1bPtmux;\x1b\x1b_Gi=1;AAAA\x1b\x1b\\\x1b\\"
	if got != want {
		t.Errorf("wrapped = %q, want %q (ESC bytes doubled inside the envelope)", got, want)
	}
}

func TestKittyEnabledModes(t *testing.T) {
	for _, env := range []string{"TERM", "TMUX", "KITTY_WINDOW_ID", "GHOSTTY_RESOURCES_DIR"} {
		t.Setenv(env, "")
	}
	m := New([]Workspace{{Name: "test"}}, 0, nil, "pixels")
	if !m.kittyEnabled() {
		t.Error("pixels mode should force kitty graphics")
	}
	t.Setenv("TERM", "xterm-kitty")
	m = New([]Workspace{{Name: "test"}}, 0, nil, "mosaic")
	if m.kittyEnabled() {
		t.Error("mosaic mode should win over detection")
	}
	m = New([]Workspace{{Name: "test"}}, 0, nil, "auto")
	if !m.kittyEnabled() {
		t.Error("auto should detect kitty TERM")
	}
}

func TestPageViewPrefersKittyPlacement(t *testing.T) {
	m := commitTestModel(t, nil)
	m.blockCache["page-1"] = []notion.BlockNode{{Block: &notionapi.ImageBlock{
		BasicBlock: notionapi.BasicBlock{ID: "img-1", Type: "image"},
		Image: notionapi.Image{
			External: &notionapi.FileObject{URL: "https://img.example/x.png"},
		},
	}}}
	m.images["img-1"] = image.NewRGBA(image.Rect(0, 0, 8, 8))
	m.kittyImgs["img-1"] = kittyPlacement{id: 7, cols: 8, rows: 4}
	m.rebuildPage(true)

	joined := strings.Join(m.pv.rendered[0], "\n")
	if !strings.Contains(joined, "\U0010EEEE") {
		t.Error("transmitted image should render placeholders, not mosaic")
	}
	if strings.Contains(joined, "▀") {
		t.Error("mosaic should not render once a placement exists")
	}
}

var _ = color.RGBA{} // keep image/color import if assertions change

func TestKittyCellDimsNeverUpscales(t *testing.T) {
	// 10px cell width, 20px cell height (typical retina-ish metrics)
	cols, rows := kittyCellDimsWith(300, 200, 72, 22, 10, 20)
	if cols > 30 {
		t.Errorf("300px-wide image in 10px cells: cols = %d, must not exceed 30 (native size)", cols)
	}
	if cols < 28 {
		t.Errorf("cols = %d, want ≈30", cols)
	}
	wantRows := 200 / 20
	if rows < wantRows || rows > wantRows+1 {
		t.Errorf("rows = %d, want ≈%d (exact aspect)", rows, wantRows)
	}

	// large image still capped by maxCols and height
	cols, rows = kittyCellDimsWith(4000, 3000, 72, 22, 10, 20)
	if rows != 22 {
		t.Errorf("large image rows = %d, want capped 22", rows)
	}
	if cols > 72 {
		t.Errorf("large image cols = %d, exceeds cap", cols)
	}
	// aspect: 22 rows * 20px = 440px tall → width ≈ 440*4/3 ≈ 587px ≈ 59 cols
	if cols < 56 || cols > 61 {
		t.Errorf("large image cols = %d, want ≈59 (aspect against real cells)", cols)
	}
}

func TestTruecolorIDForLargeIDs(t *testing.T) {
	lines := kittyPlaceholderLines(kittyPlacement{id: 0x010203, cols: 2, rows: 1}, 10)
	if !strings.Contains(lines[0], "\x1b[38;2;1;2;3m") {
		t.Errorf("large id should use truecolor encoding: %q", lines[0])
	}
}
