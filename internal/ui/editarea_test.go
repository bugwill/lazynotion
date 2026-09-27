package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestEnterInEditKeepsFirstLineVisible(t *testing.T) {
	m := New([]Workspace{{Name: "test"}}, 0, nil, "mosaic")
	m.viewer.Width = 40
	m.editing = true
	m.editArea.SetWidth(38)
	m.editArea.SetValue("first line")
	m.editArea.CursorEnd()
	m.editArea.Focus()
	m.resizeEditArea()
	// the real app renders every frame; a render before the keypress is
	// what arms the textarea's internal scroll that caused the bug
	_ = m.editArea.View()

	next, _ := m.updateInlineEdit(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	next, _ = m.updateInlineEdit(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("second")})
	m = next.(Model)

	if got := m.editArea.Value(); got != "first line\nsecond" {
		t.Fatalf("value = %q, want two lines", got)
	}
	visible := stripAnsi(strings.Join(m.editLines(), "\n"))
	if !strings.Contains(visible, "first line") {
		t.Errorf("first line scrolled out of view:\n%s", visible)
	}
	if !strings.Contains(visible, "second") {
		t.Errorf("second line missing from view:\n%s", visible)
	}
}

func TestNoLoadingFlashOnSamePageReload(t *testing.T) {
	m := New([]Workspace{{Name: "test"}}, 0, nil, "mosaic")
	m.viewer.Width = 40
	m.viewer.Height = 10
	page := notionPageForTest("page-a")
	m.selected = &page
	m.pageLoading = true

	m.renderedID = "page-a"
	if got := stripAnsi(m.viewerContent()); strings.Contains(got, "loading page") {
		t.Error("same-page reload should keep content, not show the loading placeholder")
	}

	m.renderedID = "page-b"
	if got := stripAnsi(m.viewerContent()); !strings.Contains(got, "loading page") {
		t.Error("switching to an unloaded page should show the loading placeholder")
	}
}

func TestInlineEditorBoldSelectionUsesRuneOffsetsAndUndo(t *testing.T) {
	m := New([]Workspace{{Name: "test"}}, 0, nil, "mosaic")
	m.editing = true
	m.editArea.SetWidth(20)
	m.editArea.SetValue("反思")
	m.editArea.CursorEnd()
	m.editArea.Focus()

	apply := func(key tea.KeyMsg) {
		t.Helper()
		next, _ := m.updateInlineEdit(key)
		m = next.(Model)
	}
	apply(tea.KeyMsg{Type: tea.KeyCtrlV})
	apply(tea.KeyMsg{Type: tea.KeyLeft})
	if m.editAnchor != 2 || m.editCursorIndex() != 1 {
		t.Fatalf("CJK selection offsets = (%d, %d), want (2, 1)", m.editAnchor, m.editCursorIndex())
	}
	apply(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	if got := m.editArea.Value(); got != "反**思**" {
		t.Fatalf("bolded value = %q", got)
	}
	apply(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	if got := m.editArea.Value(); got != "反思" {
		t.Fatalf("undo value = %q, want original CJK text", got)
	}
	if m.editAnchor != 2 || m.editCursorIndex() != 1 {
		t.Fatalf("undo selection offsets = (%d, %d), want (2, 1)", m.editAnchor, m.editCursorIndex())
	}
}

func TestInlineEditorPlainKeysAndUndoRemainEditingKeys(t *testing.T) {
	m := New([]Workspace{{Name: "test"}}, 0, nil, "mosaic")
	m.editing = true
	m.editArea.SetWidth(20)
	m.editArea.SetValue("abc")
	m.editArea.CursorEnd()
	m.editArea.Focus()

	apply := func(key tea.KeyMsg) {
		t.Helper()
		next, _ := m.updateInlineEdit(key)
		m = next.(Model)
	}
	apply(tea.KeyMsg{Type: tea.KeyLeft})
	if m.editAnchor != -1 {
		t.Fatalf("plain cursor movement started a selection at %d", m.editAnchor)
	}
	apply(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("bu")})
	if got := m.editArea.Value(); got != "abbuc" {
		t.Fatalf("plain b/u input = %q, want inserted characters", got)
	}
	apply(tea.KeyMsg{Type: tea.KeyCtrlZ})
	if got := m.editArea.Value(); got != "abc" {
		t.Fatalf("ctrl+z value = %q, want restored text", got)
	}
}

func TestInlineEditorUnboldDoesNotPanic(t *testing.T) {
	m := New([]Workspace{{Name: "test"}}, 0, nil, "mosaic")
	m.editing = true
	m.editArea.SetWidth(20)
	m.editArea.SetValue("**文字**")
	m.moveEditCursor(4)
	m.editAnchor = 2

	next, _ := m.updateInlineEdit(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	m = next.(Model)
	if got := m.editArea.Value(); got != "文字" {
		t.Fatalf("unbold value = %q", got)
	}
}

func TestInlineEditorSelectionHighlightTracksCJKSoftWrap(t *testing.T) {
	const value = "中文内容"
	view := "中\n文\n内\n容"
	got := highlightEditSelection(view, value, 2, 1, 3)
	if stripAnsi(got) != view {
		t.Fatalf("highlight changed visible text: %q", stripAnsi(got))
	}
	if !strings.Contains(got, "\x1b[7m文\x1b[27m") || !strings.Contains(got, "\x1b[7m内\x1b[27m") {
		t.Fatalf("selected CJK soft-wrap rows are not visibly highlighted: %q", got)
	}
}
