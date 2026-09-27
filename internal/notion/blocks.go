package notion

import (
	"context"

	"github.com/jomei/notionapi"
)

const maxBlockDepth = 10

type BlockNode struct {
	Block    notionapi.Block
	Children []BlockNode
}

func (c *Client) PageBlocks(ctx context.Context, pageID string) ([]BlockNode, error) {
	return c.childBlocks(ctx, notionapi.BlockID(pageID), 0)
}

func (c *Client) childBlocks(ctx context.Context, id notionapi.BlockID, depth int) ([]BlockNode, error) {
	if depth >= maxBlockDepth {
		return nil, nil
	}
	var nodes []BlockNode
	var cursor notionapi.Cursor
	for {
		if err := c.waitRequest(ctx); err != nil {
			return nil, err
		}
		resp, err := c.api.Block.GetChildren(ctx, id, &notionapi.Pagination{
			StartCursor: cursor,
			PageSize:    100,
		})
		if err != nil {
			return nil, err
		}
		for _, block := range resp.Results {
			node := BlockNode{Block: block}
			if block.GetHasChildren() && recursable(block.GetType()) {
				children, err := c.childBlocks(ctx, notionapi.BlockID(block.GetID().String()), depth+1)
				if err != nil {
					return nil, err
				}
				node.Children = children
			}
			nodes = append(nodes, node)
		}
		if !resp.HasMore {
			break
		}
		cursor = notionapi.Cursor(resp.NextCursor)
	}
	return nodes, nil
}

// recursable also doubles as ReplacePageBlocks' deletion guard: types listed
// here are never archived by a page replace (sub-pages would take their whole
// tree with them; link_to_page can't be recreated from markdown).
func recursable(t notionapi.BlockType) bool {
	switch string(t) {
	case "child_page", "child_database", "link_to_page":
		return false
	}
	return true
}
