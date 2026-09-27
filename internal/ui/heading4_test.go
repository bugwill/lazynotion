package ui

import (
	"strings"
	"testing"

	"github.com/jomei/notionapi"
	"github.com/justinm35/lazynotion/internal/notion"
)

func TestHeading4ViewerAndInlineEditKeepLevelAndNumber(t *testing.T) {
	h := &notion.Heading4Block{
		BasicBlock: notionapi.BasicBlock{ID: "h4", Type: "heading_4"},
		Heading4:   notionapi.Heading{RichText: notion.TextRichText("1. 肥胖症长期组合与市场分层")},
	}
	m := commitTestModel(t, h)
	if view := stripAnsi(m.viewer.View()); !strings.Contains(view, "1. 肥胖症长期组合与市场分层") || strings.Contains(view, "unsupported") {
		t.Fatalf("heading was not rendered: %q", view)
	}
	next, _ := m.startInlineEdit()
	m = next.(Model)
	if m.editArea.Value() != "#### 1. 肥胖症长期组合与市场分层" || !markeredKind("heading_4") {
		t.Fatalf("heading level lost in editor: %q", m.editArea.Value())
	}
	next, cmd := m.commitInlineEdit("#### 1. **新的标题**")
	m = next.(Model)
	if cmd == nil || m.blockCache[m.selected.ID][0].Block != h || h.Type != "heading_4" || len(h.Heading4.RichText) != 2 || !h.Heading4.RichText[1].Annotations.Bold {
		t.Fatal("edit recreated or downgraded the heading, or lost formatting")
	}
}
