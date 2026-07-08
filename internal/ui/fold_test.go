package ui

import (
	"strings"
	"testing"

	"github.com/jomei/notionapi"

	"github.com/justinm35/lazynotion/internal/convert"
	"github.com/justinm35/lazynotion/internal/notion"
)

func toggleTree() []notion.BlockNode {
	return []notion.BlockNode{
		para("a", "before"),
		{
			Block: &notionapi.ToggleBlock{
				BasicBlock: notionapi.BasicBlock{ID: "tog", Type: "toggle"},
				Toggle: notionapi.Toggle{RichText: []notionapi.RichText{
					{PlainText: "details", Text: &notionapi.Text{Content: "details"}},
				}},
			},
			Children: []notion.BlockNode{para("c1", "hidden one"), para("c2", "hidden two")},
		},
		para("b", "after"),
	}
}

func TestFlattenFolded(t *testing.T) {
	units := convert.FlattenFolded(toggleTree(), nil)
	if len(units) != 5 {
		t.Fatalf("expanded: %d units, want 5", len(units))
	}
	var tog convert.Unit
	for _, u := range units {
		if u.ID() == "tog" {
			tog = u
		}
	}
	if !tog.Foldable || tog.Collapsed {
		t.Errorf("toggle unit = foldable %v collapsed %v, want foldable expanded", tog.Foldable, tog.Collapsed)
	}
	if !strings.Contains(tog.Markdown, "▾") {
		t.Errorf("expanded toggle should show ▾: %q", tog.Markdown)
	}

	units = convert.FlattenFolded(toggleTree(), map[string]bool{"tog": true})
	if len(units) != 3 {
		t.Fatalf("collapsed: %d units, want 3 (children hidden)", len(units))
	}
	for _, u := range units {
		if u.ID() == "tog" {
			if !u.Collapsed || !strings.Contains(u.Markdown, "▸") {
				t.Errorf("collapsed toggle = %+v", u)
			}
		}
		if u.ID() == "c1" || u.ID() == "c2" {
			t.Error("collapsed toggle's children should be hidden")
		}
	}
}

func TestToggleableHeadingFolds(t *testing.T) {
	tree := []notion.BlockNode{
		{
			Block: &notionapi.Heading2Block{
				BasicBlock: notionapi.BasicBlock{ID: "h", Type: "heading_2"},
				Heading2: notionapi.Heading{
					RichText:     []notionapi.RichText{{PlainText: "Section"}},
					IsToggleable: true,
				},
			},
			Children: []notion.BlockNode{para("hc", "inside")},
		},
	}
	units := convert.FlattenFolded(tree, nil)
	if len(units) != 2 || !units[0].Foldable {
		t.Fatalf("toggleable heading should be foldable: %+v", units)
	}
	if !strings.Contains(units[0].Markdown, "## ▾ Section") {
		t.Errorf("heading marker placement: %q", units[0].Markdown)
	}
	units = convert.FlattenFolded(tree, map[string]bool{"h": true})
	if len(units) != 1 {
		t.Errorf("collapsed heading should hide children, got %d units", len(units))
	}

	// plain headings are not foldable
	plain := []notion.BlockNode{{
		Block: &notionapi.Heading2Block{
			BasicBlock: notionapi.BasicBlock{ID: "p", Type: "heading_2"},
			Heading2:   notionapi.Heading{RichText: []notionapi.RichText{{PlainText: "x"}}},
		},
		Children: []notion.BlockNode{para("pc", "y")},
	}}
	if u := convert.FlattenFolded(plain, nil); u[0].Foldable {
		t.Error("non-toggleable heading must not be foldable")
	}
}

func TestEnterFoldsAndUnfolds(t *testing.T) {
	m := commitTestModel(t, nil)
	m.blockCache["page-1"] = toggleTree()
	m.rebuildPage(true)
	m.focus = focusViewer
	m.cursorToBlock("tog")

	next, _ := m.updateKeys(key("enter"))
	m = next.(Model)
	if len(m.pv.units) != 3 {
		t.Fatalf("after fold: %d units, want 3", len(m.pv.units))
	}
	if u, _ := m.pv.current(); u.ID() != "tog" {
		t.Errorf("cursor should stay on the toggle, on %q", u.ID())
	}

	next, _ = m.updateKeys(key("enter"))
	m = next.(Model)
	if len(m.pv.units) != 5 {
		t.Fatalf("after unfold: %d units, want 5", len(m.pv.units))
	}
}

func TestToggleCommandCreatesToggle(t *testing.T) {
	m := paletteTestModel(t, true)
	m = press(t, m, "/", "t", "o", "g", "enter")
	if got := m.editArea.Value(); got != "**▸ **" {
		t.Fatalf("value = %q, want toggle scaffold", got)
	}
	m = press(t, m, "R", "o", "a", "d", "m", "a", "p")
	if got := m.editArea.Value(); got != "**▸ Roadmap**" {
		t.Fatalf("value = %q, cursor should type inside the markers", got)
	}

	next, cmd := m.commitInlineEdit(m.editArea.Value())
	m = next.(Model)
	if cmd == nil {
		t.Fatal("toggle draft should sync")
	}
	var found bool
	for _, b := range m.blockCache["page-1"] {
		if b.Block.GetType() == "toggle" {
			found = true
		}
	}
	if !found {
		t.Error("commit should create a toggle block")
	}
}
