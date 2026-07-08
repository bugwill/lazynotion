package ui

import (
	"context"
	"testing"
)

func findTestModel() Model {
	m := New([]Workspace{{Name: "test"}}, 0, nil, "mosaic")
	m.viewer.Width = 60
	m.viewer.Height = 20
	m.pv.setUnits("p", testUnits(), 60, nil)
	return m
}

func TestFindInPage(t *testing.T) {
	m := findTestModel()
	m.findQuery = "task"

	next, _ := m.jumpToMatch(0)
	m = next.(Model)
	if m.pv.cursor != 2 {
		t.Fatalf("cursor = %d, want 2 (first to-do)", m.pv.cursor)
	}

	next, _ = m.jumpToMatch(1)
	m = next.(Model)
	if m.pv.cursor != 3 {
		t.Fatalf("cursor = %d, want 3 (second to-do)", m.pv.cursor)
	}

	next, _ = m.jumpToMatch(1)
	m = next.(Model)
	if m.pv.cursor != 2 {
		t.Fatalf("cursor = %d, want 2 (wrapped)", m.pv.cursor)
	}

	next, _ = m.jumpToMatch(-1)
	m = next.(Model)
	if m.pv.cursor != 3 {
		t.Fatalf("cursor = %d, want 3 (wrapped backwards)", m.pv.cursor)
	}
}

func TestFindNoMatches(t *testing.T) {
	m := findTestModel()
	m.findQuery = "zzz-not-there"
	next, _ := m.jumpToMatch(0)
	m = next.(Model)
	if m.pv.cursor != 0 {
		t.Errorf("cursor should not move on no matches, got %d", m.pv.cursor)
	}
	if m.statusMsg == "" {
		t.Error("no-match search should report in the status bar")
	}
}

func TestUndoStack(t *testing.T) {
	m := findTestModel()

	next, _ := m.undo()
	m = next.(Model)
	if m.statusMsg != "nothing to undo" {
		t.Errorf("empty undo status = %q", m.statusMsg)
	}

	ran := 0
	m.pushUndo("block delete", "page-1", func(context.Context) error { ran++; return nil })
	next, cmd := m.undo()
	m = next.(Model)
	if len(m.undoStack) != 0 {
		t.Fatalf("undo stack should be empty, has %d", len(m.undoStack))
	}
	msg := cmd()
	done, ok := msg.(writeDoneMsg)
	if !ok || !done.reload || done.pageID != "page-1" {
		t.Fatalf("undo cmd result = %#v", msg)
	}
	if ran != 1 {
		t.Errorf("undo apply ran %d times, want 1", ran)
	}
}

func TestUndoStackCap(t *testing.T) {
	m := findTestModel()
	for i := 0; i < maxUndoDepth+10; i++ {
		m.pushUndo("x", "p", func(context.Context) error { return nil })
	}
	if len(m.undoStack) != maxUndoDepth {
		t.Errorf("stack = %d, want capped at %d", len(m.undoStack), maxUndoDepth)
	}
}
