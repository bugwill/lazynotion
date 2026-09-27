package notion

import (
	"context"
	"golang.org/x/time/rate"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestRecentDiscoversUnindexedChildrenAndUsesDatabaseTime(t *testing.T) {
	seen := map[string]int{}
	var seenMu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenMu.Lock()
		seen[r.URL.Path]++
		seenMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/search":
			w.Write([]byte(`{"results":[
   {"object":"page","id":"root","last_edited_time":"2026-09-20T00:00:00Z","parent":{"type":"workspace"}},
   {"object":"data_source","id":"ds","last_edited_time":"2099-01-01T00:00:00Z","title":[{"plain_text":"Schema name"}],"parent":{"type":"database_id","database_id":"db"}}
  ],"has_more":false}`))
		case "/data_sources/ds/query":
			w.Write([]byte(`{"results":[],"has_more":false}`))
		case "/databases/db":
			w.Write([]byte(`{"id":"db","title":[{"plain_text":"Database name"}],"last_edited_time":"2026-09-01T00:00:00Z","data_sources":[{"id":"ds"}]}`))
		case "/blocks/root/children":
			if r.URL.Query().Get("start_cursor") == "next" {
				w.Write([]byte(`{"results":[{"id":"toggle","type":"toggle","has_children":true}],"has_more":false}`))
			} else {
				w.Write([]byte(`{"results":[{"id":"missing","type":"child_page"}],"has_more":true,"next_cursor":"next"}`))
			}
		case "/blocks/toggle/children":
			w.Write([]byte(`{"results":[{"id":"link","type":"link_to_page","link_to_page":{"type":"page_id","page_id":"missing"}}],"has_more":false}`))
		case "/pages/missing":
			w.Write([]byte(`{"object":"page","id":"missing","last_edited_time":"2026-09-26T00:00:00Z","properties":{"title":{"type":"title","title":[{"plain_text":"New child absent from search"}]}}}`))
		case "/blocks/missing/children":
			w.Write([]byte(`{"results":[{"id":"root","type":"child_page"}],"has_more":false}`))
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
	pages, err := client.Recent(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 3 || pages[0].ID != "missing" || pages[1].ID != "root" || pages[2].ID != "ds" {
		t.Fatalf("wrong merged/sorted result: %+v", pages)
	}
	if pages[2].Title != "Database name" {
		t.Fatal("Recent must describe the database, not its schema")
	}
	if seen["/pages/missing"] != 1 || seen["/blocks/root/children"] != 2 {
		t.Fatalf("deduplication or pagination failed: %+v", seen)
	}
}

func TestRecentDoesNotTraverseDatabaseRowBodies(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" {
			t.Errorf("must not scan database-row body: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"results":[{"object":"page","id":"row","last_edited_time":"2026-09-26T00:00:00Z","parent":{"type":"data_source_id"}}],"has_more":false}`))
	}))
	defer srv.Close()
	old := apiBase
	apiBase = srv.URL
	defer func() { apiBase = old }()
	pages, err := NewClient("dummy").Recent(context.Background())
	if err != nil || len(pages) != 1 || pages[0].ID != "row" {
		t.Fatalf("indexed rows should remain in Recent without scanning their bodies: %+v %v", pages, err)
	}
}
