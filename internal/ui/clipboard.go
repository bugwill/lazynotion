package ui

import (
	"encoding/base64"
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type clipMsg struct {
	label string
	err   error
}

// copyCmd puts text on the clipboard: the platform tool when present,
// otherwise the OSC 52 escape (which also works over ssh, and through tmux
// via the same passthrough wrapping the graphics use).
func copyCmd(text, label string) tea.Cmd {
	return func() tea.Msg {
		if cmd := clipboardTool(); cmd != nil {
			cmd.Stdin = strings.NewReader(text)
			if err := cmd.Run(); err == nil {
				return clipMsg{label: label}
			}
		}
		tty := terminalTTY()
		if tty == nil {
			return clipMsg{err: fmt.Errorf("no clipboard tool and no tty")}
		}
		if _, err := tty.WriteString(wrapForTmux(osc52(text))); err != nil {
			return clipMsg{err: err}
		}
		return clipMsg{label: label}
	}
}

func osc52(text string) string {
	return "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(text)) + "\x07"
}

func clipboardTool() *exec.Cmd {
	if runtime.GOOS == "darwin" {
		return exec.Command("pbcopy")
	}
	for _, candidate := range [][]string{
		{"wl-copy"},
		{"xclip", "-selection", "clipboard"},
		{"xsel", "-ib"},
	} {
		if _, err := exec.LookPath(candidate[0]); err == nil {
			return exec.Command(candidate[0], candidate[1:]...)
		}
	}
	return nil
}

// blockLink deep-links to a block: the page URL plus the block anchor.
func blockLink(pageURL, blockID string) string {
	return pageURL + "#" + strings.ReplaceAll(blockID, "-", "")
}
