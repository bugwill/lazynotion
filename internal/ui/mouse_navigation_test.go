package ui

import (
	"fmt"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jomei/notionapi"
	"github.com/justinm35/lazynotion/internal/notion"
)

func tapModel(m Model, y int) Model {
	next, _ := m.Update(tea.MouseMsg{X: 8, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m = next.(Model)
	next, _ = m.Update(tea.MouseMsg{X: 8, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})
	return next.(Model)
}

func TestTapPagesDescriptionAndPagination(t *testing.T) {
	m := New([]Workspace{{Name: "test"}}, 0, nil, "mosaic")
	m.width, m.height = 80, 20
	m.layout()
	var pages []notion.Page
	for i := range 20 {
		pages = append(pages, notion.Page{ID: fmt.Sprintf("p%d", i), Title: fmt.Sprintf("Page %d", i)})
	}
	next, _ := m.Update(pagesMsg{pages: pages})
	m = next.(Model)
	if got := tapModel(m, 3); got.selected != nil {
		t.Fatal("blank list spacer activated a page")
	}
	if got := tapModel(m, m.height-3); got.selected != nil {
		t.Fatal("progress area activated a page")
	}
	got := tapModel(m, 5) // first ordinary page description, below Recent
	if got.selected == nil || got.selected.ID != "p0" {
		t.Fatalf("description tap opened %+v", got.selected)
	}
	m.sidebar.Select(m.sidebar.Paginator.PerPage)
	want := m.sidebar.SelectedItem().(pageItem).page.ID
	got = tapModel(m, 1)
	if got.selected == nil || got.selected.ID != want {
		t.Fatalf("paginated tap opened %+v, want %s", got.selected, want)
	}
}

func TestTapRecentAndDatabaseVisibleRows(t *testing.T) {
	m := New([]Workspace{{Name: "test"}}, 0, nil, "mosaic")
	m.width, m.height = 80, 24
	m.selected, m.focus, m.pageLoading = &recentPage, focusViewer, false
	m.recent = &recentState{loaded: true, pages: []notion.Page{{ID: "first"}, {ID: "second"}}}
	m.layout()
	if got := tapModel(m, 1); got.recent == nil {
		t.Fatal("Recent summary activated an item")
	}
	if got := tapModel(m, 3); got.selected.ID != "second" {
		t.Fatalf("Recent tap opened %+v", got.selected)
	}
	db := notion.Page{ID: "db", Title: "Database"}
	m.selected, m.recent = &db, nil
	m.db = &dbState{ref: db, ds: testDataSource(), rows: []notion.Row{testRow("row-1", "Row", "", false)}}
	if got := tapModel(m, 2); got.db == nil {
		t.Fatal("database separator activated a row")
	}
	if got := tapModel(m, 3); got.selected.ID != "row-1" {
		t.Fatalf("database tap opened %+v", got.selected)
	}
}

func TestTapChildPageOpensOnceAtScrolledPosition(t *testing.T) {
	child := &notionapi.ChildPageBlock{BasicBlock: notionapi.BasicBlock{ID: "child", Type: "child_page"}}
	child.ChildPage.Title = "Child"
	m := commitTestModel(t, child)
	m.focus = focusViewer
	m.blockCache["child"] = []notion.BlockNode{para("inside", "Child text")}
	m.syncViewer()
	y := 1 + m.pv.starts[0] - m.viewer.YOffset
	got := tapModel(m, y)
	if got.selected.ID != "child" || len(got.history) != 1 {
		t.Fatalf("tap failed or release drilled twice: page=%+v history=%d", got.selected, len(got.history))
	}
}

func TestMousePressOnPlainTextDoesNotCreateOrSyncBlock(t *testing.T) {
	m := commitTestModel(t, para("text", "Read only text").Block)
	m.loading = false
	m.focus = focusViewer
	m.syncViewer()
	y := 1 + m.pv.starts[0] - m.viewer.YOffset
	for _, action := range []tea.MouseAction{tea.MouseActionPress, tea.MouseActionMotion, tea.MouseActionRelease} {
		next, cmd := m.Update(tea.MouseMsg{X: 8, Y: y, Button: tea.MouseButtonLeft, Action: action})
		m = next.(Model)
		if cmd != nil || m.editing || m.localBusy() || len(m.blockCache[m.selected.ID]) != 1 {
			t.Fatalf("mouse action %v caused editing or mutation: command=%v editing=%v busy=%v blocks=%d", action, cmd != nil, m.editing, m.localBusy(), len(m.blockCache[m.selected.ID]))
		}
	}
}
