package ui

import (
	"testing"

	"github.com/justinm35/lazynotion/internal/notion"
)

func searchModel(t *testing.T) Model {
	t.Helper()
	m := New([]Workspace{{Name: "t"}}, 0, nil, "mosaic")
	m.width, m.height = 90, 30
	m.viewer.Width = 60
	m.viewer.Height = 20
	// startup: the default (unfiltered) list arrives
	next, _ := m.Update(pagesMsg{pages: []notion.Page{
		{ID: "p1", Title: "Alpha"}, {ID: "p2", Title: "Beta"}, {ID: "p3", Title: "Gamma"},
	}})
	return next.(Model)
}

func TestOpeningSearchResultRestoresDefaultList(t *testing.T) {
	m := searchModel(t)

	// a search narrows the sidebar
	next, _ := m.Update(pagesMsg{pages: []notion.Page{{ID: "p2", Title: "Beta"}}, query: "beta"})
	m = next.(Model)
	if len(m.sidebar.Items()) != 1 || m.lastQuery != "beta" {
		t.Fatalf("search state wrong: %d items, query %q", len(m.sidebar.Items()), m.lastQuery)
	}
	m.blockCache["p2"] = []notion.BlockNode{para("x", "content")}

	// opening the result leaves search mode
	next, cmd := m.openSelectedPage()
	m = next.(Model)
	if m.lastQuery != "" {
		t.Errorf("lastQuery = %q, want cleared", m.lastQuery)
	}
	if len(m.sidebar.Items()) != 3 {
		t.Errorf("sidebar should restore the default list, has %d items", len(m.sidebar.Items()))
	}
	if it, ok := m.sidebar.SelectedItem().(pageItem); !ok || it.page.ID != "p2" {
		t.Errorf("the opened page should stay highlighted in the restored list")
	}
	if cmd == nil {
		t.Error("a background refresh should still fire")
	}
	if m.selected == nil || m.selected.ID != "p2" {
		t.Errorf("page should be open, selected=%v", m.selected)
	}
}

func TestBackgroundRefreshKeepsSelection(t *testing.T) {
	m := searchModel(t)
	m.sidebar.Select(1) // Beta

	next, _ := m.Update(pagesMsg{pages: []notion.Page{
		{ID: "p0", Title: "New"}, {ID: "p1", Title: "Alpha"},
		{ID: "p2", Title: "Beta"}, {ID: "p3", Title: "Gamma"},
	}})
	m = next.(Model)
	if it, ok := m.sidebar.SelectedItem().(pageItem); !ok || it.page.ID != "p2" {
		t.Errorf("refresh should keep the highlighted page, got %v", m.sidebar.SelectedItem())
	}
}
