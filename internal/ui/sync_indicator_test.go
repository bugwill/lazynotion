package ui

import (
	"strings"
	"testing"

	"github.com/jomei/notionapi"

	"github.com/justinm35/lazynotion/internal/notion"
)

func TestSyncIndicatorLifecycle(t *testing.T) {
	m := commitTestModel(t, nil)
	m.loading = false
	todo := &notionapi.ToDoBlock{
		BasicBlock: notionapi.BasicBlock{ID: "td", Type: "to_do"},
		ToDo:       notionapi.ToDo{RichText: []notionapi.RichText{{PlainText: "x", Text: &notionapi.Text{Content: "x"}}}},
	}
	m.blockCache["page-1"] = []notion.BlockNode{{Block: todo}}
	m.rebuildPage(true)
	m.focus = focusViewer
	m.cursorToBlock("td")

	if m.syncing() {
		t.Fatal("idle model must not report syncing")
	}

	// a toggle dispatches a tracked write
	next, cmd := m.toggleCurrentToDo()
	m = next.(Model)
	if cmd == nil || !m.syncing() {
		t.Fatal("in-flight write should report syncing")
	}
	footer := stripAnsi(m.footerLine())
	if !strings.Contains(footer, "同步中") {
		t.Errorf("footer should show the static sync status: %q", footer)
	}

	// completion releases it
	next, _ = m.Update(writeDoneMsg{pageID: "page-1", status: "done"})
	m = next.(Model)
	if m.syncing() {
		t.Error("completed write should clear the indicator")
	}
	if strings.Contains(stripAnsi(m.footerLine()), "同步中") {
		t.Error("footer should drop the sync status when idle")
	}

	// error paths release it too
	m.writesInFlight = 1
	next, _ = m.Update(writeErrMsg{pageID: "page-1", err: errFake})
	m = next.(Model)
	if m.writesInFlight != 0 {
		t.Error("writeErrMsg should release the counter")
	}
}

func TestSyncIndicatorCoversQueueAndLoads(t *testing.T) {
	m := commitTestModel(t, nil)
	m.loading = false
	if m.syncing() {
		t.Fatal("baseline should be idle")
	}
	m.moveSyncing = true
	if !m.syncing() {
		t.Error("queue activity should report syncing")
	}
	m.moveSyncing = false
	m.pageLoading = true
	if !m.syncing() {
		t.Error("page loads should report syncing")
	}
}

func TestSyncStatusDoesNotScheduleAnimation(t *testing.T) {
	m := commitTestModel(t, nil)
	m.loading = false
	m.writesInFlight = 1
	next, cmd := m.Update(struct{}{})
	m = next.(Model)
	if cmd != nil {
		t.Fatal("sync status must not schedule periodic commands")
	}
	first := m.footerLine()
	if !strings.Contains(stripAnsi(first), "同步中") {
		t.Fatal("sync status missing")
	}
	if m.footerLine() != first {
		t.Fatal("sync status must remain static")
	}
}

func TestBackgroundRefreshYieldsToLocalEdits(t *testing.T) {
	m := commitTestModel(t, nil)
	m.loading = false
	m.blockCache["page-1"] = []notion.BlockNode{para2("a", "original")}
	m.rebuildPage(true)

	// user is mid-edit with a pending block in flight
	m.pv.cursor = 0
	next, _ := m.newBlockBelow()
	m = next.(Model)
	if !m.localBusy() {
		t.Fatal("editing should count as locally busy")
	}

	// a revalidation lands with server content that lacks the draft
	next, _ = m.Update(pageMsg{
		pageID: "page-1", page: notion.Page{ID: "page-1"},
		blocks: []notion.BlockNode{para2("a", "original")}, refreshed: true,
	})
	m = next.(Model)
	found := false
	for _, b := range m.blockCache["page-1"] {
		if b.Block.GetID().String() == draftBlockID {
			found = true
		}
	}
	if !found {
		t.Error("background refresh must not wipe the draft being edited")
	}
	if !m.editing {
		t.Error("the editor must survive the refresh")
	}
}

func TestMoveDonePatchesUnitsWithoutRebuild(t *testing.T) {
	m := commitTestModel(t, nil)
	m.loading = false
	m.blockCache["page-1"] = []notion.BlockNode{para2("a", "first")}
	m.rebuildPage(true)
	m.focus = focusViewer
	m.pv.cursor = 0

	// stamp an empty block (pending) then let its sync complete
	next, _ := m.updateKeys(key("enter"))
	m = next.(Model)
	pendingID := ""
	for _, u := range m.pv.units {
		if isPendingID(u.ID()) {
			pendingID = u.ID()
		}
	}
	if pendingID == "" {
		t.Fatal("expected a pending unit")
	}

	next, _ = m.handleMoveDone(moveDoneMsg{pageID: "page-1", patches: [][2]string{{pendingID, "real-9"}}})
	m = next.(Model)
	// units updated in place — no stale IDs left behind
	for _, u := range m.pv.units {
		if u.ID() == pendingID {
			t.Error("unit still carries the pending ID after the patch")
		}
	}
	found := false
	for _, u := range m.pv.units {
		if u.ID() == "real-9" {
			found = true
		}
	}
	if !found {
		t.Error("unit should carry the real ID")
	}
}
