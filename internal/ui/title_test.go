package ui

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func TestTitleClickPrefillsAndEscapeCancels(t *testing.T) {
	m := readingTestModel(t, strings.Repeat("reading content ", 200))
	m.viewer.SetYOffset(10)
	next, _ := m.Update(tea.MouseMsg{X: 3, Y: 0, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m = next.(Model)
	if m.mode != inputTitle || m.input.Value() != m.selected.Title || m.input.Position() != 0 || m.viewer.YOffset != 10 {
		t.Fatal("title click did not open prefilled input without scrolling")
	}
	m.input.SetValue("changed")
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)
	if m.mode != inputNone || m.selected.Title != "p" || cmd != nil || m.writesInFlight != 0 {
		t.Fatal("escape modified the title or started a save")
	}
}

func TestTitleClickIgnoresBorderAndOverlays(t *testing.T) {
	for _, x := range []int{1, 4, 40} {
		m := readingTestModel(t, "text")
		next, _ := m.Update(tea.MouseMsg{X: x, Y: 0, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
		if next.(Model).mode != inputNone {
			t.Fatalf("border click at %d opened title editor", x)
		}
	}
	m := readingTestModel(t, "text")
	m.showHelp = true
	if m.titleClicked(tea.MouseMsg{X: 3, Y: 0, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}) {
		t.Fatal("help overlay allowed title editing")
	}
}

func TestTitleSaveUpdatesOnlyAfterSuccess(t *testing.T) {
	for _, fail := range []bool{false, true} {
		m := readingTestModel(t, strings.Repeat("reading content ", 200))
		m.viewer.SetYOffset(10)
		m.sidebar.SetItems([]list.Item{pageItem{page: *m.selected}})
		next, cmd := m.submitInput(inputTitle, "new title")
		m = next.(Model)
		if cmd == nil || m.selected.Title != "p" || m.writesInFlight != 1 {
			t.Fatal("save should wait for API success")
		}
		page := *m.selected
		page.Title = "new title"
		msg := titleSavedMsg{page: page, wsGen: m.wsGen}
		if fail {
			msg.err = errors.New("permission denied")
		}
		next, _ = m.Update(msg)
		m = next.(Model)
		want := "new title"
		if fail {
			want = "p"
		}
		if m.selected.Title != want || m.sidebar.Items()[0].(pageItem).page.Title != want || m.writesInFlight != 0 || m.viewer.YOffset != 10 {
			t.Fatalf("unexpected completion: title=%q offset=%d busy=%d", m.selected.Title, m.viewer.YOffset, m.writesInFlight)
		}
	}
}

func TestTitleSaveIgnoresEmptyUnchangedAndStaleWorkspace(t *testing.T) {
	for _, value := range []string{" ", "p"} {
		m := readingTestModel(t, "text")
		next, cmd := m.submitInput(inputTitle, value)
		if cmd != nil || next.(Model).writesInFlight != 0 {
			t.Fatalf("unnecessary save for %q", value)
		}
	}
	m := readingTestModel(t, "text")
	m.writesInFlight = 1
	page := *m.selected
	page.Title = "other workspace title"
	next, _ := m.Update(titleSavedMsg{page: page, wsGen: m.wsGen - 1})
	m = next.(Model)
	if m.selected.Title != "p" || m.writesInFlight != 0 {
		t.Fatal("stale workspace completion changed title or left save in flight")
	}
}

func TestTitleCaretIsBlackVerticalLineAtInsertionPosition(t *testing.T) {
	profile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(profile) })
	m := readingTestModel(t, "text")
	next, _ := m.startTitleEdit()
	m = next.(Model)
	m.input.SetValue("中文Title")
	for _, tc := range []struct {
		position int
		want     string
	}{
		{0, "title: │中文Title"},
		{2, "title: 中文│Title"},
		{7, "title: 中文Title│"},
	} {
		m.input.SetCursor(tc.position)
		rendered := m.statusLine()
		if ansi.Strip(rendered) != tc.want || !strings.Contains(rendered, "38;2;0;0;0") || strings.Contains(rendered, "48;") || strings.Contains(rendered, "[7") {
			t.Fatalf("unexpected caret at %d: %q", tc.position, rendered)
		}
	}
	updated, _ := m.input.Update(cursor.BlinkMsg{})
	m.input = updated
	if !strings.Contains(ansi.Strip(m.statusLine()), "│") || m.input.Value() != "中文Title" || updated.Cursor.Mode() != cursor.CursorHide {
		t.Fatal("caret disappeared or changed the title value")
	}
	next, _ = m.startInput(inputFind, "find: ", "")
	if next.(Model).input.Cursor.Mode() != cursor.CursorBlink {
		t.Fatal("title cursor setting leaked into another input")
	}
}

func TestLongTitleCaretStaysVisibleWithinFooter(t *testing.T) {
	m := readingTestModel(t, "text")
	next, _ := m.startTitleEdit()
	m = next.(Model)
	m.width = 20
	value := strings.Repeat("中文内容", 30)
	m.input.SetValue(value)
	for _, position := range []int{0, 50, len([]rune(value))} {
		m.input.SetCursor(position)
		view := m.statusLine()
		if !strings.Contains(ansi.Strip(view), "│") || ansi.StringWidth(view) > m.width || m.input.Value() != value {
			t.Fatalf("long-title caret clipped or title changed: %q", view)
		}
	}
}
