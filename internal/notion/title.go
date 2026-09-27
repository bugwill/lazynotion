package notion

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// SetPageTitle discovers the title property so database rows with renamed
// title columns are updated just like ordinary pages.
func (c *Client) SetPageTitle(ctx context.Context, pageID, title string) error {
	data, err := c.rawRequest(ctx, http.MethodGet, "/pages/"+pageID, nil)
	if err != nil {
		return err
	}
	var page rawPage
	if err := json.Unmarshal(data, &page); err != nil {
		return err
	}
	for name, property := range page.Properties {
		if property.Type != "title" {
			continue
		}
		body, err := json.Marshal(map[string]any{"properties": map[string]any{
			name: map[string]any{"title": TextRichText(title)},
		}})
		if err != nil {
			return err
		}
		_, err = c.rawRequest(ctx, http.MethodPatch, "/pages/"+pageID, body)
		return err
	}
	return fmt.Errorf("page has no title property")
}
