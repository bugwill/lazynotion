package notion

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

func TestWindowStopsSearchAtDateAndQueriesOldDataSources(t *testing.T) {
	since := time.Date(2026, 9, 12, 12, 0, 0, 123000000, time.UTC)
	until := since.Add(14 * 24 * time.Hour)
	var mu sync.Mutex
	pageSearches, sourceSearches, rowQueries, oldSourceQueries := 0, 0, 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/search" {
			var req struct {
				Cursor string `json:"start_cursor"`
				Size   int    `json:"page_size"`
				Filter struct {
					Value string `json:"value"`
				} `json:"filter"`
			}
			json.NewDecoder(r.Body).Decode(&req)
			if req.Size != 100 {
				t.Errorf("unexpected batch size %d", req.Size)
			}
			if req.Filter.Value == "page" {
				pageSearches++
				if req.Cursor == "" {
					fmt.Fprint(w, `{"results":[{"object":"page","id":"ordinary-new","last_edited_time":"2026-09-26T10:00:00Z","parent":{"type":"workspace"}},{"object":"page","id":"row-indexed","last_edited_time":"2026-09-25T10:00:00Z","parent":{"type":"data_source_id","data_source_id":"ds"}}],"has_more":true,"next_cursor":"second"}`)
				} else if req.Cursor == "second" {
					fmt.Fprint(w, `{"results":[{"object":"page","id":"ordinary-nested","last_edited_time":"2026-09-13T10:00:00Z","parent":{"type":"page_id","page_id":"root"}},{"object":"page","id":"old","last_edited_time":"2026-09-01T10:00:00Z"}],"has_more":true,"next_cursor":"must-not-query"}`)
				} else {
					t.Errorf("read old search tail: %s", req.Cursor)
					http.NotFound(w, r)
				}
			} else if req.Filter.Value == "data_source" {
				sourceSearches++
				fmt.Fprint(w, `{"results":[{"object":"data_source","id":"ds","last_edited_time":"2020-01-01T00:00:00Z","title":[{"plain_text":"Old schema"}],"parent":{"type":"database_id","database_id":"db"}}],"has_more":false}`)
			} else {
				t.Errorf("missing object filter: %+v", req)
				http.NotFound(w, r)
			}
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/data_sources/") || !strings.HasSuffix(r.URL.Path, "/query") {
			t.Errorf("unexpected recursive query %s", r.URL)
			http.NotFound(w, r)
			return
		}
		var req struct {
			Cursor string `json:"start_cursor"`
			Filter struct {
				Timestamp string `json:"timestamp"`
				Edited    struct {
					After time.Time `json:"on_or_after"`
				} `json:"last_edited_time"`
			} `json:"filter"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if req.Filter.Timestamp != "last_edited_time" || !req.Filter.Edited.After.Equal(since) {
			t.Errorf("date filter has overlap or wrong precision: %+v", req)
		}
		mu.Lock()
		rowQueries++
		mu.Unlock()
		if strings.Contains(r.URL.Path, "cached-ds") {
			mu.Lock()
			oldSourceQueries++
			mu.Unlock()
			fmt.Fprint(w, `{"results":[{"object":"page","id":"cached-row","last_edited_time":"2026-09-26T09:00:00Z"}],"has_more":false}`)
		} else if req.Cursor == "" {
			fmt.Fprint(w, `{"results":[{"object":"page","id":"row-indexed","last_edited_time":"2026-09-25T10:00:00Z"}],"has_more":true,"next_cursor":"second"}`)
		} else {
			fmt.Fprint(w, `{"results":[{"object":"page","id":"row-unindexed","last_edited_time":"2026-09-25T09:00:00Z"},{"object":"page","id":"outside","last_edited_time":"2026-09-01T00:00:00Z"}],"has_more":false}`)
		}
	}))
	defer srv.Close()
	old := apiBase
	apiBase = srv.URL
	defer func() { apiBase = old }()
	client := NewClient("dummy")
	client.limiter.SetLimit(rate.Inf)
	report := client.RecentWindow(context.Background(), since, until, []Page{{ID: "cached-ds", Kind: KindDataSource, Title: "Cached source"}}, nil)
	if len(report.Errors) != 0 {
		t.Fatal(report.Errors)
	}
	if len(report.OrdinaryPages) != 2 || len(report.DatabaseRows) != 3 || report.SearchRows != 1 || report.RowsAbsentFromSearch != 2 {
		t.Fatalf("wrong classification/window results: %+v", report)
	}
	if pageSearches != 2 || sourceSearches != 1 || rowQueries != 3 || oldSourceQueries != 1 || report.HTTPRequests != 6 || !report.StoppedAtCutoff {
		t.Fatalf("queries did not stay bounded: %+v", report)
	}
	if report.DatabaseRows[0].ID != "cached-row" {
		t.Fatal("rows not sorted newest first")
	}
	if !report.Sources[0].CachedOnly {
		t.Fatal("cached source not retained")
	}
}
