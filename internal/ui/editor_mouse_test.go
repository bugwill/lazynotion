package ui

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/justinm35/lazynotion/internal/notion"
)

func editorMouse(t *testing.T, m Model, x, y int, action tea.MouseAction) Model {
	t.Helper()
	next, _ := m.Update(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: action})
	return next.(Model)
}

func editorTop(m Model) int { return 1 + m.pv.starts[m.pv.cursor] - m.viewer.YOffset }

func TestMouseDragSelectsCJKAndBoldUndo(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		m := interactionEditor(t, "中文：后文")
		top := editorTop(m)
		start, end := 3, 9 // border + gutter, then three two-cell characters
		if reverse {
			start, end = end, start
		}
		m = editorMouse(t, m, start, top, tea.MouseActionPress)
		m = editorMouse(t, m, end, top, tea.MouseActionMotion)
		m = editorMouse(t, m, end, top, tea.MouseActionRelease)
		if m.editSelectionLength() != 3 || m.editDragging {
			t.Fatalf("drag selection length=%d, dragging=%v", m.editSelectionLength(), m.editDragging)
		}
		visible, selected := ansiAttributeShape(t, strings.Join(m.editLines(), "\n"), 7, 27)
		if !strings.Contains(visible, "中文：后文") || !selected[0] || !selected[1] || !selected[2] || selected[3] {
			t.Fatalf("drag highlight did not match selection: %q / %v", visible, selected)
		}
		m = interactionKey(t, m, interactionRune('b'))
		if m.editArea.Value() != "**中文：**后文" {
			t.Fatalf("drag bold = %q", m.editArea.Value())
		}
		m = interactionKey(t, m, interactionRune('u'))
		if m.editArea.Value() != "中文：后文" || m.editSelectionLength() != 3 {
			t.Fatalf("drag undo lost content/selection: %q", m.editArea.Value())
		}
	}
}

func TestMouseSelectionAcrossSoftWrapAndNewline(t *testing.T) {
	m := interactionEditor(t, "中文内容\n尾字")
	m.editArea.SetWidth(4)
	m.resizeEditArea()
	m.moveEditCursor(0)
	m.settleEditScroll()
	m.syncViewer()
	top := editorTop(m)
	m = editorMouse(t, m, 3, top, tea.MouseActionPress)
	rows := editorDisplayRows(m.editArea.Value(), 4, 0, 0)
	m = editorMouse(t, m, 7, top+len(rows)-1, tea.MouseActionMotion)
	m = editorMouse(t, m, 7, top+len(rows)-1, tea.MouseActionRelease)
	if m.editAnchor != 0 || m.editCursorIndex() != len([]rune("中文内容\n尾字")) {
		t.Fatalf("wrapped multiline selection = %d..%d", m.editAnchor, m.editCursorIndex())
	}
	m = interactionKey(t, m, interactionRune('b'))
	if m.editArea.Value() != "**中文内容\n尾字**" {
		t.Fatalf("multiline drag bold = %q", m.editArea.Value())
	}
	next, _ := m.updateInlineEdit(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)
	runs, _ := notion.LocalRichText(m.blockCache["page-1"][0].Block)
	var saved strings.Builder
	for _, run := range runs {
		saved.WriteString(run.PlainText)
		if run.Annotations == nil || !run.Annotations.Bold {
			t.Fatalf("multiline selected text saved without bold: %+v", runs)
		}
	}
	if saved.String() != "中文内容\n尾字" {
		t.Fatalf("saved multiline drag changed content: %q", saved.String())
	}
}

func TestSGRMouseDragSelectsThroughActualInputParser(t *testing.T) {
	m := interactionEditor(t, "中文：后文")
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	var output bytes.Buffer
	program := tea.NewProgram(mouseProtocolProbe{model: m, stopAfter: 3},
		tea.WithInput(reader), tea.WithOutput(&output), tea.WithAltScreen(),
		tea.WithMouseCellMotion(), tea.WithoutSignalHandler())
	type result struct {
		model tea.Model
		err   error
	}
	done := make(chan result, 1)
	go func() { model, err := program.Run(); done <- result{model, err} }()
	y := editorTop(m) + 1 // SGR coordinates are one-based
	written := make(chan error, 1)
	go func() {
		_, err := fmt.Fprintf(writer, "\x1b[<0;4;%dM\x1b[<32;10;%dM\x1b[<0;10;%dm", y, y, y)
		written <- err
	}()
	select {
	case r := <-done:
		if r.err != nil {
			t.Fatal(r.err)
		}
		final := r.model.(mouseProtocolProbe)
		if len(final.events) != 3 || final.model.editSelectionLength() != 3 || final.model.editDragging {
			t.Fatalf("parsed drag did not select three CJK characters: %+v", final.events)
		}
		final.model = interactionKey(t, final.model, interactionRune('b'))
		if final.model.editArea.Value() != "**中文：**后文" {
			t.Fatal(final.model.editArea.Value())
		}
	case <-time.After(5 * time.Second):
		program.Kill()
		t.Fatal("SGR drag was not consumed")
	}
	if err := <-written; err != nil {
		t.Fatal(err)
	}
}

func TestMouseSelectionUsesScrolledRowsAndGraphemeBoundaries(t *testing.T) {
	m := interactionEditor(t, strings.Repeat("同一行\n", 35)+"á🙂中文")
	m.moveEditCursor(len([]rune(m.editArea.Value())))
	m.settleEditScroll()
	m.syncViewer()
	if m.editScroll == 0 {
		t.Fatal("fixture did not scroll inside editor")
	}
	// The repeated rows above cannot be identified reliably by matching text.
	rows := editorDisplayRows(m.editArea.Value(), m.editArea.Width(), 0, 0)
	y := editorTop(m) + len(rows) - 1 - m.editScroll
	m = editorMouse(t, m, 4, y, tea.MouseActionPress)  // after a + combining mark
	m = editorMouse(t, m, 6, y, tea.MouseActionMotion) // after emoji
	m = editorMouse(t, m, 6, y, tea.MouseActionRelease)
	if m.editSelectionLength() != 1 {
		t.Fatalf("emoji selection = %d runes", m.editSelectionLength())
	}
	m = interactionKey(t, m, interactionRune('b'))
	if !strings.HasSuffix(m.editArea.Value(), "á**🙂**中文") {
		t.Fatalf("scrolled drag split a grapheme or selected wrong row: %q", m.editArea.Value())
	}
}

func TestMouseClickAndFooterDoNotFormatOrModifyText(t *testing.T) {
	m := interactionEditor(t, "body")
	m = editorMouse(t, m, 3, editorTop(m), tea.MouseActionPress)
	m = editorMouse(t, m, 3, editorTop(m), tea.MouseActionRelease)
	m = interactionKey(t, m, interactionRune('b'))
	if m.editArea.Value() != "bbody" {
		t.Fatalf("plain click treated b as formatting: %q", m.editArea.Value())
	}
	before := m.editCursorIndex()
	m = editorMouse(t, m, 3, m.height-1, tea.MouseActionPress)
	if m.editCursorIndex() != before || m.editDragging {
		t.Fatal("footer click changed editor selection")
	}
}

func TestMouseDragBeyondEditorScrollsAndTracksVisibleRows(t *testing.T) {
	var lines []string
	for i := range 40 {
		lines = append(lines, fmt.Sprintf("row-%02d 中文", i))
	}
	m := interactionEditor(t, strings.Join(lines, "\n"))
	// Initially the long edit shows its first rows. Dragging past the box
	// must extend into subsequent rows without matching duplicate text.
	m = editorMouse(t, m, 3, editorTop(m), tea.MouseActionPress)
	for range 25 {
		y := editorTop(m) + m.editArea.Height()
		m = editorMouse(t, m, 3, y, tea.MouseActionMotion)
	}
	if m.editScroll == 0 || m.editAnchor != 0 || m.editCursorIndex() < len([]rune(strings.Join(lines[:20], "\n"))) {
		t.Fatalf("drag did not scroll/extend: offset=%d selection=%d..%d", m.editScroll, m.editAnchor, m.editCursorIndex())
	}
	m = editorMouse(t, m, 3, editorTop(m)+m.editArea.Height()-1, tea.MouseActionRelease)
	for _, key := range []tea.KeyType{tea.KeyCtrlHome, tea.KeyCtrlEnd, tea.KeyUp, tea.KeyUp, tea.KeyCtrlHome} {
		m = interactionKey(t, m, tea.KeyMsg{Type: key})
		rows := editorDisplayRows(m.editArea.Value(), m.editArea.Width(), 0, 0)
		actualTop := strings.Split(stripAnsi(m.editArea.View()), "\n")[0]
		if !strings.HasPrefix(actualTop, rows[m.editScroll].text) {
			t.Fatalf("mouse row mapping drifted after %v: offset=%d actual=%q expected=%q", key, m.editScroll, actualTop, rows[m.editScroll].text)
		}
	}
}
