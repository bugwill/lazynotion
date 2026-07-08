package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jomei/notionapi"

	"github.com/justinm35/lazynotion/internal/notion"
)

func paletteTestModel(t *testing.T, draft bool) Model {
	t.Helper()
	m := New([]Workspace{{Name: "test"}}, 0, nil, "mosaic")
	m.width, m.height = 90, 30
	m.viewer.Width = 60
	m.viewer.Height = 20
	id := "block-1"
	if draft {
		id = draftBlockID
	}
	block := &notionapi.ParagraphBlock{
		BasicBlock: notionapi.BasicBlock{ID: notionapi.BlockID(id), Type: "paragraph"},
	}
	page := notion.Page{ID: "page-1", Title: "p"}
	m.selected = &page
	m.blockCache["page-1"] = []notion.BlockNode{{Block: block}}
	m.rebuildPage(true)
	m.editing = true
	m.editArea.SetWidth(58)
	m.editArea.Focus()
	return m
}

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func press(t *testing.T, m Model, keys ...string) Model {
	t.Helper()
	for _, k := range keys {
		next, _ := m.updateInlineEdit(key(k))
		m = next.(Model)
	}
	return m
}

func TestPaletteTrigger(t *testing.T) {
	m := paletteTestModel(t, true)
	m = press(t, m, "/")
	if !m.paletteOpen {
		t.Fatal("slash on empty editor should open the palette")
	}

	m2 := paletteTestModel(t, true)
	m2.editArea.SetValue("https:/")
	m2.editArea.CursorEnd()
	m2 = press(t, m2, "/")
	if m2.paletteOpen {
		t.Fatal("slash after non-whitespace should stay literal")
	}
	if !strings.HasSuffix(m2.editArea.Value(), "https://") {
		t.Errorf("literal slash not inserted: %q", m2.editArea.Value())
	}
}

func TestPaletteFilterAndApplyPrefix(t *testing.T) {
	m := paletteTestModel(t, true)
	m = press(t, m, "/", "t", "o", "d", "o")
	entries := m.filteredPalette()
	if len(entries) == 0 || entries[0].title != "To-do" {
		t.Fatalf("filtered = %+v", entries)
	}
	m = press(t, m, "enter")
	if m.paletteOpen {
		t.Fatal("palette should close on apply")
	}
	if got := m.editArea.Value(); got != "- [ ] " {
		t.Errorf("value = %q, want %q", got, "- [ ] ")
	}
}

func TestPaletteWrapPlacesCursorInside(t *testing.T) {
	m := paletteTestModel(t, false)
	m.editArea.SetValue("hello ")
	m.editArea.CursorEnd()
	m = press(t, m, "/", "b", "o", "l", "d", "enter", "x")
	if got := m.editArea.Value(); got != "hello **x**" {
		t.Errorf("value = %q, want %q", got, "hello **x**")
	}
}

func TestPaletteHidesBlockCommandsOnExistingBlocks(t *testing.T) {
	m := paletteTestModel(t, false)
	m = press(t, m, "/")
	for _, e := range m.filteredPalette() {
		if e.draftOnly {
			t.Errorf("draft-only command %q offered on existing block", e.title)
		}
	}
}

func TestPaletteNoMatchInsertsLiterally(t *testing.T) {
	m := paletteTestModel(t, true)
	m = press(t, m, "/", "z", "z", "z", "enter")
	if got := m.editArea.Value(); got != "/zzz" {
		t.Errorf("value = %q, want literal /zzz", got)
	}
}

func TestInlineEditRoundTripsFormatting(t *testing.T) {
	bold := &notionapi.Annotations{Bold: true}
	block := &notionapi.ParagraphBlock{
		BasicBlock: notionapi.BasicBlock{ID: "block-1", Type: "paragraph"},
		Paragraph: notionapi.Paragraph{RichText: []notionapi.RichText{
			{PlainText: "already ", Text: &notionapi.Text{Content: "already "}},
			{PlainText: "bold", Text: &notionapi.Text{Content: "bold"}, Annotations: bold},
		}},
	}
	m := New([]Workspace{{Name: "test"}}, 0, nil, "mosaic")
	m.width, m.height = 90, 30
	m.viewer.Width = 60
	m.viewer.Height = 20
	page := notion.Page{ID: "page-1", Title: "p"}
	m.selected = &page
	m.blockCache["page-1"] = []notion.BlockNode{{Block: block}}
	m.rebuildPage(true)

	next, _ := m.startInlineEdit()
	m = next.(Model)
	if got := m.editArea.Value(); got != "already **bold**" {
		t.Fatalf("edit seed = %q, want markdown with markers", got)
	}

	m.editArea.SetValue("now *italic* here")
	next, cmd := m.commitInlineEdit(m.editArea.Value())
	m = next.(Model)
	if cmd == nil {
		t.Fatal("commit should produce a sync command")
	}
	var italic *notionapi.RichText
	for i := range block.Paragraph.RichText {
		if block.Paragraph.RichText[i].PlainText == "italic" {
			italic = &block.Paragraph.RichText[i]
		}
	}
	if italic == nil || italic.Annotations == nil || !italic.Annotations.Italic {
		t.Errorf("markdown italics should become annotations: %+v", block.Paragraph.RichText)
	}
}

func TestDividerCommandSavesAndExits(t *testing.T) {
	m := paletteTestModel(t, true)
	m = press(t, m, "/", "d", "i", "v")

	entries := m.filteredPalette()
	if len(entries) == 0 || entries[0].title != "Divider" {
		t.Fatalf("filtered = %+v, want Divider first", entries)
	}

	next, cmd := m.updateInlineEdit(key("enter"))
	m = next.(Model)
	if cmd == nil {
		t.Fatal("divider should sync immediately")
	}
	if m.editing {
		t.Error("divider command should leave the editor")
	}
	if m.paletteOpen {
		t.Error("palette should be closed")
	}

	blocks := m.blockCache["page-1"]
	var divider notion.BlockNode
	found := false
	for _, b := range blocks {
		if b.Block.GetType() == "divider" {
			divider, found = b, true
		}
	}
	if !found {
		t.Fatalf("no divider block created: %v", blocks)
	}
	if u, ok := m.pv.current(); !ok || u.ID() != divider.Block.GetID().String() {
		t.Errorf("cursor should sit on the divider, is on %q", func() string {
			u, _ := m.pv.current()
			return u.ID()
		}())
	}
}

func TestPageCommandCreatesAndPromptsCorrectly(t *testing.T) {
	m := paletteTestModel(t, true)
	m.editArea.SetValue("Meeting Notes")
	m.editArea.CursorEnd()

	entries := func() []paletteEntry {
		m.paletteOpen = true
		m.paletteQuery = "page"
		defer func() { m.paletteOpen = false }()
		return m.filteredPalette()
	}()
	if len(entries) == 0 || entries[0].title != "Page" {
		t.Fatalf("palette should offer Page: %+v", entries)
	}
	if entries[0].applyCmd == nil {
		t.Fatal("Page command should be wired to an applyCmd")
	}

	cmd := entries[0].applyCmd(&m)
	if cmd == nil {
		t.Fatal("titled draft should produce a create command directly")
	}
	if m.editing {
		t.Error("page command should leave the editor")
	}
	for _, b := range m.blockCache["page-1"] {
		if b.Block.GetID().String() == draftBlockID {
			t.Error("draft should be removed")
		}
	}

	// empty draft falls through to the title prompt
	m2 := paletteTestModel(t, true)
	cmd = pageCommand(&m2)
	if m2.mode != inputNewPage {
		t.Errorf("empty draft should open the title prompt, mode = %v", m2.mode)
	}
	if m2.newPageParent == nil || m2.newPageParent.ID != "page-1" {
		t.Error("prompt should target the current page as parent")
	}
	_ = cmd
}
