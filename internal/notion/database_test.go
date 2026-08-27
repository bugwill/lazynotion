package notion

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

const databaseFixture = `{
  "object": "database",
  "id": "db-1",
  "title": [{"plain_text": "Tasks"}],
  "icon": {"type": "emoji", "emoji": "📋"},
  "last_edited_time": "2026-07-01T10:00:00.000Z",
  "data_sources": [
    {"id": "ds-1", "name": "Tasks"},
    {"id": "ds-2", "name": "Archive"}
  ]
}`

const dataSourceFixture = `{
  "object": "data_source",
  "id": "ds-1",
  "title": [{"plain_text": "Tasks"}],
  "icon": null,
  "parent": {"type": "database_id", "database_id": "db-1"},
  "last_edited_time": "2026-07-01T10:00:00.000Z",
  "properties": {
    "Name":   {"id": "title", "type": "title", "title": {}},
    "Status": {"id": "st", "type": "status", "status": {}},
    "Due":    {"id": "du", "type": "date", "date": {}},
    "Done":   {"id": "dn", "type": "checkbox", "checkbox": {}},
    "Effort": {"id": "ef", "type": "number", "number": {}}
  }
}`

const queryFixture = `{
  "object": "list",
  "results": [
    {
      "object": "page",
      "id": "row-1",
      "url": "https://www.notion.so/row-1",
      "icon": {"type": "emoji", "emoji": "🔥"},
      "last_edited_time": "2026-07-02T10:00:00.000Z",
      "properties": {
        "Name":   {"id": "title", "type": "title", "title": [{"plain_text": "Ship "}, {"plain_text": "release"}]},
        "Status": {"id": "st", "type": "status", "status": {"name": "In progress", "color": "blue"}},
        "Due":    {"id": "du", "type": "date", "date": {"start": "2026-07-20T09:00:00.000Z", "end": null}},
        "Done":   {"id": "dn", "type": "checkbox", "checkbox": false},
        "Effort": {"id": "ef", "type": "number", "number": 2.5},
        "Tags":   {"id": "tg", "type": "multi_select", "multi_select": [{"name": "go", "color": "green"}, {"name": "cli", "color": "gray"}]},
        "Owner":  {"id": "ow", "type": "people", "people": [{"object": "user", "id": "u1", "name": "Justin"}]},
        "Repo":   {"id": "rp", "type": "url", "url": "https://github.com/x"},
        "Linked": {"id": "ln", "type": "relation", "relation": [{"id": "a"}, {"id": "b"}], "has_more": false},
        "Key":    {"id": "uid", "type": "unique_id", "unique_id": {"prefix": "TASK", "number": 42}},
        "Score":  {"id": "fm", "type": "formula", "formula": {"type": "number", "number": 7}},
        "Roll":   {"id": "rl", "type": "rollup", "rollup": {"type": "array", "array": [{"type": "number", "number": 1}, {"type": "number", "number": 2}]}}
      }
    },
    {
      "object": "page",
      "id": "row-2",
      "url": "https://www.notion.so/row-2",
      "icon": null,
      "last_edited_time": "2026-07-01T10:00:00.000Z",
      "properties": {
        "Name":   {"id": "title", "type": "title", "title": []},
        "Status": {"id": "st", "type": "status", "status": null},
        "Due":    {"id": "du", "type": "date", "date": null},
        "Done":   {"id": "dn", "type": "checkbox", "checkbox": true},
        "Effort": {"id": "ef", "type": "number", "number": null}
      }
    }
  ],
  "has_more": true,
  "next_cursor": "cur-2"
}`

func fixtureServer(t *testing.T, wantPath, wantVersion, fixture string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != wantPath {
			t.Errorf("unexpected request %s %s, want %s", r.Method, r.URL.Path, wantPath)
		}
		if got := r.Header.Get("Notion-Version"); got != wantVersion {
			t.Errorf("Notion-Version = %q, want %q", got, wantVersion)
		}
		w.Write([]byte(fixture))
	}))
}

func TestGetDatabase(t *testing.T) {
	srv := fixtureServer(t, "/databases/db-1", versionDataSources, databaseFixture)
	defer srv.Close()
	oldBase := apiBase
	apiBase = srv.URL
	defer func() { apiBase = oldBase }()

	db, err := NewClient("tok").GetDatabase(context.Background(), "db-1")
	if err != nil {
		t.Fatal(err)
	}
	if db.Title != "Tasks" || db.Icon.Emoji != "📋" {
		t.Errorf("db = %+v", db)
	}
	if len(db.DataSources) != 2 || db.DataSources[0].ID != "ds-1" {
		t.Errorf("data sources = %+v", db.DataSources)
	}
}

func TestGetDataSourceSchemaOrder(t *testing.T) {
	srv := fixtureServer(t, "/data_sources/ds-1", versionDataSources, dataSourceFixture)
	defer srv.Close()
	oldBase := apiBase
	apiBase = srv.URL
	defer func() { apiBase = oldBase }()

	ds, err := NewClient("tok").GetDataSource(context.Background(), "ds-1")
	if err != nil {
		t.Fatal(err)
	}
	if ds.DatabaseID != "db-1" || ds.Title != "Tasks" {
		t.Errorf("ds = %+v", ds)
	}
	if ds.TitleProperty() != "Name" {
		t.Errorf("TitleProperty = %q", ds.TitleProperty())
	}
	// title column pinned first, rest alphabetical
	var names []string
	for _, p := range ds.Properties {
		names = append(names, p.Name)
	}
	want := []string{"Name", "Done", "Due", "Effort", "Status"}
	if len(names) != len(want) {
		t.Fatalf("columns = %v", names)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("columns = %v, want %v", names, want)
		}
	}
}

func TestQueryDataSourceRows(t *testing.T) {
	srv := fixtureServer(t, "/data_sources/ds-1/query", versionDataSources, queryFixture)
	defer srv.Close()
	oldBase := apiBase
	apiBase = srv.URL
	defer func() { apiBase = oldBase }()

	page, err := NewClient("tok").QueryDataSource(context.Background(), "ds-1", "", 50)
	if err != nil {
		t.Fatal(err)
	}
	if !page.HasMore || page.NextCursor != "cur-2" {
		t.Errorf("pagination = %+v", page)
	}
	if len(page.Rows) != 2 {
		t.Fatalf("got %d rows", len(page.Rows))
	}

	r := page.Rows[0]
	if r.Title("Name") != "Ship release" {
		t.Errorf("title = %q", r.Title("Name"))
	}
	if r.Icon.Emoji != "🔥" {
		t.Errorf("icon = %+v", r.Icon)
	}
	checks := map[string]string{
		"Status": "In progress",
		"Due":    "2026-07-20",
		"Done":   "",
		"Effort": "2.5",
		"Tags":   "go, cli",
		"Owner":  "Justin",
		"Repo":   "https://github.com/x",
		"Linked": "2 linked",
		"Key":    "TASK-42",
		"Score":  "7",
		"Roll":   "1, 2",
	}
	for prop, want := range checks {
		if got := r.Properties[prop].Display(); got != want {
			t.Errorf("%s = %q, want %q", prop, got, want)
		}
	}
	if opts := r.Properties["Status"].Options; len(opts) != 1 || opts[0].Color != "blue" {
		t.Errorf("status options = %+v", opts)
	}

	// empty/null cells
	r2 := page.Rows[1]
	if r2.Title("Name") != "Untitled" {
		t.Errorf("row 2 title = %q", r2.Title("Name"))
	}
	for _, prop := range []string{"Status", "Due", "Effort"} {
		if v := r2.Properties[prop]; v.HasValue || v.Display() != "" {
			t.Errorf("row 2 %s should be empty, got %+v", prop, v)
		}
	}
	if got := r2.Properties["Done"].Display(); got != "✓" {
		t.Errorf("row 2 Done = %q, want ✓", got)
	}
}

func TestParsePropertyValueUnknownType(t *testing.T) {
	var raw rawPropertyValue
	if err := json.Unmarshal([]byte(`{"id":"x","type":"verification","verification":{"state":"verified"}}`), &raw); err != nil {
		t.Fatal(err)
	}
	v := parsePropertyValue(raw)
	if v.Display() != "" {
		t.Errorf("unknown type should display empty, got %q", v.Display())
	}
}

func TestQueryDataSourceSendsCursor(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Write([]byte(`{"results": [], "has_more": false, "next_cursor": null}`))
	}))
	defer srv.Close()
	oldBase := apiBase
	apiBase = srv.URL
	defer func() { apiBase = oldBase }()

	if _, err := NewClient("tok").QueryDataSource(context.Background(), "ds-1", "cur-2", 25); err != nil {
		t.Fatal(err)
	}
	if gotBody["start_cursor"] != "cur-2" || gotBody["page_size"] != float64(25) {
		t.Errorf("body = %+v", gotBody)
	}
}
