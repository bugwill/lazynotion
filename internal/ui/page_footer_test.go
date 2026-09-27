package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/justinm35/lazynotion/internal/notion"
)

func TestPageKeepsQueryProgressWithPagesHidden(t *testing.T) {
	m := New([]Workspace{{Name: "fixture"}}, 0, nil, "mosaic")
	m.width, m.height, m.loading = 80, 24, false
	page := notion.Page{ID: "fixture-page", Title: "Fixture"}
	m.selected, m.focus = &page, focusViewer
	m.recentQuery = &queryState{progress: notion.QueryProgress{Stage: "pages", Done: 51, Total: 106}}
	m.layout()
	view := stripAnsi(m.View())
	if strings.Contains(view, "Pages") || !strings.Contains(view, "Updating pages 51/106") || !strings.Contains(view, "━") {
		t.Fatalf("page lost progress or restored the hidden sidebar: %s", view)
	}
	m.recentQuery = nil
	if strings.Contains(stripAnsi(m.View()), "Updating pages") {
		t.Fatal("completed progress stayed visible")
	}
}

func TestFooterFitsNarrowTerminalDuringEditing(t *testing.T) {
	m := New([]Workspace{{Name: "fixture"}}, 0, nil, "mosaic")
	m.width, m.height, m.loading, m.editing = 37, 24, false, true
	for _, memory := range []string{"", "12 MB"} {
		m.memUsage = memory
		line := m.footerLine()
		if strings.Contains(line, "\n") || lipgloss.Width(line) > m.width {
			t.Fatalf("footer wrapped outside one reserved row: %q", line)
		}
	}
}
