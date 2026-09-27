package notion

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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
	ParentID   string
	ParentType string
	// DatabaseID is the containing database for data-source entries.
	DatabaseID string
}

type Client struct {
	api         *notionapi.Client
	token       string
	limiter     *rate.Limiter
	retryMu     sync.Mutex
	retryAt     time.Time
	rawRequests atomic.Uint64
}

// RawRequestCount counts HTTP attempts, including retries, for diagnostics.
func (c *Client) RawRequestCount() uint64 { return c.rawRequests.Load() }

// CacheKey scopes persisted metadata to the integration without storing its token.
func (c *Client) CacheKey() string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(c.token)))
}

func NewClient(token string) *Client {
	return &Client{
		api:   notionapi.NewClient(notionapi.Token(token)),
		token: token,
		// Conservative default for all plans; workers share this limiter.
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
// APIError keeps structured errors for retry and permission decisions.
type APIError struct {
	Status  int
	Code    string
	Reason  string
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("notion api: %d %s: %s", e.Status, http.StatusText(e.Status), e.Message)
}

func waitUntil(ctx context.Context, until time.Time) error {
	delay := time.Until(until)
	if delay <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// All SDK and raw requests share the pause established by a rate-limit reply.
func (c *Client) waitRequest(ctx context.Context) error {
	if err := c.limiter.Wait(ctx); err != nil {
		return err
	}
	for {
		c.retryMu.Lock()
		until := c.retryAt
		c.retryMu.Unlock()
		if time.Until(until) <= 0 {
			return ctx.Err()
		}
		if err := waitUntil(ctx, until); err != nil {
			return err
		}
	}
}

func (c *Client) rawRequestV(ctx context.Context, method, path string, body []byte, version string) ([]byte, error) {
	for attempt := 0; ; attempt++ {
		if err := c.waitRequest(ctx); err != nil {
			return nil, err
		}
		data, header, err := c.rawAttempt(ctx, method, path, body, version)
		apiErr, ok := err.(*APIError)
		if !ok || attempt >= 3 {
			return data, err
		}
		readOnly := method == http.MethodGet || method == http.MethodPost && (path == "/search" || strings.HasSuffix(path, "/query"))
		retry := apiErr.Status == 429 && apiErr.Reason != "public_api_request_blocked" || apiErr.Status == 529 || readOnly && (apiErr.Status == 500 || apiErr.Status == 502 || apiErr.Status == 503 || apiErr.Status == 504)
		if !retry {
			return data, err
		}
		delay := time.Second * time.Duration(1<<attempt)
		if seconds, parseErr := strconv.Atoi(header.Get("Retry-After")); parseErr == nil && seconds >= 0 {
			delay = time.Duration(seconds) * time.Second
		}
		// One shared pause prevents each worker from retrying independently. Small
		// jitter spreads retries without changing the server's minimum wait.
		until := time.Now().Add(delay + time.Duration(rand.IntN(250))*time.Millisecond)
		c.retryMu.Lock()
		if until.After(c.retryAt) {
			c.retryAt = until
		}
		c.retryMu.Unlock()
	}
}

func (c *Client) rawAttempt(ctx context.Context, method, path string, body []byte, version string) ([]byte, http.Header, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, apiBase+path, reader)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Notion-Version", version)
	req.Header.Set("Content-Type", "application/json")
	c.rawRequests.Add(1)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, resp.Header, err
	}
	if resp.StatusCode != http.StatusOK {
		var detail struct {
			Code       string `json:"code"`
			Additional struct {
				Reason string `json:"rate_limit_reason"`
			} `json:"additional_data"`
		}
		_ = json.Unmarshal(data, &detail)
		return nil, resp.Header, &APIError{Status: resp.StatusCode, Code: detail.Code, Reason: detail.Additional.Reason, Message: fmt.Sprintf("%.300s", data)}
	}
	return data, resp.Header, nil
}

type rawPage struct {
	Archived       bool            `json:"archived"`
	InTrash        bool            `json:"in_trash"`
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
		Type         string `json:"type"`
		DatabaseID   string `json:"database_id"`
		PageID       string `json:"page_id"`
		DataSourceID string `json:"data_source_id"`
	} `json:"parent"`
	// TitleRT is the top-level title array data sources carry (pages keep
	// theirs inside properties).
	TitleRT    []rawRichText `json:"title"`
	Properties map[string]struct {
		Type string `json:"type"`
		// Pages carry rich-text arrays; data-source schemas carry {}.
		Title json.RawMessage `json:"title"`
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
		var title []struct {
			PlainText string `json:"plain_text"`
		}
		if err := json.Unmarshal(prop.Title, &title); err != nil {
			continue
		}
		var b strings.Builder
		for _, t := range title {
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
func (c *Client) rawSearch(ctx context.Context, query, cursor string, pageSize int, object ...string) (*rawSearchResponse, error) {
	req := map[string]any{
		"query":     query,
		"page_size": pageSize,
		"sort":      map[string]string{"direction": "descending", "timestamp": "last_edited_time"},
	}
	if len(object) > 0 && object[0] != "" {
		req["filter"] = map[string]string{"property": "object", "value": object[0]}
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
	if err := c.waitRequest(ctx); err != nil {
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
	return c.search(ctx, query, rootOnly, true)
}

func (c *Client) SearchWithProgress(ctx context.Context, query string, rootOnly bool, report func(QueryProgress)) ([]Page, error) {
	return c.search(ctx, query, rootOnly, true, report)
}

func (c *Client) search(ctx context.Context, query string, rootOnly, bounded bool, reports ...func(QueryProgress)) ([]Page, error) {
	var report func(QueryProgress)
	if len(reports) > 0 {
		report = reports[0]
	}
	if report != nil {
		report(QueryProgress{Stage: "search"})
	}
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
				ParentID:   p.Parent.PageID,
				ParentType: p.Parent.Type,
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
		if report != nil {
			report(QueryProgress{Stage: "search", Done: len(pages)})
		}
		if !resp.HasMore || (bounded && (len(pages) >= maxSearchResults || scanned >= maxSearchScan)) {
			break
		}
		cursor = resp.NextCursor
	}
	return pages, nil
}
