package notion

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/jomei/notionapi"
)

// CreationPayload turns a fetched block (plus its cached subtree) into a
// payload the append API accepts: read-only fields stripped, children nested
// back into the type-specific field.
func CreationPayload(node BlockNode) (map[string]any, error) {
	raw, err := json.Marshal(node.Block)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	for _, k := range []string{
		"id", "created_time", "last_edited_time", "created_by",
		"last_edited_by", "has_children", "archived", "in_trash", "parent",
	} {
		delete(m, k)
	}
	typeKey, _ := m["type"].(string)
	payload, ok := m[typeKey].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("block %s has no %q payload", node.Block.GetID(), typeKey)
	}
	if len(node.Children) > 0 {
		children := make([]map[string]any, 0, len(node.Children))
		for _, child := range node.Children {
			cm, err := CreationPayload(child)
			if err != nil {
				return nil, err
			}
			children = append(children, cm)
		}
		payload["children"] = children
	}
	return m, nil
}

// CanRecreate reports whether a block survives the recreate-then-delete
// dance moving requires; reason explains a refusal.
func CanRecreate(node BlockNode) (bool, string) {
	switch b := node.Block.(type) {
	case *notionapi.ChildPageBlock, *notionapi.ChildDatabaseBlock:
		return false, "sub-pages can't be moved via the API"
	case *notionapi.LinkToPageBlock, *notionapi.LinkPreviewBlock:
		return false, "page links can't be recreated via the API"
	case *notionapi.SyncedBlock, *notionapi.TemplateBlock:
		return false, "synced blocks can't be recreated via the API"
	case *notionapi.UnsupportedBlock:
		return false, "unsupported block type"
	case *notionapi.ImageBlock:
		if b.Image.File != nil {
			return false, "uploaded images can't be recreated via the API"
		}
	case *notionapi.VideoBlock:
		if b.Video.File != nil {
			return false, "uploaded videos can't be recreated via the API"
		}
	case *notionapi.FileBlock:
		if b.File.File != nil {
			return false, "uploaded files can't be recreated via the API"
		}
	case *notionapi.PdfBlock:
		if b.Pdf.File != nil {
			return false, "uploaded files can't be recreated via the API"
		}
	case *notionapi.AudioBlock:
		if b.Audio.File != nil {
			return false, "uploaded audio can't be recreated via the API"
		}
	}
	for _, child := range node.Children {
		if ok, reason := CanRecreate(child); !ok {
			return false, reason
		}
	}
	return true, ""
}

type rawAppendResponse struct {
	Results []struct {
		ID string `json:"id"`
	} `json:"results"`
}

func (c *Client) appendRaw(ctx context.Context, parentID, afterID string, children []map[string]any) ([]string, error) {
	body := map[string]any{"children": children}
	if afterID != "" {
		body["after"] = afterID
	}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	respData, err := c.rawRequest(ctx, http.MethodPatch, "/blocks/"+parentID+"/children", data)
	if err != nil {
		return nil, err
	}
	var resp rawAppendResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(resp.Results))
	for _, r := range resp.Results {
		ids = append(ids, r.ID)
	}
	return ids, nil
}

// AppendRawBlocks appends creation-payload maps (see CreationPayload) —
// used to reinsert previously fetched blocks, whose jomei structs carry
// read-only fields the API rejects.
func (c *Client) AppendRawBlocks(ctx context.Context, parentID, afterID string, children []map[string]any) ([]string, error) {
	return c.appendRaw(ctx, parentID, afterID, children)
}

// MoveBlockAfter re-homes a block: a copy (subtree included) is appended
// after afterID, then the original is archived. The copy lands first so a
// failure can duplicate a block but never lose one. Returns the copy's new
// ID so callers can patch their local tree without a full reload.
func (c *Client) MoveBlockAfter(ctx context.Context, parentID string, node BlockNode, afterID string) (string, error) {
	payload, err := CreationPayload(node)
	if err != nil {
		return "", err
	}
	ids, err := c.appendRaw(ctx, parentID, afterID, []map[string]any{payload})
	if err != nil {
		return "", fmt.Errorf("recreating block: %w", err)
	}
	if len(ids) == 0 {
		return "", fmt.Errorf("recreating block: no block returned")
	}
	if err := c.DeleteBlock(ctx, node.Block.GetID().String()); err != nil {
		return ids[0], fmt.Errorf("removing original after copy (page now has a duplicate): %w", err)
	}
	return ids[0], nil
}

// MoveBlockToTop places node at position 0, which the append API cannot
// express directly: the node is recreated after the current first block,
// then the first block is recreated below it. Returns both new IDs.
func (c *Client) MoveBlockToTop(ctx context.Context, parentID string, node, first BlockNode) (nodeID, firstID string, err error) {
	nodeID, err = c.MoveBlockAfter(ctx, parentID, node, first.Block.GetID().String())
	if err != nil {
		return "", "", err
	}
	firstID, err = c.MoveBlockAfter(ctx, parentID, first, nodeID)
	if err != nil {
		return nodeID, "", err
	}
	return nodeID, firstID, nil
}

// ReplaceBlockID rewrites a node's block ID in place via a JSON round trip
// (block types are closed structs); on any failure the original node is
// returned unchanged.
func ReplaceBlockID(node BlockNode, newID string) BlockNode {
	raw, err := json.Marshal(node.Block)
	if err != nil {
		return node
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return node
	}
	m["id"] = newID
	patched, err := json.Marshal(m)
	if err != nil {
		return node
	}
	var blocks notionapi.Blocks
	if err := json.Unmarshal(append(append([]byte("["), patched...), ']'), &blocks); err != nil || len(blocks) != 1 {
		return node
	}
	return BlockNode{Block: blocks[0], Children: node.Children}
}
