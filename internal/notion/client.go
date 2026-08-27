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

// PageKind distinguishes what a sidebar entry opens as.
type PageKind string

const (
	KindPage       PageKind = ""            // a regular page
	KindDataSource PageKind = "data_source" // a database's data source (table)
)

type Page struct {
	ID         string
	Title      string
	Icon       icons.Icon
	Cover      string
	URL        string
	LastEdited time.Time
	Kind       PageKind
	// DatabaseID is the containing database for data-source entries.
	DatabaseID string
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

// API versions in play: pages/blocks stay on the version the client library
// speaks; data sources (databases) only exist from 2025-09-03 onward. The
// header is per-request, so the two coexist.
const (
	versionBlocks      = "2022-06-28"
	versionDataSources = "2025-09-03"
)

// rawRequest performs a direct API call for the endpoints where the client
// library loses information (search icons) or lacks support (restore).
func (c *Client) rawRequest(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	return c.rawRequestV(ctx, method, path, body, versionBlocks)
}

// rawRequestV is rawRequest with an explicit Notion-Version header.
func (c *Client) rawRequestV(ctx context.Context, method, path string, body []byte, version string) ([]byte, error) {
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
	req.Header.Set("Notion-Version", version)
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
		Type       string `json:"type"`
		DatabaseID string `json:"database_id"`
	} `json:"parent"`
	// TitleRT is the top-level title array data sources carry (pages keep
	// theirs inside properties).
	TitleRT    []rawRichText `json:"title"`
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
	if s := strings.TrimSpace(joinPlain(p.TitleRT)); s != "" {
		return s
	}
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

// rawSearch runs unfiltered under the data-sources API version, so one
// request returns pages and data sources (databases) together.
func (c *Client) rawSearch(ctx context.Context, query, cursor string, pageSize int) (*rawSearchResponse, error) {
	req := map[string]any{
		"query":     query,
		"page_size": pageSize,
		"sort":      map[string]string{"direction": "descending", "timestamp": "last_edited_time"},
	}
	if cursor != "" {
		// the API rejects an empty start_cursor
		req["start_cursor"] = cursor
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	data, err := c.rawRequestV(ctx, http.MethodPost, "/search", body, versionDataSources)
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
			page := Page{
				ID:         p.ID,
				Title:      p.title(),
				Icon:       icons.FromRaw(p.Icon),
				Cover:      p.coverURL(),
				URL:        p.URL,
				LastEdited: p.LastEditedTime,
			}
			switch p.Object {
			case "page":
				if rootOnly && p.Parent.Type != "workspace" {
					continue
				}
			case "data_source":
				// data sources always show: their parent is the database,
				// so the workspace-root test can't apply to them
				page.Kind = KindDataSource
				page.DatabaseID = p.Parent.DatabaseID
				if page.URL == "" && page.DatabaseID != "" {
					page.URL = "https://www.notion.so/" + strings.ReplaceAll(page.DatabaseID, "-", "")
				}
			default:
				continue
			}
			pages = append(pages, page)
		}
		if !resp.HasMore || len(pages) >= maxSearchResults || scanned >= maxSearchScan {
			break
		}
		cursor = resp.NextCursor
	}
	return pages, nil
}
