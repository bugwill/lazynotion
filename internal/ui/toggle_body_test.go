package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jomei/notionapi"

	"github.com/justinm35/lazynotion/internal/convert"
	"github.com/justinm35/lazynotion/internal/notion"
)

func emptyToggle(id, title string) notion.BlockNode {
	return notion.BlockNode{Block: &notionapi.ToggleBlock{
		BasicBlock: notionapi.BasicBlock{ID: notionapi.BlockID(id), Type: "toggle"},
		Toggle: notionapi.Toggle{RichText: []notionapi.RichText{
			{PlainText: title, Text: &notionapi.Text{Content: title}},
		}},
	}}
}

func TestNOnToggleAddsInsideBody(t *testing.T) {
	m := commitTestModel(t, nil)
	m.blockCache["page-1"] = []notion.BlockNode{
		emptyToggle("tog", "Details"), para2("b", "after"),
	}
	m.rebuildPage(true)
	m.cursorToBlock("tog")

	next, _ := m.newBlockBelow()
	m = next.(Model)
	if !m.editing {
		t.Fatal("should be editing the draft")
	}
	unit, _ := m.pv.current()
	if unit.ID() != draftBlockID || unit.ParentID != "tog" {
		t.Fatalf("draft should nest inside the toggle, parent=%q", unit.ParentID)
	}
	if unit.Depth != 1 {
		t.Errorf("draft depth = %d, want 1 (indented under toggle)", unit.Depth)
	}

	// commit: the create op must target the toggle as its container
	next, cmd := m.commitInlineEdit("inside the body")
	m = next.(Model)
	if cmd == nil {
		t.Fatal("commit should sync")
	}
	children, ok := notion.ChildrenOf(m.blockCache["page-1"], "tog")
	if !ok || len(children) != 1 {
		t.Fatalf("toggle should have the new child, has %d", len(children))
	}
	if !isPendingID(children[0].Block.GetID().String()) {
		t.Error("new child should carry a pending ID")
	}
}

func TestNOnNestedBlockCreatesSiblingInPlace(t *testing.T) {
	m := commitTestModel(t, nil)
	tog := emptyToggle("tog", "Details")
	tog.Children = []notion.BlockNode{para2("c1", "first child"), para2("c2", "second child")}
	m.blockCache["page-1"] = []notion.BlockNode{tog, para2("b", "after")}
	m.rebuildPage(true)
	m.cursorToBlock("c1")

	next, _ := m.newBlockBelow()
	m = next.(Model)
	unit, _ := m.pv.current()
	if unit.ParentID != "tog" {
		t.Fatalf("sibling of a nested block should stay nested, parent=%q", unit.ParentID)
	}
	children, _ := notion.ChildrenOf(m.blockCache["page-1"], "tog")
	ids := make([]string, len(children))
	for i, c := range children {
		ids[i] = c.Block.GetID().String()
	}
	if len(ids) != 3 || ids[0] != "c1" || ids[1] != draftBlockID || ids[2] != "c2" {
		t.Fatalf("children order = %v, want draft between c1 and c2", ids)
	}
}

func TestNestedCreatePatchesIDInTree(t *testing.T) {
	m := commitTestModel(t, nil)
	m.blockCache["page-1"] = []notion.BlockNode{emptyToggle("tog", "Details")}
	m.rebuildPage(true)
	m.cursorToBlock("tog")

	next, _ := m.newBlockBelow()
	m = next.(Model)
	next, _ = m.commitInlineEdit("body text")
	m = next.(Model)
	children, _ := notion.ChildrenOf(m.blockCache["page-1"], "tog")
	pendingID := children[0].Block.GetID().String()

	next, _ = m.handleMoveDone(moveDoneMsg{pageID: "page-1", patches: [][2]string{{pendingID, "real-1"}}})
	m = next.(Model)
	children, _ = notion.ChildrenOf(m.blockCache["page-1"], "tog")
	if children[0].Block.GetID().String() != "real-1" {
		t.Errorf("nested pending ID should patch to real: %q", children[0].Block.GetID().String())
	}
}

func TestToggleEditSeedRoundTrips(t *testing.T) {
	units := convert.Flatten([]notion.BlockNode{emptyToggle("tog", "My Toggle")})
	seed, ok := units[0].EditableMarkdown()
	if !ok || seed != "**▸ My Toggle**" {
		t.Fatalf("toggle seed = %q", seed)
	}
	patch := convert.ParseEditPatch(seed)
	if patch.Kind != "toggle" {
		t.Fatalf("patch kind = %q, want toggle", patch.Kind)
	}
	var text strings.Builder
	for _, r := range patch.RichText {
		text.WriteString(r.PlainText)
	}
	if text.String() != "My Toggle" {
		t.Errorf("patch text = %q", text.String())
	}
}

func TestToggleThenShiftEnterWritesInsideBody(t *testing.T) {
	m := commitTestModel(t, nil)
	m.blockCache["page-1"] = []notion.BlockNode{para2("a", "existing")}
	m.rebuildPage(true)
	m.pv.cursor = 0

	// n → /toggle → title
	next, _ := m.newBlockBelow()
	m = next.(Model)
	m = press(t, m, "/", "t", "o", "g", "g", "l", "e")
	if entries := m.filteredPalette(); len(entries) == 0 || entries[0].title != "Toggle" {
		t.Fatalf("palette must offer Toggle in a draft: %+v", entries)
	}
	m = press(t, m, "enter")
	m = press(t, m, "M", "y", " ", "T", "o", "g")

	// shift+enter: saves the toggle, opens the next draft INSIDE it
	next, cmd := m.updateInlineEdit(tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("toggle save should sync")
	}
	if !m.editing {
		t.Fatal("a new draft should be open")
	}
	unit, _ := m.pv.current()
	if unit.ID() != draftBlockID {
		t.Fatalf("cursor should be on the new draft, on %q", unit.ID())
	}
	if !isPendingID(unit.ParentID) {
		t.Fatalf("draft should nest inside the pending toggle, parent=%q", unit.ParentID)
	}
	toggleID := unit.ParentID

	// type body text and save: its create op targets the pending toggle
	m.editArea.SetValue("first body line")
	next, _ = m.commitInlineEdit(m.editArea.Value())
	m = next.(Model)
	if len(m.moveQueue) != 1 || m.moveQueue[0].parentID != toggleID {
		t.Fatalf("queued op = %+v, want parent %q", m.moveQueue, toggleID)
	}

	// the toggle's own sync completes: the queued child op's parent must
	// be patched to the real ID before it flushes
	next, cmd = m.handleMoveDone(moveDoneMsg{pageID: "page-1", patches: [][2]string{{toggleID, "real-toggle"}}})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("child create should flush after the toggle sync")
	}
	children, ok := notion.ChildrenOf(m.blockCache["page-1"], "real-toggle")
	if !ok || len(children) != 1 {
		t.Fatalf("toggle should hold its body block, children=%d ok=%v", len(children), ok)
	}
}

func TestPaletteOffersToggleInNestedDraft(t *testing.T) {
	m := commitTestModel(t, nil)
	tog := emptyToggle("tog", "Details")
	m.blockCache["page-1"] = []notion.BlockNode{tog}
	m.rebuildPage(true)
	m.cursorToBlock("tog")
	next, _ := m.newBlockBelow() // nests inside the toggle
	m = next.(Model)

	m = press(t, m, "/")
	found := false
	for _, e := range m.filteredPalette() {
		if e.title == "Toggle" {
			found = true
		}
	}
	if !found {
		t.Error("Toggle must be offered in nested drafts too")
	}
}

func TestEnterStampsEmptyBlocksBelow(t *testing.T) {
	m := commitTestModel(t, nil)
	m.blockCache["page-1"] = []notion.BlockNode{para2("a", "first"), para2("b", "second")}
	m.rebuildPage(true)
	m.focus = focusViewer
	m.cursorToBlock("a")

	next, cmd := m.updateKeys(key("enter"))
	m = next.(Model)
	if cmd == nil {
		t.Fatal("enter should sync the new block")
	}
	if m.editing {
		t.Fatal("enter must not open the editor")
	}
	ids := topIDs(m)
	if len(ids) != 3 || !isPendingID(ids[1]) || ids[2] != "b" {
		t.Fatalf("order = %v, want empty block between a and b", ids)
	}
	if u, _ := m.pv.current(); u.ID() != ids[1] {
		t.Fatalf("cursor should move onto the new block")
	}

	// keep pressing enter: blocks stack downward
	next, _ = m.updateKeys(key("enter"))
	m = next.(Model)
	ids = topIDs(m)
	if len(ids) != 4 || !isPendingID(ids[2]) {
		t.Fatalf("second enter order = %v, want another pending block after the first", ids)
	}
	if u, _ := m.pv.current(); u.ID() != ids[2] {
		t.Error("cursor should follow to the newest block")
	}
}

func TestEnterStillFoldsAndOpens(t *testing.T) {
	m := commitTestModel(t, nil)
	tog := emptyToggle("tog", "Details")
	tog.Children = []notion.BlockNode{para2("c1", "inside")}
	m.blockCache["page-1"] = []notion.BlockNode{
		tog,
		{Block: &notionapi.ChildPageBlock{BasicBlock: notionapi.BasicBlock{ID: "sub", Type: "child_page"}}},
	}
	m.rebuildPage(true)
	m.focus = focusViewer

	m.cursorToBlock("tog")
	unitsBefore := len(m.pv.units)
	next, _ := m.updateKeys(key("enter"))
	m = next.(Model)
	if len(m.pv.units) >= unitsBefore {
		t.Error("enter on a toggle should still fold it, not create a block")
	}

	m.cursorToBlock("sub")
	next, _ = m.updateKeys(key("enter"))
	m = next.(Model)
	if m.selected == nil || m.selected.ID != "sub" {
		t.Error("enter on a sub-page should still open it")
	}
}
