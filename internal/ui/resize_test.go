package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestPaneResizePreservesMouseScroll(t *testing.T) {
	for _, height := range []int{30, 24, 0} {
		t.Run(fmt.Sprint(height), func(t *testing.T) {
			m := readingTestModel(t, strings.Repeat("阅读内容 mixed text。", 150))
			m.viewer.SetYOffset(12)
			next, _ := m.Update(tea.WindowSizeMsg{Width: m.width, Height: height})
			m = next.(Model)
			if m.viewer.YOffset != 12 || m.pv.cursor != 0 {
				t.Fatalf("resize moved reading position: offset=%d cursor=%d", m.viewer.YOffset, m.pv.cursor)
			}
		})
	}
}

func TestWidthResizeKeepsVisibleTextInsteadOfBlockCursor(t *testing.T) {
	var text strings.Builder
	for i := range 150 {
		fmt.Fprintf(&text, "第%d段文字 mixed text。", i)
	}
	m := readingTestModel(t, text.String())
	m.viewer.SetYOffset(12)
	before := m.captureResizeAnchor()
	next, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: m.height})
	m = next.(Model)
	mapped, ok := m.resizeReadingMap(0)
	if !ok || before.source < 0 {
		t.Fatal("missing text anchor")
	}
	found := false
	for _, source := range mapped.positions[m.viewer.YOffset] {
		if source == before.source {
			found = true
		}
	}
	if !found || m.pv.cursor != 0 {
		t.Fatalf("resize lost text offset %d: viewport=%d cursor=%d", before.source, m.viewer.YOffset, m.pv.cursor)
	}
}

func TestWidthResizePreservesScrolledBlock(t *testing.T) {
	m := readingTestModel(t, strings.Repeat("前面的段落会重新换行。", 100))
	m.blockCache[m.selected.ID] = append(m.blockCache[m.selected.ID], para("second", strings.Repeat("正在阅读的第二段。", 100)))
	m.rebuildPage(true)
	m.viewer.SetYOffset(m.pv.starts[1] + 2)
	before := m.captureResizeAnchor()
	next, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: m.height})
	m = next.(Model)
	after := m.captureResizeAnchor()
	if before.unit != 1 || after.unit != 1 || m.pv.cursor != 0 {
		t.Fatalf("resize jumped to another block: before=%+v after=%+v cursor=%d", before, after, m.pv.cursor)
	}
}
