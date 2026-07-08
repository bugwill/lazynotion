package convert

import (
	"strings"
	"testing"

	"github.com/jomei/notionapi"

	"github.com/justinm35/lazynotion/internal/notion"
)

const sampleMarkdown = `# Title

Some **bold** and *italic* text with a [link](https://example.com).

- outer
    - inner
- [x] done task
- [ ] open task

1. first
2. second

> a quote
>
> with a nested line

` + "```go\nfmt.Println(1)\n```" + `

---

| a | b |
| --- | --- |
| 1 | 2 |

[🖼 diagram](https://img.example/x.png)
`

func TestParseMarkdown(t *testing.T) {
	blocks := ParseMarkdown(sampleMarkdown)

	wantTypes := []string{
		"heading_1", "paragraph", "bulleted_list_item", "to_do", "to_do",
		"numbered_list_item", "numbered_list_item", "quote", "code",
		"divider", "table", "image",
	}
	if len(blocks) != len(wantTypes) {
		var got []string
		for _, b := range blocks {
			got = append(got, string(b.GetType()))
		}
		t.Fatalf("got %d blocks %v, want %d %v", len(blocks), got, len(wantTypes), wantTypes)
	}
	for i, want := range wantTypes {
		if string(blocks[i].GetType()) != want {
			t.Errorf("block %d type = %s, want %s", i, blocks[i].GetType(), want)
		}
	}

	outer := blocks[2].(*notionapi.BulletedListItemBlock)
	if len(outer.BulletedListItem.Children) != 1 {
		t.Fatalf("outer bullet children = %d, want 1", len(outer.BulletedListItem.Children))
	}
	if outer.BulletedListItem.Children[0].GetType() != "bulleted_list_item" {
		t.Errorf("nested child type = %s", outer.BulletedListItem.Children[0].GetType())
	}

	done := blocks[3].(*notionapi.ToDoBlock)
	if !done.ToDo.Checked {
		t.Error("first to-do should be checked")
	}
	open := blocks[4].(*notionapi.ToDoBlock)
	if open.ToDo.Checked {
		t.Error("second to-do should be unchecked")
	}

	quote := blocks[7].(*notionapi.QuoteBlock)
	if plain(quote.Quote.RichText) != "a quote" {
		t.Errorf("quote text = %q", plain(quote.Quote.RichText))
	}
	if len(quote.Quote.Children) != 1 {
		t.Errorf("quote children = %d, want 1", len(quote.Quote.Children))
	}

	code := blocks[8].(*notionapi.CodeBlock)
	if code.Code.Language != "go" || plain(code.Code.RichText) != "fmt.Println(1)" {
		t.Errorf("code = %q lang %q", plain(code.Code.RichText), code.Code.Language)
	}

	table := blocks[10].(*notionapi.TableBlock)
	if table.Table.TableWidth != 2 || len(table.Table.Children) != 2 {
		t.Errorf("table width %d rows %d, want 2x2", table.Table.TableWidth, len(table.Table.Children))
	}

	img := blocks[11].(*notionapi.ImageBlock)
	if img.Image.External == nil || img.Image.External.URL != "https://img.example/x.png" {
		t.Errorf("image = %+v", img.Image)
	}
}

func TestParseInlineAnnotations(t *testing.T) {
	runs := parseInline("plain **bold** `code` [link](https://x.io) ~~gone~~")

	var bold, code, gone *notionapi.RichText
	var link *notionapi.RichText
	for i := range runs {
		switch runs[i].PlainText {
		case "bold":
			bold = &runs[i]
		case "code":
			code = &runs[i]
		case "link":
			link = &runs[i]
		case "gone":
			gone = &runs[i]
		}
	}
	if bold == nil || bold.Annotations == nil || !bold.Annotations.Bold {
		t.Error("bold run missing or not bold")
	}
	if code == nil || code.Annotations == nil || !code.Annotations.Code {
		t.Error("code run missing or not code")
	}
	if gone == nil || gone.Annotations == nil || !gone.Annotations.Strikethrough {
		t.Error("strikethrough run missing")
	}
	if link == nil || link.Href != "https://x.io" || link.Text.Link == nil {
		t.Error("link run missing href")
	}
}

func TestParseInlineUnmatchedMarkersStayLiteral(t *testing.T) {
	runs := parseInline("2 * 3 = 6 and a ** dangler")
	if len(runs) != 1 || runs[0].PlainText != "2 * 3 = 6 and a ** dangler" {
		t.Errorf("got %+v", runs)
	}
}

func TestRoundTrip(t *testing.T) {
	blocks := ParseMarkdown(sampleMarkdown)
	nodes := nodesFromBlocks(blocks)
	md := ToMarkdown(nodes)
	again := ParseMarkdown(md)

	if len(again) != len(blocks) {
		t.Fatalf("round trip changed block count: %d -> %d", len(blocks), len(again))
	}
	for i := range blocks {
		if blocks[i].GetType() != again[i].GetType() {
			t.Errorf("block %d type changed: %s -> %s", i, blocks[i].GetType(), again[i].GetType())
		}
	}
}

func TestMultilineBlockRoundTrip(t *testing.T) {
	nodes := []notion.BlockNode{{Block: &notionapi.ParagraphBlock{
		Paragraph: notionapi.Paragraph{RichText: rt("line one\nline two")},
	}}}
	md := ToMarkdown(nodes)
	if !strings.Contains(md, "line one  \nline two") {
		t.Errorf("newline should become a hard break, got %q", md)
	}

	parsed := ParseMarkdown(md)
	if len(parsed) != 1 {
		t.Fatalf("hard-broken paragraph should stay one block, got %d", len(parsed))
	}
	para := parsed[0].(*notionapi.ParagraphBlock)
	if got := plain(para.Paragraph.RichText); got != "line one\nline two" {
		t.Errorf("round trip text = %q, want %q", got, "line one\nline two")
	}
}
