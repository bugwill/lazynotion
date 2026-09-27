package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/justinm35/lazynotion/internal/notion"
)

type queryState struct {
	stream   <-chan tea.Msg
	progress notion.QueryProgress
	cancel   context.CancelFunc
}

type queryEvent struct {
	kind     string
	gen      int
	stream   <-chan tea.Msg
	payload  tea.Msg
	start    bool
	sequence uint64
	cancel   context.CancelFunc
}

// Stream progress through Bubble Tea messages; workers never mutate the model.
func streamQuery(kind string, gen int, run func(func(notion.QueryProgress)) tea.Msg) tea.Msg {
	ch := make(chan tea.Msg, 32)
	go func() {
		defer close(ch)
		ch <- run(func(p notion.QueryProgress) { ch <- p })
	}()
	return queryEvent{kind: kind, gen: gen, stream: ch, payload: notion.QueryProgress{Stage: "search"}, start: true}
}

func streamQueryWithContext(kind string, gen int, timeout time.Duration, run func(context.Context, func(notion.QueryProgress)) tea.Msg) tea.Msg {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	event := streamQuery(kind, gen, func(report func(notion.QueryProgress)) tea.Msg {
		defer cancel()
		return run(ctx, report)
	}).(queryEvent)
	event.cancel = cancel
	return event
}

func nextQueryEvent(event queryEvent) tea.Cmd {
	return func() tea.Msg {
		payload, ok := <-event.stream
		if !ok {
			return nil
		}
		event.payload, event.start = payload, false
		return event
	}
}

func (m Model) handleQueryEvent(event queryEvent) (tea.Model, tea.Cmd) {
	// Always drain superseded streams so their producers can finish safely.
	next := nextQueryEvent(event)
	if event.gen != m.wsGen {
		if event.cancel != nil {
			event.cancel()
		}
		return m, next
	}
	state := &m.pagesQuery
	if event.kind == "recent" {
		state = &m.recentQuery
	}
	if event.start {
		if event.kind == "recent" && event.sequence > 0 {
			if event.sequence < m.recentSequence {
				if event.cancel != nil {
					event.cancel()
				}
				return m, next
			}
			m.recentSequence = event.sequence
		}
		if *state != nil && (*state).stream == event.stream {
			return m, nil
		}
		if *state != nil && (*state).cancel != nil {
			(*state).cancel()
		}
		m.err = nil
		*state = &queryState{stream: event.stream, cancel: event.cancel}
	}
	if *state == nil || (*state).stream != event.stream {
		return m, next
	}
	if p, ok := event.payload.(notion.QueryProgress); ok {
		(*state).progress = p
		return m, next
	}
	if rows, ok := event.payload.(recentMsg); ok && rows.partial {
		updated, cmd := m.update(rows)
		return updated, tea.Batch(cmd, next)
	}
	*state = nil
	updated, cmd := m.update(event.payload)
	return updated, tea.Batch(cmd, next)
}

var queryBlue = lipgloss.NewStyle().Foreground(lipgloss.Color("117"))

// Keep query progress visible when the Pages pane is hidden inside a page.
func (m Model) pageQueryFooter() string {
	state := m.recentQuery
	if state == nil {
		state = m.pagesQuery
	}
	if m.selected == nil || state == nil {
		return ""
	}
	p := state.progress
	label := queryProgressLabel(p)
	barWidth := min(8, max(m.width/4, 1))
	label = truncateText(label, max(m.width-barWidth-2, 1))
	filled := 0
	if p.Total > 0 {
		filled = clamp(p.Done*barWidth/p.Total, 0, barWidth)
		return queryBlue.Render(label+" "+strings.Repeat("━", filled)) + pageMetaStyle.Render(strings.Repeat("─", barWidth-filled))
	}
	position := m.pulseFrame % barWidth
	return queryBlue.Render(label+" ") + pageMetaStyle.Render(strings.Repeat("─", position)) + queryBlue.Render("━") + pageMetaStyle.Render(strings.Repeat("─", barWidth-position-1))
}

func queryProgressLabel(p notion.QueryProgress) string {
	label := map[string]string{"search": "Searching", "databases": "Updating databases", "checking": "Checking pages", "pages": "Updating pages", "content": "Loading content", "rows": "Updating database rows"}[p.Stage]
	if label == "" {
		label = "Updating"
	}
	if p.Total > 0 {
		label += fmt.Sprintf(" %d/%d", p.Done, p.Total)
	} else if p.Done > 0 {
		label += fmt.Sprintf(" · %d found", p.Done)
	}
	return label
}

func (m Model) sidebarContent() string {
	height := max(m.height-m.footerHeight()-2, 0)
	if height < 3 {
		return m.sidebar.View()
	}
	body := lipgloss.NewStyle().Height(height - 2).MaxHeight(height - 2).Render(m.sidebar.View())
	state := m.recentQuery
	if state == nil {
		state = m.pagesQuery
	}
	if state == nil && !m.loading && !m.pageLoading {
		return body + "\n\n"
	}
	p := notion.QueryProgress{Stage: "search"}
	if state == nil && m.pageLoading && !m.loading && m.recent == nil {
		p.Stage = "content"
	}
	if state != nil {
		p = state.progress
	}
	width := max(m.sidebar.Width(), 1)
	label := queryProgressLabel(p)
	filled := 0
	if p.Total > 0 {
		filled = clamp(p.Done*width/p.Total, 0, width)
	}
	bar := ""
	if p.Total > 0 {
		bar = queryBlue.Render(strings.Repeat("━", filled)) + pageMetaStyle.Render(strings.Repeat("─", width-filled))
	} else {
		// Pagination has no known total: animate instead of inventing a percent.
		segment := min(4, width)
		start := m.pulseFrame % max(width-segment+1, 1)
		bar = pageMetaStyle.Render(strings.Repeat("─", start)) + queryBlue.Render(strings.Repeat("━", segment)) + pageMetaStyle.Render(strings.Repeat("─", width-start-segment))
	}
	return body + "\n" + queryBlue.Render(truncateText(label, width)) + "\n" + bar
}
