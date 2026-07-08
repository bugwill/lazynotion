package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/justinm35/lazynotion/internal/convert"
	"github.com/justinm35/lazynotion/internal/notion"
)

func TestListContinuationDetection(t *testing.T) {
	cases := []struct {
		line   string
		marker string
		empty  bool
		ok     bool
	}{
		{"- [ ] buy milk", "- [ ] ", false, true},
		{"- [x] done", "- [ ] ", false, true},
		{"- plain bullet", "- ", false, true},
		{"3. third thing", "4. ", false, true},
		{"1. first", "2. ", false, true},
		{"- [ ] ", "- [ ] ", true, true},
		{"- ", "- ", true, true},
		{"just a paragraph", "", false, false},
		{"# heading", "", false, false},
	}
	for _, c := range cases {
		marker, empty, ok := convert.ListContinuation(c.line)
		if marker != c.marker || empty != c.empty || ok != c.ok {
			t.Errorf("ListContinuation(%q) = (%q,%v,%v), want (%q,%v,%v)",
				c.line, marker, empty, ok, c.marker, c.empty, c.ok)
		}
	}
}

func TestEnterContinuesChecklist(t *testing.T) {
	m := commitTestModel(t, nil)
	m.blockCache["page-1"] = []notion.BlockNode{para2("a", "before")}
	m.rebuildPage(true)
	m.pv.cursor = 0

	// n → type a to-do → enter
	next, _ := m.newBlockBelow()
	m = next.(Model)
	m.editArea.SetValue("- [ ] buy milk")
	m.editArea.CursorEnd()
	next, cmd := m.updateInlineEdit(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("continuation should sync the finished item")
	}
	if !m.editing {
		t.Fatal("a new draft should be open")
	}
	if got := m.editArea.Value(); got != "- [ ] " {
		t.Fatalf("new draft seed = %q, want the same marker", got)
	}
	// the finished item became a real (pending) to-do block
	ids := topIDs(m)
	foundTodo := false
	for _, b := range m.blockCache["page-1"] {
		if b.Block.GetType() == "to_do" {
			foundTodo = true
		}
	}
	if !foundTodo {
		t.Fatalf("committed item should be a to-do block: %v", ids)
	}

	// type the second item, enter again: the chain continues
	m.editArea.SetValue("- [ ] buy eggs")
	m.editArea.CursorEnd()
	next, _ = m.updateInlineEdit(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if got := m.editArea.Value(); got != "- [ ] " {
		t.Fatalf("chain should continue, seed = %q", got)
	}

	// enter on the empty marker ends the list
	next, _ = m.updateInlineEdit(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if !m.editing {
		t.Fatal("ending the list should keep the (now plain) draft open")
	}
	if got := m.editArea.Value(); got != "" {
		t.Errorf("empty marker should clear, value = %q", got)
	}
}

func TestEnterStillNewlinesInPlainText(t *testing.T) {
	m := commitTestModel(t, nil)
	m.blockCache["page-1"] = []notion.BlockNode{para2("a", "before")}
	m.rebuildPage(true)
	m.pv.cursor = 0
	next, _ := m.newBlockBelow()
	m = next.(Model)
	m.editArea.SetValue("plain thought")
	m.editArea.CursorEnd()
	_ = m.editArea.View()

	next, _ = m.updateInlineEdit(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if !m.editing {
		t.Fatal("plain enter must stay in the editor")
	}
	if !strings.Contains(m.editArea.Value(), "\n") {
		t.Errorf("plain enter should insert a newline, value = %q", m.editArea.Value())
	}
}
