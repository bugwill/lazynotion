package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jomei/notionapi"

	"github.com/justinm35/lazynotion/internal/convert"
	"github.com/justinm35/lazynotion/internal/notion"
)

func (m Model) openInEditor() (tea.Model, tea.Cmd) {
	if m.selected == nil {
		return m, nil
	}
	blocks, ok := m.blockCache[m.selected.ID]
	if !ok {
		return m, nil
	}
	md := convert.ToMarkdown(blocks)
	path := filepath.Join(os.TempDir(), "lazynotion-"+m.selected.ID+".md")
	if err := os.WriteFile(path, []byte(md), 0o600); err != nil {
		m.err = err
		return m, nil
	}
	page := *m.selected
	cmd := exec.Command(editorCommand(), path)
	return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
		return editorDoneMsg{page: page, path: path, orig: md, err: err}
	})
}

func editorCommand() string {
	for _, env := range []string{"VISUAL", "EDITOR"} {
		if e := os.Getenv(env); e != "" {
			return e
		}
	}
	return "vim"
}

func (m Model) handleEditorDone(msg editorDoneMsg) (tea.Model, tea.Cmd) {
	defer os.Remove(msg.path)
	if msg.err != nil {
		m.err = fmt.Errorf("editor: %w", msg.err)
		return m, nil
	}
	edited, err := os.ReadFile(msg.path)
	if err != nil {
		m.err = err
		return m, nil
	}
	if strings.TrimSpace(string(edited)) == strings.TrimSpace(msg.orig) {
		m.statusMsg = "no changes"
		return m, nil
	}

	blocks := convert.ParseMarkdown(string(edited))
	warning := lossyWarning(m.blockCache[msg.page.ID])
	m.confirm = &pendingReplace{page: msg.page, blocks: blocks, warning: warning}
	return m, nil
}

// lossyWarning names block types on the page that a markdown round-trip
// simplifies or drops, so the user can bail out before replacing.
func lossyWarning(nodes []notion.BlockNode) string {
	found := map[string]bool{}
	var walk func([]notion.BlockNode)
	walk = func(ns []notion.BlockNode) {
		for _, n := range ns {
			switch b := n.Block.(type) {
			case *notionapi.ImageBlock:
				if b.Image.File != nil {
					found["uploaded images"] = true
				}
			case *notionapi.CalloutBlock:
				found["callouts"] = true
			case *notionapi.ColumnListBlock:
				found["columns"] = true
			case *notionapi.SyncedBlock:
				found["synced blocks"] = true
			case *notionapi.VideoBlock, *notionapi.FileBlock, *notionapi.PdfBlock, *notionapi.AudioBlock:
				found["file attachments"] = true
			case *notionapi.EmbedBlock, *notionapi.LinkPreviewBlock:
				found["embeds"] = true
			case *notionapi.EquationBlock:
				found["equations"] = true
			}
			walk(n.Children)
		}
	}
	walk(nodes)
	if len(found) == 0 {
		return ""
	}
	types := make([]string, 0, len(found))
	for t := range found {
		types = append(types, t)
	}
	sort.Strings(types)
	return strings.Join(types, ", ") + " will be simplified"
}
