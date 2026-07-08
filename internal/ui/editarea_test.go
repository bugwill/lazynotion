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
