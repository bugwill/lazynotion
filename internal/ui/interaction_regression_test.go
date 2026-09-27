package ui

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jomei/notionapi"

	"github.com/justinm35/lazynotion/internal/notion"
)

func TestEditorSelectionBoldUndoAndSaveKeepsCJKSpan(t *testing.T) {
	m := interactionEditor(t, "中文：後文")
	m.moveEditCursor(0)
	m = interactionKey(t, m, tea.KeyMsg{Type: tea.KeyCtrlV})
	for range 3 {
		m = interactionKey(t, m, tea.KeyMsg{Type: tea.KeyShiftRight})
	}

	if m.editArea.Value() != "中文：後文" || m.editAnchor != 0 || m.editCursorIndex() != 3 {
		t.Fatalf("range selection changed text or position: value=%q anchor=%d cursor=%d", m.editArea.Value(), m.editAnchor, m.editCursorIndex())
	}
	visible, reverse := ansiAttributeShape(t, strings.Join(m.editLines(), "\n"), 7, 27)
	if visible == "" || len(reverse) == 0 {
		t.Fatal("edit selection did not produce visible terminal text")
	}
	if !strings.Contains(visible, "中文：後文") {
		t.Fatalf("edit ANSI text lost the original characters: %q", visible)
	}
	for i := 0; i < 3; i++ {
		if !reverse[i] {
			t.Errorf("selected rune %d did not render with reverse video", i)
		}
	}
	if len(reverse) < 5 {
		t.Fatalf("rendered only %d runes, want the full text", len(reverse))
	}
	if reverse[4] {
		t.Error("unselected trailing rune inherited the selection style")
	}

	m = interactionKey(t, m, interactionRune('b'))
	if got := m.editArea.Value(); got != "**中文：**後文" {
		t.Fatalf("bold value = %q", got)
	}
	m = interactionKey(t, m, interactionRune('u'))
	if m.editArea.Value() != "中文：後文" || m.editAnchor != 0 || m.editCursorIndex() != 3 || m.editSelectionLength() != 3 {
		t.Fatalf("u did not restore text and selection: value=%q anchor=%d cursor=%d length=%d", m.editArea.Value(), m.editAnchor, m.editCursorIndex(), m.editSelectionLength())
	}

	m = interactionKey(t, m, interactionRune('b'))
	m = interactionKey(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.editing {
		t.Fatal("Escape should save and close the editor")
	}
	if len(m.blockCache["page-1"]) != 1 {
		t.Fatalf("page block count = %d", len(m.blockCache["page-1"]))
	}
	richText, ok := notion.LocalRichText(m.blockCache["page-1"][0].Block)
	if !ok || len(richText) != 2 || richText[0].PlainText != "中文：" || richText[0].Annotations == nil || !richText[0].Annotations.Bold || richText[1].PlainText != "後文" || (richText[1].Annotations != nil && richText[1].Annotations.Bold) {
		t.Fatalf("saved rich text does not preserve the exact bold span: %+v", richText)
	}
}

func TestEditorCJKSoftWrapAndExplicitNewlineSurviveFormatUndo(t *testing.T) {
	firstLine := strings.Repeat("界", 30)
	value := firstLine + "\n尾"
	m := interactionEditor(t, value)
	m.resizeEditArea()
	if rows := m.editAreaRows(); rows < 3 {
		t.Fatalf("test fixture needs a soft-wrapped CJK line and explicit newline, has %d rows", rows)
	}
	m.moveEditCursor(0)
	m = interactionKey(t, m, tea.KeyMsg{Type: tea.KeyCtrlW})
	if m.editSelectionLength() != len([]rune(firstLine)) || m.editCursorIndex() != len([]rune(firstLine)) {
		t.Fatalf("word selection across CJK soft wraps = %d chars at %d, want %d", m.editSelectionLength(), m.editCursorIndex(), len([]rune(firstLine)))
	}
	m = interactionKey(t, m, interactionRune('b'))
	if got := m.editArea.Value(); got != "**"+firstLine+"**\n尾" {
		t.Fatalf("formatting altered the explicit newline: %q", got)
	}
	m = interactionKey(t, m, interactionRune('u'))
	if m.editArea.Value() != value || m.editCursorIndex() != len([]rune(firstLine)) || m.editAnchor != 0 || m.editSelectionLength() != len([]rune(firstLine)) {
		t.Fatalf("undo did not restore CJK text, cursor, and selection: value=%q cursor=%d anchor=%d selection=%d", m.editArea.Value(), m.editCursorIndex(), m.editAnchor, m.editSelectionLength())
	}
}

func TestEditorShortcutLettersTypeNormallyAndCtrlZUndoesText(t *testing.T) {
	m := interactionEditor(t, "body")
	m.editArea.CursorEnd()
	m = interactionKey(t, m, interactionRune('b'))
	m = interactionKey(t, m, interactionRune('u'))
	if got := m.editArea.Value(); got != "bodybu" {
		t.Fatalf("unselected b/u should be typed literally, got %q", got)
	}
	m = interactionKey(t, m, tea.KeyMsg{Type: tea.KeyCtrlZ})
	if got := m.editArea.Value(); got != "bodyb" {
		t.Fatalf("first ctrl+z value = %q, want bodyb", got)
	}
	m = interactionKey(t, m, tea.KeyMsg{Type: tea.KeyCtrlZ})
	if got := m.editArea.Value(); got != "body" {
		t.Fatalf("second ctrl+z value = %q, want body", got)
	}
}

func TestEscapeSavesClearAndPaletteEdits(t *testing.T) {
	t.Run("clearing existing text is a save", func(t *testing.T) {
		m := interactionEditor(t, "existing")
		m.editArea.SetValue("")
		next, cmd := m.updateInlineEdit(tea.KeyMsg{Type: tea.KeyEsc})
		m = next.(Model)
		if cmd == nil || m.editing {
			t.Fatalf("Escape should save the clear and close edit (cmd=%v editing=%v)", cmd != nil, m.editing)
		}
		text, ok := notion.LocalRichText(m.blockCache["page-1"][0].Block)
		if !ok || len(text) != 0 {
			t.Fatalf("cleared rich text = %+v, want no text runs", text)
		}
	})

	t.Run("Escape commits while the palette is open", func(t *testing.T) {
		m := interactionEditor(t, "before")
		m.editArea.SetValue("after")
		m.paletteOpen = true
		next, cmd := m.updateInlineEdit(tea.KeyMsg{Type: tea.KeyEsc})
		m = next.(Model)
		text, ok := notion.LocalRichText(m.blockCache["page-1"][0].Block)
		if cmd == nil || m.editing || m.paletteOpen || !ok || len(text) != 1 || text[0].PlainText != "after" {
			t.Fatalf("palette Escape did not save and close cleanly: editing=%v palette=%v richtext=%+v", m.editing, m.paletteOpen, text)
		}
	})
}

func TestCtrlDDiscardsExistingPaletteAndDraftEdits(t *testing.T) {
	t.Run("existing block", func(t *testing.T) {
		m := interactionEditor(t, "original")
		m.editArea.SetValue("changed")
		next, _ := m.updateInlineEdit(tea.KeyMsg{Type: tea.KeyCtrlD})
		m = next.(Model)
		text, _ := notion.LocalRichText(m.blockCache["page-1"][0].Block)
		if m.editing || text[0].PlainText != "original" {
			t.Fatalf("discard changed the saved block: editing=%v text=%+v", m.editing, text)
		}
	})

	t.Run("palette edit", func(t *testing.T) {
		m := interactionEditor(t, "original")
		m.editArea.SetValue("changed")
		m.paletteOpen = true
		next, _ := m.updateInlineEdit(tea.KeyMsg{Type: tea.KeyCtrlD})
		m = next.(Model)
		text, _ := notion.LocalRichText(m.blockCache["page-1"][0].Block)
		if m.editing || m.paletteOpen || text[0].PlainText != "original" {
			t.Fatalf("palette discard changed state: editing=%v palette=%v text=%+v", m.editing, m.paletteOpen, text)
		}
	})

	t.Run("draft block", func(t *testing.T) {
		m := commitTestModel(t, &notionapi.ParagraphBlock{
			BasicBlock: notionapi.BasicBlock{ID: "existing", Type: "paragraph"},
			Paragraph:  notionapi.Paragraph{RichText: []notionapi.RichText{{PlainText: "kept"}}},
		})
		next, _ := m.newBlockBelow()
		m = next.(Model)
		if !m.editing {
			t.Fatal("new block should start in the editor")
		}
		m.editArea.SetValue("draft text")
		next, _ = m.updateInlineEdit(tea.KeyMsg{Type: tea.KeyCtrlD})
		m = next.(Model)
		if m.editing || len(m.blockCache["page-1"]) != 1 || m.blockCache["page-1"][0].Block.GetID().String() != "existing" {
			t.Fatalf("draft discard left a local block behind: editing=%v blocks=%v", m.editing, topIDs(m))
		}
	})
}

func TestPageMouseScrollKeepsTheBlockCursorAndIgnoresFooter(t *testing.T) {
	m := New([]Workspace{{Name: "test"}}, 0, nil, "mosaic")
	m.width, m.height = 80, 24
	m.layout()
	pages := []notion.Page{{ID: "home", Title: "Home"}}
	next, _ := m.Update(pagesMsg{pages: pages})
	m = next.(Model)
	blocks := make([]notion.BlockNode, 40)
	for i := range blocks {
		blocks[i] = notion.BlockNode{Block: &notionapi.ParagraphBlock{
			BasicBlock: notionapi.BasicBlock{ID: notionapi.BlockID(fmt.Sprintf("b-%02d", i)), Type: "paragraph"},
			Paragraph:  notionapi.Paragraph{RichText: []notionapi.RichText{{PlainText: fmt.Sprintf("content line %02d with enough text", i)}}},
		}}
	}
	m.blockCache["home"] = blocks
	m.sidebar.Select(1) // Recent is pinned before root pages.
	next, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if m.selected == nil || m.selected.ID != "home" || m.focus != focusViewer || m.sidebar.Width() != 0 || m.viewer.Width != m.width-2 {
		t.Fatalf("opening a root page did not fill the viewer: selected=%+v focus=%v sidebar=%d viewer=%d", m.selected, m.focus, m.sidebar.Width(), m.viewer.Width)
	}
	if m.pv.cursor != 0 || m.viewer.YOffset != 0 {
		t.Fatalf("page should open at its top, cursor=%d offset=%d", m.pv.cursor, m.viewer.YOffset)
	}

	for range 2 {
		next, _ = m.Update(tea.MouseMsg{X: 8, Y: 6, Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
		m = next.(Model)
	}
	if m.viewer.YOffset != 2 {
		t.Fatalf("mouse wheel did not scroll exactly two rows: offset=%d", m.viewer.YOffset)
	}
	if m.pv.cursor != 0 {
		t.Fatalf("mouse scrolling moved the block cursor to %d", m.pv.cursor)
	}
	next, _ = m.Update(tea.MouseMsg{X: 8, Y: 6, Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	m = next.(Model)
	if m.viewer.YOffset != 1 {
		t.Fatalf("reverse content wheel offset = %d, want 1", m.viewer.YOffset)
	}
	before := m.viewer.YOffset
	next, _ = m.Update(tea.MouseMsg{X: 8, Y: m.height - 1, Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	m = next.(Model)
	if m.viewer.YOffset != before {
		t.Fatalf("footer mouse event scrolled the page: offset %d -> %d", before, m.viewer.YOffset)
	}
}

func TestRecentAndDatabaseMouseScrollIgnoreFooter(t *testing.T) {
	pages := make([]notion.Page, 12)
	for i := range pages {
		pages[i] = notion.Page{ID: fmt.Sprintf("p-%02d", i), Title: fmt.Sprintf("Page %02d", i)}
	}
	recent := New([]Workspace{{Name: "test"}}, 0, nil, "mosaic")
	recent.width, recent.height = 80, 24
	recent.layout()
	recent.selected = &recentPage
	recent.recent = &recentState{pages: pages, cursor: 5, loaded: true}
	recent.focus = focusViewer
	next, _ := recent.Update(tea.MouseMsg{X: 8, Y: 6, Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	recent = next.(Model)
	if recent.recent.cursor != 6 {
		t.Fatalf("Recent wheel cursor = %d, want 6", recent.recent.cursor)
	}
	before := recent.recent.cursor
	next, _ = recent.Update(tea.MouseMsg{X: 8, Y: recent.height - 1, Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	recent = next.(Model)
	if recent.recent.cursor != before {
		t.Fatalf("footer mouse event moved Recent cursor: %d -> %d", before, recent.recent.cursor)
	}

	db := New([]Workspace{{Name: "test"}}, 0, nil, "mosaic")
	db.width, db.height = 80, 24
	db.layout()
	page := notion.Page{ID: "database", Title: "Database", Kind: notion.KindDataSource}
	db.selected = &page
	db.db = &dbState{ref: page, rows: make([]notion.Row, 12), cursor: 5}
	db.focus = focusViewer
	next, _ = db.Update(tea.MouseMsg{X: 8, Y: 6, Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	db = next.(Model)
	if db.db.cursor != 6 {
		t.Fatalf("database wheel cursor = %d, want 6", db.db.cursor)
	}
	before = db.db.cursor
	next, _ = db.Update(tea.MouseMsg{X: 8, Y: db.height - 1, Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	db = next.(Model)
	if db.db.cursor != before {
		t.Fatalf("footer mouse event moved database cursor: %d -> %d", before, db.db.cursor)
	}
}

func TestRootListMouseWheelChangesSelectionAndIgnoresFooter(t *testing.T) {
	m := New([]Workspace{{Name: "test"}}, 0, nil, "mosaic")
	m.width, m.height = 80, 24
	m.layout()
	pages := []notion.Page{
		{ID: "one", Title: "One"},
		{ID: "two", Title: "Two"},
		{ID: "three", Title: "Three"},
		{ID: "four", Title: "Four"},
	}
	next, _ := m.Update(pagesMsg{pages: pages})
	m = next.(Model)
	m.sidebar.Select(0)
	next, _ = m.Update(tea.MouseMsg{X: 8, Y: 6, Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	m = next.(Model)
	if m.sidebar.Index() != 1 {
		t.Fatalf("root-list wheel index = %d, want 1", m.sidebar.Index())
	}
	next, _ = m.Update(tea.MouseMsg{X: 8, Y: 6, Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	m = next.(Model)
	if m.sidebar.Index() != 0 {
		t.Fatalf("root-list reverse wheel index = %d, want 0", m.sidebar.Index())
	}
	next, _ = m.Update(tea.MouseMsg{X: 8, Y: m.height - 1, Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	m = next.(Model)
	if m.sidebar.Index() != 0 {
		t.Fatalf("footer mouse event moved the root-list selection to %d", m.sidebar.Index())
	}
}

func TestMouseInputDuringEditingDoesNotMoveOrReplaceText(t *testing.T) {
	m := interactionEditor(t, "edit body")
	m.editArea.CursorStart()
	beforeCursor := m.editCursorIndex()
	beforeText := m.editArea.Value()
	next, _ := m.Update(tea.MouseMsg{X: 8, Y: 6, Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	m = next.(Model)
	if !m.editing || m.editArea.Value() != beforeText || m.editCursorIndex() != beforeCursor {
		t.Fatalf("mouse input changed the active edit: editing=%v value=%q cursor=%d", m.editing, m.editArea.Value(), m.editCursorIndex())
	}
}

func TestBubbleTeaDecodesSGRWheelAndEnablesBothMouseModes(t *testing.T) {
	inputReader, inputWriter := io.Pipe()
	defer inputReader.Close()
	defer inputWriter.Close()
	var output bytes.Buffer
	pages := make([]notion.Page, 10)
	for i := range pages {
		pages[i] = notion.Page{ID: fmt.Sprintf("p-%02d", i), Title: fmt.Sprintf("Page %02d", i)}
	}
	model := New([]Workspace{{Name: "test"}}, 0, nil, "mosaic")
	model.width, model.height = 80, 24
	model.selected = &recentPage
	model.recent = &recentState{pages: pages, cursor: 5, loaded: true}
	model.focus = focusViewer
	model.layout()
	probe := mouseProtocolProbe{model: model}
	program := tea.NewProgram(probe,
		tea.WithInput(inputReader),
		tea.WithOutput(&output),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
		tea.WithoutSignalHandler(),
	)
	resultCh := make(chan struct {
		model tea.Model
		err   error
	}, 1)
	go func() {
		final, err := program.Run()
		resultCh <- struct {
			model tea.Model
			err   error
		}{final, err}
	}()

	writeCh := make(chan error, 1)
	go func() {
		_, err := io.WriteString(inputWriter, "\x1b[<64;9;7M\x1b[<65;9;7M")
		writeCh <- err
	}()

	var result struct {
		model tea.Model
		err   error
	}
	select {
	case result = <-resultCh:
	case <-time.After(5 * time.Second):
		program.Kill()
		t.Fatal("Bubble Tea did not consume both synthetic SGR wheel events")
	}
	if result.err != nil {
		t.Fatalf("Bubble Tea exited with error: %v", result.err)
	}
	select {
	case err := <-writeCh:
		if err != nil {
			t.Fatalf("write SGR mouse input: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("SGR input writer did not finish")
	}

	final, ok := result.model.(mouseProtocolProbe)
	if !ok {
		t.Fatalf("final model type = %T", result.model)
	}
	if len(final.events) != 2 || final.events[0].Button != tea.MouseButtonWheelUp || final.events[1].Button != tea.MouseButtonWheelDown {
		t.Fatalf("parsed mouse events = %+v, want SGR wheel up then down", final.events)
	}
	if final.events[0].X != 8 || final.events[0].Y != 6 || final.events[1].X != 8 || final.events[1].Y != 6 {
		t.Fatalf("parsed SGR coordinates = %+v, want zero-based (8,6)", final.events)
	}
	if len(final.cursors) != 2 || final.cursors[0] != 6 || final.cursors[1] != 5 {
		t.Fatalf("Recent cursor after parsed wheel events = %v, want [6 5]", final.cursors)
	}
	startup := output.String()
	for _, mode := range []string{"\x1b[?1002h", "\x1b[?1006h"} {
		if !strings.Contains(startup, mode) {
			t.Errorf("Bubble Tea startup did not emit mouse mode %q", mode)
		}
	}
}

type mouseProtocolProbe struct {
	model     Model
	events    []tea.MouseMsg
	cursors   []int
	stopAfter int
}

func (m mouseProtocolProbe) Init() tea.Cmd { return nil }

func (m mouseProtocolProbe) View() string { return m.model.View() }

func (m mouseProtocolProbe) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if mouse, ok := msg.(tea.MouseMsg); ok {
		m.events = append(m.events, mouse)
		next, cmd := m.model.Update(mouse)
		m.model = next.(Model)
		if m.model.recent != nil {
			m.cursors = append(m.cursors, m.model.recent.cursor)
		}
		if len(m.events) == max(m.stopAfter, 2) {
			return m, tea.Quit
		}
		return m, cmd
	}
	next, cmd := m.model.Update(msg)
	m.model = next.(Model)
	return m, cmd
}

func interactionEditor(t *testing.T, text string) Model {
	t.Helper()
	block := &notionapi.ParagraphBlock{
		BasicBlock: notionapi.BasicBlock{ID: "edit-block", Type: "paragraph"},
		Paragraph:  notionapi.Paragraph{RichText: []notionapi.RichText{{PlainText: text, Text: &notionapi.Text{Content: text}}}},
	}
	m := commitTestModel(t, block)
	next, _ := m.startInlineEdit()
	m = next.(Model)
	if !m.editing {
		t.Fatal("fixture failed to enter inline edit mode")
	}
	return m
}

func interactionKey(t *testing.T, m Model, key tea.KeyMsg) Model {
	t.Helper()
	next, _ := m.updateInlineEdit(key)
	model, ok := next.(Model)
	if !ok {
		t.Fatalf("inline update returned %T", next)
	}
	return model
}

func interactionRune(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}
