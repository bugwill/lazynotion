package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/jomei/notionapi"

	"github.com/justinm35/lazynotion/internal/convert"
	"github.com/justinm35/lazynotion/internal/notion"
)

func TestNoRenderedLineExceedsPane(t *testing.T) {
	long := strings.Repeat("word ", 20) + "end"
	url := "see https://example.com/a-very-long-unbreakable-path-segment-that-goes-on-forever-and-ever here"
	nodes := []notion.BlockNode{
		{Block: &notionapi.ParagraphBlock{Paragraph: notionapi.Paragraph{RichText: []notionapi.RichText{{PlainText: long}}}}},
		{Block: &notionapi.BulletedListItemBlock{BulletedListItem: notionapi.ListItem{RichText: []notionapi.RichText{{PlainText: long}}}}},
		{Block: &notionapi.QuoteBlock{Quote: notionapi.Quote{RichText: []notionapi.RichText{{PlainText: long}}}}},
		{Block: &notionapi.ToDoBlock{ToDo: notionapi.ToDo{RichText: []notionapi.RichText{{PlainText: long}}}}},
		{Block: &notionapi.ParagraphBlock{Paragraph: notionapi.Paragraph{RichText: []notionapi.RichText{{PlainText: url}}}}},
	}
	// the actual reported bug: a multi-line list item whose continuation
	// line reaches full width was rendered 2 columns too wide
	nodes = append(nodes, notion.BlockNode{Block: &notionapi.ToDoBlock{ToDo: notionapi.ToDo{
		RichText: []notionapi.RichText{{PlainText: "task\n" + strings.Repeat("continuation text that fills the whole line width ", 3)}},
	}}})
	// the two things glamour refuses to wrap: long code lines, wide tables
	nodes = append(nodes,
		notion.BlockNode{Block: &notionapi.CodeBlock{Code: notionapi.Code{
			Language: "go",
			RichText: []notionapi.RichText{{PlainText: "func veryLongFunctionName(withArguments string, andMore int) (string, error) { return withArguments, nil }"}},
		}}},
		notion.BlockNode{
			Block: &notionapi.TableBlock{Table: notionapi.Table{TableWidth: 4, HasColumnHeader: true}},
			Children: []notion.BlockNode{
				{Block: &notionapi.TableRowBlock{TableRow: notionapi.TableRow{Cells: [][]notionapi.RichText{
					{{PlainText: "a long header column"}}, {{PlainText: "another one here"}},
					{{PlainText: "third column text"}}, {{PlainText: "fourth column text"}},
				}}}},
			},
		},
	)
	var pv pageView
	pv.setUnits("", convert.Flatten(nodes), 40, nil)
	for i, lines := range pv.rendered {
		joined := ""
		for _, l := range lines {
			if w := lipgloss.Width(l); w > 38 {
				t.Errorf("unit %d line exceeds pane (%d > 38): %q", i, w, stripAnsi(l))
			}
			joined += stripAnsi(l)
		}
		_ = joined
	}
	// content must be wrapped, not lost: the code text survives in full
	var codeText string
	for _, l := range pv.rendered[6] {
		codeText += stripAnsi(l)
	}
	codeText = strings.Join(strings.Fields(codeText), " ")
	if !strings.Contains(codeText, "veryLongFunctionName") || !strings.Contains(codeText, "return withArguments, nil") {
		t.Errorf("code content lost in wrapping: %q", codeText)
	}
}
