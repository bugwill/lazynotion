package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	termansi "github.com/charmbracelet/x/ansi"
	"github.com/jomei/notionapi"
	"github.com/mattn/go-runewidth"
	"github.com/rivo/uniseg"

	"github.com/justinm35/lazynotion/internal/convert"
	"github.com/justinm35/lazynotion/internal/notion"
)

func (m Model) openInEditor() (tea.Model, tea.Cmd) {
	if m.selected == nil {
		return m, nil
	}
	blocks, ok := m.blockCache[m.selected.ID]
	if !ok {
		return m, nil
	}
	md := convert.ToMarkdown(blocks)
	path := filepath.Join(os.TempDir(), "lazynotion-"+m.selected.ID+".md")
	if err := os.WriteFile(path, []byte(md), 0o600); err != nil {
		m.err = err
		return m, nil
	}
	page := *m.selected
	cmd := exec.Command(editorCommand(), path)
	return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
		return editorDoneMsg{page: page, path: path, orig: md, err: err}
	})
}

func editorCommand() string {
	for _, env := range []string{"VISUAL", "EDITOR"} {
		if e := os.Getenv(env); e != "" {
			return e
		}
	}
	return "vim"
}

type editUndoState struct {
	value  string
	cursor int
	anchor int
}

func (m *Model) setEditValue(value string) {
	m.editArea.SetValue(value)
	m.editScroll = 0
}

// Textarea has a private viewport. Mirror its cursor-visibility rule so
// screen coordinates and selection highlighting use the same visible rows.
func (m *Model) trackEditScroll() {
	row := m.editArea.LineInfo().RowOffset
	lines := strings.Split(m.editArea.Value(), "\n")
	for _, line := range lines[:m.editArea.Line()] {
		row += len(wrapEditorLine([]rune(line), m.editArea.Width()))
	}
	if row < m.editScroll {
		m.editScroll = row
	} else if row >= m.editScroll+m.editArea.Height() {
		m.editScroll = row - m.editArea.Height() + 1
	}
}

func (m *Model) settleEditScroll() {
	// Refresh the viewport content before repositioning after a large paste.
	_ = m.editArea.View()
	m.editArea, _ = m.editArea.Update(tea.WindowSizeMsg{})
	m.trackEditScroll()
}

func (m Model) updateEditorMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown {
		return m, nil
	}
	if m.pv.cursor >= len(m.pv.starts) {
		return m, nil
	}
	top := 1 + m.pv.starts[m.pv.cursor] - m.viewer.YOffset
	left := 1 + gutterWidth
	if msg.Action == tea.MouseActionPress {
		if msg.Button != tea.MouseButtonLeft || msg.X < left || msg.X >= min(left+m.editArea.Width(), m.width-1) ||
			msg.Y < max(top, 1) || msg.Y >= min(top+m.editArea.Height(), m.height-m.footerHeight()-1) {
			return m, nil
		}
		m.editDragging = true
	} else if !m.editDragging || (msg.Action != tea.MouseActionMotion && msg.Action != tea.MouseActionRelease) {
		return m, nil
	}
	rows := editorDisplayRows(m.editArea.Value(), m.editArea.Width(), 0, 0)
	row := m.editScroll + clamp(msg.Y-top, 0, m.editArea.Height()-1)
	// Moving beyond the visible box extends the selection through long edits.
	if msg.Action == tea.MouseActionMotion {
		if msg.Y < max(top, 1) {
			row = m.editScroll - 1
		} else if msg.Y >= min(top+m.editArea.Height(), m.height-m.footerHeight()-1) {
			row = m.editScroll + m.editArea.Height()
		}
	}
	row = clamp(row, 0, len(rows)-1)
	index := editorMouseBoundary(rows[row], max(msg.X-left, 0))
	if msg.Action == tea.MouseActionPress {
		m.editAnchor = index
	}
	m.moveEditCursor(index)
	if msg.Action == tea.MouseActionRelease {
		m.editDragging = false
		if m.editAnchor == index {
			m.editAnchor = -1
		}
	}
	m.syncViewer()
	return m, nil
}

// Mouse columns count terminal cells, whereas editing offsets count runes.
// Snap to grapheme boundaries to avoid splitting combining marks or emoji.
func editorMouseBoundary(row editorDisplayRow, column int) int {
	graphemes := uniseg.NewGraphemes(row.text)
	cell, runeAt := 0, 0
	for graphemes.Next() {
		n := len(graphemes.Runes())
		width := graphemes.Width()
		if column < cell+width {
			if 2*(column-cell) >= width {
				return row.positions[runeAt+n]
			}
			return row.positions[runeAt]
		}
		cell += width
		runeAt += n
	}
	return row.positions[len(row.positions)-1]
}

func (m Model) editCursorIndex() int {
	value := m.editArea.Value()
	lines := strings.Split(value, "\n")
	row := clamp(m.editArea.Line(), 0, len(lines)-1)
	info := m.editArea.LineInfo()
	col := clamp(info.StartColumn+info.ColumnOffset, 0, len([]rune(lines[row])))
	index := col
	for i := 0; i < row; i++ {
		index += len([]rune(lines[i])) + 1
	}
	return index
}

func (m *Model) moveEditCursor(index int) {
	runes := []rune(m.editArea.Value())
	index = clamp(index, 0, len(runes))
	row, col := 0, 0
	for i := 0; i < index; i++ {
		if runes[i] == '\n' {
			row++
			col = 0
		} else {
			col++
		}
	}
	for m.editArea.Line() < row {
		// Cross logical newlines explicitly. CursorDown traverses soft-wrap
		// rows and can stall on a synthetic trailing row in a narrow textarea.
		m.editArea.CursorEnd()
		m.editArea, _ = m.editArea.Update(tea.KeyMsg{Type: tea.KeyRight})
		m.trackEditScroll()
	}
	for m.editArea.Line() > row {
		m.editArea.CursorStart()
		m.editArea, _ = m.editArea.Update(tea.KeyMsg{Type: tea.KeyLeft})
		m.trackEditScroll()
	}
	m.editArea.CursorStart()
	m.editArea.SetCursor(col)
	m.settleEditScroll()
}

func (m Model) updateInlineEdit(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.editDragging = false
	if msg.String() == "ctrl+d" || msg.String() == "ctrl+c" {
		m.stopInlineEdit()
		m.statusMsg = "edit discarded"
		if unit, ok := m.pv.current(); ok && unit.ID() == draftBlockID {
			m.removeDraft()
			m.rebuildPage(true)
		} else {
			m.syncViewer()
		}
		return m, nil
	}
	if m.paletteOpen && msg.String() == "esc" {
		return m.saveInlineEdit()
	}
	if m.paletteOpen {
		return m.updatePalette(msg)
	}
	if msg.String() == "/" && paletteTriggerOK(m.editArea.Value()) {
		m.openPalette()
		return m, nil
	}

	switch msg.String() {
	case "esc":
		return m.saveInlineEdit()
	case "shift+enter", "alt+enter":
		return m.saveAndContinue("")
	case "enter":
		// Enter on a list item saves it and continues the list on a new block,
		// an empty marker ends the list, and other blocks keep a plain newline.
		rows := strings.Split(m.editArea.Value(), "\n")
		row := clamp(m.editArea.Line(), 0, len(rows)-1)
		if marker, contentEmpty, isList := convert.ListContinuation(rows[row]); isList {
			if contentEmpty {
				before := m.editUndoSnapshot()
				rows[row] = ""
				m.setEditValue(strings.Join(rows, "\n"))
				m.editArea.CursorEnd()
				m.editAnchor = -1
				m.pushEditUndo(before)
				m.resizeEditArea()
				m.syncViewer()
				return m, nil
			}
			return m.saveAndContinue(marker)
		}
	case "ctrl+v":
		if m.editAnchor >= 0 {
			m.editAnchor = -1
			m.syncViewer()
			return m, nil
		}
		m.editAnchor = m.editCursorIndex()
		m.statusMsg = "selection started"
		m.syncViewer()
		return m, nil
	case "ctrl+w":
		m.selectEditorWord()
		m.syncViewer()
		return m, nil
	case "b":
		if m.editAnchor >= 0 && m.editAnchor != m.editCursorIndex() {
			m.applyEditorBold()
			m.resizeEditArea()
			m.syncViewer()
			return m, nil
		}
	case "u":
		if m.editAnchor >= 0 {
			if len(m.editUndo) == 0 {
				m.statusMsg = "nothing to undo in editor"
				return m, nil
			}
			m.undoEditorFormat()
			m.resizeEditArea()
			m.syncViewer()
			return m, nil
		}
	case "ctrl+z":
		if len(m.editUndo) == 0 {
			m.statusMsg = "nothing to undo in editor"
			return m, nil
		}
		m.undoEditorFormat()
		m.resizeEditArea()
		m.syncViewer()
		return m, nil
	}

	selectionMove := isEditorSelectionMove(msg) && (m.editAnchor >= 0 || strings.HasPrefix(msg.String(), "shift+"))
	if selectionMove {
		if m.editAnchor < 0 {
			m.editAnchor = m.editCursorIndex()
		}
		msg = editorMovementKey(msg)
		m.preGrowEditArea()
		var cmd tea.Cmd
		m.editArea, cmd = m.editArea.Update(msg)
		m.settleEditScroll()
		m.resizeEditArea()
		m.syncViewer()
		return m, cmd
	}
	before := m.editUndoSnapshot()
	m.editAnchor = -1
	m.preGrowEditArea()
	var cmd tea.Cmd
	m.editArea, cmd = m.editArea.Update(msg)
	m.settleEditScroll()
	if m.editArea.Value() != before.value {
		m.pushEditUndo(before)
	}
	m.resizeEditArea()
	m.syncViewer()
	return m, cmd
}

func isEditorSelectionMove(msg tea.KeyMsg) bool {
	switch msg.String() {
	case "left", "right", "up", "down", "home", "end", "ctrl+left", "ctrl+right", "ctrl+home", "ctrl+end",
		"shift+left", "shift+right", "shift+up", "shift+down", "shift+home", "shift+end":
		return true
	}
	return false
}

func editorMovementKey(msg tea.KeyMsg) tea.KeyMsg {
	switch msg.String() {
	case "shift+left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "shift+right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "shift+up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "shift+down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "shift+home":
		return tea.KeyMsg{Type: tea.KeyHome}
	case "shift+end":
		return tea.KeyMsg{Type: tea.KeyEnd}
	}
	return msg
}

func (m *Model) selectEditorWord() {
	runes := []rune(m.editArea.Value())
	pos := clamp(m.editCursorIndex(), 0, len(runes))
	if pos == len(runes) && pos > 0 {
		pos--
	}
	if len(runes) == 0 {
		m.editAnchor = pos
		return
	}
	if unicode.IsSpace(runes[pos]) {
		for pos > 0 && unicode.IsSpace(runes[pos]) {
			pos--
		}
	}
	start, end := pos, pos+1
	for start > 0 && !unicode.IsSpace(runes[start-1]) {
		start--
	}
	for end < len(runes) && !unicode.IsSpace(runes[end]) {
		end++
	}
	m.editAnchor = start
	m.moveEditCursor(end)
}

func (m *Model) applyEditorBold() {
	value := []rune(m.editArea.Value())
	cursor := m.editCursorIndex()
	anchor := m.editAnchor
	lo, hi := min(anchor, cursor), max(anchor, cursor)
	m.editUndo = append(m.editUndo, editUndoState{value: string(value), cursor: cursor, anchor: anchor})
	if len(m.editUndo) > 50 {
		m.editUndo = m.editUndo[len(m.editUndo)-50:]
	}

	// When the selected content is already bracketed by a matching pair,
	// remove that pair. Otherwise wrap exactly the selected runes.
	if lo >= 2 && hi+2 <= len(value) && string(value[lo-2:lo]) == "**" && string(value[hi:hi+2]) == "**" {
		unwrapped := make([]rune, 0, len(value)-4)
		unwrapped = append(unwrapped, value[:lo-2]...)
		unwrapped = append(unwrapped, value[lo:hi]...)
		unwrapped = append(unwrapped, value[hi+2:]...)
		m.setEditValue(string(unwrapped))
		m.editAnchor = anchor - 2
		m.moveEditCursor(cursor - 2)
		return
	}
	wrapped := make([]rune, 0, len(value)+4)
	wrapped = append(wrapped, value[:lo]...)
	wrapped = append(wrapped, '*', '*')
	wrapped = append(wrapped, value[lo:hi]...)
	wrapped = append(wrapped, '*', '*')
	wrapped = append(wrapped, value[hi:]...)
	m.setEditValue(string(wrapped))
	m.editAnchor = anchor + 2
	m.moveEditCursor(cursor + 2)
}

func (m *Model) undoEditorFormat() {
	state := m.editUndo[len(m.editUndo)-1]
	m.editUndo = m.editUndo[:len(m.editUndo)-1]
	m.setEditValue(state.value)
	m.editAnchor = state.anchor
	m.moveEditCursor(state.cursor)
}

func (m Model) editUndoSnapshot() editUndoState {
	return editUndoState{value: m.editArea.Value(), cursor: m.editCursorIndex(), anchor: m.editAnchor}
}

func (m *Model) pushEditUndo(state editUndoState) {
	m.editUndo = append(m.editUndo, state)
	if len(m.editUndo) > 50 {
		m.editUndo = m.editUndo[len(m.editUndo)-50:]
	}
}

func (m Model) editSelectionLength() int {
	return absInt(m.editCursorIndex() - m.editAnchor)
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

type editorDisplayRow struct {
	text      string
	mask      []bool
	positions []int // document rune boundary at each visual rune, plus row end
}

// highlightEditSelection overlays reverse video on selected editor runes.
// The row mapping follows Bubbles' textarea word-wrap rules, so wide CJK
// runes and soft-wrapped selections keep their exact rune positions.
func highlightEditSelection(view, value string, width, anchor, cursor int, top ...int) string {
	lo, hi := min(anchor, cursor), max(anchor, cursor)
	if lo == hi {
		return view
	}
	rows := editorDisplayRows(value, max(width, 1), lo, hi)
	rawLines := strings.Split(view, "\n")
	rowAt := 0
	for i, raw := range rawLines {
		if len(top) > 0 {
			row := top[0] + i
			if row < len(rows) {
				rawLines[i] = reverseEditorRunes(raw, rows[row].mask)
			}
			continue
		}
		plain := termansi.Strip(raw)
		for rowAt < len(rows) {
			row := rows[rowAt]
			if row.text != "" && strings.HasPrefix(plain, row.text) {
				rawLines[i] = reverseEditorRunes(raw, row.mask)
				rowAt++
				break
			}
			rowAt++ // rows above the textarea's current vertical viewport
		}
	}
	return strings.Join(rawLines, "\n")
}

func editorDisplayRows(value string, width, lo, hi int) []editorDisplayRow {
	var rows []editorDisplayRow
	docIndex := 0
	for _, line := range strings.Split(value, "\n") {
		wrapped := wrapEditorLine([]rune(line), width)
		source := []rune(line)
		sourceAt := 0
		for _, visual := range wrapped {
			mask := make([]bool, len(visual))
			positions := make([]int, len(visual)+1)
			for i, r := range visual {
				positions[i] = docIndex + sourceAt
				if sourceAt < len(source) && (source[sourceAt] == r || unicode.IsSpace(source[sourceAt]) && unicode.IsSpace(r)) {
					mask[i] = docIndex+sourceAt >= lo && docIndex+sourceAt < hi
					sourceAt++
				}
			}
			positions[len(visual)] = docIndex + sourceAt
			rows = append(rows, editorDisplayRow{text: string(visual), mask: mask, positions: positions})
		}
		docIndex += len(source) + 1
	}
	return rows
}

// Keep this small copy in sync with textarea.wrap: its wrap function is
// package-private, while selection offsets need its visual-row boundaries.
func wrapEditorLine(runes []rune, width int) [][]rune {
	lines := [][]rune{{}}
	var word []rune
	row, spaces := 0, 0
	for _, r := range runes {
		if unicode.IsSpace(r) {
			spaces++
		} else {
			word = append(word, r)
		}
		if spaces > 0 {
			if uniseg.StringWidth(string(lines[row]))+uniseg.StringWidth(string(word))+spaces > width {
				row++
				lines = append(lines, append([]rune{}, word...))
				lines[row] = append(lines[row], repeatEditorSpaces(spaces)...)
			} else {
				lines[row] = append(lines[row], word...)
				lines[row] = append(lines[row], repeatEditorSpaces(spaces)...)
			}
			spaces, word = 0, nil
			continue
		}
		lastWidth := runewidth.RuneWidth(word[len(word)-1])
		if uniseg.StringWidth(string(word))+lastWidth > width {
			if len(lines[row]) > 0 {
				row++
				lines = append(lines, nil)
			}
			lines[row] = append(lines[row], word...)
			word = nil
		}
	}
	if uniseg.StringWidth(string(lines[row]))+uniseg.StringWidth(string(word))+spaces >= width {
		lines = append(lines, append([]rune{}, word...))
		spaces++
		lines[row+1] = append(lines[row+1], repeatEditorSpaces(spaces)...)
	} else {
		lines[row] = append(lines[row], word...)
		spaces++
		lines[row] = append(lines[row], repeatEditorSpaces(spaces)...)
	}
	return lines
}

func repeatEditorSpaces(n int) []rune { return []rune(strings.Repeat(" ", n)) }

func reverseEditorRunes(line string, selected []bool) string {
	return styleSelectedRunes(line, selected, false)
}

// Reading selection also uses underline because e-ink terminals can suppress
// reverse video. Restore the original underline after each marked rune; this
// overlay must never change the formatting of adjacent links or text.
func styleSelectedRunes(line string, selected []bool, underline bool) string {
	var out strings.Builder
	visible := 0
	originalUnderline := false
	for i := 0; i < len(line); {
		if line[i] == '\x1b' {
			start := i
			i++
			if i < len(line) && line[i] == '[' {
				i++
				for i < len(line) && (line[i] < 0x40 || line[i] > 0x7e) {
					i++
				}
				if i < len(line) {
					i++
				}
			} else if i < len(line) && line[i] == ']' {
				i++
				for i < len(line) && line[i] != '\a' {
					if line[i] == '\x1b' && i+1 < len(line) && line[i+1] == '\\' {
						i += 2
						break
					}
					i++
				}
				if i < len(line) && line[i] == '\a' {
					i++
				}
			} else if i < len(line) {
				i++
			}
			escape := line[start:i]
			if underline && strings.HasPrefix(escape, "\x1b[") && strings.HasSuffix(escape, "m") {
				codes := strings.Split(escape[2:len(escape)-1], ";")
				for at := 0; at < len(codes); at++ {
					code := codes[at]
					if (code == "38" || code == "48" || code == "58") && at+1 < len(codes) {
						if codes[at+1] == "2" {
							at += 4
						} else if codes[at+1] == "5" {
							at += 2
						}
						continue
					}
					switch code {
					case "", "0", "24", "4:0":
						originalUnderline = false
					case "4", "21":
						originalUnderline = true
					default:
						if strings.HasPrefix(code, "4:") {
							originalUnderline = true
						}
					}
				}
			}
			out.WriteString(escape)
			continue
		}
		r, size := utf8.DecodeRuneInString(line[i:])
		if visible < len(selected) && selected[visible] {
			if underline {
				out.WriteString("\x1b[7;4m")
			} else {
				out.WriteString("\x1b[7m")
			}
			out.WriteRune(r)
			if underline && !originalUnderline {
				out.WriteString("\x1b[27;24m")
			} else {
				out.WriteString("\x1b[27m")
			}
		} else {
			out.WriteRune(r)
		}
		visible++
		i += size
	}
	return out.String()
}

func (m Model) handleEditorDone(msg editorDoneMsg) (tea.Model, tea.Cmd) {
	defer os.Remove(msg.path)
	if msg.err != nil {
		m.err = fmt.Errorf("editor: %w", msg.err)
		return m, nil
	}
	edited, err := os.ReadFile(msg.path)
	if err != nil {
		m.err = err
		return m, nil
	}
	if strings.TrimSpace(string(edited)) == strings.TrimSpace(msg.orig) {
		m.statusMsg = "no changes"
		return m, nil
	}

	blocks := convert.ParseMarkdown(string(edited))
	warning := lossyWarning(m.blockCache[msg.page.ID])
	m.confirm = &pendingReplace{page: msg.page, blocks: blocks, warning: warning}
	return m, nil
}

// lossyWarning names block types on the page that a markdown round-trip
// simplifies or drops, so the user can bail out before replacing.
func lossyWarning(nodes []notion.BlockNode) string {
	found := map[string]bool{}
	var walk func([]notion.BlockNode)
	walk = func(ns []notion.BlockNode) {
		for _, n := range ns {
			switch b := n.Block.(type) {
			case *notionapi.ImageBlock:
				if b.Image.File != nil {
					found["uploaded images"] = true
				}
			case *notionapi.CalloutBlock:
				found["callouts"] = true
			case *notionapi.ColumnListBlock:
				found["columns"] = true
			case *notionapi.SyncedBlock:
				found["synced blocks"] = true
			case *notionapi.VideoBlock, *notionapi.FileBlock, *notionapi.PdfBlock, *notionapi.AudioBlock:
				found["file attachments"] = true
			case *notionapi.EmbedBlock, *notionapi.LinkPreviewBlock:
				found["embeds"] = true
			case *notionapi.EquationBlock:
				found["equations"] = true
			}
			walk(n.Children)
		}
	}
	walk(nodes)
	if len(found) == 0 {
		return ""
	}
	types := make([]string, 0, len(found))
	for t := range found {
		types = append(types, t)
	}
	sort.Strings(types)
	return strings.Join(types, ", ") + " will be simplified"
}
