package notion

import (
	"context"
	"encoding/json"
	"fmt"
	"golang.org/x/time/rate"
	"net/http"
	"net/http/httptest"
	"testing"
)

const searchFixture = `{
  "object": "list",
  "results": [
    {
      "object": "page",
      "id": "page-1",
      "url": "https://www.notion.so/page-1",
      "last_edited_time": "2026-07-01T10:00:00.000Z",
      "icon": {"type": "icon", "icon": {"name": "clipboard", "color": "gray"}},
      "parent": {"type": "workspace", "workspace": true},
      "properties": {
        "title": {"type": "title", "title": [{"plain_text": "Projects"}]}
      }
    },
    {
      "object": "page",
      "id": "page-2",
      "url": "https://www.notion.so/page-2",
      "last_edited_time": "2026-07-01T09:00:00.000Z",
      "icon": {"type": "emoji", "emoji": "🎯"},
      "parent": {"type": "page_id", "page_id": "page-1"},
      "properties": {
        "Name": {"type": "title", "title": [{"plain_text": "Goals"}, {"plain_text": " 2026"}]}
      }
    },
    {
      "object": "page",
      "id": "page-3",
      "url": "https://www.notion.so/page-3",
      "last_edited_time": "2026-07-01T08:00:00.000Z",
      "icon": null,
      "parent": {"type": "page_id", "page_id": "page-1"},
      "properties": {}
    },
    {
      "object": "data_source",
      "id": "ds-1",
      "url": "",
      "last_edited_time": "2026-07-01T07:00:00.000Z",
      "icon": {"type": "emoji", "emoji": "📋"},
      "parent": {"type": "database_id", "database_id": "db-1"},
      "title": [{"plain_text": "Tasks"}],
      "properties": {
        "Name": {"type": "title", "title": {}},
        "Status": {"type": "status", "status": {"options": []}}
      }
    }
  ],
  "has_more": false,
  "next_cursor": null
}`

func TestSearchParsesRawPages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" || r.Method != http.MethodPost {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("missing auth header")
		}
		w.Write([]byte(searchFixture))
	}))
	defer srv.Close()
	oldBase := apiBase
	apiBase = srv.URL
	defer func() { apiBase = oldBase }()

	pages, err := NewClient("tok").Search(context.Background(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 4 {
		t.Fatalf("got %d pages, want 4", len(pages))
	}
	if pages[0].Title != "Projects" || pages[0].Icon.Name != "clipboard" || pages[0].Icon.Color != "gray" {
		t.Errorf("page 1 = %+v", pages[0])
	}
	if pages[1].Title != "Goals 2026" || pages[1].Icon.Emoji != "🎯" {
		t.Errorf("page 2 = %+v", pages[1])
	}
	if pages[2].Title != "Untitled" || !pages[2].Icon.IsZero() {
		t.Errorf("page 3 = %+v", pages[2])
	}
	ds := pages[3]
	if ds.Kind != KindDataSource || ds.Title != "Tasks" || ds.DatabaseID != "db-1" {
		t.Errorf("data source = %+v", ds)
	}
	if ds.URL != "https://www.notion.so/db1" {
		t.Errorf("data source url = %q", ds.URL)
	}
}

func TestSearchRootOnlyFiltersNestedPages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(searchFixture))
	}))
	defer srv.Close()
	oldBase := apiBase
	apiBase = srv.URL
	defer func() { apiBase = oldBase }()

	pages, err := NewClient("tok").Search(context.Background(), "", true)
	if err != nil {
		t.Fatal(err)
	}
	// nested pages drop out; data sources stay (their parent is always the
	// database, so the workspace-root test can't apply to them)
	if len(pages) != 2 || pages[0].ID != "page-1" || pages[1].ID != "ds-1" {
		t.Fatalf("root-only should keep the workspace-level page and the data source, got %+v", pages)
	}
}

func TestRecentIsBoundedAndSorted(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Write([]byte(`{"results":[],"has_more":false}`))
			return
		}
		requests++
		var request struct {
			Query string `json:"query"`
			Sort  struct {
				Direction string `json:"direction"`
				Timestamp string `json:"timestamp"`
			} `json:"sort"`
			Cursor string `json:"start_cursor"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if request.Query != "" || request.Sort.Direction != "descending" || request.Sort.Timestamp != "last_edited_time" {
			t.Error("Recent must request newest edits across all titles")
		}
		if requests == 2 && request.Cursor != "cursor-1" {
			t.Error("Recent must paginate with the returned cursor")
		}
		var results []map[string]any
		for i := 0; i < 50; i++ {
			results = append(results, map[string]any{"object": "page", "id": fmt.Sprintf("page-%d-%d", requests, i), "parent": map[string]any{"type": "page_id"}})
		}
		json.NewEncoder(w).Encode(map[string]any{"results": results, "has_more": requests < 3, "next_cursor": fmt.Sprintf("cursor-%d", requests)})
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
	if len(pages) != 100 || requests != 3 {
		t.Fatalf("Recent should include nested pages and stop at 100: %d pages, %d requests", len(pages), requests)
	}
}
