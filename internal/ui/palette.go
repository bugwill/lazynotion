package ui

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const paletteMaxRows = 8

// paletteEntry is one slash command. draftOnly commands change the block's
// type via markdown prefixes, which only works for not-yet-created blocks —
// the API cannot retype an existing block.
type paletteEntry struct {
	title     string
	hint      string
	aliases   []string
	draftOnly bool
	apply     func(*Model)
	// commitAfter saves the block and leaves the editor as soon as the
	// command applies — for blocks that have no text to keep typing
	commitAfter bool
	// applyCmd commands leave the editor and run an async action (page
	// creation); mutually exclusive with apply
	applyCmd func(*Model) tea.Cmd
}

var paletteCommands = []paletteEntry{
	{"Heading 1", "# ", []string{"h1", "heading1"}, true, linePrefix("# "), false, nil},
	{"Heading 2", "## ", []string{"h2", "heading2"}, true, linePrefix("## "), false, nil},
	{"Heading 3", "### ", []string{"h3", "heading3"}, true, linePrefix("### "), false, nil},
	{"To-do", "- [ ] ", []string{"todo", "task", "checkbox", "checklist"}, true, linePrefix("- [ ] "), false, nil},
	{"Bulleted list", "- ", []string{"bullet", "ul", "list"}, true, linePrefix("- "), false, nil},
	{"Numbered list", "1. ", []string{"numbered", "ol"}, true, linePrefix("1. "), false, nil},
	{"Quote", "> ", []string{"quote", "blockquote"}, true, linePrefix("> "), false, nil},
	{"Code block", "```", []string{"codeblock", "fence", "snippet"}, true, insertCodeFence, false, nil},
	{"Toggle", "▸ ", []string{"toggle", "fold", "collapse"}, true, insertToggle, false, nil},
	{"Page", "new sub-page, opens it", []string{"page", "subpage"}, true, nil, false, nil},
	{"Table", "| a | b |", []string{"table", "grid"}, true, insertTable, false, nil},
	{"Divider", "---", []string{"divider", "hr", "line"}, true, insertPlain("\n---\n"), true, nil},
	{"Bold", "**text**", []string{"bold", "b"}, false, wrapCursor("**"), false, nil},
	{"Italic", "*text*", []string{"italic", "i"}, false, wrapCursor("*"), false, nil},
	{"Strikethrough", "~~text~~", []string{"strike", "s", "del"}, false, wrapCursor("~~"), false, nil},
	{"Inline code", "`text`", []string{"code", "c", "mono"}, false, wrapCursor("`"), false, nil},
	{"Link", "[text](url)", []string{"link", "url", "a"}, false, insertLink, false, nil},
}

// pageCommand is attached in init: referencing it in the var literal would
// create an initialization cycle through the Model methods it calls.
func init() {
	for i := range paletteCommands {
		if paletteCommands[i].title == "Page" {
			paletteCommands[i].applyCmd = pageCommand
		}
	}
}

func linePrefix(prefix string) func(*Model) {
	return func(m *Model) {
		rows := strings.Split(m.editArea.Value(), "\n")
		row := clamp(m.editArea.Line(), 0, len(rows)-1)
		rows[row] = prefix + strings.TrimLeft(rows[row], " ")
		m.editArea.SetValue(strings.Join(rows, "\n"))
	}
}

func insertPlain(text string) func(*Model) {
	return func(m *Model) {
		m.editArea.InsertString(text)
	}
}

// insertToggle wraps the current line in toggle syntax (**▸ title**) with
// the cursor inside, ready to type the title; children can be added later
// with n while the toggle is under the cursor... or nested via indent in
// the same draft.
func insertToggle(m *Model) {
	rows := strings.Split(m.editArea.Value(), "\n")
	row := clamp(m.editArea.Line(), 0, len(rows)-1)
	content := strings.TrimLeft(rows[row], " ")
	rows[row] = "**▸ " + content + "**"
	m.editArea.SetValue(strings.Join(rows, "\n"))
	m.cursorBackInRow(2)
}

// insertTable scaffolds a 2x1 markdown table with the cursor in the first
// header cell; commit turns it into a real table block.
func insertTable(m *Model) {
	m.editArea.InsertString("| Column 1 | Column 2 |\n| --- | --- |\n|  |  |")
	m.editArea.CursorUp()
	m.editArea.CursorUp()
	m.editArea.SetCursor(2)
}

func insertCodeFence(m *Model) {
	m.editArea.InsertString("```\n\n```")
	m.editArea.CursorUp()
}

func insertLink(m *Model) {
	m.editArea.InsertString("[](url)")
	m.cursorBackInRow(6)
}

func wrapCursor(marker string) func(*Model) {
	return func(m *Model) {
		m.editArea.InsertString(marker + marker)
		m.cursorBackInRow(len([]rune(marker)))
	}
}

// cursorBackInRow steps the cursor back n runes, assuming it sits at the end
// of its logical row — true for the palette flows, which only trigger there.
func (m *Model) cursorBackInRow(n int) {
	rows := strings.Split(m.editArea.Value(), "\n")
	row := clamp(m.editArea.Line(), 0, len(rows)-1)
	m.editArea.SetCursor(len([]rune(rows[row])) - n)
}

// paletteTriggerOK keeps literal slashes (URLs, paths) out of the palette:
// it only opens on an empty editor or right after whitespace.
func paletteTriggerOK(value string) bool {
	if value == "" {
		return true
	}
	runes := []rune(value)
	last := runes[len(runes)-1]
	return last == ' ' || last == '\n'
}

func (m *Model) openPalette() {
	m.paletteOpen = true
	m.paletteQuery = ""
	m.paletteIndex = 0
	m.layout()
}

func (m *Model) closePalette() {
	m.paletteOpen = false
	m.layout()
}

func (m Model) editingDraft() bool {
	unit, ok := m.pv.current()
	return ok && unit.ID() == draftBlockID
}

func (m Model) filteredPalette() []paletteEntry {
	query := strings.ToLower(m.paletteQuery)
	draft := m.editingDraft()
	var out []paletteEntry
	for _, e := range paletteCommands {
		if e.draftOnly && !draft {
			continue
		}
		if query == "" || paletteMatches(e, query) {
			out = append(out, e)
		}
	}
	return out
}

func paletteMatches(e paletteEntry, query string) bool {
	if strings.Contains(strings.ToLower(e.title), query) {
		return true
	}
	for _, a := range e.aliases {
		if strings.HasPrefix(a, query) {
			return true
		}
	}
	return false
}

func (m Model) updatePalette(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	entries := m.filteredPalette()
	switch msg.String() {
	case "esc":
		m.closePalette()
		m.syncViewer()
		return m, nil
	case "enter", "tab":
		if len(entries) == 0 {
			m.editArea.InsertString("/" + m.paletteQuery)
			m.closePalette()
		} else {
			entry := entries[clamp(m.paletteIndex, 0, len(entries)-1)]
			m.closePalette()
			if entry.applyCmd != nil {
				cmd := entry.applyCmd(&m)
				m.resizeEditArea()
				m.syncViewer()
				return m, cmd
			}
			entry.apply(&m)
			if entry.commitAfter {
				value := m.editArea.Value()
				m.stopInlineEdit()
				return m.commitInlineEdit(value)
			}
		}
		m.resizeEditArea()
		m.syncViewer()
		return m, nil
	case " ":
		m.editArea.InsertString("/" + m.paletteQuery + " ")
		m.closePalette()
		m.resizeEditArea()
		m.syncViewer()
		return m, nil
	case "up", "ctrl+p":
		m.paletteIndex = clamp(m.paletteIndex-1, 0, max(len(entries)-1, 0))
		return m, nil
	case "down", "ctrl+n":
		m.paletteIndex = clamp(m.paletteIndex+1, 0, max(len(entries)-1, 0))
		return m, nil
	case "backspace":
		if m.paletteQuery == "" {
			m.closePalette()
			m.syncViewer()
			return m, nil
		}
		runes := []rune(m.paletteQuery)
		m.paletteQuery = string(runes[:len(runes)-1])
		m.paletteIndex = 0
		m.layout()
		return m, nil
	case "ctrl+c":
		m.closePalette()
		m.syncViewer()
		return m, nil
	}
	if msg.Type == tea.KeyRunes {
		m.paletteQuery += string(msg.Runes)
		m.paletteIndex = 0
		m.layout()
	}
	return m, nil
}

var (
	paletteQueryStyle    = lipgloss.NewStyle().Foreground(accentColor).Bold(true).Padding(0, 1)
	paletteItemStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Padding(0, 1)
	paletteSelectedStyle = lipgloss.NewStyle().Foreground(accentColor).Bold(true).Padding(0, 1)
	paletteHintStyle     = lipgloss.NewStyle().Foreground(dimColor)
)

func (m Model) paletteView() string {
	entries := m.filteredPalette()
	lines := make([]string, 0, len(entries)+1)
	for i, e := range entries {
		if i >= paletteMaxRows {
			break
		}
		marker, style := "  ", paletteItemStyle
		if i == clamp(m.paletteIndex, 0, max(len(entries)-1, 0)) {
			marker, style = "▸ ", paletteSelectedStyle
		}
		lines = append(lines, style.Render(marker+padRight(e.title, 16))+paletteHintStyle.Render(e.hint))
	}
	if len(entries) == 0 {
		lines = append(lines, paletteHintStyle.Render("  no matching command — enter inserts literally"))
	}
	lines = append(lines, paletteQueryStyle.Render("/"+m.paletteQuery+"▏"))
	return strings.Join(lines, "\n")
}

func padRight(s string, w int) string {
	if lipgloss.Width(s) >= w {
		return s
	}
	return s + strings.Repeat(" ", w-lipgloss.Width(s))
}

// pageCommand creates a sub-page of the current page and navigates into
// it. Text already typed in the draft becomes the title; an empty draft
// falls through to the title prompt.
func pageCommand(m *Model) tea.Cmd {
	if m.selected == nil {
		return nil
	}
	parent := *m.selected
	title := strings.TrimSpace(m.editArea.Value())
	m.stopInlineEdit()
	m.removeDraft()
	m.rebuildPage(true)

	if title == "" {
		m.newPageParent = &parent
		m.mode = inputNewPage
		m.input.Prompt = "new page under \"" + truncateText(parent.Title, 30) + "\": "
		m.input.SetValue("")
		m.input.CursorEnd()
		return m.input.Focus()
	}

	m.statusMsg = "creating page…"
	client := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		page, err := client.CreatePage(ctx, parent.ID, title)
		if err != nil {
			return errMsg{err}
		}
		return pageCreatedMsg{page: page, parentID: parent.ID}
	}
}
