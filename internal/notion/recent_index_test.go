package notion

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

func TestRecentIndexFastRefreshAndIndependentCheckpoints(t *testing.T) {
	now := time.Now().UTC()
	previous := now.Add(-10 * time.Minute)
	index := RecentIndex{Pages: map[string]Page{
		"ds1":        {ID: "ds1", Kind: KindDataSource, DatabaseID: "db"},
		"ds2":        {ID: "ds2", Kind: KindDataSource, DatabaseID: "db"},
		"remembered": {ID: "remembered", Title: "Old", LastEdited: previous, ParentType: "page_id"},
		"deleted":    {ID: "deleted", LastEdited: previous, ParentType: "page_id"},
	}, SourceSyncedAt: map[string]time.Time{"ds1": previous, "ds2": previous}, RefreshedAt: previous, DeepSyncedAt: now}
	var mu sync.Mutex
	calls := make(map[string]int)
	var filters []time.Time
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls[r.URL.Path]++
		mu.Unlock()
		switch r.URL.Path {
		case "/search":
			var request map[string]any
			json.NewDecoder(r.Body).Decode(&request)
			if request["page_size"] != float64(100) {
				t.Errorf("search batch size: %v", request)
			}
			fmt.Fprint(w, `{"results":[{"object":"page","id":"new","parent":{"type":"workspace"}}],"has_more":true,"next_cursor":"do-not-fetch"}`)
		case "/pages/remembered":
			fmt.Fprint(w, `{"object":"page","id":"remembered","properties":{"title":{"type":"title","title":[{"plain_text":"Updated"}]}},"parent":{"type":"page_id","page_id":"root"}}`)
		case "/pages/deleted":
			fmt.Fprint(w, `{"object":"page","id":"deleted","in_trash":true}`)
		case "/databases/db":
			fmt.Fprint(w, `{"id":"db","title":[{"plain_text":"Books"}],"data_sources":[{"id":"ds1"},{"id":"ds2"}]}`)
		case "/data_sources/ds1/query", "/data_sources/ds2/query":
			var request struct {
				Cursor string `json:"start_cursor"`
				Sorts  []struct {
					Timestamp string `json:"timestamp"`
					Direction string `json:"direction"`
				} `json:"sorts"`
				Filter struct {
					Timestamp string `json:"timestamp"`
					Edited    struct {
						After time.Time `json:"on_or_after"`
					} `json:"last_edited_time"`
				} `json:"filter"`
			}
			json.NewDecoder(r.Body).Decode(&request)
			if request.Filter.Timestamp != "last_edited_time" || len(request.Sorts) != 1 || request.Sorts[0].Direction != "descending" {
				t.Errorf("invalid query: %+v", request)
			}
			mu.Lock()
			filters = append(filters, request.Filter.Edited.After)
			mu.Unlock()
			if r.URL.Path == "/data_sources/ds2/query" {
				if request.Cursor == "" {
					fmt.Fprint(w, `{"results":[{"object":"page","id":"partial-row"}],"has_more":true,"next_cursor":"broken"}`)
				} else {
					w.WriteHeader(400)
					fmt.Fprint(w, `{"code":"validation_error","message":"fixture failure"}`)
				}
			} else if request.Cursor == "" {
				fmt.Fprint(w, `{"results":[{"object":"page","id":"row1"}],"has_more":true,"next_cursor":"next"}`)
			} else {
				fmt.Fprint(w, `{"results":[{"object":"page","id":"row2"}],"has_more":false}`)
			}
		default:
			t.Errorf("fast refresh must not scan bodies or search tail: %s", r.URL)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	old := apiBase
	apiBase = srv.URL
	defer func() { apiBase = old }()
	client := NewClient("dummy")
	client.limiter.SetLimit(rate.Inf)
	first := false
	result, err := client.RefreshRecent(context.Background(), index, nil, func(snapshot RecentIndex) {
		if !first {
			first = true
			mu.Lock()
			defer mu.Unlock()
			if calls["/databases/db"] != 0 {
				t.Error("first rows waited for supplements")
			}
		}
	})
	if err == nil || !first {
		t.Fatal("expected early rows and partial failure")
	}
	if calls["/search"] != 1 || calls["/databases/db"] != 1 {
		t.Fatalf("duplicate queries: %v", calls)
	}
	if !result.RefreshedAt.Equal(previous) || !result.DeepSyncedAt.Equal(now) {
		t.Fatal("partial refresh advanced freshness")
	}
	if !result.SourceSyncedAt["ds1"].After(previous) || !result.SourceSyncedAt["ds2"].Equal(previous) {
		t.Fatal("source checkpoints were not independent")
	}
	for _, id := range []string{"new", "remembered", "row1", "row2", "partial-row"} {
		if _, ok := result.Pages[id]; !ok {
			t.Fatalf("lost successful result %s", id)
		}
	}
	if result.Pages["remembered"].Title != "Updated" {
		t.Fatal("remembered unindexed metadata was not refreshed")
	}
	if _, ok := result.Pages["deleted"]; ok {
		t.Fatal("trashed page retained")
	}
	for _, cutoff := range filters {
		if delta := cutoff.Sub(previous.Add(-2 * time.Minute)); delta < -time.Second || delta > time.Second {
			t.Fatalf("overlap incorrect: %v", cutoff)
		}
	}
}

func TestRecentIndexDiscoversDeepPagesAndUnindexedDatabases(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search":
			fmt.Fprint(w, `{"results":[{"object":"page","id":"root","parent":{"type":"workspace"}}],"has_more":false}`)
		case "/blocks/root/children":
			fmt.Fprint(w, `{"results":[{"id":"child","type":"child_page"}],"has_more":false}`)
		case "/pages/child":
			fmt.Fprint(w, `{"object":"page","id":"child","parent":{"type":"page_id","page_id":"root"}}`)
		case "/blocks/child/children":
			fmt.Fprint(w, `{"results":[{"id":"grandchild","type":"child_page"},{"id":"db","type":"child_database"}],"has_more":false}`)
		case "/pages/grandchild":
			fmt.Fprint(w, `{"object":"page","id":"grandchild","last_edited_time":"2099-01-01T00:00:00Z","parent":{"type":"page_id","page_id":"child"}}`)
		case "/blocks/grandchild/children":
			fmt.Fprint(w, `{"results":[],"has_more":false}`)
		case "/databases/db":
			fmt.Fprint(w, `{"id":"db","data_sources":[{"id":"ds"}]}`)
		case "/data_sources/ds/query":
			fmt.Fprint(w, `{"results":[{"object":"page","id":"row","parent":{"type":"data_source_id","data_source_id":"ds"}}],"has_more":false}`)
		default:
			t.Errorf("unexpected body traversal: %s", r.URL)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	old := apiBase
	apiBase = srv.URL
	defer func() { apiBase = old }()
	client := NewClient("dummy")
	client.limiter.SetLimit(rate.Inf)
	result, err := client.RefreshRecent(context.Background(), RecentIndex{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Pages) != 5 || result.Top()[0].ID != "grandchild" {
		t.Fatalf("incomplete deep index: %+v", result)
	}
	if result.Graph["child"].Pages[0] != "grandchild" || result.DeepSyncedAt.IsZero() || !result.Fresh(time.Now()) {
		t.Fatal("discovery graph/freshness not saved")
	}
}

func TestRecentIndexKeepsAllMetadataAndStableTop(t *testing.T) {
	index := RecentIndex{Pages: make(map[string]Page)}
	for i := 0; i < 150; i++ {
		id := fmt.Sprintf("p%03d", i)
		index.Pages[id] = Page{ID: id}
	}
	index.Pages["ds1"] = Page{ID: "ds1", Kind: KindDataSource, DatabaseID: "db", LastEdited: time.Now()}
	index.Pages["ds2"] = Page{ID: "ds2", Kind: KindDataSource, DatabaseID: "db", LastEdited: index.Pages["ds1"].LastEdited}
	top := index.Top()
	if len(top) != 100 || len(index.Pages) != 152 || top[0].ID != "ds1" || top[1].ID != "p000" {
		t.Fatalf("unstable/deduplication failure: %+v", top[:2])
	}
	if (RecentIndex{RefreshedAt: time.Now().Add(2 * time.Minute)}).Fresh(time.Now()) {
		t.Fatal("future timestamp treated as fresh")
	}
}

func TestDeepFailureRetainsQuickFreshnessAndAvoidsImmediateRescan(t *testing.T) {
	blocks := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/search" {
			fmt.Fprint(w, `{"results":[{"object":"page","id":"root","parent":{"type":"workspace"}}],"has_more":false}`)
			return
		}
		blocks++
		w.WriteHeader(400)
		fmt.Fprint(w, `{"code":"validation_error","message":"fixture discovery failure"}`)
	}))
	defer srv.Close()
	old := apiBase
	apiBase = srv.URL
	defer func() { apiBase = old }()
	client := NewClient("dummy")
	client.limiter.SetLimit(rate.Inf)
	index, err := client.RefreshRecent(context.Background(), RecentIndex{}, nil, nil)
	if err == nil || !index.Fresh(time.Now()) || !index.DeepSyncedAt.IsZero() || index.DeepAttemptedAt.IsZero() {
		t.Fatal("deep failure lost successful quick phase")
	}
	index, err = client.RefreshRecent(context.Background(), index, nil, nil)
	if err != nil || blocks != 1 || !index.DeepSyncedAt.IsZero() {
		t.Fatalf("failed discovery repeatedly scanned: %v %d", err, blocks)
	}
}

func TestInterruptedDiscoveryResumesFailedWorkWithoutRescanningRoots(t *testing.T) {
	calls := make(map[string]int)
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls[r.URL.Path]++
		attempt := calls[r.URL.Path]
		mu.Unlock()
		switch r.URL.Path {
		case "/search":
			fmt.Fprint(w, `{"results":[{"object":"page","id":"root1","parent":{"type":"workspace"}},{"object":"page","id":"root2","parent":{"type":"workspace"}}],"has_more":false}`)
		case "/blocks/root1/children":
			fmt.Fprint(w, `{"results":[{"id":"child","type":"child_page"},{"id":"db","type":"child_database"}],"has_more":false}`)
		case "/blocks/root2/children", "/pages/child", "/databases/db":
			if attempt == 1 {
				w.WriteHeader(400)
				fmt.Fprint(w, `{"code":"validation_error","message":"fixture temporary failure"}`)
				return
			}
			switch r.URL.Path {
			case "/blocks/root2/children":
				fmt.Fprint(w, `{"results":[],"has_more":false}`)
			case "/pages/child":
				fmt.Fprint(w, `{"object":"page","id":"child","parent":{"type":"page_id","page_id":"root1"}}`)
			default:
				fmt.Fprint(w, `{"id":"db","data_sources":[{"id":"ds"}]}`)
			}
		case "/blocks/child/children", "/data_sources/ds/query":
			fmt.Fprint(w, `{"results":[],"has_more":false}`)
		default:
			t.Errorf("unexpected request %s", r.URL)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	old := apiBase
	apiBase = srv.URL
	defer func() { apiBase = old }()
	client := NewClient("dummy")
	client.limiter.SetLimit(rate.Inf)
	index, err := client.RefreshRecent(context.Background(), RecentIndex{}, nil, nil)
	if err == nil || !index.DiscoveryActive || !index.PendingPages["child"] || !index.DiscoveryVisited["root1"] {
		t.Fatalf("lost discovery checkpoint: %+v %v", index, err)
	}
	// Serialize the checkpoint to exercise restart compatibility.
	data, _ := json.Marshal(index)
	var restored RecentIndex
	json.Unmarshal(data, &restored)
	restored.DeepRetryAt = time.Time{}
	result, err := client.RefreshRecent(context.Background(), restored, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.DiscoveryActive || len(result.PendingPages) != 0 || result.DeepSyncedAt.IsZero() || result.Pages["child"].ID != "child" || result.Pages["ds"].ID != "ds" {
		t.Fatalf("resume did not complete: %+v", result)
	}
	if calls["/blocks/root1/children"] != 1 || calls["/blocks/root2/children"] != 2 || calls["/pages/child"] != 2 || calls["/databases/db"] != 2 {
		t.Fatalf("resume duplicated/lost work: %v", calls)
	}
}
