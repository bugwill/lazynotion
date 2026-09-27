package ui

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/jomei/notionapi"
	"github.com/rivo/uniseg"

	"github.com/justinm35/lazynotion/internal/notion"
)

type readingPoint struct{ unit, offset int }
type readingPress struct {
	pageID string
	msg    tea.MouseMsg
	unit   int
}
type readingMap struct {
	id, text  string
	positions [][]int // source rune for each rendered rune; decorations are -1
}
type readingSelection struct {
	pageID         string
	anchor, cursor readingPoint
	press          readingPoint
	dragging       bool
	moved          bool
	maps           map[int]readingMap
}

func richTextContent(rt notionapi.RichText) string {
	if rt.Text != nil {
		return rt.Text.Content
	}
	return rt.PlainText
}

func (m *Model) refreshReadingMaps() {
	if m.readSelection == nil {
		return
	}
	for unit, mapped := range m.readSelection.maps {
		if unit >= len(m.pv.units) || m.pv.units[unit].ID() != mapped.id {
			m.readSelection = nil
			return
		}
		runs, _ := notion.LocalRichText(m.pv.units[unit].Node.Block)
		var text strings.Builder
		for _, run := range runs {
			text.WriteString(richTextContent(run))
		}
		if text.String() != mapped.text {
			m.readSelection = nil
			return
		}
	}
	m.readSelection.maps = map[int]readingMap{}
}

// Align the entire source text with its rendered fragment. Ignore whitespace
// while finding the smallest complete subsequence, so list/heading prefixes,
// padding, soft wraps and link footnotes cannot become source offsets. Refuse
// incomplete mappings instead of guessing which API text the user selected.
func mapReadingText(id, text string, lines []string) (readingMap, bool) {
	m := readingMap{id: id, text: text, positions: make([][]int, len(lines))}
	source := []rune(text)
	var wanted []rune
	var sourceOffsets []int
	for i, r := range source {
		if !unicode.IsSpace(r) {
			wanted = append(wanted, r)
			sourceOffsets = append(sourceOffsets, i)
		}
	}
	var visible []rune
	type cell struct{ row, col int }
	var cells []cell
	for row, line := range lines {
		runes := []rune(ansi.Strip(line))
		m.positions[row] = make([]int, len(runes))
		for col, r := range runes {
			m.positions[row][col] = -1
			if !unicode.IsSpace(r) {
				visible = append(visible, r)
				cells = append(cells, cell{row, col})
			}
		}
	}
	if len(wanted) == 0 || len(wanted)*len(visible) > 8_000_000 {
		return m, false
	}
	starts := make([]int, len(wanted))
	for i := range starts {
		starts[i] = -1
	}
	bestStart, bestEnd := -1, len(visible)+1
	for i, r := range visible {
		for j := len(wanted) - 1; j >= 0; j-- {
			if r == wanted[j] {
				if j == 0 {
					starts[j] = i
				} else if starts[j-1] >= 0 {
					starts[j] = starts[j-1]
				}
			}
		}
		start := starts[len(wanted)-1]
		if start >= 0 && (bestStart < 0 || i-start < bestEnd-bestStart) {
			bestStart, bestEnd = start, i
		}
	}
	if bestStart < 0 {
		return m, false
	}
	j := 0
	for i := bestStart; i <= bestEnd && j < len(wanted); i++ {
		if visible[i] != wanted[j] {
			continue
		}
		c := cells[i]
		m.positions[c.row][c.col] = sourceOffsets[j]
		j++
	}
	// Restore spaces within each visual row only when the source gap is all
	// whitespace. Renderer indentation and links remain unselectable decorations.
	for row, positions := range m.positions {
		prev := -1
		for col, offset := range positions {
			if offset < 0 {
				continue
			}
			if prev >= 0 {
				left := positions[prev] + 1
				right := offset
				space := right >= left
				for _, r := range source[left:right] {
					if !unicode.IsSpace(r) {
						space = false
					}
				}
				if space && right > left {
					for k := prev + 1; k < col; k++ {
						positions[k] = min(left+k-prev-1, right-1)
					}
				}
			}
			prev = col
		}
		m.positions[row] = positions
	}
	return m, true
}

func (m *Model) readingMapAt(unit int) (readingMap, bool) {
	if m.readSelection == nil || unit < 0 || unit >= len(m.pv.units) {
		return readingMap{}, false
	}
	u := m.pv.units[unit]
	rts, ok := notion.LocalRichText(u.Node.Block)
	if !ok || isPendingID(u.ID()) || u.ID() == draftBlockID {
		return readingMap{}, false
	}
	var b strings.Builder
	for _, rt := range rts {
		b.WriteString(richTextContent(rt))
	}
	text := b.String()
	if mapped, ok := m.readSelection.maps[unit]; ok && mapped.id == u.ID() && mapped.text == text {
		return mapped, true
	}
	mapped, ok := mapReadingText(u.ID(), text, m.pv.rendered[unit])
	if ok {
		m.readSelection.maps[unit] = mapped
	}
	return mapped, ok
}

// Mouse positions choose whole grapheme boundaries, including CJK,
// combining accents and emoji sequences.
func readingRowBoundary(raw string, positions []int, column int) (int, bool) {
	g := uniseg.NewGraphemes(ansi.Strip(raw))
	runeAt, cellAt := 0, 0
	last := -1
	for g.Next() {
		n, w := len(g.Runes()), g.Width()
		lo, hi := -1, -1
		for i := runeAt; i < runeAt+n && i < len(positions); i++ {
			if positions[i] >= 0 {
				if lo < 0 {
					lo = positions[i]
				}
				hi = positions[i] + 1
			}
		}
		if column < cellAt+w {
			if lo < 0 {
				if last >= 0 {
					return last, true
				}
				return 0, false
			}
			if 2*(column-cellAt) >= w {
				return hi, true
			}
			return lo, true
		}
		if hi >= 0 {
			last = hi
		}
		cellAt += w
		runeAt += n
	}
	return last, last >= 0
}

func (m *Model) beginReadingSelection(msg tea.MouseMsg, unit int) {
	if m.selected == nil {
		return
	}
	m.readSelection = &readingSelection{pageID: m.selected.ID, dragging: true, maps: map[int]readingMap{}}
	mapped, ok := m.readingMapAt(unit)
	row := msg.Y - 1 + m.viewer.YOffset - m.pv.starts[unit]
	if !ok || row < 0 || row >= len(mapped.positions) {
		m.readSelection = nil
		return
	}
	offset, ok := readingRowBoundary(m.pv.rendered[unit][row], mapped.positions[row], msg.X-1-gutterWidth)
	if !ok {
		m.readSelection = nil
		return
	}
	p := readingPoint{unit, offset}
	lo, hi := readingWordSpan(mapped.text, offset)
	m.readSelection.anchor = readingPoint{unit, lo}
	m.readSelection.cursor = readingPoint{unit, hi}
	m.readSelection.press = p
	m.statusMsg = "text selected · b bold & save · esc cancel"
}

// A click outside an existing selection dismisses it. Defer a possible new
// selection until drag motion, so the same press/release cannot replace the
// selection or activate a link after dismissing it.
func (m Model) startReadingPress(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.selected == nil {
		m.readSelection = nil
		return m, nil
	}
	line := msg.Y - 1 + m.viewer.YOffset
	unitAt := -1
	inside := false
	for unit, start := range m.pv.starts {
		if line < start || line >= start+m.pv.heights[unit] {
			continue
		}
		unitAt = unit
		mapped, ok := m.readingMapAt(unit)
		row := line - start
		if ok && row < len(mapped.positions) {
			offset, hit := readingRowBoundary(m.pv.rendered[unit][row], mapped.positions[row], msg.X-1-gutterWidth)
			lo, hi := m.readSelection.span(unit, len([]rune(mapped.text)))
			inside = hit && offset >= lo && offset < hi
		}
		break
	}
	if !inside {
		m.readSelection = nil
		m.statusMsg = "selection cancelled"
		m.viewerSetContent()
	}
	if unitAt >= 0 {
		m.readPending = &readingPress{pageID: m.selected.ID, msg: msg, unit: unitAt}
	}
	return m, nil
}

func (m Model) continueReadingPress(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	pending := m.readPending
	m.readPending = nil
	if msg.Action == tea.MouseActionRelease {
		return m, nil
	}
	if pending == nil || m.selected == nil || pending.pageID != m.selected.ID {
		return m, nil
	}
	m.beginReadingSelection(pending.msg, pending.unit)
	if m.readSelection == nil {
		return m, nil
	}
	return m.updateReadingMouse(msg)
}

func (m Model) updateReadingMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	s := m.readSelection
	if s == nil || m.selected == nil || s.pageID != m.selected.ID {
		m.readSelection = nil
		return m, nil
	}
	if msg.Action == tea.MouseActionMotion {
		if !s.moved {
			s.anchor = s.press
		}
		s.moved = true
		if msg.Y <= 1 {
			m.viewer.ScrollUp(1)
		} else if msg.Y >= m.height-m.footerHeight()-2 {
			m.viewer.ScrollDown(1)
		}
	}
	line := clamp(msg.Y-1, 0, m.viewer.Height-1) + m.viewer.YOffset
	for unit, start := range m.pv.starts {
		if line < start || line >= start+m.pv.heights[unit] {
			continue
		}
		mapped, ok := m.readingMapAt(unit)
		if !ok {
			break
		}
		row := line - start
		if row >= len(mapped.positions) {
			break
		}
		offset, ok := readingRowBoundary(m.pv.rendered[unit][row], mapped.positions[row], msg.X-1-gutterWidth)
		if ok {
			s.cursor = readingPoint{unit, offset}
		}
		break
	}
	if msg.Action == tea.MouseActionRelease {
		s.dragging = false
		if !s.moved {
			if mapped, ok := m.readingMapAt(s.anchor.unit); ok {
				lo, hi := readingWordSpan(mapped.text, s.anchor.offset)
				s.anchor.offset, s.cursor.offset = lo, hi
			}
		}
		if s.anchor == s.cursor {
			m.readSelection = nil
		}
	}
	if m.readSelection != nil && s.anchor != s.cursor {
		m.statusMsg = "text selected · b bold & save · esc cancel"
	}
	m.viewerSetContent()
	return m, nil
}

func readingWordSpan(text string, offset int) (int, int) {
	offset = clamp(offset, 0, max(len([]rune(text))-1, 0))
	remaining, state, start := text, -1, 0
	for remaining != "" {
		word, rest, next := uniseg.FirstWordInString(remaining, state)
		end := start + len([]rune(word))
		if offset < end {
			if strings.TrimSpace(word) == "" {
				return offset, offset
			}
			return start, end
		}
		remaining, state, start = rest, next, end
	}
	return offset, offset
}

func (s *readingSelection) span(unit, length int) (int, int) {
	a, b := s.anchor, s.cursor
	if a.unit > b.unit || a.unit == b.unit && a.offset > b.offset {
		a, b = b, a
	}
	if unit < a.unit || unit > b.unit {
		return 0, 0
	}
	lo, hi := 0, length
	if unit == a.unit {
		lo = a.offset
	}
	if unit == b.unit {
		hi = b.offset
	}
	return clamp(lo, 0, length), clamp(hi, 0, length)
}

func (m *Model) highlightReadingSelection(content string) string {
	s := m.readSelection
	if m.selected == nil || s.pageID != m.selected.ID || s.anchor == s.cursor {
		return content
	}
	lines := strings.Split(content, "\n")
	for unit, start := range m.pv.starts {
		if unit < min(s.anchor.unit, s.cursor.unit) || unit > max(s.anchor.unit, s.cursor.unit) {
			continue
		}
		mapped, ok := m.readingMapAt(unit)
		if !ok {
			continue
		}
		lo, hi := s.span(unit, len([]rune(mapped.text)))
		if lo == hi {
			continue
		}
		for row, positions := range mapped.positions {
			if start+row >= len(lines) {
				break
			}
			mask := make([]bool, gutterWidth+len(positions))
			for i, p := range positions {
				mask[gutterWidth+i] = p >= lo && p < hi
			}
			lines[start+row] = styleSelectedRunes(lines[start+row], mask, true)
		}
	}
	return strings.Join(lines, "\n")
}

// Split text runs without reparsing Markdown: keep links, colors, italics,
// checkbox/block identity and opaque mentions untouched.
func boldRichTextRange(runs []notionapi.RichText, lo, hi int) ([]notionapi.RichText, bool) {
	var out []notionapi.RichText
	offset := 0
	changed := false
	for _, run := range runs {
		runes := []rune(richTextContent(run))
		end := offset + len(runes)
		a, b := max(lo-offset, 0), min(hi-offset, len(runes))
		if a >= b || run.Text == nil || run.Annotations != nil && run.Annotations.Bold {
			out = append(out, run)
			offset = end
			continue
		}
		bounds := []int{0, a, b, len(runes)}
		for i := 0; i < 3; i++ {
			if bounds[i] == bounds[i+1] {
				continue
			}
			part := run
			text := *run.Text
			part.Text = &text
			part.Text.Content = string(runes[bounds[i]:bounds[i+1]])
			part.PlainText = part.Text.Content
			if run.Annotations != nil {
				annotations := *run.Annotations
				part.Annotations = &annotations
			}
			if i == 1 {
				if part.Annotations == nil {
					part.Annotations = &notionapi.Annotations{}
				}
				part.Annotations.Bold = true
				changed = true
			}
			out = append(out, part)
		}
		offset = end
	}
	return out, changed
}

func (m Model) boldReadingSelection() (tea.Model, tea.Cmd) {
	s := m.readSelection
	if s == nil || m.selected == nil || s.pageID != m.selected.ID || s.anchor == s.cursor {
		return m, nil
	}
	if m.writesInFlight > 0 {
		m.statusMsg = "wait for the current save before formatting"
		return m, nil
	}
	type change struct {
		block         notionapi.Block
		before, after []notionapi.RichText
	}
	var changes []change
	for unit := range m.pv.units {
		if unit < min(s.anchor.unit, s.cursor.unit) || unit > max(s.anchor.unit, s.cursor.unit) {
			continue
		}
		mapped, ok := m.readingMapAt(unit)
		if !ok {
			continue
		}
		lo, hi := s.span(unit, len([]rune(mapped.text)))
		if lo == hi {
			continue
		}
		block := m.pv.units[unit].Node.Block
		runs, _ := notion.LocalRichText(block)
		after, changed := boldRichTextRange(runs, lo, hi)
		if changed {
			changes = append(changes, change{block, runs, after})
		}
	}
	m.readSelection = nil
	if len(changes) == 0 {
		m.statusMsg = "no editable text to bold"
		m.viewerSetContent()
		return m, nil
	}
	for _, c := range changes {
		notion.SetLocalRichText(c.block, c.after)
	}
	m.rebuildPage(true)
	client, pageID := m.client, m.selected.ID
	m.pushUndo("selection bold", pageID, func(ctx context.Context) error {
		for _, c := range changes {
			if err := client.SetBlockRichText(ctx, c.block, c.before); err != nil {
				return err
			}
		}
		return nil
	})
	m.writesInFlight++
	m.statusMsg = "saving bold selection…"
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		for _, c := range changes {
			if err := client.SetBlockRichText(ctx, c.block, c.after); err != nil {
				return writeErrMsg{pageID: pageID, err: fmt.Errorf("save bold selection: %w", err)}
			}
		}
		return writeDoneMsg{pageID: pageID, status: "bold saved (u to undo)"}
	}
}
