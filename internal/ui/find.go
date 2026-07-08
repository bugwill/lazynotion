package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// findMatches recomputes match indexes against the live units, so results
// never go stale after edits or reloads.
func (m Model) findMatches() []int {
	query := strings.ToLower(strings.TrimSpace(m.findQuery))
	if query == "" {
		return nil
	}
	var matches []int
	for i, u := range m.pv.units {
		if strings.Contains(strings.ToLower(u.SearchText()), query) {
			matches = append(matches, i)
		}
	}
	return matches
}

// jumpToMatch moves the block cursor to a match. delta +1 finds the next
// match after the cursor, -1 the previous; 0 lands on the cursor's block if
// it matches (used right after submitting a query). Wraps around.
func (m Model) jumpToMatch(delta int) (tea.Model, tea.Cmd) {
	matches := m.findMatches()
	if len(matches) == 0 {
		if strings.TrimSpace(m.findQuery) != "" {
			m.statusMsg = fmt.Sprintf("no matches for %q", m.findQuery)
		}
		return m, nil
	}

	target := -1
	switch {
	case delta >= 0:
		for _, idx := range matches {
			if idx > m.pv.cursor || (delta == 0 && idx == m.pv.cursor) {
				target = idx
				break
			}
		}
		if target == -1 {
			target = matches[0]
		}
	default:
		for i := len(matches) - 1; i >= 0; i-- {
			if matches[i] < m.pv.cursor {
				target = matches[i]
				break
			}
		}
		if target == -1 {
			target = matches[len(matches)-1]
		}
	}

	m.pv.cursor = target
	m.syncViewer()
	position := 0
	for i, idx := range matches {
		if idx == target {
			position = i + 1
			break
		}
	}
	m.statusMsg = fmt.Sprintf("match %d/%d for %q · ] next · [ prev", position, len(matches), m.findQuery)
	return m, nil
}
