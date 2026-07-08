package convert

import (
	"strings"
	"testing"

	"github.com/jomei/notionapi"

	"github.com/justinm35/lazynotion/internal/notion"
)

func TestEditableMarkdownIncludesMarkers(t *testing.T) {
	cases := []struct {
		name  string
		block notionapi.Block
		want  string
	}{
		{"todo unchecked", &notionapi.ToDoBlock{ToDo: notionapi.ToDo{RichText: rt("task")}}, "- [ ] task"},
		{"todo checked", &notionapi.ToDoBlock{ToDo: notionapi.ToDo{RichText: rt("task"), Checked: true}}, "- [x] task"},
		{"bullet", &notionapi.BulletedListItemBlock{BulletedListItem: notionapi.ListItem{RichText: rt("item")}}, "- item"},
		{"numbered", &notionapi.NumberedListItemBlock{NumberedListItem: notionapi.ListItem{RichText: rt("item")}}, "1. item"},
		{"quote", &notionapi.QuoteBlock{Quote: notionapi.Quote{RichText: rt("wise words")}}, "> wise words"},
		{"quote multiline", &notionapi.QuoteBlock{Quote: notionapi.Quote{RichText: rt("one\ntwo")}}, "> one\n> two"},
		{"heading", &notionapi.Heading2Block{Heading2: notionapi.Heading{RichText: rt("Section")}}, "## Section"},
		{"paragraph", &notionapi.ParagraphBlock{Paragraph: notionapi.Paragraph{RichText: rt("plain")}}, "plain"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			u := Unit{Node: notion.BlockNode{Block: c.block}}
			got, ok := u.EditableMarkdown()
			if !ok || got != c.want {
				t.Errorf("seed = %q (%v), want %q", got, ok, c.want)
			}
		})
	}
}

func TestParseEditPatch(t *testing.T) {
	p := ParseEditPatch("- [x] done deal")
	if p.Kind != "to_do" || !p.Checked || plain(p.RichText) != "done deal" {
		t.Errorf("todo patch = %+v", p)
	}

	p = ParseEditPatch("## New title")
	if p.Kind != "heading_2" || plain(p.RichText) != "New title" {
		t.Errorf("heading patch = %+v", p)
	}

	p = ParseEditPatch("> line one\n> line two")
	if p.Kind != "quote" || plain(p.RichText) != "line one\nline two" {
		t.Errorf("quote patch = %+v", p)
	}

	// continuation lines stay in the same block, not a second one
	p = ParseEditPatch("- [ ] task\nwith detail")
	if p.Kind != "to_do" || plain(p.RichText) != "task\nwith detail" {
		t.Errorf("multiline todo patch = %+v", p)
	}

	p = ParseEditPatch("no marker here")
	if p.Kind != "paragraph" || plain(p.RichText) != "no marker here" {
		t.Errorf("paragraph patch = %+v", p)
	}
}

func TestEditSeedPatchRoundTrip(t *testing.T) {
	u := Unit{Node: notion.BlockNode{Block: &notionapi.ToDoBlock{
		ToDo: notionapi.ToDo{RichText: rt("buy milk"), Checked: true},
	}}}
	seed, _ := u.EditableMarkdown()
	p := ParseEditPatch(seed)
	if p.Kind != "to_do" || !p.Checked || plain(p.RichText) != "buy milk" {
		t.Errorf("round trip = %+v from seed %q", p, seed)
	}
}

func TestLongTextSplitsIntoRuns(t *testing.T) {
	long := strings.Repeat("é", 5000) // multibyte: split must be rune-safe
	runs := ParseInline(long)
	if len(runs) != 3 {
		t.Fatalf("got %d runs, want 3 (2000+2000+1000)", len(runs))
	}
	var rebuilt strings.Builder
	for i, r := range runs {
		n := len([]rune(r.PlainText))
		if n > 2000 {
			t.Errorf("run %d has %d runes, exceeds Notion's 2000 limit", i, n)
		}
		if r.Text == nil || r.Text.Content != r.PlainText {
			t.Errorf("run %d Text.Content out of sync", i)
		}
		rebuilt.WriteString(r.PlainText)
	}
	if rebuilt.String() != long {
		t.Error("split runs do not reassemble to the original text")
	}
}

func TestLongBoldTextKeepsAnnotationsAcrossRuns(t *testing.T) {
	long := "**" + strings.Repeat("x", 4500) + "**"
	runs := ParseInline(long)
	if len(runs) != 3 {
		t.Fatalf("got %d runs, want 3", len(runs))
	}
	for i, r := range runs {
		if r.Annotations == nil || !r.Annotations.Bold {
			t.Errorf("run %d lost its bold annotation", i)
		}
	}
}
