package notion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"sync"
	"time"

	"github.com/justinm35/lazynotion/internal/icons"
)

const RecentCacheTTL = time.Minute
const RecentDeepInterval = 30 * time.Minute

// RecentIndex contains all discovered metadata, not just the visible top 100.
// Source checkpoints advance only after a successful query. Graph records the
// last discovered parent/child links; discovery is repeated on a slower cadence.
type RecentIndex struct {
	Pages            map[string]Page           `json:"pages"`
	Graph            map[string]RecentChildren `json:"graph"`
	SourceSyncedAt   map[string]time.Time      `json:"source_synced_at"`
	RefreshedAt      time.Time                 `json:"refreshed_at"`
	PendingPages     map[string]bool           `json:"pending_pages"`
	DiscoveryActive  bool                      `json:"discovery_active"`
	DiscoveryPending []string                  `json:"discovery_pending"`
	DiscoveryVisited map[string]bool           `json:"discovery_visited"`
	DeepRetryAt      time.Time                 `json:"deep_retry_at"`
	DeepAttemptedAt  time.Time                 `json:"deep_attempted_at"`
	DeepSyncedAt     time.Time                 `json:"deep_synced_at"`
}

type RecentChildren struct {
	Pages      []string `json:"pages"`
	Containers []string `json:"containers"`
	Databases  []string `json:"databases"`
}

func (index RecentIndex) Fresh(now time.Time) bool {
	return !index.RefreshedAt.IsZero() && now.Sub(index.RefreshedAt) >= 0 && now.Sub(index.RefreshedAt) < RecentCacheTTL
}

func (index RecentIndex) Top() []Page {
	out := make([]Page, 0, len(index.Pages))
	for _, p := range index.Pages {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].LastEdited.Equal(out[j].LastEdited) {
			return out[i].LastEdited.After(out[j].LastEdited)
		}
		return out[i].ID < out[j].ID
	})
	seen := make(map[string]bool)
	top := make([]Page, 0, min(maxSearchResults, len(out)))
	for _, p := range out {
		id := p.ID
		if p.Kind == KindDataSource && p.DatabaseID != "" {
			id = p.DatabaseID
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		top = append(top, p)
		if len(top) == maxSearchResults {
			break
		}
	}
	return top
}

func (p rawPage) recentPage() Page {
	parent := p.Parent.PageID
	if p.Parent.Type == "data_source_id" {
		parent = p.Parent.DataSourceID
	}
	if p.Parent.Type == "database_id" {
		parent = p.Parent.DatabaseID
	}
	page := Page{ID: p.ID, Title: p.title(), Icon: icons.FromRaw(p.Icon), Cover: p.coverURL(), URL: p.URL,
		LastEdited: p.LastEditedTime, ParentType: p.Parent.Type, ParentID: parent}
	if p.Object == "data_source" {
		page.Kind, page.DatabaseID = KindDataSource, p.Parent.DatabaseID
		if page.URL == "" {
			page.URL = "https://www.notion.so/" + page.DatabaseID
		}
	}
	return page
}

// RefreshRecent publishes the first search batch immediately. Deep discovery
// runs only when due; normal refreshes query data-source changes with an overlap
// and revalidate visible metadata that Search omitted. A failed stage retains
// successful work and leaves its checkpoint unchanged so the next run retries.
func (c *Client) RefreshRecent(ctx context.Context, index RecentIndex, report func(QueryProgress), publish func(RecentIndex), deepMode ...bool) (RecentIndex, error) {
	started := time.Now().UTC()
	deep := index.DiscoveryActive || index.DeepSyncedAt.IsZero() || started.Sub(index.DeepSyncedAt) >= RecentDeepInterval
	if len(deepMode) > 0 {
		deep = deepMode[0]
	}
	if deep && len(deepMode) == 0 && started.Before(index.DeepRetryAt) {
		deep = false
	}
	if deep {
		index.DeepAttemptedAt = started
	}
	if index.Pages == nil {
		index.Pages = make(map[string]Page)
	}
	if index.PendingPages == nil {
		index.PendingPages = make(map[string]bool)
	}
	if index.Graph == nil {
		index.Graph = make(map[string]RecentChildren)
	}
	if index.SourceSyncedAt == nil {
		index.SourceSyncedAt = make(map[string]time.Time)
	}
	emit := func() {
		if publish != nil {
			publish(index)
		}
	}
	var problems []error
	var problemMu sync.Mutex
	problem := func(err error) {
		if err != nil {
			problemMu.Lock()
			problems = append(problems, err)
			problemMu.Unlock()
		}
	}
	tasks := func(stage string, count int, run func(context.Context, int) error) {
		problem(progressTasks(ctx, count, stage, report, func(ctx context.Context, i int) error {
			problem(run(ctx, i))
			return ctx.Err()
		}))
	}
	seenSearch := make(map[string]bool)
	cursor := ""
	found := 0
	if report != nil {
		report(QueryProgress{Stage: "search"})
	}
	for {
		resp, err := c.rawSearch(ctx, "", cursor, 100)
		if err != nil {
			problem(err)
			break
		}
		for _, raw := range resp.Results {
			if raw.Object != "page" && raw.Object != "data_source" {
				continue
			}
			if raw.Archived || raw.InTrash {
				delete(index.Pages, raw.ID)
				continue
			}
			p := raw.recentPage()
			if p.Kind == KindDataSource {
				if old, ok := index.Pages[p.ID]; ok {
					p.LastEdited, p.Title = old.LastEdited, old.Title
				}
			}
			index.Pages[p.ID] = p
			seenSearch[p.ID] = true
			found++
		}
		emit()
		if report != nil {
			report(QueryProgress{Stage: "search", Done: found})
		}
		if !deep || !resp.HasMore {
			break
		}
		if resp.NextCursor == "" || resp.NextCursor == cursor {
			problem(fmt.Errorf("notion returned an invalid search cursor"))
			break
		}
		cursor = resp.NextCursor
	}
	if ctx.Err() != nil {
		return index, ctx.Err()
	}

	var mergeMu sync.Mutex
	// Called under mergeMu when a source has disappeared or access was revoked.
	removeSource := func(id string) {
		delete(index.Pages, id)
		delete(index.SourceSyncedAt, id)
		for key, p := range index.Pages {
			if p.ParentType == "data_source_id" && p.ParentID == id {
				delete(index.Pages, key)
			}
		}
	}
	fetch := func(ctx context.Context, id string) (fetchErr error) {
		defer func() {
			if fetchErr != nil {
				mergeMu.Lock()
				index.PendingPages[id] = true
				mergeMu.Unlock()
			}
		}()
		data, err := c.rawRequest(ctx, http.MethodGet, "/pages/"+url.PathEscape(id), nil)
		if err != nil {
			if inaccessible(err) {
				mergeMu.Lock()
				delete(index.Pages, id)
				delete(index.PendingPages, id)
				mergeMu.Unlock()
				return nil
			}
			return err
		}
		var raw rawPage
		if err := json.Unmarshal(data, &raw); err != nil {
			return err
		}
		if raw.ID == "" {
			return fmt.Errorf("notion returned page metadata without an ID")
		}
		mergeMu.Lock()
		defer mergeMu.Unlock()
		delete(index.PendingPages, id)
		if raw.Archived || raw.InTrash {
			delete(index.Pages, id)
		} else {
			index.Pages[id] = raw.recentPage()
		}
		return nil
	}

	// Fetch each containing database once, even with several data sources.
	dbDone := make(map[string]bool)
	hydrate := func(ids []string) {
		var pending []string
		for _, id := range ids {
			if id != "" && !dbDone[id] {
				dbDone[id] = true
				pending = append(pending, id)
			}
		}
		tasks("databases", len(pending), func(ctx context.Context, i int) error {
			id := pending[i]
			db, err := c.GetDatabase(ctx, id)
			if err != nil {
				if inaccessible(err) {
					mergeMu.Lock()
					for key, p := range index.Pages {
						if p.DatabaseID == id {
							removeSource(key)
						}
					}
					mergeMu.Unlock()
					return nil
				}
				return err
			}
			mergeMu.Lock()
			defer mergeMu.Unlock()
			live := make(map[string]bool)
			for _, ds := range db.DataSources {
				live[ds.ID] = true
			}
			for key, p := range index.Pages {
				if p.Kind == KindDataSource && p.DatabaseID == id && !live[key] {
					removeSource(key)
				}
			}
			for _, ds := range db.DataSources {
				p := index.Pages[ds.ID]
				p.ID, p.Kind, p.DatabaseID = ds.ID, KindDataSource, id
				p.Title, p.LastEdited = db.Title, db.LastEdited
				if p.URL == "" {
					p.URL = "https://www.notion.so/" + id
				}
				index.Pages[p.ID] = p
			}
			for key, p := range index.Pages {
				if p.DatabaseID == id {
					p.Title, p.LastEdited = db.Title, db.LastEdited
					index.Pages[key] = p
				}
			}
			return nil
		})
		emit()
	}
	var dbids []string
	for _, p := range index.Pages {
		if p.Kind == KindDataSource {
			dbids = append(dbids, p.DatabaseID)
		}
	}
	for _, children := range index.Graph {
		dbids = append(dbids, children.Databases...)
	}
	sort.Strings(dbids)
	hydrate(dbids)

	rowsDone := make(map[string]bool)
	syncRows := func() {
		var sources []string
		for _, p := range index.Pages {
			if p.Kind == KindDataSource && !rowsDone[p.ID] {
				rowsDone[p.ID] = true
				sources = append(sources, p.ID)
			}
		}
		sort.Strings(sources)
		checkpoints := make(map[string]time.Time, len(sources))
		for _, id := range sources {
			checkpoints[id] = index.SourceSyncedAt[id]
		}
		tasks("rows", len(sources), func(ctx context.Context, i int) error {
			id := sources[i]
			since := checkpoints[id]
			if deep {
				since = time.Time{}
			}
			rows, err := c.recentRows(ctx, id, since)
			mergeMu.Lock()
			defer mergeMu.Unlock()
			if err != nil && inaccessible(err) {
				removeSource(id)
				return nil
			}
			for _, p := range rows {
				index.Pages[p.ID] = p
				seenSearch[p.ID] = true
			}
			if err == nil {
				index.SourceSyncedAt[id] = started
			}
			return err
		})
		emit()
	}

	syncRows()

	// Validate visible entries Search omitted. Database rows are checked during
	// deep sync, since the incremental endpoint cannot report deletions.
	var check []string
	checkSet := make(map[string]bool)
	pendingBefore := make(map[string]bool)
	for id := range index.PendingPages {
		if !deep {
			continue
		}
		check = append(check, id)
		checkSet[id] = true
		pendingBefore[id] = true
	}
	for _, p := range index.Top() {
		isRow := p.ParentType == "data_source_id" || p.ParentType == "database_id"
		if p.Kind != KindDataSource && !seenSearch[p.ID] && (deep || !isRow) && !checkSet[p.ID] {
			check = append(check, p.ID)
		}
	}
	sort.Strings(check)
	tasks("pages", len(check), func(ctx context.Context, i int) error { return fetch(ctx, check[i]) })
	if deep && index.DiscoveryActive {
		for id := range pendingBefore {
			if p, ok := index.Pages[id]; ok && (p.ParentType == "" || p.ParentType == "workspace" || p.ParentType == "page_id") {
				index.DiscoveryPending = append(index.DiscoveryPending, id)
			}
		}
	}
	emit()

	if len(problems) == 0 {
		index.RefreshedAt = time.Now().UTC()
	}
	if deep {
		queue := index.DiscoveryPending
		if !index.DiscoveryActive {
			index.DiscoveryVisited = make(map[string]bool)
			for _, p := range index.Pages {
				if p.Kind == KindPage && (p.ParentType == "workspace" || p.ParentType == "page_id") {
					queue = append(queue, p.ID)
				}
			}
			sort.Slice(queue, func(i, j int) bool {
				rootI, rootJ := index.Pages[queue[i]].ParentType == "workspace", index.Pages[queue[j]].ParentType == "workspace"
				if rootI != rootJ {
					return rootI
				}
				return queue[i] < queue[j]
			})
		}
		index.DiscoveryActive = true
		if index.DiscoveryVisited == nil {
			index.DiscoveryVisited = make(map[string]bool)
		}
		visited := index.DiscoveryVisited
		var retry []string
		index.DiscoveryPending = queue
		for len(queue) > 0 && ctx.Err() == nil {
			var batch, pending []string
			for _, id := range queue {
				if visited[id] {
					continue
				}
				if len(batch) < recentConcurrency*4 {
					visited[id] = true
					batch = append(batch, id)
				} else {
					pending = append(pending, id)
				}
			}
			queue = pending
			children := make([]RecentChildren, len(batch))
			scanned := make([]bool, len(batch))
			tasks("checking", len(batch), func(ctx context.Context, i int) error {
				refs, containers, databases, err := c.recentChildren(ctx, batch[i])
				if err == nil {
					children[i] = RecentChildren{Pages: refs, Containers: containers, Databases: databases}
					scanned[i] = true
				}
				return err
			})
			missingSet := make(map[string]bool)
			var missing, childDBs []string
			for i, id := range batch {
				if !scanned[i] {
					delete(visited, id)
					retry = append(retry, id)
					continue
				}
				child := children[i]
				index.Graph[id] = child
				queue = append(queue, child.Containers...)
				childDBs = append(childDBs, child.Databases...)
				for _, ref := range child.Pages {
					if _, ok := index.Pages[ref]; !ok && !missingSet[ref] {
						missingSet[ref] = true
						missing = append(missing, ref)
					}
					queue = append(queue, ref)
				}
			}
			tasks("pages", len(missing), func(ctx context.Context, i int) error { return fetch(ctx, missing[i]) })
			// Only recurse page references which we can actually retrieve. Container
			// blocks remain in the queue; database rows are never scanned for bodies.
			filtered := queue[:0]
			for _, id := range queue {
				p, ok := index.Pages[id]
				if ok && p.Kind == KindPage && (p.ParentType == "" || p.ParentType == "page_id" || p.ParentType == "workspace") || !ok && !missingSet[id] {
					filtered = append(filtered, id)
				}
			}
			queue = filtered
			index.DiscoveryPending = append(append([]string{}, queue...), retry...)
			hydrate(childDBs)
			syncRows()
			emit()
		}
		index.DiscoveryPending = append(append([]string{}, queue...), retry...)
		if len(index.DiscoveryPending) == 0 && len(index.PendingPages) == 0 {
			index.DiscoveryActive = false
			index.DiscoveryVisited = nil
		}
		if !index.DiscoveryActive {
			index.DeepSyncedAt = time.Now().UTC()
		}
		if len(problems) > 0 || ctx.Err() != nil {
			index.DeepRetryAt = time.Now().Add(5 * time.Minute)
		}
	}
	if err := ctx.Err(); err != nil {
		return index, err
	}
	if len(problems) == 0 {
		index.RefreshedAt = time.Now().UTC()
		if deep && !index.DiscoveryActive {
			index.DeepSyncedAt = index.RefreshedAt
			index.DeepRetryAt = time.Time{}
		}
	}
	return index, errors.Join(problems...)
}

func (c *Client) recentRows(ctx context.Context, source string, since time.Time) ([]Page, error) {
	return c.recentRowsFrom(ctx, source, since, 2*time.Minute)
}

func (c *Client) recentRowsFrom(ctx context.Context, source string, since time.Time, overlap time.Duration, limits ...int) ([]Page, error) {
	var pages []Page
	cursor := ""
	for {
		req := map[string]any{"page_size": 100, "sorts": []any{map[string]string{"timestamp": "last_edited_time", "direction": "descending"}}}
		if !since.IsZero() {
			// Overlap accommodates timestamp precision and changes during pagination.
			req["filter"] = map[string]any{"timestamp": "last_edited_time", "last_edited_time": map[string]string{"on_or_after": since.Add(-overlap).Format(time.RFC3339Nano)}}
		}
		if cursor != "" {
			req["start_cursor"] = cursor
		}
		body, _ := json.Marshal(req)
		data, err := c.rawRequestV(ctx, http.MethodPost, "/data_sources/"+url.PathEscape(source)+"/query", body, versionDataSources)
		if err != nil {
			return pages, err
		}
		var resp rawSearchResponse
		if err := json.Unmarshal(data, &resp); err != nil {
			return pages, err
		}
		for _, raw := range resp.Results {
			if !raw.Archived && !raw.InTrash {
				p := raw.recentPage()
				p.ParentType, p.ParentID = "data_source_id", source
				pages = append(pages, p)
			}
		}
		// The newest 100 from each source suffice for the global top 100. On an
		// incremental run all changed rows must be consumed before advancing time.
		if len(limits) > 0 && limits[0] > 0 && len(pages) >= limits[0] {
			return pages[:limits[0]], nil
		}
		if !resp.HasMore || since.IsZero() && len(pages) >= maxSearchResults {
			return pages, nil
		}
		if resp.NextCursor == "" || resp.NextCursor == cursor {
			return pages, fmt.Errorf("notion returned an invalid data source cursor")
		}
		cursor = resp.NextCursor
	}
}

func inaccessible(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && (apiErr.Status == http.StatusNotFound || apiErr.Status == http.StatusForbidden && apiErr.Code == "restricted_resource")
}
