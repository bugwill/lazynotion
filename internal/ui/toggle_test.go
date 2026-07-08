package ui

import (
	"strings"
	"testing"

	"github.com/jomei/notionapi"

	"github.com/justinm35/lazynotion/internal/notion"
)

func TestToggleUpdatesRenderedCheckbox(t *testing.T) {
	todo := &notionapi.ToDoBlock{
		BasicBlock: notionapi.BasicBlock{ID: "todo-1", Type: "to_do"},
		ToDo: notionapi.ToDo{
			RichText: []notionapi.RichText{{PlainText: "buy milk"}},
			Checked:  false,
		},
	}
	m := New([]Workspace{{Name: "test"}}, 0, nil, "mosaic")
	m.viewer.Width = 60
	m.viewer.Height = 20
	page := notion.Page{ID: "page-1", Title: "p"}
	m.selected = &page
	m.blockCache["page-1"] = []notion.BlockNode{{Block: todo}}
	m.rebuildPage(true)

	before := stripAnsi(strings.Join(m.pv.rendered[0], " "))
	if strings.Contains(before, "✓") {
		t.Fatalf("to-do should start unchecked: %q", before)
	}

	next, cmd := m.toggleCurrentToDo()
	m = next.(Model)
	if cmd == nil {
		t.Fatal("toggle should produce a sync command")
	}

	after := stripAnsi(strings.Join(m.pv.rendered[0], " "))
	if !strings.Contains(after, "✓") {
		t.Errorf("checked to-do should render a checkmark immediately, got %q", after)
	}
}
