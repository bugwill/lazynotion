package notion

import (
	"testing"

	"github.com/jomei/notionapi"
)

func TestCreationPayload(t *testing.T) {
	now := notionapi.BasicBlock{
		Object: "block", ID: "abc", Type: "bulleted_list_item", HasChildren: true,
	}
	node := BlockNode{
		Block: &notionapi.BulletedListItemBlock{
			BasicBlock: now,
			BulletedListItem: notionapi.ListItem{RichText: []notionapi.RichText{
				{PlainText: "outer", Text: &notionapi.Text{Content: "outer"}},
			}},
		},
		Children: []BlockNode{{Block: &notionapi.ParagraphBlock{
			BasicBlock: notionapi.BasicBlock{Object: "block", ID: "child", Type: "paragraph"},
		}}},
	}
	payload, err := CreationPayload(node)
	if err != nil {
		t.Fatal(err)
	}
	for _, banned := range []string{"id", "has_children", "created_time"} {
		if _, present := payload[banned]; present {
			t.Errorf("payload contains read-only field %q", banned)
		}
	}
	item, ok := payload["bulleted_list_item"].(map[string]any)
	if !ok {
		t.Fatalf("missing type payload: %v", payload)
	}
	kids, ok := item["children"].([]map[string]any)
	if !ok || len(kids) != 1 {
		t.Fatalf("children not nested: %v", item)
	}
	if _, present := kids[0]["id"]; present {
		t.Error("child payload keeps read-only id")
	}
}

func TestCanRecreate(t *testing.T) {
	ok, _ := CanRecreate(BlockNode{Block: &notionapi.ParagraphBlock{}})
	if !ok {
		t.Error("paragraph should be recreatable")
	}
	ok, reason := CanRecreate(BlockNode{Block: &notionapi.ImageBlock{
		Image: notionapi.Image{File: &notionapi.FileObject{URL: "https://s3/x.png"}},
	}})
	if ok || reason == "" {
		t.Error("uploaded image should refuse with a reason")
	}
	ok, _ = CanRecreate(BlockNode{
		Block:    &notionapi.ParagraphBlock{},
		Children: []BlockNode{{Block: &notionapi.SyncedBlock{}}},
	})
	if ok {
		t.Error("synced block in subtree should refuse")
	}
}
