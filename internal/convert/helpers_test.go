package convert

import (
	"github.com/jomei/notionapi"

	"github.com/justinm35/lazynotion/internal/notion"
)

func nodesFromBlocks(blocks []notionapi.Block) []notion.BlockNode {
	return NodesFromBlocks(blocks)
}
