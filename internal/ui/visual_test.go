package ui

import (
	"strings"
	"testing"

	"github.com/jomei/notionapi"

	"github.com/justinm35/lazynotion/internal/notion"
)

func visualModel(t *testing.T) Model {
	t.Helper()
	m := commitTestModel(t, nil)
	m.blockCache["page-1"] = []notion.BlockNode{
		para2("a", "first line"),
		{Block: &notionapi.ToDoBlock{
			BasicBlock: notionapi.BasicBlock{ID: "b", Type: "to_do"},
			ToDo: notionapi.ToDo{Checked: true, RichText: []notionapi.RichText{
				{PlainText: "done task", Text: &notionapi.Text{Content: "done task"}},
			}},
		}},
		para2("c", "third line"),
	}
	m.rebuildPage(true)
	m.focus = focusViewer
	m.loading = false
	return m
}

func TestYankSingleBlockCopiesContent(t *testing.T) {
	m := visualModel(t)
	m.cursorToBlock("b")
	text, count := m.yankSelection()
	if count != 1 || text != "- [x] done task" {
		t.Errorf("yank = %q (%d), want the block's markdown", text, count)
	}
	next, cmd := m.updateKeys(key("y"))
	m = next.(Model)
	if cmd == nil {
		t.Error("y should produce a copy command")
	}
}

func TestVisualRangeYank(t *testing.T) {
	m := visualModel(t)
	m.cursorToBlock("a")

	next, _ := m.updateKeys(key("v"))
	m = next.(Model)
	if m.visualAnchor != 0 {
		t.Fatalf("v should anchor at the cursor, anchor=%d", m.visualAnchor)
	}
	// extend down two blocks
	next, _ = m.updateKeys(key("j"))
	m = next.(Model)
	next, _ = m.updateKeys(key("j"))
	m = next.(Model)
	if m.visualAnchor != 0 || m.pv.cursor != 2 {
		t.Fatalf("selection should span 0..2, anchor=%d cursor=%d", m.visualAnchor, m.pv.cursor)
	}

	// all three blocks carry the selection gutter
	m.syncViewer()
	content := m.pv.assemble(true, nil)
	if strings.Count(content, "▌") < 3 {
		t.Errorf("all selected blocks should show the gutter bar:\n%s", stripAnsi(content))
	}

	text, count := m.yankSelection()
	if count != 3 {
		t.Fatalf("count = %d, want 3", count)
	}
	want := "first line\n- [x] done task\nthird line"
	if text != want {
		t.Errorf("yank = %q, want %q", text, want)
	}

	next, cmd := m.updateKeys(key("y"))
	m = next.(Model)
	if cmd == nil {
		t.Error("visual y should copy")
	}
	if m.visualAnchor != -1 {
		t.Error("yank should leave visual mode")
	}
}

func TestVisualEscAndOtherKeysCancel(t *testing.T) {
	m := visualModel(t)
	next, _ := m.updateKeys(key("v"))
	m = next.(Model)
	next, _ = m.updateKeys(key("esc"))
	m = next.(Model)
	if m.visualAnchor != -1 {
		t.Error("esc should cancel visual mode")
	}
	if m.focus != focusViewer {
		t.Error("esc in visual mode should not leave the viewer")
	}

	next, _ = m.updateKeys(key("v"))
	m = next.(Model)
	next, _ = m.updateKeys(key(" ")) // space toggles a to-do → drops visual first
	m = next.(Model)
	if m.visualAnchor != -1 {
		t.Error("non-movement keys should drop visual mode")
	}
}
