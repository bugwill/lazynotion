package notion

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

func TestRecentOverlapsIndependentRequests(t *testing.T) {
	var mu sync.Mutex
	active, peak := 0, 0
	starts := map[string]int{}
	release := map[string]chan struct{}{"blocks": make(chan struct{}), "pages": make(chan struct{})}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/search" {
			fmt.Fprint(w, `{"results":[`)
			for i := 0; i < recentConcurrency; i++ {
				if i > 0 {
					fmt.Fprint(w, ",")
				}
				fmt.Fprintf(w, `{"object":"page","id":"root-%d","parent":{"type":"workspace"}}`, i)
			}
			fmt.Fprint(w, `],"has_more":false}`)
			return
		}
		parts := strings.Split(r.URL.Path, "/")
		if parts[1] == "blocks" && strings.HasPrefix(parts[2], "child-") {
			fmt.Fprint(w, `{"results":[],"has_more":false}`)
			return
		}
		phase := parts[1]
		mu.Lock()
		active++
		peak = max(peak, active)
		starts[phase]++
		if starts[phase] == recentConcurrency {
			close(release[phase])
		}
		mu.Unlock()
		defer func() { mu.Lock(); active--; mu.Unlock() }()
		select {
		case <-release[phase]:
		case <-r.Context().Done():
			return
		}
		if phase == "blocks" {
			fmt.Fprintf(w, `{"results":[{"id":"child-%s","type":"child_page"}],"has_more":false}`, parts[2])
		} else {
			fmt.Fprintf(w, `{"object":"page","id":"%s"}`, parts[2])
		}
	}))
	defer srv.Close()
	old := apiBase
	apiBase = srv.URL
	defer func() { apiBase = old }()
	client := NewClient("dummy")
	client.limiter.SetLimit(rate.Inf)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var updates []QueryProgress
	pages, err := client.RecentWithProgress(ctx, func(p QueryProgress) { updates = append(updates, p) })
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 2*recentConcurrency {
		t.Fatalf("lost results: %d", len(pages))
	}
	for _, stage := range []string{"checking", "pages"} {
		done := -1
		for _, p := range updates {
			if p.Stage != stage {
				continue
			}
			if p.Done == 0 {
				done = -1
			}
			if p.Total != recentConcurrency || p.Done <= done {
				t.Fatalf("non-monotonic progress: %+v", updates)
			}
			done = p.Done
		}
		if done != recentConcurrency {
			t.Fatalf("missing completed progress for %s", stage)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if peak != recentConcurrency {
		t.Fatalf("expected %d in flight, got %d", recentConcurrency, peak)
	}
}

func TestRecentParallelCancelsAndWaitsForWorkers(t *testing.T) {
	var wg sync.WaitGroup
	err := recentParallel(context.Background(), 10, func(ctx context.Context, i int) error {
		wg.Add(1)
		defer wg.Done()
		if i == 0 {
			return fmt.Errorf("fixture failure")
		}
		<-ctx.Done()
		return ctx.Err()
	})
	if err == nil || err.Error() != "fixture failure" {
		t.Fatalf("unexpected error: %v", err)
	}
	wg.Wait()
}
