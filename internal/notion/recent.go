package notion

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// Recent returns a fresh, supplemented snapshot. The UI uses RefreshRecent to
// retain an index across runs and receive rows before discovery finishes.
func (c *Client) Recent(ctx context.Context) ([]Page, error) {
	return c.RecentWithProgress(ctx, nil)
}

func (c *Client) RecentWithProgress(ctx context.Context, report func(QueryProgress)) ([]Page, error) {
	index, err := c.RefreshRecent(ctx, RecentIndex{}, report, nil)
	return index.Top(), err
}

// recentChildren scans one container's pages sequentially and does no metadata
// fetching; callers can parallelize independent containers and page lookups.
func (c *Client) recentChildren(ctx context.Context, id string) (refs, containers, databases []string, err error) {
	cursor := ""
	for {
		path := "/blocks/" + url.PathEscape(id) + "/children?page_size=100"
		if cursor != "" {
			path += "&start_cursor=" + url.QueryEscape(cursor)
		}
		data, err := c.rawRequest(ctx, http.MethodGet, path, nil)
		if err != nil {
			if inaccessible(err) {
				return refs, containers, databases, nil
			}
			return nil, nil, nil, err
		}
		var children struct {
			Results []struct {
				ID          string `json:"id"`
				Type        string `json:"type"`
				HasChildren bool   `json:"has_children"`
				Archived    bool   `json:"archived"`
				InTrash     bool   `json:"in_trash"`
				Link        struct {
					Type   string `json:"type"`
					PageID string `json:"page_id"`
				} `json:"link_to_page"`
			} `json:"results"`
			HasMore    bool   `json:"has_more"`
			NextCursor string `json:"next_cursor"`
		}
		if err := json.Unmarshal(data, &children); err != nil {
			return nil, nil, nil, err
		}
		for _, block := range children.Results {
			if block.Archived || block.InTrash {
				continue
			}
			switch block.Type {
			case "child_page":
				refs = append(refs, block.ID)
			case "link_to_page":
				if block.Link.Type == "page_id" {
					refs = append(refs, block.Link.PageID)
				}
			case "child_database":
				databases = append(databases, block.ID)
			default:
				if block.HasChildren {
					containers = append(containers, block.ID)
				}
			}
		}
		if !children.HasMore {
			return refs, containers, databases, nil
		}
		if children.NextCursor == "" || children.NextCursor == cursor {
			return nil, nil, nil, fmt.Errorf("notion returned an invalid block cursor")
		}
		cursor = children.NextCursor
	}
}
