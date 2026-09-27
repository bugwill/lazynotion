package ui

import tea "github.com/charmbracelet/bubbletea"

// A touch tap arrives as a mouse press/release pair. Activate on press only,
// so the release cannot accidentally activate a row in the newly opened page.
func (m Model) activateMouseRow(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.selected == nil {
		row := msg.Y - 1 // top border
		delegate := pageDelegate{}
		stride := delegate.Height() + delegate.Spacing()
		if row < 0 || row >= m.sidebar.Height() || row%stride >= delegate.Height() {
			return m, nil
		}
		index := m.sidebar.Paginator.Page*m.sidebar.Paginator.PerPage + row/stride
		if row/stride >= m.sidebar.Paginator.PerPage || index >= len(m.sidebar.VisibleItems()) {
			return m, nil
		}
		m.sidebar.Select(index)
		m.focus = focusSidebar
		return m.openSelectedPage()
	}
	if m.focus != focusViewer || m.pageLoading {
		return m, nil
	}
	if m.recent != nil {
		row := msg.Y - 2 // border and summary
		height := max(m.viewer.Height-2, 1)
		start := max(m.recent.cursor-height+1, 0)
		index := start + row
		if !m.recent.loaded || row < 0 || row >= height || index >= len(m.recent.pages) {
			return m, nil
		}
		m.recent.cursor = index
		return m.openRecentItem()
	}
	if m.db != nil {
		row := msg.Y - 3 // border, column header, separator
		index := m.db.scroll + row
		if m.db.ds == nil || row < 0 || row >= m.dbVisibleRows() || index >= len(m.db.rows) {
			return m, nil
		}
		m.db.cursor = index
		return m.openDBRow()
	}
	line := msg.Y - 1 + m.viewer.YOffset
	for i, start := range m.pv.starts {
		if line >= start && line < start+m.pv.heights[i] {
			m.pv.cursor = i
			m.visualAnchor = -1
			m.readSelection = nil
			unit, ok := m.pv.current()
			if ok {
				_, _, page := unit.PageRef()
				_, _, database := unit.DatabaseRef()
				if page || database {
					m.syncViewer()
					return m.openChildPage()
				}
			}
			m.beginReadingSelection(msg, i)
			if m.readSelection == nil && ok && unit.Foldable {
				m.syncViewer()
				return m.openChildPage()
			}
			m.viewerSetContent()
			return m, nil
		}
	}
	return m, nil
}
