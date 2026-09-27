package ui

import (
	"context"
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

// Sync status is shown only in the global footer.
func (m Model) sidebarContent() string {
	height := max(m.height-m.footerHeight()-2, 0)
	return lipgloss.NewStyle().Height(height).MaxHeight(height).Render(m.sidebar.View())
}
