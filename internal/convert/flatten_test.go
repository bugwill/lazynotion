package convert

import (
	"strings"
	"testing"

	"github.com/jomei/notionapi"

	"github.com/justinm35/lazynotion/internal/notion"
)

func TestFlatten(t *testing.T) {
	nodes := []notion.BlockNode{
		{Block: &notionapi.Heading2Block{Heading2: notionapi.Heading{RichText: rt("Section")}}},
		{
			Block: &notionapi.BulletedListItemBlock{BulletedListItem: notionapi.ListItem{RichText: rt("outer")}},
			Children: []notion.BlockNode{
				{Block: &notionapi.BulletedListItemBlock{BulletedListItem: notionapi.ListItem{RichText: rt("inner")}}},
			},
		},
		{Block: &notionapi.NumberedListItemBlock{NumberedListItem: notionapi.ListItem{RichText: rt("first")}}},
		{Block: &notionapi.NumberedListItemBlock{NumberedListItem: notionapi.ListItem{RichText: rt("second")}}},
		{
			Block: &notionapi.QuoteBlock{Quote: notionapi.Quote{RichText: rt("quoted")}},
			Children: []notion.BlockNode{
				{Block: &notionapi.ParagraphBlock{Paragraph: notionapi.Paragraph{RichText: rt("nested")}}},
			},
		},
		{
			Block: &notionapi.ColumnListBlock{},
			Children: []notion.BlockNode{
				{
					Block: &notionapi.ColumnBlock{},
					Children: []notion.BlockNode{
						{Block: &notionapi.ParagraphBlock{Paragraph: notionapi.Paragraph{RichText: rt("in column")}}},
					},
				},
			},
		},
	}

	units := Flatten(nodes)

	want := []struct {
		md    string
		depth int
	}{
		{"## Section", 0},
		{"- outer", 0},
		{"- inner", 1},
		{"1. first", 0},
		{"2. second", 0},
		{"> quoted\n> \n> nested", 0},
		{"in column", 0},
	}
	if len(units) != len(want) {
		t.Fatalf("got %d units, want %d: %+v", len(units), len(want), units)
	}
	for i, w := range want {
		if units[i].Markdown != w.md {
			t.Errorf("unit %d markdown = %q, want %q", i, units[i].Markdown, w.md)
		}
		if units[i].Depth != w.depth {
			t.Errorf("unit %d depth = %d, want %d", i, units[i].Depth, w.depth)
		}
	}
}

func TestUnitHelpers(t *testing.T) {
	todo := Unit{Node: notion.BlockNode{Block: &notionapi.ToDoBlock{ToDo: notionapi.ToDo{RichText: rt("task"), Checked: true}}}}
	checked, ok := todo.IsToDo()
	if !ok || !checked {
		t.Errorf("IsToDo = (%v, %v), want (true, true)", checked, ok)
	}
	text, ok := todo.PlainText()
	if !ok || text != "task" {
		t.Errorf("PlainText = (%q, %v), want (\"task\", true)", text, ok)
	}
	divider := Unit{Node: notion.BlockNode{Block: &notionapi.DividerBlock{}}}
	if _, ok := divider.PlainText(); ok {
		t.Error("divider should not be text-editable")
	}
}

func TestPageRef(t *testing.T) {
	child := Unit{Node: notion.BlockNode{Block: &notionapi.ChildPageBlock{
		BasicBlock: notionapi.BasicBlock{ID: "page-1", Type: "child_page"},
	}}}
	if id, _, ok := child.PageRef(); !ok || id != "page-1" {
		t.Errorf("child page ref = %q, %v", id, ok)
	}

	link := Unit{Node: notion.BlockNode{Block: &notionapi.LinkToPageBlock{
		LinkToPage: notionapi.LinkToPage{Type: "page_id", PageID: "page-2"},
	}}}
	if id, _, ok := link.PageRef(); !ok || id != "page-2" {
		t.Errorf("link_to_page ref = %q, %v", id, ok)
	}

	para := Unit{Node: notion.BlockNode{Block: &notionapi.ParagraphBlock{}}}
	if _, _, ok := para.PageRef(); ok {
		t.Error("paragraph should not be a page ref")
	}
}

func TestLinkToPageAndAudioSurviveRoundTrip(t *testing.T) {
	nodes := []notion.BlockNode{
		{Block: &notionapi.LinkToPageBlock{LinkToPage: notionapi.LinkToPage{Type: "page_id", PageID: "p"}}},
		{Block: &notionapi.AudioBlock{Audio: notionapi.Audio{External: &notionapi.FileObject{URL: "https://a.io/x.mp3"}}}},
	}
	md := ToMarkdown(nodes)
	if !strings.Contains(md, "linked page") || !strings.Contains(md, "🔊") {
		t.Fatalf("markdown missing new block types: %q", md)
	}
	parsed := ParseMarkdown(md)
	// the linked-page line is dropped (block is preserved server-side by
	// ReplacePageBlocks); audio becomes a bookmark keeping the URL
	if len(parsed) != 1 || parsed[0].GetType() != "bookmark" {
		t.Fatalf("unexpected parse result: %+v", parsed)
	}
}
