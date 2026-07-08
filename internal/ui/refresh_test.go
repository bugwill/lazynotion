package ui

import (
	"testing"

	"github.com/jomei/notionapi"

	"github.com/justinm35/lazynotion/internal/notion"
)

func TestViewerRefreshTargetsCurrentPageOnly(t *testing.T) {
	m := New([]Workspace{{Name: "test"}}, 0, nil, "mosaic")
	m.width, m.height = 90, 30
	m.viewer.Width = 60
	m.viewer.Height = 20
	page := notion.Page{ID: "page-1", Title: "p"}
	m.selected = &page
	m.focus = focusViewer
	m.loading = false
	m.blockCache["page-1"] = []notion.BlockNode{{Block: &notionapi.ParagraphBlock{
		BasicBlock: notionapi.BasicBlock{ID: "b1", Type: "paragraph"},
	}}}
	m.blockCache["other-page"] = []notion.BlockNode{}
	m.rebuildPage(true)

	next, cmd := m.updateKeys(key("r"))
	m = next.(Model)
	if cmd == nil {
		t.Fatal("refresh should produce a load command")
	}
	if !m.pageLoading {
		t.Error("page should be reloading")
	}
	if m.loading {
		t.Error("viewer refresh should not reload the sidebar")
	}
	if _, present := m.blockCache["page-1"]; present {
		t.Error("current page cache should be dropped")
	}
	if _, present := m.blockCache["other-page"]; !present {
		t.Error("other pages' cache should survive a single-page refresh")
	}
}

func TestSidebarRefreshReloadsEverything(t *testing.T) {
	m := New([]Workspace{{Name: "test"}}, 0, nil, "mosaic")
	m.width, m.height = 90, 30
	m.focus = focusSidebar
	m.blockCache["other-page"] = []notion.BlockNode{}

	next, cmd := m.updateKeys(key("r"))
	m = next.(Model)
	if cmd == nil {
		t.Fatal("refresh should produce a load command")
	}
	if !m.loading {
		t.Error("sidebar refresh should reload the page list")
	}
	if len(m.blockCache) != 0 {
		t.Error("sidebar refresh should clear the page cache")
	}
}
