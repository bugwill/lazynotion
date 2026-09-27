package ui

import (
	"github.com/justinm35/lazynotion/internal/cache"
	"github.com/justinm35/lazynotion/internal/notion"
	"testing"
)

func TestRecentNavigation(t *testing.T) {
	m := New([]Workspace{{Name: "test", RootOnly: true, RootPages: "recent"}}, 0, nil, "mosaic")
	next, _ := m.Update(pagesMsg{pages: []notion.Page{{ID: "root", Title: "Root"}}})
	m = next.(Model)
	if m.sidebar.Items()[0].(pageItem).page.ID != recentID || m.selected.ID != recentID || m.recent == nil {
		t.Fatal("Recent should be pinned and open on startup")
	}
	next, _ = m.Update(recentMsg{pages: []notion.Page{{ID: "nested", Title: "Nested"}}})
	m = next.(Model)
	m.blockCache["nested"] = []notion.BlockNode{para("b", "body")}
	next, _ = m.updateKeys(key("enter"))
	m = next.(Model)
	if m.selected.ID != "nested" || m.recent != nil {
		t.Fatal("Recent entry should open the actual page")
	}
	next, _ = m.updateKeys(key("esc"))
	m = next.(Model)
	if m.recent == nil || m.selected.ID != recentID {
		t.Fatal("Esc should restore Recent")
	}
	next, _ = m.updateKeys(key("esc"))
	m = next.(Model)
	if m.recent != nil || m.selected != nil || m.focus != focusSidebar {
		t.Fatal("Esc should return to home")
	}
	next, _ = m.Update(pagesMsg{pages: []notion.Page{{ID: "root", Title: "Root"}}})
	m = next.(Model)
	if m.recent != nil {
		t.Fatal("background refresh must not reopen Recent")
	}
}

func TestRecentCatalogCache(t *testing.T) {
	store, err := cache.OpenAt(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	client := notion.NewClient("dummy")
	pages := []notion.Page{{ID: "nested", Title: "Cached"}}
	if err := store.SavePages("recent-v2:"+client.CacheKey(), pages); err != nil {
		t.Fatal(err)
	}
	m := New([]Workspace{{Name: "test", Client: client}}, 0, store, "mosaic")
	msg, ok := m.loadRecent(false)().(recentMsg)
	if !ok || !msg.fromCache || msg.pages[0].Title != "Cached" {
		t.Fatal("Recent should load from disk without contacting Notion")
	}
	if client.CacheKey() == notion.NewClient("another").CacheKey() {
		t.Fatal("integrations must have distinct cache keys")
	}
}

func TestBrowseListCache(t *testing.T) {
	store, _ := cache.OpenAt(t.TempDir())
	client := notion.NewClient("dummy")
	store.SavePages(client.CacheKey()+":true:", []notion.Page{{ID: "root", Title: "Cached root"}})
	m := New([]Workspace{{Name: "test", Client: client, RootOnly: true}}, 0, store, "mosaic")
	msg, ok := m.loadPages("")().(pagesMsg)
	if !ok || !msg.fromCache || len(msg.pages) != 1 {
		t.Fatal("browse list should load from disk")
	}
	next, cmd := m.Update(msg)
	m = next.(Model)
	if cmd == nil || m.sidebar.Items()[0].(pageItem).page.ID != recentID {
		t.Fatal("cached list should include Recent and schedule revalidation")
	}
}

func TestRecentDatabaseCache(t *testing.T) {
	store, err := cache.OpenAt(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	client := notion.NewClient("dummy")
	ref := notion.Page{ID: "ds", Title: "Books", Kind: notion.KindDataSource}
	cached := cachedDatabase{Source: &notion.DataSource{ID: "ds"}, Rows: &notion.RowPage{}, Sources: 1}
	if err := store.SaveMetadata("database:"+client.CacheKey()+":ds", cached); err != nil {
		t.Fatal(err)
	}
	m := New([]Workspace{{Name: "test", Client: client}}, 0, store, "mosaic")
	msg, ok := m.loadDatabaseView(ref)().(dbLoadedMsg)
	if !ok || !msg.fromCache || msg.ds.ID != "ds" {
		t.Fatal("database should load schema and rows from disk")
	}
}

func TestRecentDoesNotAllowPageIconEdits(t *testing.T) {
	m := New([]Workspace{{Name: "test", RootPages: "recent"}}, 0, nil, "mosaic")
	next, _ := m.updateKeys(key("I"))
	m = next.(Model)
	if m.mode != inputNone {
		t.Fatal("Recent must not send page edits with its virtual ID")
	}
}

func TestOldBrowseRefreshDoesNotReplaceSearch(t *testing.T) {
	m := searchModel(t)
	next, _ := m.Update(pagesMsg{pages: []notion.Page{{ID: "p", Title: "Match"}}, query: "match"})
	m = next.(Model)
	next, _ = m.Update(pagesMsg{pages: []notion.Page{{ID: "root", Title: "Old"}}, background: true})
	m = next.(Model)
	if m.lastQuery != "match" || len(m.sidebar.Items()) != 1 {
		t.Fatal("an old background refresh must not overwrite the active search")
	}
}

func TestRecentStartupCompatibility(t *testing.T) {
	for _, value := range []string{"recent", " Recent "} {
		m := New([]Workspace{{Name: "test", RootPages: value}}, 0, nil, "mosaic")
		if m.recent == nil || m.selected.Title != "Recent" {
			t.Fatalf("%q should open Recent", value)
		}
	}
}

func TestLibraryDoesNotStartRecent(t *testing.T) {
	if startsInRecent("library") {
		t.Fatal("library compatibility must be removed")
	}
}
