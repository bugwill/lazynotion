package ui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/jomei/notionapi"

	"github.com/justinm35/lazynotion/internal/convert"
	"github.com/justinm35/lazynotion/internal/notion"
)

func testUnits() []convert.Unit {
	rt := func(s string) []notionapi.RichText {
		return []notionapi.RichText{{PlainText: s}}
	}
	return convert.Flatten([]notion.BlockNode{
		{Block: &notionapi.Heading1Block{Heading1: notionapi.Heading{RichText: rt("Section")}}},
		{Block: &notionapi.ParagraphBlock{Paragraph: notionapi.Paragraph{RichText: rt("some text here")}}},
		{Block: &notionapi.ToDoBlock{ToDo: notionapi.ToDo{RichText: rt("task one")}}},
		{Block: &notionapi.ToDoBlock{ToDo: notionapi.ToDo{RichText: rt("task two"), Checked: true}}},
	})
}

func TestPageViewCursorMapping(t *testing.T) {
	var pv pageView
	pv.setUnits("My Page", testUnits(), 60, nil)

	if len(pv.rendered) != 4 {
		t.Fatalf("rendered %d units, want 4", len(pv.rendered))
	}

	content := pv.assemble(true, nil)
	lines := strings.Split(content, "\n")

	for i := range pv.units {
		start := pv.starts[i]
		if start >= len(lines) {
			t.Fatalf("unit %d start %d beyond content (%d lines)", i, start, len(lines))
		}
		if i > 0 && start <= pv.starts[i-1] {
			t.Errorf("starts not increasing: unit %d at %d, unit %d at %d", i-1, pv.starts[i-1], i, start)
		}
	}

	cursorLine := lines[pv.starts[0]]
	if !strings.Contains(cursorLine, "▌") {
		t.Errorf("cursor gutter missing on unit 0 line: %q", cursorLine)
	}

	pv.move(2)
	content = pv.assemble(true, nil)
	lines = strings.Split(content, "\n")
	if !strings.Contains(lines[pv.starts[2]], "▌") {
		t.Errorf("cursor gutter missing on unit 2 line: %q", lines[pv.starts[2]])
	}
	if strings.Contains(lines[pv.starts[0]], "▌") {
		t.Errorf("stale cursor gutter on unit 0 line: %q", lines[pv.starts[0]])
	}

	unfocused := pv.assemble(false, nil)
	if strings.Contains(unfocused, "▌") {
		t.Error("cursor gutter should not render when viewer is unfocused")
	}
}

func TestPageViewMoveClamps(t *testing.T) {
	var pv pageView
	pv.setUnits("p", testUnits(), 60, nil)
	pv.move(-10)
	if pv.cursor != 0 {
		t.Errorf("cursor = %d after moving up past start", pv.cursor)
	}
	pv.move(100)
	if pv.cursor != 3 {
		t.Errorf("cursor = %d after moving past end, want 3", pv.cursor)
	}

	var empty pageView
	empty.setUnits("p", nil, 60, nil)
	empty.move(1)
	if _, ok := empty.current(); ok {
		t.Error("current() should be false for empty page")
	}
}

func TestPageViewInlineEditLines(t *testing.T) {
	var pv pageView
	pv.setUnits("p", testUnits(), 60, nil)
	pv.move(1)

	edit := []string{"editing line one", "editing line two"}
	content := pv.assemble(true, edit)
	lines := strings.Split(content, "\n")

	got := lines[pv.starts[1] : pv.starts[1]+pv.heights[1]]
	if len(got) != 2 {
		t.Fatalf("edit unit height = %d, want 2", len(got))
	}
	for i, l := range got {
		if !strings.Contains(l, edit[i]) {
			t.Errorf("edit line %d = %q, want to contain %q", i, l, edit[i])
		}
	}
	if pv.starts[2] <= pv.starts[1]+1 {
		t.Error("following unit should shift down for taller edit area")
	}
}

func TestParagraphNewlinesRenderAsLines(t *testing.T) {
	units := convert.Flatten([]notion.BlockNode{
		{Block: &notionapi.ParagraphBlock{Paragraph: notionapi.Paragraph{
			RichText: []notionapi.RichText{{PlainText: "line one\nline two"}},
		}}},
	})
	var pv pageView
	pv.setUnits("p", units, 60, nil)

	if len(pv.rendered[0]) < 2 {
		t.Fatalf("multi-line paragraph rendered as %d line(s): %q", len(pv.rendered[0]), pv.rendered[0])
	}
	joined := stripAnsi(strings.Join(pv.rendered[0], "|"))
	if !strings.Contains(joined, "line one") || !strings.Contains(joined, "line two") {
		t.Errorf("rendered lines missing content: %q", joined)
	}
	for _, l := range pv.rendered[0] {
		plain := stripAnsi(l)
		if strings.Contains(plain, "line one") && strings.Contains(plain, "line two") {
			t.Errorf("newline collapsed onto one line: %q", plain)
		}
	}
}

func TestNotionStrongCJKWithAdjacentPunctuationAndRuns(t *testing.T) {
	strong := &notionapi.Annotations{Bold: true}
	nodes := []notion.BlockNode{{Block: &notionapi.ParagraphBlock{Paragraph: notionapi.Paragraph{RichText: []notionapi.RichText{
		{PlainText: "捕捉历史性机会：", Annotations: strong},
		{PlainText: "过去其实有很多大机会，"},
		{PlainText: "反思", Annotations: strong},
		{PlainText: "与", Annotations: strong},
		{PlainText: "验证。"},
		{PlainText: "重点：\n第二行", Annotations: strong},
	}}}}}
	markdown := convert.ToMarkdown(nodes)
	if !strings.Contains(markdown, "**捕捉历史性机会：**过去") || !strings.Contains(markdown, "**反思与**验证。") || !strings.Contains(markdown, "**重点：**  \n**第二行**") {
		t.Fatalf("bold Markdown lost a CJK span, adjacent run, or hard break: %q", markdown)
	}
	var pv pageView
	pv.setUnits("p", convert.Flatten(nodes), 80, nil)
	rendered := strings.Join(pv.rendered[0], "\n")
	plain := stripAnsi(rendered)
	if strings.Contains(plain, "**") || strings.Contains(plain, "\u2060") {
		t.Fatalf("Markdown delimiters or rendering hints leaked into output: %q", plain)
	}
	if !strings.Contains(rendered, ";1m捕捉历史性机会：") || !strings.Contains(rendered, ";1m反思与") || !strings.Contains(rendered, ";1m第二行") {
		t.Fatalf("CJK strong spans did not render bold: %q", rendered)
	}
	if !strings.Contains(plain, "捕捉历史性机会：过去") || !strings.Contains(plain, "反思与验证。") {
		t.Fatalf("rendering changed CJK punctuation or plain suffix: %q", plain)
	}
}

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripAnsi(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}

func notionPageForTest(id string) notion.Page {
	return notion.Page{ID: id, Title: "t"}
}
