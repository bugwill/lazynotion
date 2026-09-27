package ui

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/justinm35/lazynotion/internal/cache"
	"github.com/justinm35/lazynotion/internal/notion"
)

type recentTestTransport struct{ target *url.URL }

func (transport recentTestTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	request := r.Clone(r.Context())
	request.URL.Scheme, request.URL.Host = transport.target.Scheme, transport.target.Host
	request.URL.Path = request.URL.Path[len("/v1"):]
	return http.DefaultTransport.RoundTrip(request)
}

func fixtureRecentClient(t *testing.T, handler http.HandlerFunc) *notion.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	target, _ := url.Parse(server.URL)
	old := http.DefaultClient
	http.DefaultClient = &http.Client{Transport: recentTestTransport{target}}
	t.Cleanup(func() { http.DefaultClient = old; server.Close() })
	return notion.NewClient("dummy-recent-fixture")
}

func TestFreshRecentCacheDoesNotScheduleNetwork(t *testing.T) {
	client := fixtureRecentClient(t, func(w http.ResponseWriter, r *http.Request) { t.Errorf("fresh cache contacted network: %s", r.URL) })
	store, _ := cache.OpenAt(t.TempDir())
	index := notion.RecentIndex{Pages: map[string]notion.Page{"p": {ID: "p", Title: "Cached"}}, RefreshedAt: time.Now()}
	if err := store.SaveMetadata(recentIndexKey(client), index); err != nil {
		t.Fatal(err)
	}
	m := New([]Workspace{{Name: "fixture", Client: client, RootPages: "recent"}}, 0, store, "mosaic")
	m.loading = false
	msg := m.loadRecent(false)().(recentMsg)
	next, cmd := m.Update(msg)
	if msg.refresh || cmd != nil || next.(Model).recent.pages[0].Title != "Cached" {
		t.Fatal("fresh snapshot should remain usable without revalidation")
	}
	index.RefreshedAt = time.Now().Add(-2 * time.Minute)
	loader := &recentLoader{indexes: map[string]notion.RecentIndex{recentIndexKey(client): index}}
	stale := loader.load(client, store, 0, false).(recentMsg)
	if !stale.refresh {
		t.Fatal("stale snapshot did not request refresh")
	}
}

func TestRecentRefreshCoalescesAndPersistsIndex(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	client := fixtureRecentClient(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			close(started)
		}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		fmt.Fprint(w, `{"results":[{"object":"page","id":"row","parent":{"type":"data_source_id"}}],"has_more":false}`)
	})
	store, _ := cache.OpenAt(t.TempDir())
	loader := &recentLoader{}
	first := loader.load(client, store, 0, true).(queryEvent)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("query did not start")
	}
	second := loader.load(client, store, 0, true).(queryEvent)
	if first.stream != second.stream {
		t.Fatal("repeated refresh launched another task")
	}
	// A second start must not schedule another channel reader.
	m := New([]Workspace{{Name: "fixture", Client: client, RootPages: "recent"}}, 0, store, "mosaic")
	next, _ := m.Update(first)
	m = next.(Model)
	_, cmd := m.Update(second)
	if cmd != nil {
		t.Fatal("joined refresh scheduled a competing stream reader")
	}
	close(release)
	partial, final := false, false
	for msg := range first.stream {
		if rows, ok := msg.(recentMsg); ok {
			if rows.partial {
				partial = true
			} else {
				final = true
				if rows.err != nil {
					t.Fatal(rows.err)
				}
			}
		}
	}
	if calls.Load() != 1 || !partial || !final {
		t.Fatalf("unexpected query lifecycle: calls=%d partial=%v final=%v", calls.Load(), partial, final)
	}
	reloaded := (&recentLoader{}).load(client, store, 0, false).(recentMsg)
	if reloaded.refresh || len(reloaded.pages) != 1 {
		t.Fatal("successful index not fresh across restart")
	}
}

func TestWorkspaceCancellationStopsRecentAndDoesNotWriteOldIndex(t *testing.T) {
	started, canceled := make(chan struct{}), make(chan struct{})
	client := fixtureRecentClient(t, func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		close(started)
		<-r.Context().Done()
		close(canceled)
	})
	store, _ := cache.OpenAt(t.TempDir())
	loader := &recentLoader{}
	event := loader.load(client, store, 0, true).(queryEvent)
	<-started
	loader.cancel()
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("network request survived cancellation")
	}
	for range event.stream {
	}
	var index notion.RecentIndex
	if store.LoadMetadata(recentIndexKey(client), &index) {
		t.Fatal("canceled workspace wrote a cache")
	}
}

func TestPartialRecentRowsKeepProgressAndSelection(t *testing.T) {
	m := New([]Workspace{{Name: "fixture", RootPages: "recent"}}, 0, nil, "mosaic")
	m.recent.pages = []notion.Page{{ID: "a"}, {ID: "b"}}
	m.recent.cursor = 1
	ch := make(chan tea.Msg)
	m.recentQuery = &queryState{stream: ch}
	event := queryEvent{kind: "recent", stream: ch, payload: recentMsg{partial: true, pages: []notion.Page{{ID: "b"}, {ID: "a"}}}}
	next, _ := m.Update(event)
	m = next.(Model)
	if m.recentQuery == nil || m.recent.cursor != 0 || !m.recent.loaded {
		t.Fatal("partial rows lost active progress or selection")
	}
	event.payload = recentMsg{pages: m.recent.pages, err: context.DeadlineExceeded}
	next, _ = m.Update(event)
	m = next.(Model)
	if m.recentQuery != nil || m.err == nil || len(m.recent.pages) != 2 {
		t.Fatal("partial failure discarded rows or left progress active")
	}
}

func TestDelayedRecentStartCannotSupersedeNewerRefresh(t *testing.T) {
	m := New([]Workspace{{Name: "fixture"}}, 0, nil, "mosaic")
	newer, older := make(chan tea.Msg), make(chan tea.Msg)
	m.recentSequence = 2
	m.recentQuery = &queryState{stream: newer}
	canceled := false
	event := queryEvent{kind: "recent", stream: older, start: true, sequence: 1, cancel: func() { canceled = true }}
	next, _ := m.Update(event)
	if !canceled || next.(Model).recentQuery.stream != newer {
		t.Fatal("delayed start replaced newer work")
	}
}

func TestDeepRequestInvalidatesPersistedCadence(t *testing.T) {
	client := notion.NewClient("dummy")
	store, _ := cache.OpenAt(t.TempDir())
	index := notion.RecentIndex{DeepSyncedAt: time.Now(), DeepAttemptedAt: time.Now()}
	store.SaveMetadata(recentIndexKey(client), index)
	loader := &recentLoader{}
	loader.requestDeep(client, store)
	saved := loader.indexes[recentIndexKey(client)]
	if !saved.DeepSyncedAt.IsZero() || !saved.DeepAttemptedAt.IsZero() {
		t.Fatal("forced deep sync kept the old cadence")
	}
}

func TestBackgroundRowsUpdateRecentHistoryWithoutStoppingPageLoad(t *testing.T) {
	m := New([]Workspace{{Name: "fixture"}}, 0, nil, "mosaic")
	state := &recentState{pages: []notion.Page{{ID: "old"}}, loaded: true}
	m.history = []historyEntry{{page: recentPage, recent: state}}
	m.pageLoading = true
	next, _ := m.handleRecent(recentMsg{partial: true, pages: []notion.Page{{ID: "new"}}})
	if state.pages[0].ID != "new" || !next.(Model).pageLoading {
		t.Fatal("background Recent update changed child loading or lost history")
	}
}

func TestRecentFirstBatchIsDurableBeforeDiscoveryCompletes(t *testing.T) {
	started := make(chan struct{})
	client := fixtureRecentClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/search" {
			fmt.Fprint(w, `{"results":[{"object":"page","id":"root","parent":{"type":"workspace"}}],"has_more":false}`)
			return
		}
		close(started)
		<-r.Context().Done()
	})
	store, _ := cache.OpenAt(t.TempDir())
	loader := &recentLoader{}
	loader.requestDeep(client, store)
	event := loader.load(client, store, 0, true).(queryEvent)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("discovery did not start")
	}
	var saved notion.RecentIndex
	if !store.LoadMetadata(recentIndexKey(client), &saved) || saved.Pages["root"].ID != "root" {
		t.Fatal("first results were not durable during discovery")
	}
	loader.cancel()
	for range event.stream {
	}
}

func TestQuickRecentDoesNotResumePendingDiscovery(t *testing.T) {
	client := fixtureRecentClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" {
			t.Errorf("quick refresh resumed deep work: %s", r.URL)
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `{"results":[],"has_more":true,"next_cursor":"old-pages"}`)
	})
	key := recentIndexKey(client)
	loader := &recentLoader{indexes: map[string]notion.RecentIndex{key: {
		Pages: map[string]notion.Page{}, DiscoveryActive: true,
		DiscoveryPending: []string{"root"}, PendingPages: map[string]bool{"unseen": true},
	}}}
	event := loader.load(client, nil, 0, true).(queryEvent)
	var final recentMsg
	for msg := range event.stream {
		if m, ok := msg.(recentMsg); ok && !m.partial {
			final = m
		}
	}
	if final.err != nil {
		t.Fatal(final.err)
	}
	saved := loader.indexes[key]
	if !saved.DiscoveryActive || len(saved.DiscoveryPending) != 1 || !saved.PendingPages["unseen"] {
		t.Fatal("quick refresh discarded deep checkpoint")
	}
	if saved.RefreshedAt.IsZero() {
		t.Fatal("quick refresh did not complete")
	}
}
