package convert

import (
	"strings"
	"testing"

	"github.com/jomei/notionapi"

	"github.com/justinm35/lazynotion/internal/notion"
)

func rt(text string) []notionapi.RichText {
	return []notionapi.RichText{{PlainText: text}}
}

func rtAnnotated(text string, a notionapi.Annotations) []notionapi.RichText {
	return []notionapi.RichText{{PlainText: text, Annotations: &a}}
}

func TestToMarkdown(t *testing.T) {
	emoji := notionapi.Emoji("⚠️")
	nodes := []notion.BlockNode{
		{Block: &notionapi.Heading1Block{Heading1: notionapi.Heading{RichText: rt("Title")}}},
		{Block: &notionapi.ParagraphBlock{Paragraph: notionapi.Paragraph{
			RichText: []notionapi.RichText{
				{PlainText: "bold ", Annotations: &notionapi.Annotations{Bold: true}},
				{PlainText: "link", Href: "https://example.com"},
			},
		}}},
		{
			Block: &notionapi.BulletedListItemBlock{BulletedListItem: notionapi.ListItem{RichText: rt("outer")}},
			Children: []notion.BlockNode{
				{Block: &notionapi.BulletedListItemBlock{BulletedListItem: notionapi.ListItem{RichText: rt("inner")}}},
			},
		},
		{Block: &notionapi.ToDoBlock{ToDo: notionapi.ToDo{RichText: rt("task"), Checked: true}}},
		{
			Block: &notionapi.QuoteBlock{Quote: notionapi.Quote{RichText: rt("quoted")}},
			Children: []notion.BlockNode{
				{Block: &notionapi.ParagraphBlock{Paragraph: notionapi.Paragraph{RichText: rt("nested quote")}}},
			},
		},
		{Block: &notionapi.CalloutBlock{Callout: notionapi.Callout{RichText: rt("careful"), Icon: &notionapi.Icon{Emoji: &emoji}}}},
		{Block: &notionapi.CodeBlock{Code: notionapi.Code{RichText: rt("fmt.Println(1)"), Language: "go"}}},
		{
			Block: &notionapi.TableBlock{Table: notionapi.Table{HasColumnHeader: true}},
			Children: []notion.BlockNode{
				{Block: &notionapi.TableRowBlock{TableRow: notionapi.TableRow{Cells: [][]notionapi.RichText{rt("a"), rt("b")}}}},
				{Block: &notionapi.TableRowBlock{TableRow: notionapi.TableRow{Cells: [][]notionapi.RichText{rt("1"), rt("2")}}}},
			},
		},
		{Block: &notionapi.ImageBlock{Image: notionapi.Image{
			External: &notionapi.FileObject{URL: "https://img.example/x.png"},
			Caption:  rt("diagram"),
		}}},
		{Block: &notionapi.UnsupportedBlock{BasicBlock: notionapi.BasicBlock{Type: "audio"}}},
	}

	md := ToMarkdown(nodes)

	for _, want := range []string{
		"# Title",
		"**bold** [link](https://example.com)",
		"- outer",
		"    - inner",
		"- [x] task",
		"> quoted",
		"> nested quote",
		"> ⚠️ careful",
		"```go\nfmt.Println(1)\n```",
		"| a | b |",
		"| --- | --- |",
		"| 1 | 2 |",
		"[🖼 diagram](https://img.example/x.png)",
		"*[unsupported block: audio]*",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q\n---\n%s", want, md)
		}
	}
}

func TestAnnotateKeepsSurroundingSpaces(t *testing.T) {
	got := annotate("word ", &notionapi.Annotations{Bold: true})
	if got != "**word** " {
		t.Errorf("got %q, want %q", got, "**word** ")
	}
}
