package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jomei/notionapi"

	"github.com/justinm35/lazynotion/internal/notion"
)

func commitTestModel(t *testing.T, block notionapi.Block) Model {
	t.Helper()
	m := New([]Workspace{{Name: "test"}}, 0, nil, "mosaic")
	m.width, m.height = 90, 30
	m.viewer.Width = 60
	m.viewer.Height = 20
	page := notion.Page{ID: "page-1", Title: "p"}
	m.selected = &page
	if block != nil {
		m.blockCache["page-1"] = []notion.BlockNode{{Block: block}}
	}
	m.rebuildPage(true)
	return m
}

func TestCommitTodoEditSyncsCheckbox(t *testing.T) {
	todo := &notionapi.ToDoBlock{
		BasicBlock: notionapi.BasicBlock{ID: "td", Type: "to_do"},
		ToDo:       notionapi.ToDo{RichText: []notionapi.RichText{{PlainText: "task", Text: &notionapi.Text{Content: "task"}}}},
	}
	m := commitTestModel(t, todo)
	next, _ := m.startInlineEdit()
	m = next.(Model)
	if got := m.editArea.Value(); got != "- [ ] task" {
		t.Fatalf("seed = %q", got)
	}

	next, cmd := m.commitInlineEdit("- [x] task done")
	m = next.(Model)
	if cmd == nil {
		t.Fatal("commit should sync")
	}
	if !todo.ToDo.Checked {
		t.Error("checkbox edit should check the to-do")
	}
	if got := todo.ToDo.RichText[0].PlainText; got != "task done" {
		t.Errorf("text = %q, want %q (marker stripped)", got, "task done")
	}
}

func TestCommitMarkerChangeConvertsBlock(t *testing.T) {
	para := &notionapi.ParagraphBlock{
		BasicBlock: notionapi.BasicBlock{ID: "p1", Type: "paragraph"},
		Paragraph:  notionapi.Paragraph{RichText: []notionapi.RichText{{PlainText: "idea", Text: &notionapi.Text{Content: "idea"}}}},
	}
	m := commitTestModel(t, para)
	m.pv.cursor = 0

	next, cmd := m.commitInlineEdit("- [ ] idea")
	m = next.(Model)
	if cmd == nil {
		t.Fatal("conversion should produce a sync command")
	}
	got := m.blockCache["page-1"][0].Block
	if got.GetType() != "to_do" {
		t.Errorf("local block type = %s, want to_do", got.GetType())
	}
}

func TestCommitMarkerChangeOnNestedBlockKeepsType(t *testing.T) {
	child := &notionapi.BulletedListItemBlock{
		BasicBlock:       notionapi.BasicBlock{ID: "child", Type: "bulleted_list_item"},
		BulletedListItem: notionapi.ListItem{RichText: []notionapi.RichText{{PlainText: "nested", Text: &notionapi.Text{Content: "nested"}}}},
	}
	parent := &notionapi.BulletedListItemBlock{
		BasicBlock:       notionapi.BasicBlock{ID: "parent", Type: "bulleted_list_item"},
		BulletedListItem: notionapi.ListItem{RichText: []notionapi.RichText{{PlainText: "outer", Text: &notionapi.Text{Content: "outer"}}}},
	}
	m := New([]Workspace{{Name: "test"}}, 0, nil, "mosaic")
	m.width, m.height = 90, 30
	m.viewer.Width = 60
	m.viewer.Height = 20
	page := notion.Page{ID: "page-1", Title: "p"}
	m.selected = &page
	m.blockCache["page-1"] = []notion.BlockNode{{Block: parent, Children: []notion.BlockNode{{Block: child}}}}
	m.rebuildPage(true)
	m.pv.cursor = 1 // the nested bullet

	next, cmd := m.commitInlineEdit("# heading attempt")
	m = next.(Model)
	if cmd == nil {
		t.Fatal("fallback save should still sync text")
	}
	if child.GetType() != "bulleted_list_item" {
		t.Errorf("nested block type changed to %s", child.GetType())
	}
	if got := child.BulletedListItem.RichText[0].PlainText; got != "heading attempt" {
		t.Errorf("text = %q", got)
	}
	if m.statusMsg == "" {
		t.Error("fallback should explain itself in the status bar")
	}
}

func TestEmptyDraftEditAreaIsOneRow(t *testing.T) {
	m := commitTestModel(t, &notionapi.ParagraphBlock{
		BasicBlock: notionapi.BasicBlock{ID: "p1", Type: "paragraph"},
	})
	m.pv.cursor = 0
	next, _ := m.newBlockBelow()
	m = next.(Model)
	if !m.editing {
		t.Fatal("should be editing draft")
	}
	if h := m.editArea.Height(); h != 1 {
		t.Errorf("empty draft edit area height = %d, want 1", h)
	}
	if lines := m.editLines(); len(lines) != 1 {
		t.Errorf("edit gutter %d lines tall, want 1", len(lines))
	}
}

func TestEmptyDraftSavesEmptyBlock(t *testing.T) {
	m := commitTestModel(t, &notionapi.ParagraphBlock{
		BasicBlock: notionapi.BasicBlock{ID: "p1", Type: "paragraph"},
	})
	m.pv.cursor = 0
	next, _ := m.newBlockBelow()
	m = next.(Model)

	next, cmd := m.commitInlineEdit("")
	m = next.(Model)
	if cmd == nil {
		t.Fatal("empty draft should still sync an empty block")
	}
	blocks := m.blockCache["page-1"]
	if len(blocks) != 2 {
		t.Fatalf("page should keep the new empty block, has %d", len(blocks))
	}
	if blocks[1].Block.GetType() != "paragraph" {
		t.Errorf("empty block type = %s", blocks[1].Block.GetType())
	}
}

func TestEmptyBlockRendersOneLine(t *testing.T) {
	m := commitTestModel(t, &notionapi.ParagraphBlock{
		BasicBlock: notionapi.BasicBlock{ID: "p1", Type: "paragraph"},
	})
	if len(m.pv.rendered[0]) != 1 {
		t.Errorf("empty paragraph rendered %d lines, want 1", len(m.pv.rendered[0]))
	}
}

func TestConsecutiveEmptyBlocksStayInPlace(t *testing.T) {
	m := commitTestModel(t, nil)
	m.blockCache["page-1"] = []notion.BlockNode{
		para2("a", "first"), para2("b", "second"), para2("c", "third"),
	}
	m.rebuildPage(true)
	m.pv.cursor = 0

	// first empty block: n, esc
	next, _ := m.newBlockBelow()
	m = next.(Model)
	next, cmd := m.commitInlineEdit("")
	m = next.(Model)
	if cmd == nil {
		t.Fatal("first empty block should sync")
	}
	ids := topIDs(m)
	if len(ids) != 4 || !isPendingID(ids[1]) {
		t.Fatalf("after first commit order = %v, want pending block at index 1", ids)
	}

	// cursor sits on the pending block; n again must insert right below
	// it, NOT at the bottom of the page (the reported bug)
	m.pv.cursor = 1
	next, _ = m.newBlockBelow()
	m = next.(Model)
	ids = topIDs(m)
	if ids[2] != draftBlockID {
		t.Fatalf("second draft order = %v, want draft at index 2", ids)
	}

	// commit it; the create op must anchor after the first pending block
	next, cmd = m.commitInlineEdit("")
	m = next.(Model)
	if cmd != nil {
		t.Fatal("second create should queue behind the in-flight first sync")
	}
	ids = topIDs(m)
	if !isPendingID(ids[1]) || !isPendingID(ids[2]) {
		t.Fatalf("after second commit order = %v", ids)
	}

	// first sync completes: pending-1 becomes real; queued create flushes
	next, cmd = m.handleMoveDone(moveDoneMsg{pageID: "page-1", patches: [][2]string{{ids[1], "real-1"}}})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("queued create should flush after the first completes")
	}
	ids = topIDs(m)
	if ids[1] != "real-1" || !isPendingID(ids[2]) {
		t.Errorf("after patch order = %v, want real-1 then pending", ids)
	}
}

func para2(id, text string) notion.BlockNode {
	return notion.BlockNode{Block: &notionapi.ParagraphBlock{
		BasicBlock: notionapi.BasicBlock{ID: notionapi.BlockID(id), Type: "paragraph"},
		Paragraph: notionapi.Paragraph{RichText: []notionapi.RichText{
			{PlainText: text, Text: &notionapi.Text{Content: text}},
		}},
	}}
}

func topIDs(m Model) []string {
	var ids []string
	for _, b := range m.blockCache["page-1"] {
		ids = append(ids, b.Block.GetID().String())
	}
	return ids
}

func TestShiftEnterSavesAndOpensNextBlock(t *testing.T) {
	m := commitTestModel(t, nil)
	m.blockCache["page-1"] = []notion.BlockNode{para2("a", "first")}
	m.rebuildPage(true)
	m.pv.cursor = 0

	next, _ := m.newBlockBelow()
	m = next.(Model)
	m.editArea.SetValue("groceries")

	next, cmd := m.updateInlineEdit(tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("save-and-continue should sync the saved block")
	}
	if !m.editing {
		t.Fatal("a fresh block should be open for editing")
	}
	ids := topIDs(m)
	if len(ids) != 3 || !isPendingID(ids[1]) || ids[2] != draftBlockID {
		t.Fatalf("order = %v, want [a, pending, draft]", ids)
	}
	if u, _ := m.pv.current(); u.ID() != draftBlockID {
		t.Errorf("cursor should be on the new draft, on %q", u.ID())
	}
	if m.editArea.Value() != "" {
		t.Errorf("new block should start empty, has %q", m.editArea.Value())
	}
}

func TestEditBoxGrowsWithPane(t *testing.T) {
	m := commitTestModel(t, &notionapi.ParagraphBlock{
		BasicBlock: notionapi.BasicBlock{ID: "p1", Type: "paragraph"},
	})
	m.viewer.Height = 40
	m.pv.cursor = 0
	next, _ := m.startInlineEdit()
	m = next.(Model)

	m.editArea.SetValue(strings.Repeat("line\n", 24) + "line")
	m.resizeEditArea()
	if h := m.editArea.Height(); h != 25 {
		t.Errorf("25-line content in a 40-row pane: height = %d, want 25", h)
	}

	m.editArea.SetValue(strings.Repeat("line\n", 99) + "line")
	m.resizeEditArea()
	if h := m.editArea.Height(); h != 38 {
		t.Errorf("100-line content should cap at pane height - 2 = 38, got %d", h)
	}
}
