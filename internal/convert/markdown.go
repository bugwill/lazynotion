package convert

import (
	"fmt"
	"strings"

	"github.com/jomei/notionapi"

	"github.com/justinm35/lazynotion/internal/icons"
	"github.com/justinm35/lazynotion/internal/notion"
)

func ToMarkdown(nodes []notion.BlockNode) string {
	var b strings.Builder
	renderNodes(&b, nodes, 0)
	return strings.TrimSpace(b.String()) + "\n"
}

func renderNodes(b *strings.Builder, nodes []notion.BlockNode, depth int) {
	for _, node := range nodes {
		renderNode(b, node, depth)
	}
}

func renderNode(b *strings.Builder, node notion.BlockNode, depth int) {
	pad := strings.Repeat("    ", depth)

	switch block := node.Block.(type) {
	case *notionapi.ParagraphBlock:
		writeLine(b, pad, hardBreaks(inline(block.Paragraph.RichText)))
		renderNodes(b, node.Children, depth)

	case *notionapi.Heading1Block:
		writeLine(b, pad, "# "+oneLine(inline(block.Heading1.RichText)))
		renderNodes(b, node.Children, depth)

	case *notionapi.Heading2Block:
		writeLine(b, pad, "## "+oneLine(inline(block.Heading2.RichText)))
		renderNodes(b, node.Children, depth)

	case *notionapi.Heading3Block:
		writeLine(b, pad, "### "+oneLine(inline(block.Heading3.RichText)))
		renderNodes(b, node.Children, depth)

	case *notionapi.BulletedListItemBlock:
		writeLine(b, pad, "- "+hardBreaks(inline(block.BulletedListItem.RichText)))
		renderNodes(b, node.Children, depth+1)

	case *notionapi.NumberedListItemBlock:
		writeLine(b, pad, "1. "+hardBreaks(inline(block.NumberedListItem.RichText)))
		renderNodes(b, node.Children, depth+1)

	case *notionapi.ToDoBlock:
		box := "[ ]"
		if block.ToDo.Checked {
			box = "[x]"
		}
		writeLine(b, pad, "- "+box+" "+hardBreaks(inline(block.ToDo.RichText)))
		renderNodes(b, node.Children, depth+1)

	case *notionapi.ToggleBlock:
		writeLine(b, pad, "**▸ "+oneLine(inline(block.Toggle.RichText))+"**")
		renderNodes(b, node.Children, depth)

	case *notionapi.QuoteBlock:
		writeQuote(b, pad, "> "+hardBreaks(inline(block.Quote.RichText)), node.Children)

	case *notionapi.CalloutBlock:
		icon := icons.FromNotion(block.Callout.Icon).Glyph()
		if icon == "" {
			icon = "💡"
		}
		writeQuote(b, pad, "> "+icon+" "+hardBreaks(inline(block.Callout.RichText)), node.Children)

	case *notionapi.CodeBlock:
		lang := codeLanguage(block.Code.Language)
		text := plain(block.Code.RichText)
		writeLine(b, pad, "```"+lang+"\n"+text+"\n```")

	case *notionapi.DividerBlock:
		writeLine(b, pad, "---")

	case *notionapi.TableBlock:
		renderTable(b, pad, node)

	case *notionapi.ImageBlock:
		label := plain(block.Image.Caption)
		if label == "" {
			label = "image"
		}
		writeLine(b, pad, fmt.Sprintf("[🖼 %s](%s)", label, block.Image.GetURL()))

	case *notionapi.BookmarkBlock:
		writeLine(b, pad, linkLine("🔖", plain(block.Bookmark.Caption), block.Bookmark.URL))

	case *notionapi.EmbedBlock:
		writeLine(b, pad, linkLine("🔗", plain(block.Embed.Caption), block.Embed.URL))

	case *notionapi.LinkPreviewBlock:
		writeLine(b, pad, linkLine("🔗", "", block.LinkPreview.URL))

	case *notionapi.VideoBlock:
		writeLine(b, pad, linkLine("🎬", plain(block.Video.Caption), fileURL(block.Video.File, block.Video.External)))

	case *notionapi.AudioBlock:
		writeLine(b, pad, linkLine("🔊", plain(block.Audio.Caption), fileURL(block.Audio.File, block.Audio.External)))

	case *notionapi.LinkToPageBlock:
		writeLine(b, pad, "📄 *linked page*")

	case *notionapi.FileBlock:
		writeLine(b, pad, linkLine("📎", plain(block.File.Caption), fileURL(block.File.File, block.File.External)))

	case *notionapi.PdfBlock:
		writeLine(b, pad, linkLine("📎", plain(block.Pdf.Caption), fileURL(block.Pdf.File, block.Pdf.External)))

	case *notionapi.ChildPageBlock:
		writeLine(b, pad, "📄 **"+block.ChildPage.Title+"**")

	case *notionapi.ChildDatabaseBlock:
		writeLine(b, pad, "🗃 **"+block.ChildDatabase.Title+"**")

	case *notionapi.EquationBlock:
		writeLine(b, pad, "```\n"+block.Equation.Expression+"\n```")

	case *notionapi.ColumnListBlock, *notionapi.ColumnBlock, *notionapi.SyncedBlock, *notionapi.TemplateBlock:
		renderNodes(b, node.Children, depth)

	case *notionapi.TableOfContentsBlock, *notionapi.BreadcrumbBlock:

	default:
		writeLine(b, pad, fmt.Sprintf("*[unsupported block: %s]*", node.Block.GetType()))
	}
}

func writeLine(b *strings.Builder, pad, line string) {
	if strings.TrimSpace(line) == "" {
		return
	}
	for _, l := range strings.Split(line, "\n") {
		b.WriteString(pad + l + "\n")
	}
	b.WriteString("\n")
}

// hardBreaks turns in-block newlines into markdown hard line breaks so the
// renderer keeps them (a bare newline is a soft wrap and collapses).
func hardBreaks(s string) string {
	return strings.ReplaceAll(s, "\n", "  \n")
}

// oneLine flattens newlines for constructs that must stay on one markdown
// line (headings, toggles, table cells).
func oneLine(s string) string {
	return strings.ReplaceAll(s, "\n", " ")
}

func writeQuote(b *strings.Builder, pad, head string, children []notion.BlockNode) {
	if len(children) == 0 {
		writeLine(b, pad, head)
		return
	}
	for _, l := range strings.Split(head, "\n") {
		b.WriteString(pad + l + "\n")
	}
	b.WriteString(pad + "> \n")
	var inner strings.Builder
	renderNodes(&inner, children, 0)
	for _, l := range strings.Split(strings.TrimSpace(inner.String()), "\n") {
		b.WriteString(pad + "> " + l + "\n")
	}
	b.WriteString("\n")
}

func renderTable(b *strings.Builder, pad string, node notion.BlockNode) {
	var rows [][]string
	for _, child := range node.Children {
		row, ok := child.Block.(*notionapi.TableRowBlock)
		if !ok {
			continue
		}
		var cells []string
		for _, cell := range row.TableRow.Cells {
			cells = append(cells, strings.ReplaceAll(oneLine(inline(cell)), "|", "\\|"))
		}
		rows = append(rows, cells)
	}
	if len(rows) == 0 {
		return
	}
	var t strings.Builder
	t.WriteString(pad + "| " + strings.Join(rows[0], " | ") + " |\n")
	t.WriteString(pad + "|" + strings.Repeat(" --- |", len(rows[0])) + "\n")
	for _, row := range rows[1:] {
		t.WriteString(pad + "| " + strings.Join(row, " | ") + " |\n")
	}
	b.WriteString(t.String() + "\n")
}

func codeLanguage(lang string) string {
	switch lang {
	case "plain text":
		return ""
	case "c++":
		return "cpp"
	case "c#":
		return "csharp"
	case "f#":
		return "fsharp"
	case "objective-c":
		return "objectivec"
	case "shell":
		return "sh"
	}
	return lang
}

func linkLine(icon, caption, url string) string {
	if url == "" {
		return ""
	}
	label := caption
	if label == "" {
		label = url
	}
	return fmt.Sprintf("[%s %s](%s)", icon, label, url)
}

func fileURL(file, external *notionapi.FileObject) string {
	if file != nil {
		return file.URL
	}
	if external != nil {
		return external.URL
	}
	return ""
}

func plain(rts []notionapi.RichText) string {
	var b strings.Builder
	for _, rt := range rts {
		b.WriteString(rt.PlainText)
	}
	return b.String()
}

func inline(rts []notionapi.RichText) string {
	var b strings.Builder
	for i := 0; i < len(rts); {
		rt := rts[i]
		if rt.Type == "equation" && rt.Equation != nil {
			b.WriteString("`" + rt.Equation.Expression + "`")
			i++
			continue
		}

		// Notion may split one annotated string into several rich-text
		// fragments (including at its API length limit). Wrapping each
		// fragment independently can produce adjacent delimiters such as
		// **捕捉** **历史性** or **one****two**, which some Markdown
		// renderers fail to treat as one strong span. Join neighboring runs
		// with the same visible formatting before adding Markdown syntax.
		end := i + 1
		for end < len(rts) && sameInlineStyle(rt, rts[end]) {
			end++
		}
		var text strings.Builder
		for _, part := range rts[i:end] {
			text.WriteString(part.PlainText)
		}
		rendered := annotate(text.String(), rt.Annotations)
		if rt.Href != "" {
			rendered = "[" + rendered + "](" + rt.Href + ")"
		}
		b.WriteString(rendered)
		i = end
	}
	return b.String()
}

func sameInlineStyle(a, b notionapi.RichText) bool {
	if b.Type == "equation" || a.Href != b.Href {
		return false
	}
	return inlineAnnotationKey(a.Annotations) == inlineAnnotationKey(b.Annotations)
}

func inlineAnnotationKey(a *notionapi.Annotations) [5]bool {
	if a == nil {
		return [5]bool{}
	}
	return [5]bool{a.Bold, a.Italic, a.Strikethrough, a.Code, a.Underline}
}

func annotate(text string, a *notionapi.Annotations) string {
	if a == nil {
		return text
	}
	// Keep inline styles self-contained on each hard line. The viewer renders
	// hard breaks one line at a time, so a marker opened before a newline would
	// otherwise be parsed in a different fragment from its closing marker.
	if strings.Contains(text, "\n") {
		lines := strings.Split(text, "\n")
		for i := range lines {
			lines[i] = annotateLine(lines[i], a)
		}
		return strings.Join(lines, "\n")
	}
	return annotateLine(text, a)
}

func annotateLine(text string, a *notionapi.Annotations) string {
	trimmed := strings.TrimLeft(text, " ")
	lead := text[:len(text)-len(trimmed)]
	core := strings.TrimRight(trimmed, " ")
	trail := trimmed[len(core):]
	if core == "" {
		return text
	}
	if a.Code {
		return lead + "`" + core + "`" + trail
	}
	if a.Bold {
		core = "**" + core + "**"
	}
	if a.Italic {
		core = "*" + core + "*"
	}
	if a.Strikethrough {
		core = "~~" + core + "~~"
	}
	return lead + core + trail
}
