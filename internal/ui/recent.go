package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/justinm35/lazynotion/internal/notion"
)

const recentID = "lazynotion:recent"

var recentPage = notion.Page{ID: recentID, Title: "Recent"}

func startsInRecent(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "recent":
		return true
	default:
		return false
	}
}

type recentState struct {
	pages  []notion.Page
	cursor int
	loaded bool
}

type recentMsg struct {
	pages     []notion.Page
	wsGen     int
	fromCache bool
	refresh   bool
	partial   bool
	err       error
}

func (m Model) openRecent() (tea.Model, tea.Cmd) {
	m.selected = &recentPage
	m.db = nil
	m.recent = &recentState{}
	m.history = nil
	m.pendingRestore = nil
	m.pv = pageView{}
	m.renderedID = ""
	m.visualAnchor = -1
	m.focus = focusViewer
	m.pageLoading = true
	m.err = nil
	m.layout()
	return m, m.loadRecent(false)
}

func (m Model) loadRecent(force bool) tea.Cmd {
	client, store, gen, loader := m.client, m.store, m.wsGen, m.recentLoader
	return func() tea.Msg { return loader.load(client, store, gen, force) }
}

func (m Model) handleRecent(msg recentMsg) (tea.Model, tea.Cmd) {
	if msg.wsGen != m.wsGen {
		return m, nil
	}
	state := m.recent
	if state == nil {
		for i := range m.history {
			if m.history[i].recent != nil {
				state = m.history[i].recent
				break
			}
		}
	}
	if state == nil {
		return m, nil
	}
	previous := ""
	if len(state.pages) > 0 {
		previous = state.pages[clamp(state.cursor, 0, len(state.pages)-1)].ID
	}
	state.pages, state.loaded, state.cursor = msg.pages, true, 0
	for i, p := range msg.pages {
		if p.ID == previous {
			state.cursor = i
			break
		}
	}
	if m.recent != nil {
		m.pageLoading = false
		if msg.err != nil {
			if errors.Is(msg.err, context.DeadlineExceeded) {
				m.err = fmt.Errorf("Recent update timed out; completed results cached. Press r for quick refresh or R to resume deep scan")
			} else {
				m.err = fmt.Errorf("Recent partially updated: %w", msg.err)
			}
		}
	}
	if msg.fromCache && msg.refresh {
		return m, m.loadRecent(true)
	}
	return m, nil
}

func (m Model) updateRecentKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		m.recent.cursor = clamp(m.recent.cursor+1, 0, max(len(m.recent.pages)-1, 0))
	case "k", "up":
		m.recent.cursor = max(m.recent.cursor-1, 0)
	case "g":
		m.recent.cursor = 0
	case "G":
		m.recent.cursor = max(len(m.recent.pages)-1, 0)
	}
	return m, nil
}

func (m Model) openRecentItem() (tea.Model, tea.Cmd) {
	if len(m.recent.pages) == 0 {
		return m, nil
	}
	p := m.recent.pages[m.recent.cursor]
	m.pageLoading = false
	m.history = append(m.history, historyEntry{page: recentPage, recent: m.recent})
	if p.Kind == notion.KindDataSource {
		return m.openDatabaseView(p)
	}
	return m.openPage(p)
}

func (m Model) recentView() string {
	if !m.recent.loaded {
		return pageMetaStyle.Render("loading Recent…")
	}
	rows := []string{pageMetaStyle.Render(fmt.Sprintf("%d recently modified pages/databases · newest first", len(m.recent.pages)))}
	if len(m.recent.pages) == 0 {
		return strings.Join(append(rows, "no accessible pages or databases"), "\n")
	}
	height := max(m.viewer.Height-2, 1)
	start := max(m.recent.cursor-height+1, 0)
	for i := start; i < min(start+height, len(m.recent.pages)); i++ {
		p := m.recent.pages[i]
		title := p.Title
		if p.Kind == notion.KindDataSource {
			title += " · database"
		}
		edited := " · edited " + relTime(p.LastEdited)
		title = truncateText(title, max(m.viewer.Width-3-len(edited), 1)) + edited
		if i == m.recent.cursor {
			rows = append(rows, selectedTitleStyle.Render("› "+title))
		} else {
			rows = append(rows, "  "+title)
		}
	}
	return strings.Join(rows, "\n")
}
