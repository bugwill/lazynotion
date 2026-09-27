package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/justinm35/lazynotion/internal/notion"
)

func TestProgressStaysAtSidebarBottomAndClearsOnCompletion(t *testing.T) {
	m := New([]Workspace{{Name: "test"}}, 0, nil, "mosaic")
	m.width, m.height = 90, 30
	m.loading = false
	m.layout()
	ch := make(chan tea.Msg)
	event := queryEvent{kind: "recent", gen: 0, stream: ch, start: true, payload: notion.QueryProgress{Stage: "pages", Done: 2, Total: 4}}
	next, _ := m.Update(event)
	m = next.(Model)
	lines := strings.Split(m.sidebarContent(), "\n")
	if !strings.Contains(lines[len(lines)-2], "Updating pages 2/4") {
		t.Fatal("progress must be at the bottom of Pages")
	}
	if lipgloss.Width(lines[len(lines)-1]) != m.sidebar.Width() {
		t.Fatal("bar must fit sidebar width")
	}
	if !m.syncing() {
		t.Fatal("background query must show the sync status")
	}
	event.start = false
	event.payload = recentMsg{}
	next, _ = m.Update(event)
	m = next.(Model)
	if m.recentQuery != nil || strings.Contains(m.sidebarContent(), "Updating pages") {
		t.Fatal("completed query must hide progress even after leaving Recent")
	}
}

func TestProgressIgnoresOldWorkspaceAndClearsOnFailure(t *testing.T) {
	m := New([]Workspace{{Name: "test"}}, 0, nil, "mosaic")
	m.loading = false
	m.wsGen = 1
	ch := make(chan tea.Msg)
	event := queryEvent{kind: "pages", gen: 0, stream: ch, start: true, payload: notion.QueryProgress{Stage: "search"}}
	next, _ := m.Update(event)
	m = next.(Model)
	if m.pagesQuery != nil {
		t.Fatal("old workspace progress must be ignored")
	}
	event.gen = 1
	next, _ = m.Update(event)
	m = next.(Model)
	event.start = false
	event.payload = errMsg{fmt.Errorf("fixture failure")}
	next, _ = m.Update(event)
	m = next.(Model)
	if m.pagesQuery != nil || m.err == nil {
		t.Fatal("failure must hide progress and preserve the error")
	}
}

func TestSupersededProgressDoesNotHideNewQuery(t *testing.T) {
	m := New([]Workspace{{Name: "test"}}, 0, nil, "mosaic")
	oldStream, newStream := make(chan tea.Msg), make(chan tea.Msg)
	m.pagesQuery = &queryState{stream: newStream}
	event := queryEvent{kind: "pages", gen: 0, stream: oldStream, payload: pagesMsg{}}
	next, _ := m.Update(event)
	m = next.(Model)
	if m.pagesQuery == nil || m.pagesQuery.stream != newStream {
		t.Fatal("old completion must not erase a new query's progress")
	}
}

func TestOldWorkspaceStreamIsDrained(t *testing.T) {
	done := make(chan struct{})
	event := streamQuery("recent", 0, func(report func(notion.QueryProgress)) tea.Msg {
		defer close(done)
		for i := 0; i < 80; i++ {
			report(notion.QueryProgress{Stage: "pages", Done: i, Total: 80})
		}
		return errMsg{fmt.Errorf("old workspace error")}
	})
	m := New([]Workspace{{Name: "test"}}, 0, nil, "mosaic")
	m.loading, m.wsGen = false, 1
	for event != nil {
		next, cmd := m.Update(event)
		m = next.(Model)
		if cmd == nil {
			t.Fatal("stale stream must keep draining")
		}
		event = cmd()
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("producer did not finish")
	}
	if m.err != nil || m.recentQuery != nil {
		t.Fatal("old workspace messages must not alter current UI")
	}
}

func TestUnknownProgressUsesStaticSyncStatus(t *testing.T) {
	m := New([]Workspace{{Name: "test"}}, 0, nil, "mosaic")
	m.width, m.height = 90, 30
	m.layout()
	m.pagesQuery = &queryState{progress: notion.QueryProgress{Stage: "search"}}
	if !strings.Contains(stripAnsi(m.sidebarContent()), "同步中") {
		t.Fatal("unknown sidebar progress must show static sync status")
	}
	m.selected = &notion.Page{ID: "page", Title: "Page"}
	if got := stripAnsi(m.pageQueryFooter()); got != "同步中" {
		t.Fatalf("unknown page progress = %q", got)
	}
}
