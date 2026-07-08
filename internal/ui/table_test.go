package ui

import (
	"strings"
	"testing"

	"github.com/jomei/notionapi"

	"github.com/justinm35/lazynotion/internal/notion"
)

func testTable() notion.BlockNode {
	row := func(id string, cells ...string) notion.BlockNode {
		cc := make([][]notionapi.RichText, len(cells))
		for i, c := range cells {
			cc[i] = []notionapi.RichText{{PlainText: c, Text: &notionapi.Text{Content: c}}}
		}
		return notion.BlockNode{Block: &notionapi.TableRowBlock{
			BasicBlock: notionapi.BasicBlock{ID: notionapi.BlockID(id), Type: "table_row"},
			TableRow:   notionapi.TableRow{Cells: cc},
		}}
	}
	return notion.BlockNode{
		Block: &notionapi.TableBlock{
			BasicBlock: notionapi.BasicBlock{ID: "tbl", Type: "table"},
			Table:      notionapi.Table{TableWidth: 2, HasColumnHeader: true},
		},
		Children: []notion.BlockNode{
			row("r1", "Name", "Role"),
			row("r2", "Ada", "Engineer"),
		},
	}
}

func tableModel(t *testing.T) Model {
	t.Helper()
	m := commitTestModel(t, nil)
	m.blockCache["page-1"] = []notion.BlockNode{testTable()}
	m.rebuildPage(true)
	m.focus = focusViewer
	m.cursorToBlock("tbl")
	return m
}

func TestTableEditSeed(t *testing.T) {
	m := tableModel(t)
	next, _ := m.startInlineEdit()
	m = next.(Model)
	if !m.editing {
		t.Fatal("tables should now be editable")
	}
	seed := m.editArea.Value()
	for _, want := range []string{"| Name | Role |", "| Ada | Engineer |", "| --- | --- |"} {
		if !strings.Contains(seed, want) {
			t.Errorf("seed missing %q:\n%s", want, seed)
		}
	}
}

func TestTableCellEditUpdatesRowsInPlace(t *testing.T) {
	m := tableModel(t)
	next, _ := m.startInlineEdit()
	m = next.(Model)
	edited := strings.Replace(m.editArea.Value(), "Engineer", "Founder", 1)

	next, cmd := m.commitInlineEdit(edited)
	m = next.(Model)
	if cmd == nil {
		t.Fatal("cell edit should sync")
	}
	// identity preserved: same table and row IDs, cell updated locally
	tbl := m.blockCache["page-1"][0]
	if tbl.Block.GetID().String() != "tbl" {
		t.Error("content edit must not rebuild the table")
	}
	row := tbl.Children[1].Block.(*notionapi.TableRowBlock)
	if got := row.TableRow.Cells[1][0].PlainText; got != "Founder" {
		t.Errorf("cell = %q, want Founder", got)
	}
	if len(m.undoStack) == 0 || m.undoStack[len(m.undoStack)-1].desc != "table edit" {
		t.Error("cell edits should be undoable")
	}
}

func TestTableShapeChangeRebuilds(t *testing.T) {
	m := tableModel(t)
	next, _ := m.startInlineEdit()
	m = next.(Model)
	edited := m.editArea.Value() + "\n| Grace | Admiral |"

	next, cmd := m.commitInlineEdit(edited)
	m = next.(Model)
	if cmd == nil {
		t.Fatal("shape change should sync")
	}
	tbl := m.blockCache["page-1"][0]
	if tbl.Block.GetID().String() == "tbl" {
		t.Error("adding a row should swap in the rebuilt table locally")
	}
	if len(tbl.Children) != 3 {
		t.Errorf("rebuilt table rows = %d, want 3", len(tbl.Children))
	}
}

func TestTableMangledEditReopensEditor(t *testing.T) {
	m := tableModel(t)
	next, _ := m.startInlineEdit()
	m = next.(Model)

	next, _ = m.commitInlineEdit("no pipes here at all")
	m = next.(Model)
	tbl := m.blockCache["page-1"][0]
	if tbl.Block.GetID().String() != "tbl" || len(tbl.Children) != 2 {
		t.Error("a mangled edit must leave the table untouched")
	}
	if !m.editing {
		t.Error("the editor should reopen so the input isn't lost")
	}
	if m.statusMsg == "" {
		t.Error("the problem should be explained")
	}
}

func TestTablePaletteCommand(t *testing.T) {
	m := paletteTestModel(t, true)
	m = press(t, m, "/", "t", "a", "b", "enter")
	value := m.editArea.Value()
	if !strings.Contains(value, "| Column 1 | Column 2 |") || !strings.Contains(value, "| --- | --- |") {
		t.Fatalf("table scaffold = %q", value)
	}
	// cursor should sit in the first header cell
	m = press(t, m, "X")
	if !strings.Contains(m.editArea.Value(), "| X") {
		t.Errorf("typing should land in the first cell: %q", m.editArea.Value())
	}
}
