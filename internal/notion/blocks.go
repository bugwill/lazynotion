package notion

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"

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
		resp, err := c.blockChildren(ctx, id, cursor)
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

type blockChildrenResponse struct {
	Results    []notionapi.Block
	HasMore    bool
	NextCursor string
}

func (c *Client) blockChildren(ctx context.Context, id notionapi.BlockID, cursor notionapi.Cursor) (blockChildrenResponse, error) {
	query := url.Values{"page_size": {"100"}}
	if cursor != "" {
		query.Set("start_cursor", string(cursor))
	}
	data, err := c.rawRequest(ctx, http.MethodGet, "/blocks/"+id.String()+"/children?"+query.Encode(), nil)
	if err != nil {
		return blockChildrenResponse{}, err
	}
	var raw struct {
		Results    []json.RawMessage `json:"results"`
		HasMore    bool              `json:"has_more"`
		NextCursor string            `json:"next_cursor"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return blockChildrenResponse{}, err
	}
	resp := blockChildrenResponse{HasMore: raw.HasMore, NextCursor: raw.NextCursor}
	for _, data := range raw.Results {
		block, err := DecodeBlock(data)
		if err != nil {
			return blockChildrenResponse{}, err
		}
		resp.Results = append(resp.Results, block)
	}
	return resp, nil
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
