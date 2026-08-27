package convert

import (
	"strconv"
	"strings"

	"github.com/jomei/notionapi"

	"github.com/justinm35/lazynotion/internal/notion"
)

// Unit is one cursor-addressable block: its markdown fragment rendered
// standalone, plus enough metadata for the viewer to act on it.
type Unit struct {
	Node     notion.BlockNode
	Depth    int
	Markdown string
	// TopLevel marks direct children of the page — the only valid anchors
	// for the append API's `after` parameter.
	TopLevel bool
	// Foldable marks toggles and toggleable headings that have children;
	// Collapsed means those children were omitted from the unit list.
	Foldable  bool
	Collapsed bool
	// ParentID is the block containing this unit ("" for page level) —
	// the append target when creating siblings.
	ParentID string
}

func (u Unit) ID() string {
	return u.Node.Block.GetID().String()
}

func (u Unit) Image() (url, caption string, ok bool) {
	img, isImage := u.Node.Block.(*notionapi.ImageBlock)
	if !isImage {
		return "", "", false
	}
	return img.Image.GetURL(), plain(img.Image.Caption), true
}

// SearchText returns the text in-page search matches against — block text
// plus captions, titles, code and table cells.
func (u Unit) SearchText() string {
	if text, ok := u.PlainText(); ok {
		return text
	}
	switch b := u.Node.Block.(type) {
	case *notionapi.CodeBlock:
		return plain(b.Code.RichText)
	case *notionapi.ImageBlock:
		return plain(b.Image.Caption)
	case *notionapi.BookmarkBlock:
		return plain(b.Bookmark.Caption) + " " + b.Bookmark.URL
	case *notionapi.ChildPageBlock:
		return b.ChildPage.Title
	case *notionapi.ChildDatabaseBlock:
		return b.ChildDatabase.Title
	case *notionapi.EquationBlock:
		return b.Equation.Expression
	case *notionapi.TableBlock:
		var cells []string
		for _, child := range u.Node.Children {
			if row, ok := child.Block.(*notionapi.TableRowBlock); ok {
				for _, cell := range row.TableRow.Cells {
					cells = append(cells, plain(cell))
				}
			}
		}
		return strings.Join(cells, " ")
	}
	return ""
}

// NestTarget reports blocks that own a body new blocks should go into
// (toggles and toggleable headings) — even before they have children.
func (u Unit) NestTarget() bool {
	return foldableNode(u.Node)
}

// PageRef resolves blocks that point at another page: child pages (whose
// block ID is the page ID) and link_to_page blocks.
func (u Unit) PageRef() (pageID, title string, ok bool) {
	switch block := u.Node.Block.(type) {
	case *notionapi.ChildPageBlock:
		return u.ID(), block.ChildPage.Title, true
	case *notionapi.LinkToPageBlock:
		if block.LinkToPage.PageID != "" {
			return string(block.LinkToPage.PageID), "linked page", true
		}
	}
	return "", "", false
}

// DatabaseRef resolves blocks that point at a database: a child_database
// block's ID is the database ID.
func (u Unit) DatabaseRef() (databaseID, title string, ok bool) {
	if db, isDB := u.Node.Block.(*notionapi.ChildDatabaseBlock); isDB {
		return u.ID(), db.ChildDatabase.Title, true
	}
	return "", "", false
}

func (u Unit) IsToDo() (checked bool, ok bool) {
	if todo, isTodo := u.Node.Block.(*notionapi.ToDoBlock); isTodo {
		return todo.ToDo.Checked, true
	}
	return false, false
}

// PlainText returns the block's editable text, or ok=false for blocks
// without a single rich_text payload (tables, images, dividers, ...).
func (u Unit) PlainText() (string, bool) {
	rts, ok := editableRichText(u.Node.Block)
	if !ok {
		return "", false
	}
	return plain(rts), true
}

// EditableMarkdown renders the block as editing markdown: the block marker
// (- , - [ ], > , # , **▸ …**) plus inline markdown, so both round-trip
// through ParseEditPatch. Callouts have no markdown marker and seed as
// plain inline text.
func (u Unit) EditableMarkdown() (string, bool) {
	// tables edit as their markdown grid, which round-trips the parser
	if _, isTable := u.Node.Block.(*notionapi.TableBlock); isTable {
		return strings.TrimSpace(u.Markdown), true
	}
	rts, ok := editableRichText(u.Node.Block)
	if !ok {
		return "", false
	}
	text := inline(rts)
	switch b := u.Node.Block.(type) {
	case *notionapi.Heading1Block:
		return "# " + oneLine(text), true
	case *notionapi.Heading2Block:
		return "## " + oneLine(text), true
	case *notionapi.Heading3Block:
		return "### " + oneLine(text), true
	case *notionapi.BulletedListItemBlock:
		return "- " + text, true
	case *notionapi.NumberedListItemBlock:
		return "1. " + text, true
	case *notionapi.ToDoBlock:
		if b.ToDo.Checked {
			return "- [x] " + text, true
		}
		return "- [ ] " + text, true
	case *notionapi.QuoteBlock:
		return "> " + strings.ReplaceAll(text, "\n", "\n> "), true
	case *notionapi.ToggleBlock:
		return "**▸ " + oneLine(text) + "**", true
	}
	return text, true
}

func editableRichText(b notionapi.Block) ([]notionapi.RichText, bool) {
	switch block := b.(type) {
	case *notionapi.ParagraphBlock:
		return block.Paragraph.RichText, true
	case *notionapi.Heading1Block:
		return block.Heading1.RichText, true
	case *notionapi.Heading2Block:
		return block.Heading2.RichText, true
	case *notionapi.Heading3Block:
		return block.Heading3.RichText, true
	case *notionapi.BulletedListItemBlock:
		return block.BulletedListItem.RichText, true
	case *notionapi.NumberedListItemBlock:
		return block.NumberedListItem.RichText, true
	case *notionapi.ToDoBlock:
		return block.ToDo.RichText, true
	case *notionapi.ToggleBlock:
		return block.Toggle.RichText, true
	case *notionapi.QuoteBlock:
		return block.Quote.RichText, true
	case *notionapi.CalloutBlock:
		return block.Callout.RichText, true
	}
	return nil, false
}

// Flatten walks the tree in document order and emits one Unit per
// addressable block. Structural wrappers contribute no unit of their own;
// quotes, callouts and tables consume their children into a single unit.
func Flatten(nodes []notion.BlockNode) []Unit {
	return FlattenFolded(nodes, nil)
}

// FlattenFolded omits the children of blocks whose IDs are in collapsed —
// the client-side fold state for toggles and toggleable headings (Notion's
// API does not expose an open/closed state).
func FlattenFolded(nodes []notion.BlockNode, collapsed map[string]bool) []Unit {
	var units []Unit
	flattenInto(&units, nodes, 0, true, collapsed, "")
	return units
}

// foldableNode reports blocks whose children can be folded away.
func foldableNode(node notion.BlockNode) bool {
	switch b := node.Block.(type) {
	case *notionapi.ToggleBlock:
		return true
	case *notionapi.Heading1Block:
		return b.Heading1.IsToggleable
	case *notionapi.Heading2Block:
		return b.Heading2.IsToggleable
	case *notionapi.Heading3Block:
		return b.Heading3.IsToggleable
	}
	return false
}

// withFoldMarker sets the ▸/▾ state indicator in a foldable block's
// fragment. Toggles already carry a ▸ from the renderer; toggleable
// headings get one inserted after the heading marker.
func withFoldMarker(md string, node notion.BlockNode, expanded bool) string {
	marker := "▸"
	if expanded {
		marker = "▾"
	}
	if _, isToggle := node.Block.(*notionapi.ToggleBlock); isToggle {
		return strings.Replace(md, "▸", marker, 1)
	}
	if idx := strings.Index(md, " "); idx > 0 {
		return md[:idx+1] + marker + " " + md[idx+1:]
	}
	return md
}

func flattenInto(units *[]Unit, nodes []notion.BlockNode, depth int, topLevel bool, collapsed map[string]bool, parentID string) {
	ordinal := 0
	for _, node := range nodes {
		if _, ok := node.Block.(*notionapi.NumberedListItemBlock); ok {
			ordinal++
		} else {
			ordinal = 0
		}

		switch node.Block.(type) {
		case *notionapi.ColumnListBlock, *notionapi.ColumnBlock,
			*notionapi.SyncedBlock, *notionapi.TemplateBlock:
			flattenInto(units, node.Children, depth, false, collapsed, node.Block.GetID().String())
			continue
		case *notionapi.TableOfContentsBlock, *notionapi.BreadcrumbBlock:
			continue
		}

		unit := Unit{
			Node:     node,
			Depth:    depth,
			Markdown: fragment(node, ordinal),
			TopLevel: topLevel,
			ParentID: parentID,
		}
		if foldableNode(node) && len(node.Children) > 0 {
			unit.Foldable = true
			unit.Collapsed = collapsed[node.Block.GetID().String()]
			unit.Markdown = withFoldMarker(unit.Markdown, node, !unit.Collapsed)
		}
		*units = append(*units, unit)
		if unit.Collapsed {
			continue
		}

		switch node.Block.(type) {
		case *notionapi.TableBlock, *notionapi.QuoteBlock, *notionapi.CalloutBlock:
			// children already folded into the fragment
		case *notionapi.BulletedListItemBlock, *notionapi.NumberedListItemBlock,
			*notionapi.ToDoBlock, *notionapi.ToggleBlock:
			flattenInto(units, node.Children, depth+1, false, collapsed, node.Block.GetID().String())
		default:
			flattenInto(units, node.Children, depth, false, collapsed, node.Block.GetID().String())
		}
	}
}

// fragment renders a single block as standalone markdown, without the
// children that Flatten emits as separate units.
func fragment(node notion.BlockNode, ordinal int) string {
	shallow := node
	switch node.Block.(type) {
	case *notionapi.TableBlock, *notionapi.QuoteBlock, *notionapi.CalloutBlock:
	default:
		shallow.Children = nil
	}

	var b strings.Builder
	renderNode(&b, shallow, 0)
	md := strings.TrimSpace(b.String())

	if ordinal > 1 {
		if rest, found := strings.CutPrefix(md, "1. "); found {
			md = strconv.Itoa(ordinal) + ". " + rest
		}
	}
	return md
}
