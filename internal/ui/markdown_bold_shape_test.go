package ui

import (
	"strconv"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/jomei/notionapi"

	"github.com/justinm35/lazynotion/internal/convert"
	"github.com/justinm35/lazynotion/internal/notion"
)

func TestCJKBoldMarkdownRendersExactStrongSpan(t *testing.T) {
	bold := &notionapi.Annotations{Bold: true}
	cases := []struct {
		name string
		runs []notionapi.RichText
		want string
	}{
		{
			name: "fullwidth punctuation before plain continuation",
			runs: []notionapi.RichText{
				{PlainText: "中文：", Annotations: bold},
				{PlainText: "后文"},
			},
			want: "**中文：**后文",
		},
		{
			name: "adjacent API fragments with the same style",
			runs: []notionapi.RichText{
				{PlainText: "中", Annotations: bold},
				{PlainText: "文：", Annotations: &notionapi.Annotations{Bold: true}},
				{PlainText: "后文"},
			},
			want: "**中文：**后文",
		},
		{
			name: "multiline strong text",
			runs: []notionapi.RichText{
				{PlainText: "第一行\n第二行", Annotations: bold},
				{PlainText: " 后文"},
			},
			want: "**第一行**  \n**第二行** 后文",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			block := &notionapi.ParagraphBlock{Paragraph: notionapi.Paragraph{RichText: tc.runs}}
			units := convert.Flatten([]notion.BlockNode{{Block: block}})
			if len(units) != 1 {
				t.Fatalf("got %d units, want one", len(units))
			}
			if units[0].Markdown != tc.want {
				t.Fatalf("Markdown = %q, want %q", units[0].Markdown, tc.want)
			}

			var pv pageView
			pv.setUnits("", units, 50, nil)
			rendered := strings.Join(pv.rendered[0], "\n")
			plain, boldByRune := ansiAttributeShape(t, rendered, 1, 22)
			plain, boldByRune = trimANSIAttributeRowPadding(plain, boldByRune)
			wantPlain := plainRichText(tc.runs)
			if plain != wantPlain {
				t.Fatalf("rendered plain text = %q, want original text %q (rendered %q)", plain, wantPlain, rendered)
			}

			wantBold := expectedInlineBold(tc.runs)
			if len(boldByRune) != len(wantBold) {
				t.Fatalf("rendered %d runes, want %d", len(boldByRune), len(wantBold))
			}
			for i, want := range wantBold {
				if boldByRune[i] != want {
					got := []rune(plain)[i]
					t.Errorf("character %q at text rune %d has bold=%v, want %v; rendered %q", got, i, boldByRune[i], want, rendered)
				}
			}
			if strings.Contains(plain, "**") {
				t.Errorf("Markdown markers leaked into rendered text: %q", plain)
			}
		})
	}
}

func TestFragmentRendererPreservesInlineMarkdownShapes(t *testing.T) {
	tests := []struct {
		name        string
		markdown    string
		want        string
		boldStart   int
		boldEnd     int
		italicStart int
		italicEnd   int
	}{
		{name: "link", markdown: "[linked text](https://example.test/path)", want: "linked text https://example.test/path\n", boldStart: 0, boldEnd: 11},
		{name: "inline code", markdown: "`code()`", want: " code()\n"},
		{name: "escaped stars", markdown: `\*literal\*`, want: "*literal*\n"},
		{name: "nested strong and emphasis", markdown: "**bold _italic_** plain", want: "bold italic plain\n", boldStart: 0, boldEnd: 11, italicStart: 5, italicEnd: 11},
	}

	renderer := newFragmentRenderer(80)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rendered, err := renderer.Render(tc.markdown)
			if err != nil {
				t.Fatal(err)
			}
			visible, bold := ansiAttributeShape(t, rendered, 1, 22)
			visible, bold = trimANSIAttributeRowPadding(visible, bold)
			visibleForItalic, italic := ansiAttributeShape(t, rendered, 3, 23)
			visibleForItalic, italic = trimANSIAttributeRowPadding(visibleForItalic, italic)
			if visible != tc.want || visibleForItalic != tc.want {
				t.Fatalf("rendered visible text = %q / %q, want %q", visible, visibleForItalic, tc.want)
			}
			wantBold, wantItalic := make([]bool, len([]rune(tc.want))), make([]bool, len([]rune(tc.want)))
			for i := tc.boldStart; i < tc.boldEnd; i++ {
				wantBold[i] = true
			}
			for i := tc.italicStart; i < tc.italicEnd; i++ {
				wantItalic[i] = true
			}
			for i := range wantBold {
				if bold[i] != wantBold[i] {
					t.Errorf("rune %q at %d has bold=%v, want %v", []rune(tc.want)[i], i, bold[i], wantBold[i])
				}
				if italic[i] != wantItalic[i] {
					t.Errorf("rune %q at %d has italic=%v, want %v", []rune(tc.want)[i], i, italic[i], wantItalic[i])
				}
			}
			if strings.Contains(visible, "**") || strings.Contains(visible, "<") {
				t.Errorf("Markdown or HTML source leaked into the visible text: %q", visible)
			}
		})
	}
}

func trimANSIAttributeRowPadding(plain string, attributes []bool) (string, []bool) {
	runes := []rune(plain)
	var visible strings.Builder
	trimmed := make([]bool, 0, len(attributes))
	for start := 0; start < len(runes); {
		end := start
		for end < len(runes) && runes[end] != '\n' {
			end++
		}
		trimEnd := end
		for trimEnd > start && runes[trimEnd-1] == ' ' {
			trimEnd--
		}
		visible.WriteString(string(runes[start:trimEnd]))
		trimmed = append(trimmed, attributes[start:trimEnd]...)
		if end < len(runes) {
			visible.WriteRune('\n')
			trimmed = append(trimmed, attributes[end])
			end++
		}
		start = end
	}
	return visible.String(), trimmed
}

func plainRichText(runs []notionapi.RichText) string {
	var b strings.Builder
	for _, run := range runs {
		b.WriteString(run.PlainText)
	}
	return b.String()
}

func expectedInlineBold(runs []notionapi.RichText) []bool {
	var mask []bool
	for _, run := range runs {
		bold := run.Annotations != nil && run.Annotations.Bold
		for _, r := range run.PlainText {
			mask = append(mask, bold && !unicode.IsSpace(r))
		}
	}
	return mask
}

// ansiAttributeShape reads one SGR on/off attribute and preserves visible
// runes. It checks terminal output rather than source markup, so a different
// styled span cannot mask a bad one.
func ansiAttributeShape(t *testing.T, rendered string, enabledCode, disabledCode int) (string, []bool) {
	t.Helper()
	var plain strings.Builder
	var boldByRune []bool
	bold := false
	for i := 0; i < len(rendered); {
		if rendered[i] != '\x1b' || i+1 >= len(rendered) || rendered[i+1] != '[' {
			r, size := utf8.DecodeRuneInString(rendered[i:])
			if r == utf8.RuneError && size == 1 {
				t.Fatalf("invalid UTF-8 in rendered output near %q", rendered[i:])
			}
			plain.WriteRune(r)
			boldByRune = append(boldByRune, bold)
			i += size
			continue
		}
		end := i + 2
		for end < len(rendered) && (rendered[end] < 0x40 || rendered[end] > 0x7e) {
			end++
		}
		if end >= len(rendered) {
			t.Fatalf("unterminated ANSI CSI in rendered output %q", rendered)
		}
		if rendered[end] == 'm' {
			params := strings.TrimPrefix(rendered[i+2:end], "?")
			if params == "" {
				bold = false
			} else {
				for _, raw := range strings.Split(params, ";") {
					code, err := strconv.Atoi(raw)
					if err != nil {
						continue
					}
					switch code {
					case 0, disabledCode:
						bold = false
					case enabledCode:
						bold = true
					}
				}
			}
		}
		i = end + 1
	}
	return plain.String(), boldByRune
}
