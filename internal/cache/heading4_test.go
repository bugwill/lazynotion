package cache

import (
	"testing"
	"time"

	"github.com/jomei/notionapi"
	"github.com/justinm35/lazynotion/internal/notion"
)

func TestHeading4CachePreservesTypeAndInvalidatesLossyOldBlocks(t *testing.T) {
	s := testStore(t)
	h := &notion.Heading4Block{BasicBlock: notionapi.BasicBlock{ID: "h4", Type: "heading_4"}, Heading4: notionapi.Heading{RichText: notion.TextRichText("1. 四级标题"), IsToggleable: true}}
	if err := s.Save("page", time.Now(), []notion.BlockNode{{Block: h}}); err != nil {
		t.Fatal(err)
	}
	nodes, _, ok := s.Load("page")
	if !ok || len(nodes) != 1 {
		t.Fatal("cache did not load")
	}
	got, ok := nodes[0].Block.(*notion.Heading4Block)
	if !ok || got.ID != "h4" || !got.Heading4.IsToggleable || got.Heading4.RichText[0].Text.Content != "1. 四级标题" {
		t.Fatal("heading metadata lost in cache")
	}
	if err := s.Save("old", time.Now(), []notion.BlockNode{{Block: &notionapi.UnsupportedBlock{}}}); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := s.Load("old"); ok {
		t.Fatal("lossy old SDK cache must be refetched")
	}
}
