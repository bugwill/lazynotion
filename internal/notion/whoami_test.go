package notion

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWhoAmI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users/me" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer ntn_test" {
			t.Errorf("auth header = %q", got)
		}
		w.Write([]byte(`{"object":"user","name":"lazynotion","type":"bot","bot":{"workspace_name":"Justin's Notion"}}`))
	}))
	defer srv.Close()
	oldBase := apiBase
	apiBase = srv.URL
	defer func() { apiBase = oldBase }()

	bot, err := NewClient("ntn_test").WhoAmI(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if bot.Name != "lazynotion" || bot.WorkspaceName != "Justin's Notion" {
		t.Errorf("bot = %+v", bot)
	}
}

func TestWhoAmIBadToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"object":"error","status":401,"code":"unauthorized"}`))
	}))
	defer srv.Close()
	oldBase := apiBase
	apiBase = srv.URL
	defer func() { apiBase = oldBase }()

	if _, err := NewClient("ntn_bad").WhoAmI(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}
