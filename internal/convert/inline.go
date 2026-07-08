package convert

import (
	"strings"

	"github.com/jomei/notionapi"
)

// ParseInline converts markdown inline syntax (bold, italic, strikethrough,
// code, links) into Notion rich text runs — the write-side counterpart of
// the inline renderer, used when saving inline edits.
func ParseInline(s string) []notionapi.RichText {
	return splitLongRuns(parseInline(s))
}

// InlineMarkdown renders rich text as inline markdown — the comparison
// form for detecting changed table cells.
func InlineMarkdown(rts []notionapi.RichText) string {
	return inline(rts)
}

// maxRunLen is Notion's per-rich-text-run character limit; longer text is
// stored as consecutive runs sharing the same formatting.
const maxRunLen = 2000

func splitLongRuns(rts []notionapi.RichText) []notionapi.RichText {
	out := make([]notionapi.RichText, 0, len(rts))
	for _, rt := range rts {
		runes := []rune(rt.PlainText)
		if len(runes) <= maxRunLen {
			out = append(out, rt)
			continue
		}
		for start := 0; start < len(runes); start += maxRunLen {
			end := min(start+maxRunLen, len(runes))
			chunk := rt
			chunk.PlainText = string(runes[start:end])
			if rt.Text != nil {
				text := *rt.Text
				text.Content = chunk.PlainText
				chunk.Text = &text
			}
			out = append(out, chunk)
		}
	}
	return out
}

// parseInline converts markdown inline syntax (bold, italic, strikethrough,
// code, links) into Notion rich text runs. Unmatched markers stay literal.
func parseInline(s string) []notionapi.RichText {
	var out []notionapi.RichText
	var buf strings.Builder

	flush := func() {
		if buf.Len() > 0 {
			out = append(out, plainRun(buf.String()))
			buf.Reset()
		}
	}
	annotated := func(inner []notionapi.RichText, set func(*notionapi.Annotations)) {
		flush()
		for i := range inner {
			if inner[i].Annotations == nil {
				inner[i].Annotations = &notionapi.Annotations{}
			}
			set(inner[i].Annotations)
		}
		out = append(out, inner...)
	}

	// Emphasis content may not start or end with a space (CommonMark),
	// which keeps "2 * 3 = 6" and dangling markers literal.
	emphasizable := func(content string) bool {
		return content != "" && !strings.HasPrefix(content, " ") && !strings.HasSuffix(content, " ")
	}

	i := 0
	for i < len(s) {
		rest := s[i:]
		switch {
		case strings.HasPrefix(rest, "**"):
			if end := strings.Index(rest[2:], "**"); end > 0 && emphasizable(rest[2:2+end]) {
				annotated(parseInline(rest[2:2+end]), func(a *notionapi.Annotations) { a.Bold = true })
				i += 4 + end
				continue
			}
		case strings.HasPrefix(rest, "~~"):
			if end := strings.Index(rest[2:], "~~"); end > 0 && emphasizable(rest[2:2+end]) {
				annotated(parseInline(rest[2:2+end]), func(a *notionapi.Annotations) { a.Strikethrough = true })
				i += 4 + end
				continue
			}
		case rest[0] == '*':
			if end := strings.IndexByte(rest[1:], '*'); end > 0 && emphasizable(rest[1:1+end]) {
				annotated(parseInline(rest[1:1+end]), func(a *notionapi.Annotations) { a.Italic = true })
				i += 2 + end
				continue
			}
		case rest[0] == '`':
			if end := strings.IndexByte(rest[1:], '`'); end > 0 {
				flush()
				run := plainRun(rest[1 : 1+end])
				run.Annotations = &notionapi.Annotations{Code: true}
				out = append(out, run)
				i += 2 + end
				continue
			}
		case rest[0] == '[':
			if mid := strings.Index(rest, "]("); mid > 0 {
				if end := strings.IndexByte(rest[mid+2:], ')'); end >= 0 {
					url := rest[mid+2 : mid+2+end]
					inner := parseInline(rest[1:mid])
					flush()
					for j := range inner {
						inner[j].Href = url
						if inner[j].Text != nil {
							inner[j].Text.Link = &notionapi.Link{Url: url}
						}
					}
					out = append(out, inner...)
					i += mid + 2 + end + 1
					continue
				}
			}
		}
		buf.WriteByte(s[i])
		i++
	}
	flush()
	if out == nil {
		out = []notionapi.RichText{}
	}
	return out
}

func plainRun(text string) notionapi.RichText {
	return notionapi.RichText{
		Type:      "text",
		Text:      &notionapi.Text{Content: text},
		PlainText: text,
	}
}

// notionLanguage maps a markdown fence language to Notion's accepted enum;
// unknown values fall back to "plain text" so the API doesn't reject the page.
func notionLanguage(lang string) string {
	lang = strings.ToLower(strings.TrimSpace(lang))
	switch lang {
	case "sh", "zsh":
		return "shell"
	case "js":
		return "javascript"
	case "ts":
		return "typescript"
	case "py":
		return "python"
	case "golang":
		return "go"
	case "cpp":
		return "c++"
	case "csharp":
		return "c#"
	case "fsharp":
		return "f#"
	case "objectivec", "objc":
		return "objective-c"
	case "rb":
		return "ruby"
	case "yml":
		return "yaml"
	case "text", "txt", "":
		return "plain text"
	}
	if notionLanguages[lang] {
		return lang
	}
	return "plain text"
}

var notionLanguages = map[string]bool{
	"abap": true, "arduino": true, "bash": true, "basic": true, "c": true,
	"clojure": true, "coffeescript": true, "c++": true, "c#": true, "css": true,
	"dart": true, "diff": true, "docker": true, "elixir": true, "elm": true,
	"erlang": true, "flow": true, "fortran": true, "f#": true, "gherkin": true,
	"glsl": true, "go": true, "graphql": true, "groovy": true, "haskell": true,
	"html": true, "java": true, "javascript": true, "json": true, "julia": true,
	"kotlin": true, "latex": true, "less": true, "lisp": true, "livescript": true,
	"lua": true, "makefile": true, "markdown": true, "markup": true, "matlab": true,
	"mermaid": true, "nix": true, "objective-c": true, "ocaml": true, "pascal": true,
	"perl": true, "php": true, "plain text": true, "powershell": true, "prolog": true,
	"protobuf": true, "python": true, "r": true, "reason": true, "ruby": true,
	"rust": true, "sass": true, "scala": true, "scheme": true, "scss": true,
	"shell": true, "sql": true, "swift": true, "typescript": true, "vb.net": true,
	"verilog": true, "vhdl": true, "visual basic": true, "webassembly": true,
	"xml": true, "yaml": true,
}
