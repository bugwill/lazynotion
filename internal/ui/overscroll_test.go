package ui

import (
	"strings"
	"testing"

	"github.com/justinm35/lazynotion/internal/notion"
)

func TestOverscrollPastLastBlock(t *testing.T) {
	blocks := make([]notion.BlockNode, 0, 30)
	for i := 0; i < 30; i++ {
		id := string(rune('a'+i%26)) + strings.Repeat("x", i/26+1)
		blocks = append(blocks, para(id, "line content"))
	}
	m := New([]Workspace{{Name: "t"}}, 0, nil, "mosaic")
	m.width, m.height = 90, 22
	m.viewer.Width = 60
	m.viewer.Height = 20
	page := notion.Page{ID: "page-1", Title: "p"}
	m.selected = &page
	m.blockCache["page-1"] = blocks
	m.rebuildPage(true)

	total := m.viewer.TotalLineCount()
	realLines := total - m.viewer.Height/2
	if total <= realLines {
		t.Fatalf("content should carry overscroll padding: total=%d", total)
	}

	// scroll as far down as the viewport allows
	m.viewer.GotoBottom()
	lastRealLine := realLines - 1
	screenRow := lastRealLine - m.viewer.YOffset
	// with half-screen padding, the last content line should sit around
	// the middle of the viewport, not pinned to the bottom edge
	if screenRow > m.viewer.Height/2 {
		t.Errorf("last line at screen row %d of %d — overscroll should lift it to ~%d",
			screenRow, m.viewer.Height, m.viewer.Height/2-1)
	}
	if screenRow < 0 {
		t.Errorf("last line scrolled out of view entirely (row %d)", screenRow)
	}
}

func TestEnsureVisibleUnaffectedByPadding(t *testing.T) {
	m := New([]Workspace{{Name: "t"}}, 0, nil, "mosaic")
	m.width, m.height = 90, 22
	m.viewer.Width = 60
	m.viewer.Height = 20
	page := notion.Page{ID: "page-1", Title: "p"}
	m.selected = &page
	m.blockCache["page-1"] = []notion.BlockNode{
		para("a", "one"), para("b", "two"), para("c", "three"),
	}
	m.rebuildPage(true)

	// overscroll, then move the cursor: the view must snap back to it
	m.viewer.GotoBottom()
	m.pv.cursor = 0
	m.syncViewer()
	if m.viewer.YOffset != 0 {
		t.Errorf("cursor at top should pull the view back, YOffset=%d", m.viewer.YOffset)
	}
}
