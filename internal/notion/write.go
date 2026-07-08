package notion

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/jomei/notionapi"
)

// TextRichText builds plain rich text, split into Notion's maximum run
// length of 2000 characters so long content never bounces off the API.
func TextRichText(text string) []notionapi.RichText {
	runes := []rune(text)
	const maxRun = 2000
	out := make([]notionapi.RichText, 0, len(runes)/maxRun+1)
	for start := 0; ; start += maxRun {
		end := min(start+maxRun, len(runes))
		chunk := string(runes[start:end])
		out = append(out, notionapi.RichText{
			Type:      "text",
			Text:      &notionapi.Text{Content: chunk},
			PlainText: chunk,
		})
		if end == len(runes) {
			break
		}
	}
	return out
}

// SetLocalRichText mutates the in-memory block for optimistic UI updates;
// it does not call the API.
func SetLocalRichText(block notionapi.Block, rts []notionapi.RichText) bool {
	switch b := block.(type) {
	case *notionapi.ParagraphBlock:
		b.Paragraph.RichText = rts
	case *notionapi.Heading1Block:
		b.Heading1.RichText = rts
	case *notionapi.Heading2Block:
		b.Heading2.RichText = rts
	case *notionapi.Heading3Block:
		b.Heading3.RichText = rts
	case *notionapi.BulletedListItemBlock:
		b.BulletedListItem.RichText = rts
	case *notionapi.NumberedListItemBlock:
		b.NumberedListItem.RichText = rts
	case *notionapi.ToDoBlock:
		b.ToDo.RichText = rts
	case *notionapi.ToggleBlock:
		b.Toggle.RichText = rts
	case *notionapi.QuoteBlock:
		b.Quote.RichText = rts
	case *notionapi.CalloutBlock:
		b.Callout.RichText = rts
	default:
		return false
	}
	return true
}

func (c *Client) update(ctx context.Context, id string, req *notionapi.BlockUpdateRequest) error {
	if err := c.limiter.Wait(ctx); err != nil {
		return err
	}
	_, err := c.api.Block.Update(ctx, notionapi.BlockID(id), req)
	return err
}

// SetToDo writes a to-do's checked state. It takes values rather than the
// block pointer so callers can apply optimistic local updates first; the
// rich_text is re-sent because the API payload cannot omit that field.
func (c *Client) SetToDo(ctx context.Context, blockID string, richText []notionapi.RichText, checked bool) error {
	return c.update(ctx, blockID, &notionapi.BlockUpdateRequest{
		ToDo: &notionapi.ToDo{
			RichText: richText,
			Checked:  checked,
		},
	})
}

// LocalRichText reads a block's editable rich text without calling the API.
func LocalRichText(block notionapi.Block) ([]notionapi.RichText, bool) {
	switch b := block.(type) {
	case *notionapi.ParagraphBlock:
		return b.Paragraph.RichText, true
	case *notionapi.Heading1Block:
		return b.Heading1.RichText, true
	case *notionapi.Heading2Block:
		return b.Heading2.RichText, true
	case *notionapi.Heading3Block:
		return b.Heading3.RichText, true
	case *notionapi.BulletedListItemBlock:
		return b.BulletedListItem.RichText, true
	case *notionapi.NumberedListItemBlock:
		return b.NumberedListItem.RichText, true
	case *notionapi.ToDoBlock:
		return b.ToDo.RichText, true
	case *notionapi.ToggleBlock:
		return b.Toggle.RichText, true
	case *notionapi.QuoteBlock:
		return b.Quote.RichText, true
	case *notionapi.CalloutBlock:
		return b.Callout.RichText, true
	}
	return nil, false
}

// SetBlockText replaces a block's rich text with a single plain-text run.
// Inline annotations on the old text are dropped by design (quick edit).
func (c *Client) SetBlockText(ctx context.Context, block notionapi.Block, text string) error {
	return c.SetBlockRichText(ctx, block, TextRichText(text))
}

// SetBlockRichText writes the given rich text runs to a block — used by
// undo to restore the pre-edit text with its annotations intact.
func (c *Client) SetBlockRichText(ctx context.Context, block notionapi.Block, rts []notionapi.RichText) error {
	req := &notionapi.BlockUpdateRequest{}
	switch b := block.(type) {
	case *notionapi.ParagraphBlock:
		req.Paragraph = &notionapi.Paragraph{RichText: rts}
	case *notionapi.Heading1Block:
		req.Heading1 = &notionapi.Heading{RichText: rts, IsToggleable: b.Heading1.IsToggleable}
	case *notionapi.Heading2Block:
		req.Heading2 = &notionapi.Heading{RichText: rts, IsToggleable: b.Heading2.IsToggleable}
	case *notionapi.Heading3Block:
		req.Heading3 = &notionapi.Heading{RichText: rts, IsToggleable: b.Heading3.IsToggleable}
	case *notionapi.BulletedListItemBlock:
		req.BulletedListItem = &notionapi.ListItem{RichText: rts}
	case *notionapi.NumberedListItemBlock:
		req.NumberedListItem = &notionapi.ListItem{RichText: rts}
	case *notionapi.ToDoBlock:
		req.ToDo = &notionapi.ToDo{RichText: rts, Checked: b.ToDo.Checked}
	case *notionapi.ToggleBlock:
		req.Toggle = &notionapi.Toggle{RichText: rts}
	case *notionapi.QuoteBlock:
		req.Quote = &notionapi.Quote{RichText: rts}
	case *notionapi.CalloutBlock:
		req.Callout = &notionapi.Callout{RichText: rts, Icon: b.Callout.Icon}
	default:
		return fmt.Errorf("block type %s is not text-editable", block.GetType())
	}
	return c.update(ctx, block.GetID().String(), req)
}

// CreatePage creates an empty page under parentID (the API cannot create
// workspace-root pages, so a parent page is required).
func (c *Client) CreatePage(ctx context.Context, parentID, title string) (Page, error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return Page{}, err
	}
	created, err := c.api.Page.Create(ctx, &notionapi.PageCreateRequest{
		Parent: notionapi.Parent{
			Type:   notionapi.ParentTypePageID,
			PageID: notionapi.PageID(parentID),
		},
		Properties: notionapi.Properties{
			"title": notionapi.TitleProperty{Title: TextRichText(title)},
		},
	})
	if err != nil {
		return Page{}, err
	}
	return Page{
		ID:         created.ID.String(),
		Title:      title,
		URL:        created.URL,
		LastEdited: created.LastEditedTime,
	}, nil
}

// DeleteBlock archives a block (Notion moves it to Trash, restorable).
func (c *Client) DeleteBlock(ctx context.Context, blockID string) error {
	if err := c.limiter.Wait(ctx); err != nil {
		return err
	}
	_, err := c.api.Block.Delete(ctx, notionapi.BlockID(blockID))
	return err
}

// RestoreBlock un-archives a deleted block, putting it back where it was.
// The client library has no unarchive support, so this is a raw API call.
func (c *Client) RestoreBlock(ctx context.Context, blockID string) error {
	_, err := c.rawRequest(ctx, http.MethodPatch, "/blocks/"+blockID, []byte(`{"archived": false}`))
	return err
}

// AppendBlocks adds blocks to parentID, after the given block if afterID is
// non-empty, otherwise at the bottom of the page. Returns created block IDs.
func (c *Client) AppendBlocks(ctx context.Context, parentID, afterID string, blocks []notionapi.Block) ([]string, error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return nil, err
	}
	resp, err := c.api.Block.AppendChildren(ctx, notionapi.BlockID(parentID), &notionapi.AppendBlockChildrenRequest{
		After:    notionapi.BlockID(afterID),
		Children: blocks,
	})
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(resp.Results))
	for _, b := range resp.Results {
		ids = append(ids, b.GetID().String())
	}
	return ids, nil
}

// ReplacePageBlocks archives every existing top-level block of the page and
// appends the given blocks in their place. Destructive: callers must confirm
// with the user first.
func (c *Client) ReplacePageBlocks(ctx context.Context, pageID string, blocks []notionapi.Block) error {
	var existing []string
	var cursor notionapi.Cursor
	for {
		if err := c.limiter.Wait(ctx); err != nil {
			return err
		}
		resp, err := c.api.Block.GetChildren(ctx, notionapi.BlockID(pageID), &notionapi.Pagination{
			StartCursor: cursor,
			PageSize:    100,
		})
		if err != nil {
			return err
		}
		for _, b := range resp.Results {
			// Archiving a child_page/child_database block trashes the whole
			// sub-page, so those survive a replace (they end up at the top).
			if !recursable(b.GetType()) {
				continue
			}
			existing = append(existing, b.GetID().String())
		}
		if !resp.HasMore {
			break
		}
		cursor = notionapi.Cursor(resp.NextCursor)
	}

	for _, id := range existing {
		if err := c.limiter.Wait(ctx); err != nil {
			return err
		}
		if _, err := c.api.Block.Delete(ctx, notionapi.BlockID(id)); err != nil {
			return fmt.Errorf("deleting old block %s: %w", id, err)
		}
	}

	for start := 0; start < len(blocks); start += 100 {
		end := min(start+100, len(blocks))
		if err := c.limiter.Wait(ctx); err != nil {
			return err
		}
		if _, err := c.api.Block.AppendChildren(ctx, notionapi.BlockID(pageID), &notionapi.AppendBlockChildrenRequest{
			Children: blocks[start:end],
		}); err != nil {
			return fmt.Errorf("appending new blocks: %w", err)
		}
	}
	return nil
}

// SetPageIcon updates a page's icon; the payload is the API's icon object
// (emoji or external). The client library has no page-icon support, so
// this goes through the raw API.
func (c *Client) SetPageIcon(ctx context.Context, pageID string, icon map[string]any) error {
	body, err := json.Marshal(map[string]any{"icon": icon})
	if err != nil {
		return err
	}
	_, err = c.rawRequest(ctx, http.MethodPatch, "/pages/"+pageID, body)
	return err
}

// UpdateTableRow rewrites one row's cells in place, preserving the table's
// identity — used when a table edit changes content but not shape.
func (c *Client) UpdateTableRow(ctx context.Context, rowID string, cells [][]notionapi.RichText) error {
	return c.update(ctx, rowID, &notionapi.BlockUpdateRequest{
		TableRow: &notionapi.TableRow{Cells: cells},
	})
}
