package notion

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSetPageTitleUsesActualTitleProperty(t *testing.T) {
	for _, name := range []string{"title", "项目名称"} {
		t.Run(name, func(t *testing.T) {
			patches := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/pages/page-1" {
					t.Errorf("unexpected path %s", r.URL.Path)
				}
				if r.Method == http.MethodGet {
					key, _ := json.Marshal(name)
					fmt.Fprintf(w, `{"properties":{%s:{"type":"title","title":[]},"Status":{"type":"status"}}}`, key)
					return
				}
				if r.Method != http.MethodPatch {
					t.Errorf("unexpected method %s", r.Method)
				}
				patches++
				var body struct {
					Properties map[string]struct {
						Title []struct{ Text struct{ Content string } }
					}
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				p := body.Properties[name]
				if len(body.Properties) != 1 || len(p.Title) != 1 || p.Title[0].Text.Content != "新的标题" {
					t.Errorf("unexpected patch %+v", body)
				}
				fmt.Fprint(w, `{}`)
			}))
			defer srv.Close()
			old := apiBase
			apiBase = srv.URL
			defer func() { apiBase = old }()
			if err := NewClient("dummy").SetPageTitle(context.Background(), "page-1", "新的标题"); err != nil {
				t.Fatal(err)
			}
			if patches != 1 {
				t.Fatalf("patches=%d", patches)
			}
		})
	}
}
