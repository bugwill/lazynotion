package ui

import (
	"strings"
	"testing"

	"github.com/jomei/notionapi"

	"github.com/justinm35/lazynotion/internal/notion"
)

func testDataSource() *notion.DataSource {
	return &notion.DataSource{
		ID:         "ds-1",
		DatabaseID: "db-1",
		Title:      "Tasks",
		Properties: []notion.PropertyConfig{
			{ID: "title", Name: "Name", Type: "title"},
			{ID: "dn", Name: "Done", Type: "checkbox"},
			{ID: "st", Name: "Status", Type: "status"},
		},
	}
}

func testRow(id, name, status string, done bool) notion.Row {
	props := map[string]notion.PropertyValue{
		"Name": {Type: "title", Text: name, HasValue: name != ""},
		"Done": {Type: "checkbox", Checkbox: done, HasValue: true},
	}
	if status != "" {
		props["Status"] = notion.PropertyValue{
			Type: "status", Options: []notion.SelectOption{{Name: status, Color: "blue"}}, HasValue: true,
		}
	}
	return notion.Row{ID: id, URL: "https://www.notion.so/" + id, Properties: props}
}

func testDBModel() Model {
	m := New([]Workspace{{Name: "test"}}, 0, nil, "mosaic")
	m.width, m.height = 90, 30
	m.viewer.Width = 70
	m.viewer.Height = 20
	m.loading = false
	ref := notion.Page{ID: "ds-1", Title: "Tasks", Kind: notion.KindDataSource}
	m.selected = &ref
	m.focus = focusViewer
	m.db = &dbState{
		ref:  ref,
		dsID: "ds-1",
		ds:   testDataSource(),
		rows: []notion.Row{
			testRow("row-1", "Ship release", "In progress", false),
			testRow("row-2", "Write docs", "Done", true),
			testRow("row-3", "", "", false),
		},
	}
	return m
}

func TestDBGridRendersHeaderAndRows(t *testing.T) {
	m := testDBModel()
	out := m.dbGridView()
	plain := stripAnsi(out)
	for _, want := range []string{"Name", "Done", "Status", "Ship release", "Write docs", "In progress", "Untitled"} {
		if want == "Untitled" {
			continue // empty titles render empty in the grid, not "Untitled"
		}
		if !strings.Contains(plain, want) {
			t.Errorf("grid missing %q:\n%s", want, plain)
		}
	}
	if !strings.Contains(plain, "✓") {
		t.Errorf("checked checkbox should render ✓:\n%s", plain)
	}
	if !strings.Contains(plain, "3 rows") {
		t.Errorf("grid missing row count:\n%s", plain)
	}
}

func TestDBKeysMoveCursorAndOpenRow(t *testing.T) {
	m := testDBModel()
	next, _ := m.updateKeys(key("j"))
	m = next.(Model)
	if m.db.cursor != 1 {
		t.Fatalf("cursor = %d, want 1", m.db.cursor)
	}
	next, _ = m.updateKeys(key("G"))
	m = next.(Model)
	if m.db.cursor != 2 {
		t.Fatalf("G: cursor = %d, want 2", m.db.cursor)
	}
	next, _ = m.updateKeys(key("g"))
	m = next.(Model)
	if m.db.cursor != 0 {
		t.Fatalf("g: cursor = %d, want 0", m.db.cursor)
	}

	// enter opens the row as a page and stashes the grid in history
	next, cmd := m.updateKeys(key("enter"))
	m = next.(Model)
	if m.db != nil {
		t.Fatal("db view should close when opening a row")
	}
	if m.selected == nil || m.selected.ID != "row-1" {
		t.Fatalf("selected = %+v, want row-1", m.selected)
	}
	if m.selected.Title != "Ship release" {
		t.Errorf("row title = %q", m.selected.Title)
	}
	if cmd == nil {
		t.Fatal("opening an uncached row should trigger a load")
	}
	if len(m.history) != 1 || m.history[0].db == nil {
		t.Fatalf("history should hold the db state, got %+v", m.history)
	}

	// esc restores the grid exactly as it was
	next, _ = m.updateKeys(key("esc"))
	m = next.(Model)
	if m.db == nil {
		t.Fatal("esc should restore the db view")
	}
	if m.db.rows[0].ID != "row-1" || m.selected.ID != "ds-1" {
		t.Errorf("restored db = %+v selected = %+v", m.db.ref, m.selected)
	}
}

func TestDBReadOnlyKeysAreBlocked(t *testing.T) {
	m := testDBModel()
	for _, k := range []string{"d", "e", "i", "N", "a"} {
		next, cmd := m.updateKeys(key(k))
		m = next.(Model)
		if cmd != nil {
			t.Errorf("%q should be inert in db view", k)
		}
		if m.db == nil || len(m.db.rows) != 3 {
			t.Fatalf("%q mutated the db view", k)
		}
	}
	// n is handled globally; it must not create a draft block
	next, _ := m.updateKeys(key("n"))
	m = next.(Model)
	if m.editing {
		t.Error("n must not open the editor in db view")
	}
}

func TestDBColumnScroll(t *testing.T) {
	m := testDBModel()
	next, _ := m.updateKeys(key("l"))
	m = next.(Model)
	if m.db.colOff != 1 {
		t.Fatalf("l: colOff = %d, want 1", m.db.colOff)
	}
	out := stripAnsi(m.dbGridView())
	if !strings.Contains(out, "Name") {
		t.Errorf("title column must stay pinned after scrolling:\n%s", out)
	}
	next, _ = m.updateKeys(key("h"))
	m = next.(Model)
	if m.db.colOff != 0 {
		t.Fatalf("h: colOff = %d, want 0", m.db.colOff)
	}
}

func TestDBLoadMoreNearBottom(t *testing.T) {
	m := testDBModel()
	m.db.hasMore = true
	m.db.nextCursor = "cur-2"
	next, cmd := m.updateKeys(key("j"))
	m = next.(Model)
	if cmd == nil {
		t.Fatal("moving near the end with more rows should fetch the next page")
	}
	if !m.db.loadingMore {
		t.Fatal("loadingMore should be set")
	}
	// arriving rows append and clear the flag
	next, _ = m.Update(dbRowsMsg{dsID: "ds-1", page: &notion.RowPage{
		Rows: []notion.Row{testRow("row-4", "More work", "", false)},
	}})
	m = next.(Model)
	if len(m.db.rows) != 4 || m.db.loadingMore {
		t.Fatalf("rows = %d loadingMore = %v", len(m.db.rows), m.db.loadingMore)
	}
}

func TestDBLoadedMsgPopulatesView(t *testing.T) {
	m := New([]Workspace{{Name: "test"}}, 0, nil, "mosaic")
	m.width, m.height = 90, 30
	m.loading = false
	ref := notion.Page{ID: "db-1", Title: "Tasks"}
	next, _ := m.openDatabaseView(ref)
	m = next.(Model)
	if m.db == nil || !m.pageLoading {
		t.Fatal("openDatabaseView should enter loading state")
	}
	next, _ = m.Update(dbLoadedMsg{
		key: "db-1", ds: testDataSource(), sources: 2,
		rows: &notion.RowPage{Rows: []notion.Row{testRow("row-1", "A", "", false)}},
	})
	m = next.(Model)
	if m.pageLoading || m.db.ds == nil || len(m.db.rows) != 1 {
		t.Fatalf("db state = %+v", m.db)
	}
	if !strings.Contains(m.statusMsg, "2 data sources") {
		t.Errorf("multi-source note missing, status = %q", m.statusMsg)
	}
	// stale result for a different view is dropped
	next, _ = m.Update(dbLoadedMsg{key: "other", ds: testDataSource(), rows: &notion.RowPage{}})
	m = next.(Model)
	if len(m.db.rows) != 1 {
		t.Error("stale dbLoadedMsg must not clobber the current view")
	}
}

func TestOpenChildDatabaseFromPage(t *testing.T) {
	m := New([]Workspace{{Name: "test"}}, 0, nil, "mosaic")
	m.width, m.height = 90, 30
	m.viewer.Width = 60
	m.viewer.Height = 20
	m.loading = false
	parent := notion.Page{ID: "parent", Title: "Parent"}
	m.selected = &parent
	m.focus = focusViewer
	m.blockCache["parent"] = []notion.BlockNode{
		para("a", "intro"),
		{Block: &notionapi.ChildDatabaseBlock{
			BasicBlock:    notionapi.BasicBlock{ID: "db-1", Type: "child_database"},
			ChildDatabase: struct {
				Title string `json:"title"`
			}{Title: "Tasks"},
		}},
	}
	m.rebuildPage(true)
	m.pv.cursor = 1

	next, cmd := m.updateKeys(key("enter"))
	m = next.(Model)
	if m.db == nil || m.db.ref.ID != "db-1" {
		t.Fatalf("enter on child_database should open the db view, db = %+v", m.db)
	}
	if cmd == nil {
		t.Fatal("db view should start loading")
	}
	if len(m.history) != 1 || m.history[0].page.ID != "parent" {
		t.Fatalf("history = %+v", m.history)
	}

	// esc goes back to the parent page
	next, _ = m.updateKeys(key("esc"))
	m = next.(Model)
	if m.db != nil || m.selected.ID != "parent" {
		t.Fatalf("esc should return to parent, db = %v selected = %+v", m.db, m.selected)
	}
}
