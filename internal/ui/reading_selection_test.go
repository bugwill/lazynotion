package ui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/jomei/notionapi"
	"github.com/rivo/uniseg"

	"github.com/justinm35/lazynotion/internal/notion"
)

func readingTestModel(t *testing.T, text string) Model {
	m := commitTestModel(t, para("text", text).Block)
	m.loading, m.pageLoading, m.pulsing = false, false, true
	m.focus = focusViewer
	m.layout()
	m.rebuildPage(true)
	return m
}

func readingCoordinate(t *testing.T, m Model, unit, offset int) (int, int) {
	t.Helper()
	runs, _ := notion.LocalRichText(m.pv.units[unit].Node.Block)
	var text strings.Builder
	for _, r := range runs {
		text.WriteString(richTextContent(r))
	}
	mapped, ok := mapReadingText(m.pv.units[unit].ID(), text.String(), m.pv.rendered[unit])
	if !ok {
		t.Fatalf("cannot map %q to %q", text.String(), m.pv.rendered[unit])
	}
	for row, positions := range mapped.positions {
		plain := []rune(ansi.Strip(m.pv.rendered[unit][row]))
		for col, p := range positions {
			if p == offset {
				return 1 + gutterWidth + uniseg.StringWidth(string(plain[:col])), 1 + m.pv.starts[unit] + row - m.viewer.YOffset
			}
			if p == offset-1 && offset == len([]rune(text.String())) {
				return 1 + gutterWidth + uniseg.StringWidth(string(plain[:col+1])), 1 + m.pv.starts[unit] + row - m.viewer.YOffset
			}
		}
	}
	t.Fatalf("no visual offset %d in %q", offset, mapped)
	return 0, 0
}

func dragReading(t *testing.T, m Model, fromUnit, from, toUnit, to int) Model {
	t.Helper()
	x, y := readingCoordinate(t, m, fromUnit, from)
	x2, y2 := readingCoordinate(t, m, toUnit, to)
	for _, msg := range []tea.MouseMsg{{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}, {X: x2, Y: y2, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion}, {X: x2, Y: y2, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease}} {
		next, cmd := m.Update(msg)
		m = next.(Model)
		if cmd != nil || m.editing || m.writesInFlight != 0 {
			t.Fatal("selecting text entered editing or started a save")
		}
	}
	return m
}

func TestReadingSelectionBoldCJKPreservesAnnotationsAndSaves(t *testing.T) {
	requests := make(chan []byte, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PATCH" || !strings.HasSuffix(r.URL.Path, "/blocks/text") {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		requests <- body
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"object":"block","id":"text","type":"paragraph","paragraph":{"rich_text":[]}}`)
	}))
	defer server.Close()
	m := readingTestModel(t, "中文：后文")
	m.client = notionClientForTestServer(t, server)
	block := m.blockCache[m.selected.ID][0].Block.(*notionapi.ParagraphBlock)
	block.Paragraph.RichText[0].Annotations = &notionapi.Annotations{Italic: true, Color: "red"}
	block.Paragraph.RichText[0].Text.Link = &notionapi.Link{Url: "https://example.com/"}
	m.rebuildPage(true)
	m = dragReading(t, m, 0, 0, 0, 3)
	if m.readSelection == nil {
		t.Fatal("drag did not select text")
	}
	visible, reverse := ansiAttributeShape(t, m.viewer.View(), 7, 27)
	at := strings.Index(visible, "中文：")
	if at < 0 {
		t.Fatalf("selected text disappeared: %q", visible)
	}
	runeAt := len([]rune(visible[:at]))
	for i := 0; i < 3; i++ {
		if !reverse[runeAt+i] {
			t.Fatalf("selected rune %d not highlighted", i)
		}
	}
	next, cmd := m.Update(interactionRune('b'))
	m = next.(Model)
	if m.editing || cmd == nil || m.writesInFlight != 1 || len(m.blockCache[m.selected.ID]) != 1 {
		t.Fatal("bold did not autosave in reading mode")
	}
	if _, ok := cmd().(writeDoneMsg); !ok {
		t.Fatal("save did not finish")
	}
	var payload struct {
		Paragraph struct {
			RichText []notionapi.RichText `json:"rich_text"`
		} `json:"paragraph"`
	}
	if err := json.Unmarshal(<-requests, &payload); err != nil {
		t.Fatal(err)
	}
	runs := payload.Paragraph.RichText
	if len(runs) != 2 || runs[0].Text.Content != "中文：" || !runs[0].Annotations.Bold || runs[1].Annotations.Bold {
		t.Fatalf("incorrect saved span: %+v", runs)
	}
	for _, run := range runs {
		if !run.Annotations.Italic || run.Annotations.Color != "red" || run.Text.Link.Url != "https://example.com/" {
			t.Fatal("bold lost original formatting or link")
		}
	}
	if len(m.undoStack) != 1 {
		t.Fatal("format did not provide undo")
	}
	if err := m.undoStack[0].apply(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(<-requests, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Paragraph.RichText) != 1 || payload.Paragraph.RichText[0].Annotations.Bold {
		t.Fatal("undo did not restore original annotation")
	}
}

func TestReadingSelectionCrossBlockAndResizeKeepsOffsets(t *testing.T) {
	m := readingTestModel(t, "第一段文字")
	m.blockCache[m.selected.ID] = append(m.blockCache[m.selected.ID], para("second", "第二段文字"))
	m.rebuildPage(true)
	m = dragReading(t, m, 0, 2, 1, 3)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 22})
	m = next.(Model)
	if m.readSelection == nil || m.readSelection.anchor.offset != 2 || m.readSelection.cursor.offset != 3 {
		t.Fatal("keyboard resize lost selection")
	}
	next, cmd := m.Update(interactionRune('b'))
	m = next.(Model)
	if cmd == nil || m.editing || len(m.blockCache[m.selected.ID]) != 2 {
		t.Fatal("cross block bold created or edited blocks")
	}
	for i, node := range m.blockCache[m.selected.ID] {
		runs, _ := notion.LocalRichText(node.Block)
		if len(runs) != 2 {
			t.Fatalf("block %d has %d runs", i, len(runs))
		}
		if i == 0 && (runs[0].Text.Content != "第一" || runs[0].Annotations != nil && runs[0].Annotations.Bold || !runs[1].Annotations.Bold) {
			t.Fatal("first block span wrong")
		}
		if i == 1 && (runs[0].Text.Content != "第二段" || !runs[0].Annotations.Bold || runs[1].Annotations != nil && runs[1].Annotations.Bold) {
			t.Fatal("second block span wrong")
		}
	}
}

func TestReadingMapWrapPrefixesAndGraphemes(t *testing.T) {
	for _, test := range []struct {
		text  string
		lines []string
	}{
		{"1中文：后文", []string{"1. 1中文：", "   后文"}},
		{"中文 e\u0301 👩‍💻 后文", []string{"  中文 e\u0301 👩‍💻 后文"}},
		{"first\nsecond", []string{"first", "", "second"}},
	} {
		mapped, ok := mapReadingText("id", test.text, test.lines)
		if !ok {
			t.Fatalf("mapping failed for %q", test.text)
		}
		for row, positions := range mapped.positions {
			plain := []rune(test.lines[row])
			for i, p := range positions {
				if p >= 0 && !unicodeSpace(plain[i]) && []rune(test.text)[p] != plain[i] {
					t.Fatalf("mismatched source offset %d", p)
				}
			}
		}
	}
	mapped, ok := mapReadingText("id", "e\u0301后", []string{"e\u0301后"})
	if !ok {
		t.Fatal("combining text unmapped")
	}
	if offset, _ := readingRowBoundary("e\u0301后", mapped.positions[0], 1); offset != 2 {
		t.Fatalf("grapheme split at %d", offset)
	}
	if _, ok := mapReadingText("id", "missing text", []string{"text"}); ok {
		t.Fatal("incomplete source alignment accepted")
	}
}

func TestReadingPressSelectsWordAndEscapeCancelsWithoutNavigation(t *testing.T) {
	m := readingTestModel(t, "hello world")
	x, y := readingCoordinate(t, m, 0, 2)
	next, cmd := m.Update(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m = next.(Model)
	if cmd != nil || m.readSelection == nil || m.readSelection.anchor.offset != 0 || m.readSelection.cursor.offset != 5 || m.editing {
		t.Fatal("press did not select the word without editing")
	}
	next, _ = m.Update(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})
	m = next.(Model)
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)
	if cmd != nil || m.selected.ID != "page-1" || m.readSelection != nil {
		t.Fatal("escape did not cancel selection safely")
	}
}

func TestReadingUnderlineIsDisplayOnlyAndOutsideTapCancels(t *testing.T) {
	m := readingTestModel(t, "中文：后文")
	before, _ := json.Marshal(m.blockCache[m.selected.ID])
	m = dragReading(t, m, 0, 0, 0, 3)
	if !strings.Contains(m.viewer.View(), "\x1b[7;4m") {
		t.Fatal("selection has no e-ink underline")
	}
	x, y := readingCoordinate(t, m, 0, 4)
	for _, action := range []tea.MouseAction{tea.MouseActionPress, tea.MouseActionRelease} {
		next, cmd := m.Update(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: action})
		m = next.(Model)
		if cmd != nil || m.editing || m.writesInFlight != 0 {
			t.Fatal("outside click caused editing or saving")
		}
	}
	if m.readSelection != nil || strings.Contains(m.viewer.View(), "\x1b[7;4m") {
		t.Fatal("outside tap did not remove the selection overlay")
	}
	after, _ := json.Marshal(m.blockCache[m.selected.ID])
	if !bytes.Equal(before, after) {
		t.Fatal("display-only underline changed stored block data")
	}
}

func TestReadingInsideTapPreservesSelectionAndOutsideDragStartsAnother(t *testing.T) {
	m := dragReading(t, readingTestModel(t, "hello world"), 0, 0, 0, 5)
	x, y := readingCoordinate(t, m, 0, 2)
	for _, action := range []tea.MouseAction{tea.MouseActionPress, tea.MouseActionRelease} {
		next, _ := m.Update(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: action})
		m = next.(Model)
	}
	if m.readSelection == nil || m.readSelection.anchor.offset != 0 || m.readSelection.cursor.offset != 5 {
		t.Fatal("inside tap replaced the existing range")
	}
	m = dragReading(t, m, 0, 6, 0, 11)
	if m.readSelection == nil || m.readSelection.anchor.offset != 6 || m.readSelection.cursor.offset != 11 {
		t.Fatal("drag outside selection failed to start a new range")
	}
}

func TestReadingOverlayRestoresExistingUnderlineAndIgnoresColorParameters(t *testing.T) {
	plain := styleSelectedRunes("\x1b[4;38;2;24;4;0mlink\x1b[0m tail", []bool{false, true, true, false}, true)
	if strings.Contains(plain, "\x1b[27;24m") {
		t.Fatal("selected link lost its original underline")
	}
	_, underlined := ansiAttributeShape(t, styleSelectedRunes("plain", []bool{false, true, true, false, false}, true), 4, 24)
	if underlined[0] || !underlined[1] || !underlined[2] || underlined[3] || underlined[4] {
		t.Fatalf("underline leaked outside selection: %v", underlined)
	}
}

func TestReadingBoldFailureReturnsErrorAndReloads(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"object":"error","status":403,"code":"restricted_resource","message":"No permission"}`)
	}))
	defer server.Close()
	m := readingTestModel(t, "中文：后文")
	m.client = notionClientForTestServer(t, server)
	m = dragReading(t, m, 0, 3, 0, 0)
	next, cmd := m.Update(interactionRune('b'))
	m = next.(Model)
	msg := cmd()
	if _, ok := msg.(writeErrMsg); !ok {
		t.Fatalf("failed save reported success: %T", msg)
	}
	next, reload := m.Update(msg)
	m = next.(Model)
	if m.err == nil || !m.pageLoading || reload == nil || m.writesInFlight != 0 {
		t.Fatal("save failure was not surfaced with a reload")
	}
}

func unicodeSpace(r rune) bool { return r == ' ' || r == '\n' || r == '\t' }

func TestReadingSGRDragUsesActualParserWithoutEditing(t *testing.T) {
	m := readingTestModel(t, "中文：后文")
	x, y := readingCoordinate(t, m, 0, 0)
	x2, y2 := readingCoordinate(t, m, 0, 3)
	input, writer := io.Pipe()
	defer input.Close()
	defer writer.Close()
	var output bytes.Buffer
	program := tea.NewProgram(mouseProtocolProbe{model: m, stopAfter: 3}, tea.WithInput(input), tea.WithOutput(&output), tea.WithoutSignalHandler())
	done := make(chan tea.Model, 1)
	go func() {
		result, err := program.Run()
		if err != nil {
			t.Error(err)
		}
		done <- result
	}()
	go func() {
		fmt.Fprintf(writer, "\x1b[<0;%d;%dM\x1b[<32;%d;%dM\x1b[<0;%d;%dm", x+1, y+1, x2+1, y2+1, x2+1, y2+1)
	}()
	select {
	case result := <-done:
		final := result.(mouseProtocolProbe).model
		if final.editing || final.readSelection == nil || final.readSelection.anchor.offset != 0 || final.readSelection.cursor.offset != 3 {
			t.Fatalf("SGR selection failed: %+v", final.readSelection)
		}
	case <-time.After(3 * time.Second):
		program.Kill()
		t.Fatal("mouse parser timed out")
	}
}
