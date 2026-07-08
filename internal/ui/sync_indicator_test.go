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
	if !strings.Contains(footer, "⇅") {
		t.Errorf("footer should show the sync glyph: %q", footer)
	}

	// completion releases it
	next, _ = m.Update(writeDoneMsg{pageID: "page-1", status: "done"})
	m = next.(Model)
	if m.syncing() {
		t.Error("completed write should clear the indicator")
	}
	if strings.Contains(stripAnsi(m.footerLine()), "⇅") {
		t.Error("footer should drop the glyph when idle")
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

func TestPulseTicksOnlyWhileSyncing(t *testing.T) {
	m := commitTestModel(t, nil)
	m.loading = false

	// syncing: pulse advances and rearms
	m.writesInFlight = 1
	m.pulsing = true
	next, cmd := m.Update(pulseMsg{})
	m = next.(Model)
	if cmd == nil || m.pulseFrame != 1 {
		t.Errorf("pulse should advance and rearm while syncing (frame=%d)", m.pulseFrame)
	}

	// idle: pulse stops, no further ticks
	m.writesInFlight = 0
	next, cmd = m.Update(pulseMsg{})
	m = next.(Model)
	if m.pulsing {
		t.Error("pulse should stop when idle")
	}
	if cmd != nil {
		t.Error("no tick should be scheduled when idle")
	}
}

func TestUpdateArmsPulseWhenSyncBegins(t *testing.T) {
	m := commitTestModel(t, nil)
	m.loading = false
	m.writesInFlight = 1
	next, cmd := m.Update(pulseArmProbe{})
	m = next.(Model)
	if !m.pulsing || cmd == nil {
		t.Error("any message leaving the model syncing should arm the pulse")
	}
}

type pulseArmProbe struct{}

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
