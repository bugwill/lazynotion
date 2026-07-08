package ui

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	normalTitleStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	normalDescStyle    = lipgloss.NewStyle().Foreground(dimColor)
	selectedTitleStyle = lipgloss.NewStyle().Foreground(accentColor).Bold(true)
	selectedDescStyle  = lipgloss.NewStyle().Foreground(accentDimColor)
)

// pageDelegate renders sidebar rows itself so page icons can carry their own
// colors — embedding ANSI inside the default delegate's styles would reset
// the row styling mid-line.
type pageDelegate struct{}

func (pageDelegate) Height() int                         { return 2 }
func (pageDelegate) Spacing() int                        { return 1 }
func (pageDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }
func (pageDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	it, ok := item.(pageItem)
	if !ok {
		return
	}
	width := m.Width() - 2
	if width < 8 {
		return
	}
	selected := index == m.Index()

	titleStyle, descStyle, bar := normalTitleStyle, normalDescStyle, "  "
	if selected {
		titleStyle, descStyle = selectedTitleStyle, selectedDescStyle
		bar = lipgloss.NewStyle().Foreground(accentColor).Render("▎") + " "
	}

	title := truncateText(it.page.Title, width-2)
	desc := truncateText("edited "+relTime(it.page.LastEdited), width-2)

	fmt.Fprintf(w, "%s%s\n%s%s",
		bar, titleStyle.Render(title),
		bar, descStyle.Render(desc))
}

// truncateText shortens plain (ANSI-free) text to max display cells.
func truncateText(s string, max int) string {
	if max < 1 {
		return ""
	}
	if lipgloss.Width(s) <= max {
		return s
	}
	var b strings.Builder
	w := 0
	for _, r := range s {
		rw := lipgloss.Width(string(r))
		if w+rw > max-1 {
			break
		}
		b.WriteRune(r)
		w += rw
	}
	return b.String() + "…"
}
