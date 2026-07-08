package cache

import (
	"os"
	"testing"
	"time"

	"github.com/jomei/notionapi"

	"github.com/justinm35/lazynotion/internal/notion"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	s, err := OpenAt(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func sampleTree() []notion.BlockNode {
	return []notion.BlockNode{
		{Block: &notionapi.Heading1Block{
			BasicBlock: notionapi.BasicBlock{Object: "block", ID: "h1", Type: "heading_1"},
			Heading1: notionapi.Heading{RichText: []notionapi.RichText{
				{PlainText: "Title", Text: &notionapi.Text{Content: "Title"}},
			}},
		}},
		{
			Block: &notionapi.BulletedListItemBlock{
				BasicBlock: notionapi.BasicBlock{Object: "block", ID: "li", Type: "bulleted_list_item"},
				BulletedListItem: notionapi.ListItem{RichText: []notionapi.RichText{
					{PlainText: "outer", Text: &notionapi.Text{Content: "outer"}},
				}},
			},
			Children: []notion.BlockNode{
				{Block: &notionapi.ToDoBlock{
					BasicBlock: notionapi.BasicBlock{Object: "block", ID: "td", Type: "to_do"},
					ToDo: notionapi.ToDo{Checked: true, RichText: []notionapi.RichText{
						{PlainText: "task", Text: &notionapi.Text{Content: "task"}},
					}},
				}},
			},
		},
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	s := testStore(t)
	edited := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)

	if s.Has("page-1") {
		t.Fatal("fresh store should not have the page")
	}
	if err := s.Save("page-1", edited, sampleTree()); err != nil {
		t.Fatal(err)
	}
	if !s.Has("page-1") {
		t.Fatal("saved page should exist")
	}

	nodes, gotEdited, ok := s.Load("page-1")
	if !ok {
		t.Fatal("load should hit")
	}
	if !gotEdited.Equal(edited) {
		t.Errorf("last edited = %v, want %v", gotEdited, edited)
	}
	if len(nodes) != 2 {
		t.Fatalf("got %d nodes, want 2", len(nodes))
	}
	h1, ok := nodes[0].Block.(*notionapi.Heading1Block)
	if !ok || h1.Heading1.RichText[0].PlainText != "Title" {
		t.Errorf("heading did not round trip: %#v", nodes[0].Block)
	}
	if len(nodes[1].Children) != 1 {
		t.Fatalf("children lost: %+v", nodes[1])
	}
	todo, ok := nodes[1].Children[0].Block.(*notionapi.ToDoBlock)
	if !ok || !todo.ToDo.Checked || todo.ToDo.RichText[0].PlainText != "task" {
		t.Errorf("nested to-do did not round trip: %#v", nodes[1].Children[0].Block)
	}
}

func TestInvalidate(t *testing.T) {
	s := testStore(t)
	if err := s.Save("page-1", time.Now(), sampleTree()); err != nil {
		t.Fatal(err)
	}
	s.Invalidate("page-1")
	if s.Has("page-1") {
		t.Error("invalidated page should be gone")
	}
	if _, _, ok := s.Load("page-1"); ok {
		t.Error("load after invalidate should miss")
	}
}

func TestLoadCorruptEntryIsAMiss(t *testing.T) {
	s := testStore(t)
	if err := s.Save("page-1", time.Now(), sampleTree()); err != nil {
		t.Fatal(err)
	}
	if err := writeCorrupt(s, "page-1"); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := s.Load("page-1"); ok {
		t.Error("corrupt entry should read as a miss")
	}
}

func writeCorrupt(s *Store, pageID string) error {
	return os.WriteFile(s.path(pageID), []byte("{not json"), 0o600)
}
