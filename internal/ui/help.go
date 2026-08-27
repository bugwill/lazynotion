package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	helpBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(accentColor).
			Padding(1, 3)
	helpSectionStyle = lipgloss.NewStyle().Bold(true).Foreground(accentColor)
	helpKeyStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	helpNoteStyle    = lipgloss.NewStyle().Foreground(dimColor)
)

func helpRow(key, desc string) string {
	return helpKeyStyle.Render(fmt.Sprintf("%-14s", key)) + helpNoteStyle.Render(desc)
}

func (m Model) helpView() string {
	lines := []string{
		helpSectionStyle.Render("global"),
		helpRow("tab / 1 / 2", "switch pane focus"),
		helpRow("/", "search the workspace"),
		helpRow("r", "refresh current page (viewer) / everything (sidebar)"),
		helpRow("ctrl+o", "open current page in browser"),
		helpRow("?", "toggle this help"),
		helpRow("q / ctrl+c", "quit"),
		"",
		helpSectionStyle.Render("sidebar"),
		helpRow("j / k", "move between pages"),
		helpRow("enter", "open page"),
		helpRow("n", "new page under the highlighted one"),
		helpRow("w", "switch workspace"),
		"",
		helpSectionStyle.Render("viewer"),
		helpRow("j / k", "move block cursor"),
		helpRow("g / G", "first / last block"),
		helpRow("enter", "open sub-page / fold toggle / new block below"),
		helpRow("esc", "back to parent page, then sidebar"),
		helpRow("space", "toggle to-do"),
		helpRow("i / e", "edit block in place"),
		helpRow("n / N", "new block below / above cursor"),
		helpRow("J / K", "move block down / up"),
		helpRow("a", "append blocks after cursor"),
		helpRow("d", "delete block"),
		helpRow("u", "undo delete / edit / toggle"),
		helpRow("y / Y", "yank block content / copy block link"),
		helpRow("v", "visual select blocks (j/k extend, y yank)"),
		helpRow("I", "set page icon (emoji or \"name color\")"),
		helpRow("/", "find in page"),
		helpRow("] / [", "next / previous match"),
		helpRow("E", "edit whole page in $EDITOR"),
		"",
		helpSectionStyle.Render("database view (read-only)"),
		helpRow("j / k", "move between rows"),
		helpRow("h / l", "scroll columns (title stays pinned)"),
		helpRow("ctrl+d / u", "half-page down / up"),
		helpRow("enter", "open the row as a page"),
		helpRow("esc", "back to the grid / previous page"),
		"",
		helpNoteStyle.Render("inline editor: esc saves · shift/alt+enter saves & opens next · enter continues lists / adds a line · ctrl+c discards"),
		helpNoteStyle.Render("/ in the editor opens the command palette: headings, lists, bold, links, …"),
		helpNoteStyle.Render("markdown works everywhere: **bold** · *italic* · `code` · [link](url) · ~~strike~~"),
	}
	box := helpBoxStyle.Render(strings.Join(lines, "\n"))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}
