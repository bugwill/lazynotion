package ui

import (
	"strings"

	"github.com/justinm35/lazynotion/internal/notion"
)

type resizeAnchor struct {
	offset, width, unit, row, source int
}

func (m *Model) resizeReadingMap(unit int) (readingMap, bool) {
	if unit < 0 || unit >= len(m.pv.units) {
		return readingMap{}, false
	}
	runs, ok := notion.LocalRichText(m.pv.units[unit].Node.Block)
	if !ok {
		return readingMap{}, false
	}
	var text strings.Builder
	for _, run := range runs {
		text.WriteString(richTextContent(run))
	}
	return mapReadingText(m.pv.units[unit].ID(), text.String(), m.pv.rendered[unit])
}

// Anchor resizes to the text being read, independently of the block cursor.
func (m *Model) captureResizeAnchor() resizeAnchor {
	a := resizeAnchor{offset: m.viewer.YOffset, width: m.viewer.Width, unit: -1, source: -1}
	for unit, start := range m.pv.starts {
		if start > a.offset {
			break
		}
		a.unit, a.row = unit, a.offset-start
	}
	if mapped, ok := m.resizeReadingMap(a.unit); ok && a.row < len(mapped.positions) {
		for _, offset := range mapped.positions[a.row] {
			if offset >= 0 {
				a.source = offset
				break
			}
		}
	}
	return a
}

func (m *Model) restoreResizeAnchor(a resizeAnchor) {
	offset := a.offset
	if a.width != m.viewer.Width && a.unit >= 0 && a.unit < len(m.pv.starts) {
		row := a.row
		if a.source >= 0 {
			if mapped, ok := m.resizeReadingMap(a.unit); ok {
				found := false
				for i, positions := range mapped.positions {
					for _, source := range positions {
						if source >= a.source {
							row, found = i, true
							break
						}
					}
					if found {
						break
					}
				}
			}
		}
		offset = m.pv.starts[a.unit] + row
	}
	m.viewer.SetYOffset(offset)
}
