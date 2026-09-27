package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/justinm35/lazynotion/internal/notion"
)

func TestPageShowsOnlyGlobalSyncStatus(t *testing.T) {
	m := New([]Workspace{{Name: "fixture"}}, 0, nil, "mosaic")
	m.width, m.height, m.loading = 80, 24, false
	page := notion.Page{ID: "fixture-page", Title: "Fixture"}
	m.selected, m.focus = &page, focusViewer
	m.recentQuery = &queryState{progress: notion.QueryProgress{Stage: "pages", Done: 51, Total: 106}}
	m.layout()
	view := stripAnsi(m.View())
	if strings.Contains(view, "Updating pages") || strings.Contains(view, "51/106") || strings.Contains(view, "━") || strings.Count(view, "同步中") != 1 {
		t.Fatalf("page should show only one static sync status: %s", view)
	}
	m.recentQuery = nil
	if strings.Contains(stripAnsi(m.View()), "同步中") {
		t.Fatal("completed progress stayed visible")
	}
}

func TestFooterFitsNarrowTerminalDuringEditing(t *testing.T) {
	m := New([]Workspace{{Name: "fixture"}}, 0, nil, "mosaic")
	m.width, m.height, m.loading, m.editing = 37, 24, false, true
	for _, syncing := range []bool{false, true} {
		m.moveSyncing = syncing
		line := m.footerLine()
		if strings.Contains(line, "\n") || lipgloss.Width(line) > m.width {
			t.Fatalf("footer wrapped outside one reserved row: %q", line)
		}
	}
}
