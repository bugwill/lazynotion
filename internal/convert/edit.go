package convert

import (
	"strconv"
	"strings"

	"github.com/jomei/notionapi"
)

// EditPatch is the interpreted result of an inline block edit: the block
// kind the typed marker implies, the inline-parsed text, and (for to-dos)
// the checkbox state.
type EditPatch struct {
	Kind     string
	RichText []notionapi.RichText
	Checked  bool
}

// ParseEditPatch reads a block edit: the first line's marker decides the
// kind, continuation lines stay part of the same block's text.
func ParseEditPatch(value string) EditPatch {
	lines := strings.Split(value, "\n")
	first := lines[0]

	kind := "paragraph"
	checked := false
	content := first
	switch {
	case toggleTextRe.MatchString(first):
		kind = "toggle"
		content = toggleTextRe.FindStringSubmatch(first)[1]
	case headingRe.MatchString(first):
		m := headingRe.FindStringSubmatch(first)
		switch len(m[1]) {
		case 1:
			kind = "heading_1"
		case 2:
			kind = "heading_2"
		default:
			kind = "heading_3"
		}
		content = m[2]
	case todoRe.MatchString(first):
		m := todoRe.FindStringSubmatch(first)
		kind = "to_do"
		checked = m[1] != " "
		content = m[2]
	case bulletRe.MatchString(first):
		kind = "bulleted_list_item"
		content = bulletRe.FindStringSubmatch(first)[1]
	case numberedRe.MatchString(first):
		kind = "numbered_list_item"
		content = numberedRe.FindStringSubmatch(first)[2]
	case strings.HasPrefix(first, ">"):
		kind = "quote"
		content = strings.TrimPrefix(strings.TrimPrefix(first, ">"), " ")
	}

	for _, l := range lines[1:] {
		if kind == "quote" {
			l = strings.TrimPrefix(strings.TrimPrefix(l, ">"), " ")
		}
		content += "\n" + l
	}
	return EditPatch{Kind: kind, RichText: ParseInline(content), Checked: checked}
}

// ListContinuation reports whether a line is a list item that should
// continue onto a new block on enter, notion-style. marker is what the
// next item starts with; empty means the item has no content yet (enter
// should end the list instead).
func ListContinuation(line string) (marker string, empty bool, ok bool) {
	switch {
	case todoRe.MatchString(line):
		m := todoRe.FindStringSubmatch(line)
		return "- [ ] ", strings.TrimSpace(m[2]) == "", true
	case bulletRe.MatchString(line):
		m := bulletRe.FindStringSubmatch(line)
		return "- ", strings.TrimSpace(m[1]) == "", true
	case numberedRe.MatchString(line):
		m := numberedRe.FindStringSubmatch(line)
		n, _ := strconv.Atoi(m[1])
		return strconv.Itoa(n+1) + ". ", strings.TrimSpace(m[2]) == "", true
	}
	return "", false, false
}

// BuildBlock constructs a fresh block of the given kind — used when an edit
// changes a block's marker and the block gets recreated as the new type.
func BuildBlock(kind string, rts []notionapi.RichText, checked bool) notionapi.Block {
	switch kind {
	case "heading_1":
		return headingBlock(1, rts)
	case "heading_2":
		return headingBlock(2, rts)
	case "heading_3":
		return headingBlock(3, rts)
	case "to_do":
		return &notionapi.ToDoBlock{BasicBlock: basic("to_do"), ToDo: notionapi.ToDo{RichText: rts, Checked: checked}}
	case "bulleted_list_item":
		return &notionapi.BulletedListItemBlock{BasicBlock: basic("bulleted_list_item"), BulletedListItem: notionapi.ListItem{RichText: rts}}
	case "numbered_list_item":
		return &notionapi.NumberedListItemBlock{BasicBlock: basic("numbered_list_item"), NumberedListItem: notionapi.ListItem{RichText: rts}}
	case "quote":
		return &notionapi.QuoteBlock{BasicBlock: basic("quote"), Quote: notionapi.Quote{RichText: rts}}
	case "toggle":
		return &notionapi.ToggleBlock{BasicBlock: basic("toggle"), Toggle: notionapi.Toggle{RichText: rts}}
	default:
		return &notionapi.ParagraphBlock{BasicBlock: basic("paragraph"), Paragraph: notionapi.Paragraph{RichText: rts}}
	}
}
