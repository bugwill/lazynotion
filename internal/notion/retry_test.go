package notion

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

func TestRawRequestRetriesReadsButNotAmbiguousWrites(t *testing.T) {
	searches, writes := 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/search" {
			searches++
			if searches == 1 {
				w.Header().Set("Retry-After", "0")
				w.WriteHeader(429)
				fmt.Fprint(w, `{"code":"rate_limited"}`)
				return
			}
			fmt.Fprint(w, `{"results":[],"has_more":false}`)
			return
		}
		writes++
		w.WriteHeader(503)
		fmt.Fprint(w, `{"code":"service_unavailable"}`)
	}))
	defer srv.Close()
	old := apiBase
	apiBase = srv.URL
	defer func() { apiBase = old }()
	client := NewClient("dummy")
	client.limiter.SetLimit(rate.Inf)
	if _, err := client.rawRequest(context.Background(), http.MethodPost, "/search", []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	_, err := client.rawRequest(context.Background(), http.MethodPost, "/pages", []byte(`{}`))
	if err == nil || searches != 2 || writes != 1 {
		t.Fatalf("retry policy failed: %d %d %v", searches, writes, err)
	}
}

func TestRetryAfterWaitIsCancelableAndPermissionErrorsStructured(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(429)
		fmt.Fprint(w, `{"code":"rate_limited"}`)
	}))
	defer srv.Close()
	old := apiBase
	apiBase = srv.URL
	defer func() { apiBase = old }()
	client := NewClient("dummy")
	client.limiter.SetLimit(rate.Inf)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := client.rawRequest(ctx, http.MethodGet, "/pages/p", nil)
	if !errors.Is(err, context.DeadlineExceeded) || calls != 1 {
		t.Fatalf("retry ignored cancellation: %v %d", err, calls)
	}
	if inaccessible(&APIError{Status: 403, Code: "workspace_block_limit_reached"}) || !inaccessible(&APIError{Status: 403, Code: "restricted_resource"}) {
		t.Fatal("unrelated 403 conflated with page permissions")
	}
}
