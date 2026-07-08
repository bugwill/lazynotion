package ui

import (
	"testing"

	"github.com/jomei/notionapi"

	"github.com/justinm35/lazynotion/internal/notion"
)

func TestEscRestoresCursorPosition(t *testing.T) {
	parentBlocks := []notion.BlockNode{
		para("a", "one"), para("b", "two"), para("c", "three"),
		{Block: &notionapi.ChildPageBlock{
			BasicBlock: notionapi.BasicBlock{ID: "child-page", Type: "child_page"},
		}},
		para("d", "four"),
	}
	childBlocks := []notion.BlockNode{para("x", "inside")}

	m := New([]Workspace{{Name: "test"}}, 0, nil, "mosaic")
	m.width, m.height = 90, 30
	m.viewer.Width = 60
	m.viewer.Height = 20
	m.loading = false
	parent := notion.Page{ID: "parent", Title: "Parent"}
	m.selected = &parent
	m.focus = focusViewer
	m.blockCache["parent"] = parentBlocks
	m.blockCache["child-page"] = childBlocks
	m.rebuildPage(true)

	m.pv.cursor = 3 // the child page block
	next, _ := m.openChildPage()
	m = next.(Model)
	if m.selected.ID != "child-page" {
		t.Fatalf("should be inside child, on %q", m.selected.ID)
	}
	if m.pv.cursor != 0 {
		t.Fatalf("child should open at top, cursor %d", m.pv.cursor)
	}

	next, _ = m.updateKeys(key("esc"))
	m = next.(Model)
	if m.selected.ID != "parent" {
		t.Fatalf("esc should return to parent, on %q", m.selected.ID)
	}
	if m.pv.cursor != 3 {
		t.Errorf("cursor = %d, want 3 (restored)", m.pv.cursor)
	}
	if m.pendingRestore != nil {
		t.Error("restore should be consumed")
	}
}

func TestEscRestoreWaitsForAsyncLoad(t *testing.T) {
	m := New([]Workspace{{Name: "test"}}, 0, nil, "mosaic")
	m.width, m.height = 90, 30
	m.viewer.Width = 60
	m.viewer.Height = 20
	m.loading = false
	child := notion.Page{ID: "child-page", Title: "Child"}
	m.selected = &child
	m.focus = focusViewer
	m.blockCache["child-page"] = []notion.BlockNode{para("x", "inside")}
	m.rebuildPage(true)
	m.history = []historyEntry{{page: notion.Page{ID: "parent", Title: "Parent"}, cursor: 2, offset: 0}}

	// parent NOT in memory: esc starts an async load; restore stays pending
	next, cmd := m.updateKeys(key("esc"))
	m = next.(Model)
	if cmd == nil {
		t.Fatal("uncached parent should trigger a load")
	}
	if m.pendingRestore == nil {
		t.Fatal("restore should stay pending until the page renders")
	}

	// page arrives
	parentBlocks := []notion.BlockNode{para("a", "one"), para("b", "two"), para("c", "three")}
	next, _ = m.Update(pageMsg{pageID: "parent", blocks: parentBlocks, page: notion.Page{ID: "parent"}})
	m = next.(Model)
	if m.pv.cursor != 2 {
		t.Errorf("cursor = %d, want 2 after async restore", m.pv.cursor)
	}
	if m.pendingRestore != nil {
		t.Error("restore should be consumed after render")
	}
}
