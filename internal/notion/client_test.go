package notion

import (
	"context"
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
	if len(pages) != 3 {
		t.Fatalf("got %d pages, want 3", len(pages))
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
	if len(pages) != 1 || pages[0].ID != "page-1" {
		t.Fatalf("root-only should keep just the workspace-level page, got %+v", pages)
	}
}
