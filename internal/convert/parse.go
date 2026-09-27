package convert

import (
	"regexp"
	"strings"

	"github.com/jomei/notionapi"
	"github.com/justinm35/lazynotion/internal/notion"
)

// ParseMarkdown converts edited markdown back into Notion blocks. It covers
// the same subset ToMarkdown emits; anything else becomes a paragraph.
func ParseMarkdown(md string) []notionapi.Block {
	p := &parser{lines: strings.Split(md, "\n")}
	return p.parse()
}

type parser struct {
	lines []string
	pos   int
}

type nestFrame struct {
	indent int
	attach func(notionapi.Block)
}

var (
	numberedRe   = regexp.MustCompile(`^(\d+)[.)] (.*)$`)
	mediaLinkRe  = regexp.MustCompile(`^\[(🖼|🔖|🔗|🎬|📎|🔊)\s*(.*?)\]\((\S+)\)$`)
	childPageRe  = regexp.MustCompile(`^(📄|🗃) \*\*.*\*\*$|^📄 \*linked page\*$`)
	tableSepRe   = regexp.MustCompile(`^\|?[\s|:-]+\|?$`)
	fenceOpenRe  = regexp.MustCompile("^```(.*)$")
	headingRe    = regexp.MustCompile(`^(#{1,6}) (.*)$`)
	todoRe       = regexp.MustCompile(`^[-*] \[([ xX])\] (.*)$`)
	bulletRe     = regexp.MustCompile(`^[-*] (.*)$`)
	toggleTextRe = regexp.MustCompile(`^\*\*▸ (.*)\*\*$`)
)

func basic(t notionapi.BlockType) notionapi.BasicBlock {
	return notionapi.BasicBlock{Object: "block", Type: t}
}

func (p *parser) parse() []notionapi.Block {
	var top []notionapi.Block
	var stack []nestFrame

	appendBlock := func(indent int, b notionapi.Block, attach func(notionapi.Block)) {
		for len(stack) > 0 && indent <= stack[len(stack)-1].indent {
			stack = stack[:len(stack)-1]
		}
		if len(stack) > 0 {
			stack[len(stack)-1].attach(b)
		} else {
			top = append(top, b)
		}
		if attach != nil {
			stack = append(stack, nestFrame{indent: indent, attach: attach})
		}
	}

	for p.pos < len(p.lines) {
		raw := p.lines[p.pos]
		line := strings.TrimLeft(raw, " \t")
		if strings.TrimSpace(line) == "" {
			p.pos++
			continue
		}
		indent := indentWidth(raw)

		switch {
		case fenceOpenRe.MatchString(line):
			lang := strings.TrimSpace(strings.TrimPrefix(line, "```"))
			p.pos++
			var code []string
			for p.pos < len(p.lines) && !strings.HasPrefix(strings.TrimLeft(p.lines[p.pos], " \t"), "```") {
				code = append(code, p.lines[p.pos])
				p.pos++
			}
			p.pos++
			appendBlock(indent, &notionapi.CodeBlock{
				BasicBlock: basic("code"),
				Code: notionapi.Code{
					RichText: textRichText(strings.Join(dedent(code, indent), "\n")),
					Language: notionLanguage(lang),
				},
			}, nil)

		case line == "---" || line == "***":
			p.pos++
			appendBlock(indent, &notionapi.DividerBlock{BasicBlock: basic("divider")}, nil)

		case headingRe.MatchString(line):
			m := headingRe.FindStringSubmatch(line)
			p.pos++
			appendBlock(indent, headingBlock(len(m[1]), parseInline(m[2])), nil)

		case todoRe.MatchString(line):
			m := todoRe.FindStringSubmatch(line)
			p.pos++
			block := &notionapi.ToDoBlock{
				BasicBlock: basic("to_do"),
				ToDo:       notionapi.ToDo{RichText: parseInline(m[2]), Checked: m[1] != " "},
			}
			appendBlock(indent, block, func(c notionapi.Block) { block.ToDo.Children = append(block.ToDo.Children, c) })

		case bulletRe.MatchString(line):
			m := bulletRe.FindStringSubmatch(line)
			p.pos++
			block := &notionapi.BulletedListItemBlock{
				BasicBlock:       basic("bulleted_list_item"),
				BulletedListItem: notionapi.ListItem{RichText: parseInline(m[1])},
			}
			appendBlock(indent, block, func(c notionapi.Block) {
				block.BulletedListItem.Children = append(block.BulletedListItem.Children, c)
			})

		case numberedRe.MatchString(line):
			m := numberedRe.FindStringSubmatch(line)
			p.pos++
			block := &notionapi.NumberedListItemBlock{
				BasicBlock:       basic("numbered_list_item"),
				NumberedListItem: notionapi.ListItem{RichText: parseInline(m[2])},
			}
			appendBlock(indent, block, func(c notionapi.Block) {
				block.NumberedListItem.Children = append(block.NumberedListItem.Children, c)
			})

		case strings.HasPrefix(line, ">"):
			appendBlock(indent, p.parseQuote(indent), nil)

		case strings.HasPrefix(line, "|"):
			if t := p.parseTable(indent); t != nil {
				appendBlock(indent, t, nil)
			}

		case toggleTextRe.MatchString(line):
			m := toggleTextRe.FindStringSubmatch(line)
			p.pos++
			block := &notionapi.ToggleBlock{
				BasicBlock: basic("toggle"),
				Toggle:     notionapi.Toggle{RichText: parseInline(m[1])},
			}
			appendBlock(indent, block, func(c notionapi.Block) { block.Toggle.Children = append(block.Toggle.Children, c) })

		case childPageRe.MatchString(line):
			// child pages/databases cannot be recreated from markdown; the
			// original blocks are preserved by ReplacePageBlocks instead.
			p.pos++

		case mediaLinkRe.MatchString(line):
			m := mediaLinkRe.FindStringSubmatch(line)
			p.pos++
			appendBlock(indent, mediaBlock(m[1], m[2], m[3]), nil)

		default:
			appendBlock(indent, p.parseParagraph(), nil)
		}
	}
	return top
}

func (p *parser) parseParagraph() notionapi.Block {
	var parts []string
	for p.pos < len(p.lines) {
		line := strings.TrimLeft(p.lines[p.pos], " \t")
		if strings.TrimSpace(line) == "" || isMarkerLine(line) {
			break
		}
		// strip hard-break markers so they round-trip back to plain newlines
		parts = append(parts, strings.TrimRight(line, " "))
		p.pos++
	}
	return &notionapi.ParagraphBlock{
		BasicBlock: basic("paragraph"),
		Paragraph:  notionapi.Paragraph{RichText: parseInline(strings.Join(parts, "\n"))},
	}
}

func (p *parser) parseQuote(indent int) notionapi.Block {
	var content []string
	for p.pos < len(p.lines) {
		line := strings.TrimLeft(p.lines[p.pos], " \t")
		if !strings.HasPrefix(line, ">") {
			break
		}
		content = append(content, strings.TrimPrefix(strings.TrimPrefix(line, ">"), " "))
		p.pos++
	}
	var filled []string
	for _, l := range content {
		if strings.TrimSpace(l) != "" {
			filled = append(filled, l)
		}
	}
	if len(filled) == 0 {
		filled = []string{""}
	}
	quote := &notionapi.QuoteBlock{
		BasicBlock: basic("quote"),
		Quote:      notionapi.Quote{RichText: parseInline(filled[0])},
	}
	for _, l := range filled[1:] {
		quote.Quote.Children = append(quote.Quote.Children, &notionapi.ParagraphBlock{
			BasicBlock: basic("paragraph"),
			Paragraph:  notionapi.Paragraph{RichText: parseInline(l)},
		})
	}
	return quote
}

func (p *parser) parseTable(indent int) notionapi.Block {
	var rows [][]string
	for p.pos < len(p.lines) {
		line := strings.TrimLeft(p.lines[p.pos], " \t")
		if !strings.HasPrefix(line, "|") {
			break
		}
		p.pos++
		if tableSepRe.MatchString(line) {
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		for i := range cells {
			cells[i] = strings.ReplaceAll(strings.TrimSpace(cells[i]), "\\|", "|")
		}
		rows = append(rows, cells)
	}
	if len(rows) == 0 {
		return nil
	}
	width := 0
	for _, r := range rows {
		width = max(width, len(r))
	}
	table := &notionapi.TableBlock{
		BasicBlock: basic("table"),
		Table:      notionapi.Table{TableWidth: width, HasColumnHeader: true},
	}
	for _, r := range rows {
		cells := make([][]notionapi.RichText, width)
		for i := range cells {
			if i < len(r) {
				cells[i] = parseInline(r[i])
			} else {
				cells[i] = []notionapi.RichText{}
			}
		}
		table.Table.Children = append(table.Table.Children, &notionapi.TableRowBlock{
			BasicBlock: basic("table_row"),
			TableRow:   notionapi.TableRow{Cells: cells},
		})
	}
	return table
}

func isMarkerLine(line string) bool {
	return headingRe.MatchString(line) ||
		todoRe.MatchString(line) ||
		bulletRe.MatchString(line) ||
		numberedRe.MatchString(line) ||
		strings.HasPrefix(line, ">") ||
		strings.HasPrefix(line, "|") ||
		strings.HasPrefix(line, "```") ||
		line == "---" || line == "***" ||
		toggleTextRe.MatchString(line) ||
		childPageRe.MatchString(line) ||
		mediaLinkRe.MatchString(line)
}

func headingBlock(level int, rts []notionapi.RichText) notionapi.Block {
	switch level {
	case 1:
		return &notionapi.Heading1Block{BasicBlock: basic("heading_1"), Heading1: notionapi.Heading{RichText: rts}}
	case 2:
		return &notionapi.Heading2Block{BasicBlock: basic("heading_2"), Heading2: notionapi.Heading{RichText: rts}}
	case 4:
		return &notion.Heading4Block{BasicBlock: basic("heading_4"), Heading4: notionapi.Heading{RichText: rts}}
	default:
		return &notionapi.Heading3Block{BasicBlock: basic("heading_3"), Heading3: notionapi.Heading{RichText: rts}}
	}
}

func mediaBlock(icon, caption, url string) notionapi.Block {
	if icon == "🖼" {
		img := notionapi.Image{
			Type:     "external",
			External: &notionapi.FileObject{URL: url},
		}
		if caption != "" && caption != "image" {
			img.Caption = textRichText(caption)
		}
		return &notionapi.ImageBlock{BasicBlock: basic("image"), Image: img}
	}
	bm := notionapi.Bookmark{URL: url}
	if caption != "" && caption != url {
		bm.Caption = textRichText(caption)
	}
	return &notionapi.BookmarkBlock{BasicBlock: basic("bookmark"), Bookmark: bm}
}

func indentWidth(line string) int {
	w := 0
	for _, r := range line {
		switch r {
		case ' ':
			w++
		case '\t':
			w += 4
		default:
			return w
		}
	}
	return w
}

func dedent(lines []string, indent int) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		trimmed := l
		for cut := 0; cut < indent && len(trimmed) > 0 && (trimmed[0] == ' ' || trimmed[0] == '\t'); cut++ {
			trimmed = trimmed[1:]
		}
		out[i] = trimmed
	}
	return out
}

func textRichText(text string) []notionapi.RichText {
	return splitLongRuns([]notionapi.RichText{{
		Type:      "text",
		Text:      &notionapi.Text{Content: text},
		PlainText: text,
	}})
}
