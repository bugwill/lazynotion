package ui

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/truncate"
)

var (
	accentColor    = lipgloss.Color("117")
	accentDimColor = lipgloss.Color("110")
	dimColor       = lipgloss.Color("241")
	errColor       = lipgloss.Color("203")
	warnColor      = lipgloss.Color("214")

	borderTitleFocused = lipgloss.NewStyle().Bold(true).Foreground(accentColor)
	borderTitleBlurred = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("252"))

	focusedPaneStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(accentColor)

	blurredPaneStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(dimColor)

	statusStyle  = lipgloss.NewStyle().Foreground(dimColor).Padding(0, 1)
	errStyle     = lipgloss.NewStyle().Foreground(errColor).Padding(0, 1)
	confirmStyle = lipgloss.NewStyle().Foreground(warnColor).Bold(true).Padding(0, 1)

	pageMetaStyle = lipgloss.NewStyle().Foreground(dimColor)
)

const (
	sidebarHelp = "j/k move · enter open · n new page · w workspace · / search · ? help · q quit"
	viewerHelp  = "j/k blocks · enter fold/open · i edit · n/N new · d delete · u undo · / find · esc back · ? help"
	dbHelp      = "j/k rows · h/l columns · enter open row · y copy link · r refresh · esc back · ? help"
)

func (m Model) View() string {
	if m.width == 0 {
		return "loading…"
	}
	if m.showHelp {
		return m.helpView()
	}

	paneHeight := m.height - m.footerHeight()
	sidebarWidth := clamp(m.width/3, 24, 40)
	viewerWidth := m.width - sidebarWidth

	sidebarStyle, viewerStyle := blurredPaneStyle, blurredPaneStyle
	if m.focus == focusSidebar {
		sidebarStyle = focusedPaneStyle
	} else {
		viewerStyle = focusedPaneStyle
	}

	sidebar := sidebarStyle.
		Width(sidebarWidth - 2).
		Height(paneHeight - 2).
		Render(m.sidebar.View())
	sidebar = withBorderTitle(sidebar, sidebarTitle(m.workspaces, m.wsIndex), m.focus == focusSidebar)

	viewer := viewerStyle.
		Width(viewerWidth - 2).
		Height(paneHeight - 2).
		Render(m.viewerContent())
	viewer = withBorderTitle(viewer, m.viewerTitle(), m.focus == focusViewer)

	panes := lipgloss.JoinHorizontal(lipgloss.Top, sidebar, viewer)
	footer := m.footerLine()
	if m.paletteOpen {
		footer = m.paletteView()
	}
	return lipgloss.JoinVertical(lipgloss.Left, panes, footer)
}

// footerLine right-aligns the sync indicator and memory readout.
func (m Model) footerLine() string {
	left := m.statusLine()
	right := ""
	if m.syncing() {
		shade := pulseShades[m.pulseFrame%len(pulseShades)]
		right = lipgloss.NewStyle().Foreground(lipgloss.Color(shade)).Render("⇅") + " "
	}
	if m.memUsage != "" || right != "" {
		right = right + statusStyle.Render(m.memUsage)
	}
	if right == "" {
		return left
	}
	right = lipgloss.NewStyle().Render(right)
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		// the sync/memory readout outranks the tail of the help text
		left = truncate.String(left, uint(max(m.width-lipgloss.Width(right)-1, 0)))
		gap = max(m.width-lipgloss.Width(left)-lipgloss.Width(right), 1)
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m Model) viewerTitle() string {
	if m.selected == nil {
		return ""
	}
	return m.selected.Title
}

// withBorderTitle embeds a title into a bordered pane's top edge —
// lazygit-style emphasis through position and weight instead of a
// background pill, which reads poorly on accent colors.
func withBorderTitle(box, title string, focused bool) string {
	if title == "" {
		return box
	}
	lines := strings.SplitN(box, "\n", 2)
	if len(lines) < 2 {
		return box
	}
	width := lipgloss.Width(lines[0])

	titleStyle, borderColor := borderTitleBlurred, dimColor
	if focused {
		titleStyle, borderColor = borderTitleFocused, accentColor
	}
	title = truncateText(title, max(width-8, 4))
	styled := titleStyle.Render(title)
	rest := width - lipgloss.Width(styled) - 6
	if rest < 0 {
		return box
	}
	border := lipgloss.NewStyle().Foreground(borderColor)
	top := border.Render("╭─") + " " + styled + " " + border.Render(strings.Repeat("─", rest)+"─╮")
	return top + "\n" + lines[1]
}

func (m Model) viewerContent() string {
	if m.db != nil {
		return m.dbGridView()
	}
	var hint string
	switch {
	case m.selected == nil:
		hint = "select a page and press enter"
	// a same-page background reload (after a write) keeps the current
	// content on screen instead of flashing the loading placeholder
	case m.pageLoading && m.renderedID != m.selected.ID:
		hint = "loading page…"
	default:
		return m.viewer.View()
	}
	return lipgloss.Place(
		m.viewer.Width, m.viewer.Height,
		lipgloss.Center, lipgloss.Center,
		pageMetaStyle.Render(hint),
	)
}

func (m Model) statusLine() string {
	switch {
	case m.editing:
		return confirmStyle.Render("editing block · esc save · ctrl+c discard")
	case m.confirm != nil:
		prompt := fmt.Sprintf("replace %q in notion?", m.confirm.page.Title)
		if m.confirm.warning != "" {
			prompt += " (" + m.confirm.warning + ")"
		}
		return confirmStyle.Render(prompt + " y/n")
	case m.mode != inputNone:
		return m.input.View()
	case m.loading:
		return statusStyle.Render("searching notion…")
	case m.pageLoading:
		return statusStyle.Render("loading page…")
	case m.err != nil:
		return errStyle.Render("error: " + m.err.Error())
	case m.statusMsg != "":
		return statusStyle.Render(m.statusMsg)
	case m.focus == focusViewer && m.db != nil:
		return statusStyle.Render(dbHelp)
	case m.focus == focusViewer:
		return statusStyle.Render(viewerHelp)
	default:
		return statusStyle.Render(fmt.Sprintf("%d pages · %s", len(m.sidebar.Items()), sidebarHelp))
	}
}

func openInBrowser(url string) tea.Cmd {
	return func() tea.Msg {
		var cmd *exec.Cmd
		switch runtime.GOOS {
		case "darwin":
			cmd = exec.Command("open", url)
		case "windows":
			cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
		default:
			cmd = exec.Command("xdg-open", url)
		}
		if err := cmd.Start(); err != nil {
			return errMsg{err}
		}
		return nil
	}
}
