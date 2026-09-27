package notion

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jomei/notionapi"
	"golang.org/x/time/rate"
)

const heading4Fixture = `{"object":"block","id":"h4","type":"heading_4","has_children":true,"heading_4":{"rich_text":[{"type":"text","plain_text":"1. 肥胖症长期组合与市场分层","text":{"content":"1. 肥胖症长期组合与市场分层"},"annotations":{"bold":true}}],"color":"blue","is_toggleable":true}}`

func TestHeading4DecodeCloneAndCreation(t *testing.T) {
	block, err := DecodeBlock([]byte(heading4Fixture))
	if err != nil {
		t.Fatal(err)
	}
	h, ok := block.(*Heading4Block)
	if !ok || h.ID != "h4" || !h.Heading4.IsToggleable || !h.Heading4.RichText[0].Annotations.Bold {
		t.Fatalf("heading metadata lost: %+v", block)
	}
	node := ReplaceBlockID(BlockNode{Block: h}, "new-id")
	if _, ok := node.Block.(*Heading4Block); !ok || node.Block.GetID() != "new-id" {
		t.Fatal("cloning lost heading type or ID")
	}
	payload, err := CreationPayload(node)
	if err != nil {
		t.Fatal(err)
	}
	if payload["type"] != "heading_4" || payload["heading_4"] == nil || payload["id"] != nil {
		t.Fatalf("bad creation payload: %+v", payload)
	}
}

func TestHeading4FetchPaginationUpdateAndAppend(t *testing.T) {
	gets, patches := 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/blocks/page/children":
			gets++
			if r.URL.Query().Get("start_cursor") == "" {
				fmt.Fprintf(w, `{"results":[%s],"has_more":true,"next_cursor":"next"}`, heading4Fixture)
			} else {
				if r.URL.Query().Get("start_cursor") != "next" {
					t.Error("lost pagination cursor")
				}
				fmt.Fprint(w, `{"results":[{"type":"paragraph","id":"tail","paragraph":{"rich_text":[]}}],"has_more":false}`)
			}
		case r.Method == http.MethodGet && r.URL.Path == "/blocks/h4/children":
			fmt.Fprint(w, `{"results":[{"type":"paragraph","id":"child","paragraph":{"rich_text":[]}}],"has_more":false}`)
		case r.Method == http.MethodPatch && r.URL.Path == "/blocks/h4":
			patches++
			var body struct {
				Heading4 notionapi.Heading `json:"heading_4"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if !body.Heading4.IsToggleable || body.Heading4.Color != "blue" || len(body.Heading4.RichText) != 1 || body.Heading4.RichText[0].Text.Content != "改后的标题" {
				t.Errorf("heading update lost content or metadata: %+v", body)
			}
			fmt.Fprint(w, `{}`)
		case r.Method == http.MethodPatch && r.URL.Path == "/blocks/page/children":
			fmt.Fprint(w, `{"results":[{"id":"created-h4","type":"heading_4"}]}`)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(400)
		}
	}))
	defer srv.Close()
	old := apiBase
	apiBase = srv.URL
	defer func() { apiBase = old }()
	c := NewClient("dummy")
	c.limiter = rate.NewLimiter(rate.Inf, 1)
	nodes, err := c.PageBlocks(context.Background(), "page")
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 || len(nodes[0].Children) != 1 || gets != 2 {
		t.Fatalf("fetch lost heading children or pagination: %+v", nodes)
	}
	h := nodes[0].Block.(*Heading4Block)
	if err := c.SetBlockRichText(context.Background(), h, TextRichText("改后的标题")); err != nil {
		t.Fatal(err)
	}
	ids, err := c.AppendBlocks(context.Background(), "page", "", []notionapi.Block{h})
	if err != nil || len(ids) != 1 || ids[0] != "created-h4" || patches != 1 {
		t.Fatalf("append lost heading ID: %v %v", ids, err)
	}
}
