package convert

import (
	"github.com/jomei/notionapi"

	"github.com/justinm35/lazynotion/internal/notion"
)

// NodesFromBlocks lifts freshly built blocks (children nested inside the
// type-specific payloads, as ParseMarkdown produces) into the BlockNode tree
// the renderer walks.
func NodesFromBlocks(blocks []notionapi.Block) []notion.BlockNode {
	nodes := make([]notion.BlockNode, 0, len(blocks))
	for _, b := range blocks {
		nodes = append(nodes, notion.BlockNode{
			Block:    b,
			Children: NodesFromBlocks(inlineChildren(b)),
		})
	}
	return nodes
}

func inlineChildren(b notionapi.Block) []notionapi.Block {
	switch block := b.(type) {
	case *notionapi.ParagraphBlock:
		return block.Paragraph.Children
	case *notion.Heading4Block:
		return block.Heading4.Children
	case *notionapi.BulletedListItemBlock:
		return block.BulletedListItem.Children
	case *notionapi.NumberedListItemBlock:
		return block.NumberedListItem.Children
	case *notionapi.ToDoBlock:
		return block.ToDo.Children
	case *notionapi.ToggleBlock:
		return block.Toggle.Children
	case *notionapi.QuoteBlock:
		return block.Quote.Children
	case *notionapi.CalloutBlock:
		return block.Callout.Children
	case *notionapi.TableBlock:
		return block.Table.Children
	}
	return nil
}
