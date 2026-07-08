package ui

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/justinm35/lazynotion/internal/notion"
)

const maxUndoDepth = 50

// undoRecord captures how to reverse one destructive quick-edit. The apply
// closure holds the client it was created with, so undo keeps working after
// a workspace switch. Deleted top-level blocks additionally carry the node
// and its position: un-archiving via the API appends at the page bottom, so
// positional undo reinserts a copy through the create queue instead.
type undoRecord struct {
	desc   string
	pageID string
	apply  func(context.Context) error
	// reinsertion data for top-level block deletes
	node     *notion.BlockNode
	anchorID string
}

func (m *Model) pushUndo(desc, pageID string, apply func(context.Context) error) {
	m.undoStack = append(m.undoStack, undoRecord{desc: desc, pageID: pageID, apply: apply})
	if len(m.undoStack) > maxUndoDepth {
		m.undoStack = m.undoStack[len(m.undoStack)-maxUndoDepth:]
	}
}

func (m Model) undo() (tea.Model, tea.Cmd) {
	if len(m.undoStack) == 0 {
		m.statusMsg = "nothing to undo"
		return m, nil
	}
	rec := m.undoStack[len(m.undoStack)-1]
	m.undoStack = m.undoStack[:len(m.undoStack)-1]

	// positional restore: reinsert the deleted block where it was, via the
	// same pending-ID create queue every other insert uses
	if rec.node != nil && m.selected != nil && m.selected.ID == rec.pageID {
		if blocks, cached := m.blockCache[rec.pageID]; cached {
			if payload, err := notion.CreationPayload(*rec.node); err == nil {
				m.pendingSeq++
				pendingID := fmt.Sprintf("pending-%d", m.pendingSeq)
				restored := notion.ReplaceBlockID(*rec.node, pendingID)

				inserted := make([]notion.BlockNode, 0, len(blocks)+1)
				placed := false
				if rec.anchorID == "" {
					inserted = append(inserted, restored)
					placed = true
				}
				for _, b := range blocks {
					inserted = append(inserted, b)
					if !placed && b.Block.GetID().String() == rec.anchorID {
						inserted = append(inserted, restored)
						placed = true
					}
				}
				if !placed {
					inserted = append(inserted, restored)
				}
				m.blockCache[rec.pageID] = inserted
				m.rebuildPage(true)
				m.cursorToBlock(pendingID)
				m.statusMsg = "restoring block…"
				m.moveQueue = append(m.moveQueue, moveOp{
					pageID:      rec.pageID,
					pendingIDs:  []string{pendingID},
					rawPayloads: []map[string]any{payload},
				})
				if !m.moveSyncing {
					if cmd := m.flushNextMove(); cmd != nil {
						return m, cmd
					}
				}
				return m, nil
			}
		}
	}

	m.statusMsg = "undoing " + rec.desc + "…"
	m.writesInFlight++
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := rec.apply(ctx); err != nil {
			return writeErrMsg{pageID: rec.pageID, err: err}
		}
		return writeDoneMsg{pageID: rec.pageID, status: "undid " + rec.desc, reload: true}
	}
}
