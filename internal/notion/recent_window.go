package notion

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// WindowReport extracts metadata only; it never traverses page/block bodies.
// Search is index-backed, so the ordinary-page list cannot be exhaustive.
type WindowReport struct {
	Pages                []Page         `json:"pages"`
	Since                time.Time      `json:"since"`
	Until                time.Time      `json:"until"`
	OrdinaryPages        []Page         `json:"ordinary_pages"`
	DatabaseRows         []Page         `json:"database_rows"`
	Sources              []WindowSource `json:"sources"`
	SearchBatches        int            `json:"search_batches"`
	SourceBatches        int            `json:"source_batches"`
	SearchObjects        int            `json:"search_objects_read"`
	SearchRows           int            `json:"database_rows_in_search"`
	RowsAbsentFromSearch int            `json:"database_rows_absent_from_search"`
	StoppedAtCutoff      bool           `json:"search_stopped_at_cutoff"`
	HTTPRequests         uint64         `json:"http_requests"`
	ElapsedSeconds       float64        `json:"elapsed_seconds"`
	Errors               []string       `json:"errors,omitempty"`
}

type WindowSource struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	DatabaseID string `json:"database_id"`
	Rows       int    `json:"rows"`
	CachedOnly bool   `json:"cached_only"`
	Error      string `json:"error,omitempty"`
}

func sortWindowPages(pages map[string]Page) []Page {
	out := make([]Page, 0, len(pages))
	for _, page := range pages {
		out = append(out, page)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].LastEdited.Equal(out[j].LastEdited) {
			return out[i].LastEdited.After(out[j].LastEdited)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func (c *Client) RecentWindow(ctx context.Context, since, until time.Time, cached []Page, report func(QueryProgress)) WindowReport {
	started := time.Now()
	requestsBefore := c.RawRequestCount()
	result := WindowReport{Since: since, Until: until}
	ordinary := make(map[string]Page)
	searchRows := make(map[string]bool)
	rows := make(map[string]Page)
	inWindow := func(p Page) bool { return !p.LastEdited.Before(since) && !p.LastEdited.After(until) }
	progress := func(stage string, done int) {
		if report != nil {
			report(QueryProgress{Stage: stage, Done: done})
		}
	}
	addErr := func(err error) {
		if err != nil {
			result.Errors = append(result.Errors, err.Error())
		}
	}

	// Search can sort by edit time but cannot filter by a date. Stop as soon as
	// the descending stream crosses the boundary; don't enumerate old pages.
	cursor := ""
	cursors := make(map[string]bool)
	for ctx.Err() == nil {
		batch, err := c.rawSearch(ctx, "", cursor, 100, "page")
		result.SearchBatches++
		if err != nil {
			addErr(err)
			break
		}
		old := false
		for _, raw := range batch.Results {
			result.SearchObjects++
			if raw.LastEditedTime.Before(since) {
				old = true
				continue
			}
			if raw.Object != "page" || raw.Archived || raw.InTrash {
				continue
			}
			p := raw.recentPage()
			if !inWindow(p) {
				continue
			}
			if p.ParentType == "data_source_id" || p.ParentType == "database_id" {
				searchRows[p.ID] = true
				rows[p.ID] = p
			} else {
				ordinary[p.ID] = p
			}
		}
		progress("search", len(ordinary)+len(searchRows))
		if old {
			result.StoppedAtCutoff = true
			break
		}
		if len(ordinary)+len(searchRows) >= 100 || !batch.HasMore {
			break
		}
		if batch.NextCursor == "" || cursors[batch.NextCursor] {
			addErr(fmt.Errorf("invalid search cursor"))
			break
		}
		cursors[batch.NextCursor] = true
		cursor = batch.NextCursor
	}
	result.SearchRows = len(searchRows)

	// An old data-source schema can have recently edited rows. Enumerate the
	// small source directory without the date cutoff; supplement with the local
	// index so previously discovered unindexed sources are also queried.
	sources := make(map[string]Page)
	indexed := make(map[string]bool)
	for _, p := range cached {
		if p.Kind == KindDataSource {
			sources[p.ID] = p
		}
	}
	cursor = ""
	cursors = make(map[string]bool)
	for ctx.Err() == nil {
		batch, err := c.rawSearch(ctx, "", cursor, 100, "data_source")
		result.SourceBatches++
		if err != nil {
			addErr(err)
			break
		}
		for _, raw := range batch.Results {
			if raw.Object == "data_source" && !raw.Archived && !raw.InTrash {
				p := raw.recentPage()
				sources[p.ID] = p
				indexed[p.ID] = true
			}
		}
		progress("sources", len(sources))
		if !batch.HasMore {
			break
		}
		if batch.NextCursor == "" || cursors[batch.NextCursor] {
			addErr(fmt.Errorf("invalid source cursor"))
			break
		}
		cursors[batch.NextCursor] = true
		cursor = batch.NextCursor
	}
	var ids []string
	for id := range sources {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	result.Sources = make([]WindowSource, len(ids))
	var mergeMu sync.Mutex
	err := progressTasks(ctx, len(ids), "rows", report, func(ctx context.Context, i int) error {
		id := ids[i]
		source := sources[id]
		found, queryErr := c.recentRowsFrom(ctx, id, since, 0, 100)
		detail := WindowSource{ID: id, Title: source.Title, DatabaseID: source.DatabaseID, CachedOnly: !indexed[id]}
		mergeMu.Lock()
		defer mergeMu.Unlock()
		for _, p := range found {
			if inWindow(p) {
				rows[p.ID] = p
				detail.Rows++
			}
		}
		if queryErr != nil {
			detail.Error = queryErr.Error()
			addErr(fmt.Errorf("data source %s: %w", source.Title, queryErr))
		}
		result.Sources[i] = detail
		return ctx.Err()
	})
	addErr(err)
	if ctx.Err() != nil {
		addErr(ctx.Err())
	}
	for id := range rows {
		if !searchRows[id] {
			result.RowsAbsentFromSearch++
		}
	}
	combined := make(map[string]Page)
	for id, p := range ordinary {
		combined[id] = p
	}
	for id, p := range rows {
		combined[id] = p
	}
	result.Pages = sortWindowPages(combined)
	if len(result.Pages) > 100 {
		result.Pages = result.Pages[:100]
	}
	result.OrdinaryPages = make([]Page, 0)
	result.DatabaseRows = make([]Page, 0)
	for _, p := range result.Pages {
		if _, ok := rows[p.ID]; ok {
			result.DatabaseRows = append(result.DatabaseRows, p)
		} else {
			result.OrdinaryPages = append(result.OrdinaryPages, p)
		}
	}
	result.HTTPRequests = c.RawRequestCount() - requestsBefore
	result.ElapsedSeconds = time.Since(started).Seconds()
	return result
}
