package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/cursor"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/jomei/notionapi"
	"github.com/justinm35/lazynotion/internal/notion"
)

type titleSavedMsg struct {
	page  notion.Page
	wsGen int
	err   error
}

func (m *Model) configureInputCursor(mode inputMode) {
	if mode == inputTitle {
		// The title view draws its own caret, independent of terminal
		// cursor colors and reverse-video support.
		m.input.Cursor.SetMode(cursor.CursorHide)
		return
	}
	m.input.Cursor.Style = lipgloss.NewStyle()
	m.input.Cursor.SetMode(cursor.CursorBlink)
}

func (m Model) titleInputView() string {
	prompt := m.input.PromptStyle.Render(m.input.Prompt)
	width := max(m.width-lipgloss.Width(prompt), 1)
	value := []rune(m.input.Value())
	position := clamp(m.input.Position(), 0, len(value))
	left, right := string(value[:position]), string(value[position:])
	leftLimit := width - 1 // always reserve a cell for the caret
	if right != "" {
		leftLimit = max(leftLimit-2, 0) // keep the next CJK character visible
	}
	left = ansi.TruncateLeft(left, max(ansi.StringWidth(left)-leftLimit, 0), "")
	right = ansi.Truncate(right, max(width-ansi.StringWidth(left)-1, 0), "")
	caret := lipgloss.NewStyle().Foreground(lipgloss.Color("#000000")).Render("│")
	return prompt + m.input.TextStyle.Render(left) + caret + m.input.TextStyle.Render(right)
}

func (m Model) titleClicked(msg tea.MouseMsg) bool {
	if msg.Y != 0 || msg.Button != tea.MouseButtonLeft || msg.Action != tea.MouseActionPress ||
		m.selected == nil || m.recent != nil || m.db != nil || m.pageLoading ||
		m.editing || m.showHelp || m.paletteOpen || m.confirm != nil || m.mode != inputNone {
		return false
	}
	width := lipgloss.Width(truncateText(m.viewerTitle(), max(m.width-8, 4)))
	return msg.X >= 3 && msg.X < 3+width
}

func (m Model) startTitleEdit() (tea.Model, tea.Cmd) {
	if m.localBusy() {
		m.statusMsg = "wait for the current save before renaming"
		return m, nil
	}
	m.readPending, m.readSelection = nil, nil
	m.viewerSetContent()
	return m.startInput(inputTitle, "title: ", m.selected.Title)
}

func (m Model) saveTitle(value string) (tea.Model, tea.Cmd) {
	if m.selected == nil || m.recent != nil || m.db != nil || m.localBusy() {
		return m, nil
	}
	title := strings.TrimSpace(value)
	if title == "" {
		m.statusMsg = "title cannot be empty"
		return m, nil
	}
	if title == m.selected.Title {
		return m, nil
	}
	page, client, generation := *m.selected, m.client, m.wsGen
	page.Title = title
	m.err = nil
	m.writesInFlight++
	m.statusMsg = "saving title…"
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := client.SetPageTitle(ctx, page.ID, title)
		return titleSavedMsg{page: page, wsGen: generation, err: err}
	}
}

func (m Model) handleTitleSaved(msg titleSavedMsg) (tea.Model, tea.Cmd) {
	m.writesInFlight = max(m.writesInFlight-1, 0)
	if msg.wsGen != m.wsGen {
		return m, nil
	}
	if msg.err != nil {
		m.err = fmt.Errorf("save title: %w", msg.err)
		m.statusMsg = "title was not saved"
		return m, nil
	}
	id, title := msg.page.ID, msg.page.Title
	if m.selected != nil && m.selected.ID == id {
		page := *m.selected
		page.Title = title
		m.selected = &page
	}
	var cmds []tea.Cmd
	for i, item := range m.sidebar.Items() {
		if pi, ok := item.(pageItem); ok && pi.page.ID == id {
			pi.page.Title = title
			cmds = append(cmds, m.sidebar.SetItem(i, pi))
		}
	}
	for i := range m.defaultPages {
		if m.defaultPages[i].ID == id {
			m.defaultPages[i].Title = title
		}
	}
	for i := range m.history {
		if m.history[i].page.ID == id {
			m.history[i].page.Title = title
		}
		if recent := m.history[i].recent; recent != nil {
			for j := range recent.pages {
				if recent.pages[j].ID == id {
					recent.pages[j].Title = title
				}
			}
		}
		if db := m.history[i].db; db != nil && db.ds != nil {
			for j := range db.rows {
				if db.rows[j].ID == id {
					name := db.ds.TitleProperty()
					property := db.rows[j].Properties[name]
					property.Text = title
					db.rows[j].Properties[name] = property
				}
			}
		}
	}
	for pageID, nodes := range m.blockCache {
		if renameChildPageReferences(nodes, id, title) && m.store != nil {
			m.store.Invalidate(pageID)
		}
	}
	if m.recent != nil {
		for i := range m.recent.pages {
			if m.recent.pages[i].ID == id {
				m.recent.pages[i].Title = title
			}
		}
	}
	m.recentLoader.remember(m.client, m.store, msg.page, true)
	if m.store != nil {
		m.store.Invalidate(id)
	}
	m.statusMsg = "title saved"
	return m, tea.Batch(cmds...)
}

func renameChildPageReferences(nodes []notion.BlockNode, id, title string) bool {
	changed := false
	for _, node := range nodes {
		if child, ok := node.Block.(*notionapi.ChildPageBlock); ok && string(child.ID) == id {
			child.ChildPage.Title = title
			changed = true
		}
		if renameChildPageReferences(node.Children, id, title) {
			changed = true
		}
	}
	return changed
}
