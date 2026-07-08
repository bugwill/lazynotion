package notion

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jomei/notionapi"
	"golang.org/x/time/rate"

	"github.com/justinm35/lazynotion/internal/icons"
)

const maxSearchResults = 100

// apiBase is a var so tests can point the raw client at a fixture server.
var apiBase = "https://api.notion.com/v1"

type Page struct {
	ID         string
	Title      string
	Icon       icons.Icon
	Cover      string
	URL        string
	LastEdited time.Time
}

type Client struct {
	api     *notionapi.Client
	token   string
	limiter *rate.Limiter
}

func NewClient(token string) *Client {
	return &Client{
		api:   notionapi.NewClient(notionapi.Token(token)),
		token: token,
		// Notion allows short bursts above its 3 req/s average
		limiter: rate.NewLimiter(rate.Limit(3), 6),
	}
}

// rawRequest performs a direct API call for the endpoints where the client
// library loses information (search icons) or lacks support (restore).
func (c *Client) rawRequest(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return nil, err
	}
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, apiBase+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Notion-Version", "2022-06-28")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("notion api: %s: %.300s", resp.Status, data)
	}
	return data, nil
}

type rawPage struct {
	Object         string          `json:"object"`
	ID             string          `json:"id"`
	URL            string          `json:"url"`
	LastEditedTime time.Time       `json:"last_edited_time"`
	Icon           json.RawMessage `json:"icon"`
	Cover          *struct {
		External *struct {
			URL string `json:"url"`
		} `json:"external"`
		File *struct {
			URL string `json:"url"`
		} `json:"file"`
	} `json:"cover"`
	Parent struct {
		Type string `json:"type"`
	} `json:"parent"`
	Properties map[string]struct {
		Type  string `json:"type"`
		Title []struct {
			PlainText string `json:"plain_text"`
		} `json:"title"`
	} `json:"properties"`
}

func (p rawPage) coverURL() string {
	if p.Cover == nil {
		return ""
	}
	if p.Cover.External != nil {
		return p.Cover.External.URL
	}
	if p.Cover.File != nil {
		return p.Cover.File.URL
	}
	return ""
}

func (p rawPage) title() string {
	for _, prop := range p.Properties {
		if prop.Type != "title" {
			continue
		}
		var b strings.Builder
		for _, t := range prop.Title {
			b.WriteString(t.PlainText)
		}
		if s := strings.TrimSpace(b.String()); s != "" {
			return s
		}
	}
	return "Untitled"
}

type rawSearchResponse struct {
	Results    []rawPage `json:"results"`
	HasMore    bool      `json:"has_more"`
	NextCursor string    `json:"next_cursor"`
}

func (c *Client) rawSearch(ctx context.Context, query, cursor string, pageSize int) (*rawSearchResponse, error) {
	body, err := json.Marshal(map[string]any{
		"query":        query,
		"page_size":    pageSize,
		"filter":       map[string]string{"value": "page", "property": "object"},
		"sort":         map[string]string{"direction": "descending", "timestamp": "last_edited_time"},
		"start_cursor": cursor,
	})
	if err != nil {
		return nil, err
	}
	if cursor == "" {
		// the API rejects an empty start_cursor
		body, _ = json.Marshal(map[string]any{
			"query":     query,
			"page_size": pageSize,
			"filter":    map[string]string{"value": "page", "property": "object"},
			"sort":      map[string]string{"direction": "descending", "timestamp": "last_edited_time"},
		})
	}
	data, err := c.rawRequest(ctx, http.MethodPost, "/search", body)
	if err != nil {
		return nil, err
	}
	var resp rawSearchResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// PageLastEdited fetches just the page's metadata — one request, used to
// decide whether a disk-cached copy is still fresh.
func (c *Client) PageLastEdited(ctx context.Context, pageID string) (time.Time, error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return time.Time{}, err
	}
	page, err := c.api.Page.Get(ctx, notionapi.PageID(pageID))
	if err != nil {
		return time.Time{}, err
	}
	return page.LastEditedTime, nil
}

// maxSearchScan bounds how many pages a root-only search reads through
// looking for workspace-level pages before giving up.
const maxSearchScan = 500

// Search goes through the raw API rather than the client library: the
// library drops icon payload shapes it doesn't know (Notion's newer
// "icon" type), which made icons invisible. With rootOnly, pages nested
// under other pages are filtered out — the API cannot filter by parent,
// so the filter is applied while paginating.
func (c *Client) Search(ctx context.Context, query string, rootOnly bool) ([]Page, error) {
	var pages []Page
	cursor := ""
	scanned := 0
	for {
		resp, err := c.rawSearch(ctx, query, cursor, 50)
		if err != nil {
			return nil, err
		}
		for _, p := range resp.Results {
			scanned++
			if p.Object != "page" {
				continue
			}
			if rootOnly && p.Parent.Type != "workspace" {
				continue
			}
			pages = append(pages, Page{
				ID:         p.ID,
				Title:      p.title(),
				Icon:       icons.FromRaw(p.Icon),
				Cover:      p.coverURL(),
				URL:        p.URL,
				LastEdited: p.LastEditedTime,
			})
		}
		if !resp.HasMore || len(pages) >= maxSearchResults || scanned >= maxSearchScan {
			break
		}
		cursor = resp.NextCursor
	}
	return pages, nil
}
