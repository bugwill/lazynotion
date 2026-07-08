package ui

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jomei/notionapi"

	"github.com/justinm35/lazynotion/internal/notion"
)

type moveOp struct {
	pageID  string
	blockID string // move ops: the block whose position syncs
	// create ops: local placeholder IDs and the payloads to append —
	// either fresh blocks or sanitized maps of previously fetched ones.
	// parentID targets a container block; "" appends to the page itself.
	parentID    string
	pendingIDs  []string
	payloads    []notionapi.Block
	rawPayloads []map[string]any
}

func (op moveOp) isCreate() bool { return len(op.pendingIDs) > 0 }

func isPendingID(id string) bool { return strings.HasPrefix(id, "pending-") }

func (m *Model) enqueueCreate(pageID string, pendingIDs []string, payloads []notionapi.Block) {
	m.moveQueue = append(m.moveQueue, moveOp{pageID: pageID, pendingIDs: pendingIDs, payloads: payloads})
}

type moveDoneMsg struct {
	pageID string
	// old → new block ID rewrites from the recreate dance
	patches [][2]string
	err     error
}

// moveCurrentBlock swaps the cursor's block with a neighbor instantly and
// queues the server sync. Syncs run one at a time: each move recreates the
// block under a new ID, so the next sync must wait for that ID — firing
// them concurrently archives blocks twice ("Can't edit block that is
// archived") and litters duplicates.
func (m Model) moveCurrentBlock(delta int) (tea.Model, tea.Cmd) {
	unit, ok := m.pv.current()
	if !ok || m.selected == nil {
		return m, nil
	}
	if !unit.TopLevel {
		m.statusMsg = "only top-level blocks can be moved (for now)"
		return m, nil
	}
	if isPendingID(unit.ID()) {
		m.statusMsg = "block is still syncing — try again in a moment"
		return m, nil
	}
	if ok, reason := notion.CanRecreate(unit.Node); !ok {
		m.statusMsg = reason
		return m, nil
	}
	blocks := m.blockCache[m.selected.ID]
	idx := -1
	for i, b := range blocks {
		if b.Block.GetID().String() == unit.ID() {
			idx = i
			break
		}
	}
	if idx == -1 {
		return m, nil
	}
	if delta > 0 && idx == len(blocks)-1 {
		m.statusMsg = "already at the bottom"
		return m, nil
	}
	if delta < 0 && idx == 0 {
		m.statusMsg = "already at the top"
		return m, nil
	}
	// landing at position 0 recreates the displaced first block too
	if delta < 0 && idx == 1 {
		if ok, reason := notion.CanRecreate(blocks[0]); !ok {
			m.statusMsg = "can't move above: " + reason
			return m, nil
		}
	}

	blocks[idx], blocks[idx+delta] = blocks[idx+delta], blocks[idx]
	m.rebuildPage(true)
	m.cursorToBlock(unit.ID())
	m.statusMsg = "moving block…"

	m.enqueueMove(m.selected.ID, unit.ID())
	if !m.moveSyncing {
		if cmd := m.flushNextMove(); cmd != nil {
			return m, cmd
		}
	}
	return m, nil
}

// enqueueMove coalesces: a block already queued syncs once, to wherever it
// locally sits when its turn comes — three quick J presses are one sync.
func (m *Model) enqueueMove(pageID, blockID string) {
	for _, op := range m.moveQueue {
		if op.pageID == pageID && op.blockID == blockID {
			return
		}
	}
	m.moveQueue = append(m.moveQueue, moveOp{pageID: pageID, blockID: blockID})
}

// flushNextMove starts the next queued sync, resolving the block's target
// from its *current* local position. Returns nil when the queue is drained.
func (m *Model) flushNextMove() tea.Cmd {
	for len(m.moveQueue) > 0 {
		op := m.moveQueue[0]
		m.moveQueue = m.moveQueue[1:]

		if op.isCreate() {
			if cmd := m.flushCreate(op); cmd != nil {
				return cmd
			}
			continue
		}

		blocks := m.blockCache[op.pageID]
		idx := -1
		for i, b := range blocks {
			if b.Block.GetID().String() == op.blockID {
				idx = i
				break
			}
		}
		if idx == -1 {
			continue // deleted or replaced meanwhile
		}
		node := blocks[idx]
		client := m.client
		m.moveSyncing = true

		if idx == 0 {
			if len(blocks) < 2 {
				m.moveSyncing = false
				continue
			}
			first := blocks[1]
			return func() tea.Msg {
				ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
				defer cancel()
				nodeID, firstID, err := client.MoveBlockToTop(ctx, op.pageID, node, first)
				if err != nil {
					return moveDoneMsg{pageID: op.pageID, err: err}
				}
				return moveDoneMsg{pageID: op.pageID, patches: [][2]string{
					{node.Block.GetID().String(), nodeID},
					{first.Block.GetID().String(), firstID},
				}}
			}
		}

		afterID := blocks[idx-1].Block.GetID().String()
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			newID, err := client.MoveBlockAfter(ctx, op.pageID, node, afterID)
			if err != nil {
				return moveDoneMsg{pageID: op.pageID, err: err}
			}
			return moveDoneMsg{pageID: op.pageID, patches: [][2]string{
				{node.Block.GetID().String(), newID},
			}}
		}
	}
	m.moveSyncing = false
	return nil
}

// flushCreate syncs locally inserted pending blocks. The anchor is resolved
// now, from the current local order — by this point every earlier create in
// the queue has been patched to its real ID, so anchors are always valid.
func (m *Model) flushCreate(op moveOp) tea.Cmd {
	tree := m.blockCache[op.pageID]
	siblings := tree
	container := op.pageID
	if op.parentID != "" {
		container = op.parentID
		if children, ok := notion.ChildrenOf(tree, op.parentID); ok {
			siblings = children
		} else {
			siblings = nil
		}
	}
	idx := -1
	for i, b := range siblings {
		if b.Block.GetID().String() == op.pendingIDs[0] {
			idx = i
			break
		}
	}
	blocks := siblings
	client := m.client
	m.moveSyncing = true

	appendFn := func(ctx context.Context, afterID string) ([]string, error) {
		if op.rawPayloads != nil {
			return client.AppendRawBlocks(ctx, container, afterID, op.rawPayloads)
		}
		return client.AppendBlocks(ctx, container, afterID, op.payloads)
	}

	// pending span vanished (a forced reload replaced the tree): append at
	// the bottom rather than losing the user's content
	if idx == -1 {
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			if _, err := appendFn(ctx, ""); err != nil {
				return moveDoneMsg{pageID: op.pageID, err: err}
			}
			return moveDoneMsg{pageID: op.pageID}
		}
	}

	var afterID string
	if idx > 0 {
		afterID = blocks[idx-1].Block.GetID().String()
	}
	var successor *notion.BlockNode
	if op.parentID == "" && idx == 0 && len(blocks) > len(op.pendingIDs) {
		// inserting at the very top: append after the displaced first
		// block, then shuffle it back below the new ones
		succ := blocks[idx+len(op.pendingIDs)]
		successor = &succ
		afterID = succ.Block.GetID().String()
	}

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		ids, err := appendFn(ctx, afterID)
		if err != nil {
			return moveDoneMsg{pageID: op.pageID, err: err}
		}
		patches := make([][2]string, 0, len(ids)+1)
		for i, id := range ids {
			if i < len(op.pendingIDs) {
				patches = append(patches, [2]string{op.pendingIDs[i], id})
			}
		}
		if successor != nil && len(ids) > 0 {
			newSuccID, err := client.MoveBlockAfter(ctx, op.pageID, *successor, ids[len(ids)-1])
			if err != nil {
				return moveDoneMsg{pageID: op.pageID, err: err}
			}
			patches = append(patches, [2]string{successor.Block.GetID().String(), newSuccID})
		}
		return moveDoneMsg{pageID: op.pageID, patches: patches}
	}
}

func (m Model) handleMoveDone(msg moveDoneMsg) (tea.Model, tea.Cmd) {
	m.moveSyncing = false
	if m.store != nil {
		m.store.Invalidate(msg.pageID)
	}
	if msg.err != nil {
		m.moveQueue = nil
		m.err = msg.err
		delete(m.blockCache, msg.pageID)
		if m.selected != nil && m.selected.ID == msg.pageID {
			m.pageLoading = true
			return m, m.loadPage(*m.selected, true)
		}
		return m, nil
	}

	blocks := m.blockCache[msg.pageID]
	for _, p := range msg.patches {
		notion.PatchBlockIDInTree(blocks, p[0], p[1])
	}
	// queued ops may reference an ID this sync just rewrote — including
	// as their parent (a block created inside a still-pending toggle)
	for i := range m.moveQueue {
		for _, p := range msg.patches {
			if m.moveQueue[i].blockID == p[0] {
				m.moveQueue[i].blockID = p[1]
			}
			if m.moveQueue[i].parentID == p[0] {
				m.moveQueue[i].parentID = p[1]
			}
		}
	}
	// an ID patch changes nothing visible: update the live units in place
	// instead of re-rendering the whole page (which lagged rapid entry)
	if m.selected != nil && m.selected.ID == msg.pageID {
		for i := range m.pv.units {
			for _, p := range msg.patches {
				if m.pv.units[i].ID() == p[0] {
					if node, ok := notion.FindNode(blocks, p[1]); ok {
						m.pv.units[i].Node = node
					}
				}
				if m.pv.units[i].ParentID == p[0] {
					m.pv.units[i].ParentID = p[1]
				}
			}
		}
	}
	if cmd := m.flushNextMove(); cmd != nil {
		return m, cmd
	}
	m.statusMsg = "synced"
	return m, nil
}

func (m *Model) cursorToBlock(id string) {
	for i, u := range m.pv.units {
		if u.ID() == id {
			m.pv.cursor = i
			break
		}
	}
	m.syncViewer()
}
