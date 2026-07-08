package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/jomei/notionapi"

	"github.com/justinm35/lazynotion/internal/notion"
)

func para(id, text string) notion.BlockNode {
	return notion.BlockNode{Block: &notionapi.ParagraphBlock{
		BasicBlock: notionapi.BasicBlock{ID: notionapi.BlockID(id), Type: "paragraph"},
		Paragraph: notionapi.Paragraph{RichText: []notionapi.RichText{
			{PlainText: text, Text: &notionapi.Text{Content: text}},
		}},
	}}
}

func moveTestModel(t *testing.T) Model {
	t.Helper()
	m := New([]Workspace{{Name: "test"}}, 0, nil, "mosaic")
	m.width, m.height = 90, 30
	m.viewer.Width = 60
	m.viewer.Height = 20
	page := notion.Page{ID: "page-1", Title: "p"}
	m.selected = &page
	m.blockCache["page-1"] = []notion.BlockNode{para("a", "first"), para("b", "second"), para("c", "third")}
	m.rebuildPage(true)
	return m
}

func topLevelIDs(m Model) []string {
	var ids []string
	for _, b := range m.blockCache["page-1"] {
		ids = append(ids, b.Block.GetID().String())
	}
	return ids
}

func TestMoveBlockDown(t *testing.T) {
	m := moveTestModel(t)
	m.pv.cursor = 0
	next, cmd := m.moveCurrentBlock(1)
	m = next.(Model)
	if cmd == nil {
		t.Fatal("move should produce a sync command")
	}
	if got := strings.Join(topLevelIDs(m), ","); got != "b,a,c" {
		t.Errorf("order = %s, want b,a,c", got)
	}
	if u, _ := m.pv.current(); u.ID() != "a" {
		t.Errorf("cursor should follow moved block, on %q", u.ID())
	}
}

func TestMoveBlockUpToTop(t *testing.T) {
	m := moveTestModel(t)
	m.pv.cursor = 1
	next, cmd := m.moveCurrentBlock(-1)
	m = next.(Model)
	if cmd == nil {
		t.Fatal("move should produce a sync command")
	}
	if got := strings.Join(topLevelIDs(m), ","); got != "b,a,c" {
		t.Errorf("order = %s, want b,a,c", got)
	}
	if u, _ := m.pv.current(); u.ID() != "b" {
		t.Errorf("cursor should follow block b, on %q", u.ID())
	}
}

func TestMoveBlockBoundaries(t *testing.T) {
	m := moveTestModel(t)
	m.pv.cursor = 0
	next, cmd := m.moveCurrentBlock(-1)
	m = next.(Model)
	if cmd != nil || m.statusMsg != "already at the top" {
		t.Errorf("move up at top: cmd=%v status=%q", cmd, m.statusMsg)
	}
	m.pv.cursor = 2
	next, cmd = m.moveCurrentBlock(1)
	m = next.(Model)
	if cmd != nil || m.statusMsg != "already at the bottom" {
		t.Errorf("move down at bottom: cmd=%v status=%q", cmd, m.statusMsg)
	}
}

func TestMoveRefusesSubPageSwap(t *testing.T) {
	m := moveTestModel(t)
	m.blockCache["page-1"][0] = notion.BlockNode{Block: &notionapi.ChildPageBlock{
		BasicBlock: notionapi.BasicBlock{ID: "sub", Type: "child_page"},
	}}
	m.rebuildPage(true)
	m.pv.cursor = 1
	next, cmd := m.moveCurrentBlock(-1)
	m = next.(Model)
	if cmd != nil {
		t.Fatal("moving above a sub-page should refuse")
	}
	if !strings.Contains(m.statusMsg, "sub-pages") {
		t.Errorf("status = %q", m.statusMsg)
	}
}

func TestNewBlockAbovePlacesDraftBeforeCursor(t *testing.T) {
	m := moveTestModel(t)
	m.pv.cursor = 1
	next, _ := m.newBlockAbove()
	m = next.(Model)
	if got := strings.Join(topLevelIDs(m), ","); got != "a,draft-block,b,c" {
		t.Errorf("order = %s, want draft before b", got)
	}
	if !m.editing {
		t.Error("should be editing the draft")
	}
	if u, _ := m.pv.current(); u.ID() != draftBlockID {
		t.Errorf("cursor on %q, want draft", u.ID())
	}
}

func TestNewBlockAboveFirst(t *testing.T) {
	m := moveTestModel(t)
	m.pv.cursor = 0
	next, _ := m.newBlockAbove()
	m = next.(Model)
	if got := strings.Join(topLevelIDs(m), ","); got != "draft-block,a,b,c" {
		t.Errorf("order = %s, want draft first", got)
	}
}

func TestConsecutiveMovesSerializeAndPatchIDs(t *testing.T) {
	m := moveTestModel(t)
	m.pv.cursor = 0

	// first J starts a sync
	next, cmd := m.moveCurrentBlock(1)
	m = next.(Model)
	if cmd == nil || !m.moveSyncing {
		t.Fatal("first move should start syncing")
	}
	// second J while syncing only queues
	next, cmd = m.moveCurrentBlock(1)
	m = next.(Model)
	if cmd != nil {
		t.Fatal("second move should wait for the first sync")
	}
	if len(m.moveQueue) != 1 {
		t.Fatalf("queue = %d ops, want 1 (coalesced)", len(m.moveQueue))
	}
	if got := strings.Join(topLevelIDs(m), ","); got != "b,c,a" {
		t.Errorf("local order = %s, want b,c,a", got)
	}

	// first sync completes: a was recreated as a2
	next, cmd = m.handleMoveDone(moveDoneMsg{pageID: "page-1", patches: [][2]string{{"a", "a2"}}})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("queued move should flush after the first completes")
	}
	if got := strings.Join(topLevelIDs(m), ","); got != "b,c,a2" {
		t.Errorf("ids after patch = %s, want b,c,a2", got)
	}

	// second sync completes, queue drains
	next, cmd = m.handleMoveDone(moveDoneMsg{pageID: "page-1", patches: [][2]string{{"a2", "a3"}}})
	m = next.(Model)
	if cmd != nil || m.moveSyncing {
		t.Error("queue should be drained")
	}
}

func TestMoveErrorResyncs(t *testing.T) {
	m := moveTestModel(t)
	m.pv.cursor = 0
	next, _ := m.moveCurrentBlock(1)
	m = next.(Model)

	next, cmd := m.handleMoveDone(moveDoneMsg{pageID: "page-1", err: errFake})
	m = next.(Model)
	if cmd == nil || !m.pageLoading {
		t.Error("failed move should force a page reload")
	}
	if len(m.moveQueue) != 0 {
		t.Error("failed move should clear the queue")
	}
}

var errFake = fmt.Errorf("boom")

func TestUndoDeleteRestoresPosition(t *testing.T) {
	m := moveTestModel(t)
	m.focus = focusViewer
	m.cursorToBlock("b") // the middle block

	next, cmd := m.deleteCurrentBlock()
	m = next.(Model)
	if cmd == nil {
		t.Fatal("delete should sync")
	}
	if got := strings.Join(topLevelIDs(m), ","); got != "a,c" {
		t.Fatalf("after delete order = %s, want a,c", got)
	}

	next, cmd = m.undo()
	m = next.(Model)
	if cmd == nil {
		t.Fatal("undo should sync the reinsertion")
	}
	ids := topLevelIDs(m)
	if len(ids) != 3 || ids[0] != "a" || ids[2] != "c" {
		t.Fatalf("undo order = %v, want the block back in the middle", ids)
	}
	if !isPendingID(ids[1]) {
		t.Fatalf("restored block should carry a pending ID, got %q", ids[1])
	}
	if u, _ := m.pv.current(); u.ID() != ids[1] {
		t.Errorf("cursor should sit on the restored block")
	}

	// the reinsert flushed straight into the sync queue (payload
	// sanitization is covered by the CreationPayload tests)
	if !m.moveSyncing {
		t.Error("reinsert sync should be in flight")
	}
	if len(m.moveQueue) != 0 {
		t.Errorf("queue should have flushed, has %d ops", len(m.moveQueue))
	}
}

func TestUndoDeleteFirstBlockRestoresToTop(t *testing.T) {
	m := moveTestModel(t)
	m.focus = focusViewer
	m.cursorToBlock("a")

	next, _ := m.deleteCurrentBlock()
	m = next.(Model)
	next, _ = m.undo()
	m = next.(Model)
	ids := topLevelIDs(m)
	if len(ids) != 3 || !isPendingID(ids[0]) || ids[1] != "b" {
		t.Fatalf("undo order = %v, want restored block first", ids)
	}
}
