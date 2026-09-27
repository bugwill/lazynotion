package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/justinm35/lazynotion/internal/cache"
	"github.com/justinm35/lazynotion/internal/notion"
)

// Shared by Model copies: only one Recent refresh runs at a time. The UI has
// exactly one consumer of its stream, even when several commands join the job.
type recentLoader struct {
	mu           sync.Mutex
	job          *recentJob
	indexes      map[string]notion.RecentIndex
	sequence     uint64
	deepRequests map[string]bool
}

type recentJob struct {
	key      string
	gen      int
	ch       chan tea.Msg
	cancel   context.CancelFunc
	latest   []notion.Page
	sequence uint64
	updates  map[string]notion.Page
}

func recentIndexKey(client *notion.Client) string { return "recent-index-v1:" + client.CacheKey() }

func (loader *recentLoader) cancel() {
	loader.mu.Lock()
	defer loader.mu.Unlock()
	if loader.job != nil {
		loader.job.cancel()
		loader.job = nil
	}
}

func (loader *recentLoader) load(client *notion.Client, store *cache.Store, gen int, force bool) tea.Msg {
	loader.mu.Lock()
	defer loader.mu.Unlock()
	key := recentIndexKey(client)
	if loader.indexes == nil {
		loader.indexes = make(map[string]notion.RecentIndex)
	}
	index, ok := loader.indexes[key]
	if !ok {
		if store != nil {
			store.LoadMetadata(key, &index)
			if index.Pages == nil {
				// Migrate the old snapshot without treating it as a successful sync.
				if pages, hit := store.LoadPages("recent-v2:" + client.CacheKey()); hit {
					index.Pages = make(map[string]notion.Page)
					for _, p := range pages {
						index.Pages[p.ID] = p
					}
				}
			}
		}
		loader.indexes[key] = index
	}
	if !force && loader.job != nil && loader.job.key == key && loader.job.gen == gen {
		return recentMsg{pages: loader.job.latest, wsGen: gen, fromCache: true, refresh: true}
	}
	if !force && index.Pages != nil {
		pages := index.Top()
		refresh := !index.Fresh(time.Now())
		return recentMsg{pages: pages, wsGen: gen, fromCache: true, refresh: refresh}
	}
	if job := loader.job; job != nil && job.key == key && job.gen == gen {
		return queryEvent{kind: "recent", gen: gen, stream: job.ch, start: true, cancel: job.cancel, sequence: job.sequence, payload: notion.QueryProgress{Stage: "search"}}
	}
	if loader.job != nil {
		loader.job.cancel()
	}
	// Refresh mutates maps; detach from the immutable snapshot used by cache
	// readers and from any canceled worker which has not yet returned.
	data, _ := json.Marshal(index)
	var working notion.RecentIndex
	_ = json.Unmarshal(data, &working)
	deep := loader.deepRequests[key]
	delete(loader.deepRequests, key)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	loader.sequence++
	job := &recentJob{sequence: loader.sequence, updates: make(map[string]notion.Page), key: key, gen: gen, ch: make(chan tea.Msg, 32), cancel: cancel, latest: index.Top()}
	loader.job = job
	go func() {
		defer cancel()
		defer close(job.ch)
		send := func(msg tea.Msg) {
			select {
			case job.ch <- msg:
			case <-ctx.Done():
			}
		}
		lastSaved := time.Time{}
		result, err := client.RefreshRecent(ctx, working,
			func(p notion.QueryProgress) { send(p) },
			func(snapshot notion.RecentIndex) {
				pages := snapshot.Top()
				loader.mu.Lock()
				job.latest = pages
				if loader.job == job && ctx.Err() == nil && time.Since(lastSaved) >= 2*time.Second {
					// The producer pauses here; detach maps before publishing a durable
					// checkpoint to readers. Interrupted discovery resumes from this queue.
					data, _ := json.Marshal(snapshot)
					var saved notion.RecentIndex
					_ = json.Unmarshal(data, &saved)
					for id, p := range job.updates {
						if saved.Pages == nil {
							saved.Pages = make(map[string]notion.Page)
						}
						saved.Pages[id] = p
						saved.RefreshedAt = time.Time{}
					}
					loader.indexes[key] = saved
					if store != nil {
						_ = store.SaveMetadata(key, saved)
					}
					lastSaved = time.Now()
				}
				loader.mu.Unlock()
				send(recentMsg{pages: pages, wsGen: gen, partial: true})
			}, deep)
		loader.mu.Lock()
		if loader.job == job {
			for id, p := range job.updates {
				if result.Pages == nil {
					result.Pages = make(map[string]notion.Page)
				}
				result.Pages[id] = p
				result.RefreshedAt = time.Time{}
			}
			// Partial successes are durable; their old freshness time causes a retry.
			if ctx.Err() != context.Canceled {
				loader.indexes[key] = result
				if store != nil {
					if saveErr := store.SaveMetadata(key, result); saveErr != nil {
						if err == nil {
							err = fmt.Errorf("Recent cache: %w", saveErr)
						}
					}
				}
			}
			loader.job = nil
		}
		loader.mu.Unlock()
		job.ch <- recentMsg{pages: result.Top(), wsGen: gen, err: err}
	}()
	return queryEvent{kind: "recent", gen: gen, stream: job.ch, start: true, cancel: cancel, sequence: job.sequence, payload: notion.QueryProgress{Stage: "search"}}
}

// remember incorporates metadata learned while browsing/writing without
// waiting for the next inventory scan. Writes invalidate snapshot freshness.
func (loader *recentLoader) remember(client *notion.Client, store *cache.Store, page notion.Page, edited bool) {
	if client == nil || page.ID == "" || page.ID == recentID {
		return
	}
	loader.mu.Lock()
	defer loader.mu.Unlock()
	key := recentIndexKey(client)
	if loader.indexes == nil {
		loader.indexes = make(map[string]notion.RecentIndex)
	}
	index, ok := loader.indexes[key]
	if !ok && store != nil {
		store.LoadMetadata(key, &index)
	}
	if index.Pages == nil {
		index.Pages = make(map[string]notion.Page)
	}
	if edited {
		page.LastEdited = time.Now().UTC()
		index.RefreshedAt = time.Time{}
	}
	old, exists := index.Pages[page.ID]
	if exists && page.ParentID == "" {
		page.ParentID, page.ParentType = old.ParentID, old.ParentType
	}
	if !exists || edited || page.LastEdited.After(old.LastEdited) {
		index.Pages[page.ID] = page
		loader.indexes[key] = index
		if job := loader.job; job != nil && job.key == key {
			job.updates[page.ID] = page
		}
		if store != nil {
			_ = store.SaveMetadata(key, index)
		}
	}
}

func (loader *recentLoader) requestDeep(client *notion.Client, store *cache.Store) {
	loader.mu.Lock()
	defer loader.mu.Unlock()
	key := recentIndexKey(client)
	if loader.indexes == nil {
		loader.indexes = make(map[string]notion.RecentIndex)
	}
	index, ok := loader.indexes[key]
	if !ok && store != nil {
		store.LoadMetadata(key, &index)
	}
	if loader.deepRequests == nil {
		loader.deepRequests = make(map[string]bool)
	}
	loader.deepRequests[key] = true
	index.DeepSyncedAt, index.DeepAttemptedAt, index.DeepRetryAt = time.Time{}, time.Time{}, time.Time{}
	loader.indexes[key] = index
	if loader.job != nil {
		loader.job.cancel()
		loader.job = nil
	}
}
