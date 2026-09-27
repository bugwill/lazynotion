package ui

import (
	"context"
	"fmt"
	"image"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/jomei/notionapi"

	"github.com/justinm35/lazynotion/internal/cache"
	"github.com/justinm35/lazynotion/internal/convert"
	"github.com/justinm35/lazynotion/internal/notion"
)

type focusArea int

const (
	focusSidebar focusArea = iota
	focusViewer
)

type inputMode int

const (
	inputNone inputMode = iota
	inputSearch
	inputAppend
	inputNewPage
	inputFind
	inputIcon
	inputTitle
)

// Workspace pairs a display name with its own API client (each token gets
// its own rate limiter).
type Workspace struct {
	Name   string
	Client *notion.Client
	// RootOnly limits the browse list to workspace-level pages; explicit
	// searches still find nested pages
	RootOnly  bool
	RootPages string
}

type pagesMsg struct {
	pages      []notion.Page
	query      string
	wsGen      int
	fromCache  bool
	background bool
}

type pageMsg struct {
	pageID string
	blocks []notion.BlockNode
	wsGen  int
	page   notion.Page
	// fromCache marks a disk-cache hit that still needs revalidation;
	// refreshed marks revalidation finding newer content
	fromCache    bool
	cachedEdited time.Time
	refreshed    bool
}

type errMsg struct{ err error }

// writeErrMsg reloads the page to resync after a failed optimistic write.
type writeErrMsg struct {
	pageID string
	err    error
}

type writeDoneMsg struct {
	pageID string
	status string
	reload bool
}

type pageCreatedMsg struct {
	page     notion.Page
	parentID string
}

// historyEntry remembers where the reader was when drilling into a
// sub-page, so esc returns to the same block and scroll position. Entries
// with db set restore a database grid instead (cursor and rows intact).
type historyEntry struct {
	page   notion.Page
	cursor int
	offset int
	recent *recentState
	db     *dbState
}

type cursorRestore struct {
	pageID string
	cursor int
	offset int
}

type editorDoneMsg struct {
	page notion.Page
	path string
	orig string
	err  error
}

// draftBlockID marks a block that exists only locally: created by `n`,
// synced to Notion when the inline edit is saved.
const draftBlockID = "draft-block"

type pendingReplace struct {
	page    notion.Page
	blocks  []notionapi.Block
	warning string
}

type pageItem struct{ page notion.Page }

func (i pageItem) FilterValue() string { return i.page.Title }

type Model struct {
	client         *notion.Client
	recent         *recentState
	store          *cache.Store
	workspaces     []Workspace
	wsIndex        int
	wsGen          int
	pagesQuery     *queryState
	recentQuery    *queryState
	recentLoader   *recentLoader
	recentSequence uint64
	sidebar        list.Model
	viewer         viewport.Model
	pv             pageView
	input          textinput.Model
	editArea       textarea.Model
	editing        bool
	editOrig       string
	editAnchor     int
	editScroll     int // mirrors the textarea's private vertical offset
	editDragging   bool
	editUndo       []editUndoState
	mode           inputMode
	focus          focusArea
	loading        bool
	pageLoading    bool
	lastQuery      string
	selected       *notion.Page
	db             *dbState // non-nil: the viewer shows a database grid
	defaultPages   []notion.Page
	startupPending bool
	renderedID     string
	history        []historyEntry
	pendingRestore *cursorRestore
	blockCache     map[string][]notion.BlockNode
	images         map[string]image.Image
	imagesMode     string
	kittyImgs      map[string]kittyPlacement
	kittySeq       int
	kittyNoticed   bool
	collapsed      map[string]bool
	confirm        *pendingReplace
	newPageParent  *notion.Page
	showHelp       bool
	undoStack      []undoRecord
	visualAnchor   int
	readSelection  *readingSelection
	readPending    *readingPress
	moveQueue      []moveOp
	moveSyncing    bool
	writesInFlight int
	pendingSeq     int
	findQuery      string
	paletteOpen    bool
	paletteQuery   string
	paletteIndex   int
	statusMsg      string
	err            error
	width          int
	height         int
}

// New builds the app model; store may be nil (caching disabled).
// imagesMode is "auto" (detect kitty/ghostty), "pixels" or "mosaic".
func New(workspaces []Workspace, startIndex int, store *cache.Store, imagesMode string) Model {
	sidebar := list.New([]list.Item{pageItem{page: recentPage}}, pageDelegate{}, 0, 0)
	sidebar.SetShowTitle(false) // the pane border carries the title
	sidebar.SetShowHelp(false)
	sidebar.SetFilteringEnabled(false)
	sidebar.SetShowStatusBar(false)

	editArea := textarea.New()
	editArea.ShowLineNumbers = false
	editArea.Prompt = ""
	// long text is split into 2000-char rich-text runs on save, so the
	// editor itself doesn't need Notion's per-run cap
	editArea.CharLimit = 0

	m := Model{
		client:         workspaces[startIndex].Client,
		recentLoader:   &recentLoader{},
		store:          store,
		workspaces:     workspaces,
		wsIndex:        startIndex,
		sidebar:        sidebar,
		viewer:         viewport.New(0, 0),
		input:          textinput.New(),
		editArea:       editArea,
		loading:        true,
		startupPending: true,
		blockCache:     make(map[string][]notion.BlockNode),
		images:         make(map[string]image.Image),
		imagesMode:     imagesMode,
		kittyImgs:      make(map[string]kittyPlacement),
		collapsed:      make(map[string]bool),
		visualAnchor:   -1,
		editAnchor:     -1,
	}
	if startsInRecent(workspaces[startIndex].RootPages) {
		m.selected = &recentPage
		m.recent = &recentState{}
		m.focus = focusViewer
		m.pageLoading = true
		m.startupPending = false
	}
	return m
}

func sidebarTitle(workspaces []Workspace, index int) string {
	if len(workspaces) > 1 {
		return "Pages · " + workspaces[index].Name
	}
	return "Pages"
}

func (m Model) switchWorkspace() (tea.Model, tea.Cmd) {
	if len(m.workspaces) < 2 {
		return m, nil
	}
	m.wsIndex = (m.wsIndex + 1) % len(m.workspaces)
	m.wsGen++
	m.recentLoader.cancel()
	if m.pagesQuery != nil && m.pagesQuery.cancel != nil {
		m.pagesQuery.cancel()
	}
	m.pagesQuery, m.recentQuery = nil, nil
	ws := m.workspaces[m.wsIndex]
	m.client = ws.Client
	m.selected = nil
	m.db = nil
	m.recent = nil
	m.history = nil
	m.defaultPages = nil
	m.startupPending = true
	m.renderedID = ""
	m.pv = pageView{}
	m.blockCache = make(map[string][]notion.BlockNode)
	m.images = make(map[string]image.Image)
	m.collapsed = make(map[string]bool)
	m.focus = focusSidebar
	m.loading = true
	m.pageLoading = false
	m.err = nil
	m.lastQuery = ""
	m.statusMsg = "workspace: " + ws.Name
	cmd := m.sidebar.SetItems(nil)
	return m, tea.Batch(cmd, m.loadPages(""))
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.loadPages("")}
	if m.recent != nil {
		cmds = append(cmds, m.loadRecent(false))
	}
	return tea.Batch(cmds...)
}

func (m Model) loadPages(query string) tea.Cmd {
	return m.fetchPages(query, false)
}

func (m Model) fetchPages(query string, force bool) tea.Cmd {
	client, gen := m.client, m.wsGen
	// the root filter shapes the browse list only; a typed search should
	// still reach nested pages
	rootOnly := m.workspaces[m.wsIndex].RootOnly && strings.TrimSpace(query) == ""
	store := m.store
	key := ""
	if client != nil {
		key = fmt.Sprintf("%s:%t:%s", client.CacheKey(), rootOnly, query)
	}
	return func() tea.Msg {
		if !force && store != nil {
			if pages, ok := store.LoadPages(key); ok {
				return pagesMsg{pages: pages, query: query, wsGen: gen, fromCache: true}
			}
		}
		return streamQueryWithContext("pages", gen, 30*time.Second, func(ctx context.Context, report func(notion.QueryProgress)) tea.Msg {
			pages, err := client.SearchWithProgress(ctx, query, rootOnly, report)
			if err != nil {
				return errMsg{err}
			}
			if store != nil {
				_ = store.SavePages(key, pages)
			}
			return pagesMsg{pages: pages, query: query, wsGen: gen, background: force}
		})
	}
}

func (m Model) loadPage(page notion.Page, force bool) tea.Cmd {
	client, gen, store := m.client, m.wsGen, m.store
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if !force && store != nil {
			if nodes, edited, ok := store.Load(page.ID); ok {
				return pageMsg{
					pageID: page.ID, blocks: nodes, wsGen: gen, page: page,
					fromCache: true, cachedEdited: edited,
				}
			}
		}
		blocks, err := client.PageBlocks(ctx, page.ID)
		if err != nil {
			return errMsg{err}
		}
		if store != nil {
			_ = store.Save(page.ID, page.LastEdited, blocks)
		}
		return pageMsg{pageID: page.ID, blocks: blocks, wsGen: gen, page: page}
	}
}

// revalidatePage runs after a disk-cache hit: one metadata request decides
// whether the cached copy is stale; only then are blocks refetched. Errors
// are silent — the cached copy stays on screen.
func (m Model) revalidatePage(page notion.Page, cachedEdited time.Time) tea.Cmd {
	client, gen, store := m.client, m.wsGen, m.store
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		live, err := client.PageLastEdited(ctx, page.ID)
		if err != nil || !live.After(cachedEdited) {
			return nil
		}
		blocks, err := client.PageBlocks(ctx, page.ID)
		if err != nil {
			return nil
		}
		if store != nil {
			_ = store.Save(page.ID, live, blocks)
		}
		return pageMsg{pageID: page.ID, blocks: blocks, wsGen: gen, page: page, refreshed: true}
	}
}

// prefetchChildren quietly loads a few uncached sub-pages of the page being
// read, so drilling in feels instant.
func (m Model) prefetchChildren(blocks []notion.BlockNode) tea.Cmd {
	var cmds []tea.Cmd
	for _, u := range convert.Flatten(blocks) {
		if len(cmds) >= 4 {
			break
		}
		cp, ok := u.Node.Block.(*notionapi.ChildPageBlock)
		if !ok {
			continue
		}
		id := u.ID()
		if _, inMemory := m.blockCache[id]; inMemory {
			continue
		}
		if m.store != nil && m.store.Has(id) {
			continue
		}
		page := notion.Page{ID: id, Title: cp.ChildPage.Title}
		if t := u.Node.Block.GetLastEditedTime(); t != nil {
			page.LastEdited = *t
		}
		cmds = append(cmds, m.loadPage(page, false))
	}
	return tea.Batch(cmds...)
}

// Update keeps reading selections consistent with the current page.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.update(msg)
	model, ok := next.(Model)
	if !ok {
		return next, cmd
	}
	if model.readSelection != nil && (model.selected == nil || model.selected.ID != model.readSelection.pageID || model.editing || model.db != nil || model.recent != nil) {
		model.readSelection = nil
	}
	if model.readPending != nil && (model.selected == nil || model.selected.ID != model.readPending.pageID || model.editing || model.db != nil || model.recent != nil) {
		model.readPending = nil
	}
	return model, cmd
}

func (m Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.MouseMsg:
		if m.titleClicked(msg) {
			return m.startTitleEdit()
		}
		if m.editing && !m.showHelp && !m.paletteOpen && m.confirm == nil && m.mode == inputNone {
			return m.updateEditorMouse(msg)
		}
		if !m.editing && !m.showHelp && !m.paletteOpen && m.confirm == nil && m.mode == inputNone {
			if m.readPending != nil && (msg.Action == tea.MouseActionMotion || msg.Action == tea.MouseActionRelease) {
				return m.continueReadingPress(msg)
			}
			if m.readSelection != nil && !m.readSelection.dragging && msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress {
				return m.startReadingPress(msg)
			}
		}
		if m.readSelection != nil && m.readSelection.dragging && !m.editing && !m.showHelp && !m.paletteOpen && m.mode == inputNone && m.confirm == nil && (msg.Action == tea.MouseActionMotion || msg.Action == tea.MouseActionRelease) {
			return m.updateReadingMouse(msg)
		}
		if m.showHelp || m.paletteOpen || m.editing || m.confirm != nil || m.mode != inputNone ||
			msg.X <= 0 || msg.X >= m.width-1 || msg.Y <= 0 || msg.Y >= m.height-m.footerHeight()-1 {
			return m, nil
		}
		switch msg.Button {
		case tea.MouseButtonLeft:
			if msg.Action == tea.MouseActionPress {
				return m.activateMouseRow(msg)
			}
		case tea.MouseButtonWheelUp, tea.MouseButtonWheelDown:
			// Mobile users navigate in the finger's direction: a downward
			// swipe (wheel-up input) advances. One row per event keeps lists
			// controllable when Whip emits several events per gesture.
			step := 1
			if msg.Button == tea.MouseButtonWheelDown {
				step = -step
			}
			if m.selected == nil && m.focus == focusSidebar {
				count := len(m.sidebar.VisibleItems())
				if count > 0 {
					m.sidebar.Select(clamp(m.sidebar.Index()+step, 0, count-1))
				}
				return m, nil
			}
			if m.focus != focusViewer {
				return m, nil
			}
			if m.recent != nil {
				m.recent.cursor = clamp(m.recent.cursor+step, 0, max(len(m.recent.pages)-1, 0))
				return m, nil
			}
			if m.db != nil {
				m.db.cursor = clamp(m.db.cursor+step, 0, max(len(m.db.rows)-1, 0))
				return m, nil
			}
			if m.selected != nil {
				// Content follows standard wheel direction; lists above move
				// their cursor in the tablet finger's direction.
				step = -step
				if step < 0 {
					m.viewer.ScrollUp(-step)
				} else {
					m.viewer.ScrollDown(step)
				}
			}
		}
		return m, nil
	case queryEvent:
		return m.handleQueryEvent(msg)

	case tea.WindowSizeMsg:
		if msg.Width <= 0 || msg.Height <= 0 || msg.Width == m.width && msg.Height == m.height {
			return m, nil
		}
		anchor := m.captureResizeAnchor()
		m.readPending = nil
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		m.rebuildPage(true)
		if !m.editing {
			m.restoreResizeAnchor(anchor)
		}
		if m.editing {
			if unit, ok := m.pv.current(); ok {
				m.editArea.SetWidth(max(m.viewer.Width-gutterWidth-2*unit.Depth, 20))
				m.resizeEditArea()
				m.settleEditScroll()
				m.syncViewer()
			}
		}
		return m, nil

	case pagesMsg:
		if msg.background && msg.query != m.lastQuery {
			return m, nil
		}
		if msg.wsGen != m.wsGen {
			return m, nil
		}
		m.loading = false
		m.lastQuery = msg.query
		if msg.query == "" {
			msg.pages = withRecent(msg.pages)
			m.defaultPages = msg.pages
		}
		prevID := ""
		if it, ok := m.sidebar.SelectedItem().(pageItem); ok {
			prevID = it.page.ID
		}
		items := make([]list.Item, len(msg.pages))
		for i, p := range msg.pages {
			items[i] = pageItem{page: p}
		}
		cmd := m.sidebar.SetItems(items)
		m.sidebar.ResetSelected()
		for i, p := range msg.pages {
			if prevID != "" && p.ID == prevID {
				m.sidebar.Select(i)
				break
			}
		}
		cmds := []tea.Cmd{cmd}
		if msg.fromCache {
			cmds = append(cmds, m.fetchPages(msg.query, true))
		}
		if msg.query == "" && m.startupPending {
			m.startupPending = false
			if startsInRecent(m.workspaces[m.wsIndex].RootPages) {
				next, openCmd := m.openRecent()
				return next, tea.Batch(append(cmds, openCmd)...)
			}
		}
		return m, tea.Batch(cmds...)

	case recentMsg:
		return m.handleRecent(msg)

	case pageMsg:
		if msg.wsGen != m.wsGen {
			return m, nil
		}
		// the local tree is the freshest state while edits or syncs are
		// outstanding: a background refresh (revalidation, stale reload)
		// replacing it would wipe drafts and pending blocks mid-typing
		if _, cached := m.blockCache[msg.pageID]; cached &&
			m.selected != nil && m.selected.ID == msg.pageID && m.localBusy() {
			m.pageLoading = false
			return m, nil
		}
		if !msg.fromCache {
			m.recentLoader.remember(m.client, m.store, msg.page, false)
		}
		m.blockCache[msg.pageID] = msg.blocks
		selected := m.selected != nil && m.selected.ID == msg.pageID
		var cmds []tea.Cmd
		if selected {
			m.pageLoading = false
			m.rebuildPage(true)
			if msg.refreshed {
				m.statusMsg = "updated from notion"
			}
			cmds = append(cmds, m.fetchImages(msg.pageID, convert.Flatten(msg.blocks)))
			cmds = append(cmds, m.prefetchChildren(msg.blocks))
			if msg.page.Cover != "" && m.images[coverKey(msg.pageID)] == nil {
				cmds = append(cmds, downloadImage(msg.pageID, coverKey(msg.pageID), msg.page.Cover))
			}
		}
		if msg.fromCache {
			cmds = append(cmds, m.revalidatePage(msg.page, msg.cachedEdited))
		}
		return m, tea.Batch(cmds...)

	case imageMsg:
		if msg.err != nil || msg.img == nil {
			return m, nil
		}
		m.images[msg.blockID] = msg.img
		if m.selected != nil && m.selected.ID == msg.pageID {
			m.rebuildPage(true)
		}
		if !m.kittyEnabled() && !m.kittyNoticed {
			if hint := kittySupportHint(); hint != "" {
				m.kittyNoticed = true
				m.statusMsg = hint
			}
		}
		if _, placed := m.kittyImgs[msg.blockID]; m.kittyEnabled() && !placed {
			m.kittySeq++
			bounds := msg.img.Bounds()
			maxCols, maxRows := maxImageCellsW, maxImageCellsH
			if strings.HasPrefix(msg.blockID, coverKeyPrefix) {
				maxCols, maxRows = maxCoverCols, maxCoverRows
			}
			cols, rows := kittyCellDims(bounds.Dx(), bounds.Dy(), maxCols, maxRows)
			if cols > 0 {
				placement := kittyPlacement{id: m.kittySeq, cols: cols, rows: rows}
				return m, transmitKittyImage(msg.pageID, msg.blockID, msg.img, placement)
			}
		}
		return m, nil

	case kittyMsg:
		if msg.err != nil {
			// mosaic stays — but say so once, or a silent fallback would
			// be indistinguishable from a broken sharp-image path
			if !m.kittyNoticed {
				m.kittyNoticed = true
				m.statusMsg = "sharp images unavailable (" + msg.err.Error() + ") — using mosaic"
			}
			return m, nil
		}
		if !m.kittyNoticed {
			m.kittyNoticed = true
			m.statusMsg = "sharp images active (kitty graphics)"
		}
		m.kittyImgs[msg.blockID] = msg.placement
		if m.selected != nil && m.selected.ID == msg.pageID {
			m.rebuildPage(true)
		}
		return m, nil

	case writeDoneMsg:
		m.writesInFlight = max(m.writesInFlight-1, 0)
		m.statusMsg = msg.status
		if m.selected != nil && m.selected.ID == msg.pageID {
			m.recentLoader.remember(m.client, m.store, *m.selected, true)
		}
		if m.store != nil {
			m.store.Invalidate(msg.pageID)
		}
		if msg.reload {
			// drop the stale caches even when the page isn't on screen
			// (an undo can target a page navigated away from)
			delete(m.blockCache, msg.pageID)
			if m.selected != nil && m.selected.ID == msg.pageID {
				m.pageLoading = true
				return m, m.loadPage(*m.selected, true)
			}
		}
		return m, nil

	case titleSavedMsg:
		return m.handleTitleSaved(msg)

	case writeErrMsg:
		m.writesInFlight = max(m.writesInFlight-1, 0)
		m.err = msg.err
		delete(m.blockCache, msg.pageID)
		if m.store != nil {
			m.store.Invalidate(msg.pageID)
		}
		if m.selected != nil && m.selected.ID == msg.pageID {
			m.pageLoading = true
			return m, m.loadPage(*m.selected, true)
		}
		return m, nil

	case pageCreatedMsg:
		m.statusMsg = fmt.Sprintf("created %q", msg.page.Title)
		recentCreated := msg.page
		recentCreated.ParentType, recentCreated.ParentID = "page_id", msg.parentID
		m.recentLoader.remember(m.client, m.store, recentCreated, true)
		if msg.parentID != "" {
			delete(m.blockCache, msg.parentID)
			if m.store != nil {
				m.store.Invalidate(msg.parentID)
			}
		}
		m.history = nil
		next, openCmd := m.openPage(msg.page)
		model := next.(Model)
		return model, tea.Batch(openCmd, model.loadPages(model.lastQuery))
	case dbLoadedMsg:
		return m.handleDBLoaded(msg)

	case dbRowsMsg:
		return m.handleDBRows(msg)

	case moveDoneMsg:
		return m.handleMoveDone(msg)

	case clipMsg:
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.statusMsg = msg.label
		}
		return m, nil

	case editorDoneMsg:
		return m.handleEditorDone(msg)

	case errMsg:
		m.loading = false
		m.pageLoading = false
		m.err = msg.err
		return m, nil

	case tea.KeyMsg:
		m.statusMsg = ""
		m.readPending = nil
		if m.readSelection != nil && !m.editing && m.mode == inputNone && !m.showHelp && !m.paletteOpen && m.confirm == nil {
			if msg.String() == "b" {
				return m.boldReadingSelection()
			}
			m.readSelection = nil
			m.viewerSetContent()
			if msg.String() == "esc" {
				return m, nil
			}
		}
		if m.showHelp {
			if msg.String() == "ctrl+c" {
				return m, tea.Quit
			}
			m.showHelp = false
			return m, nil
		}
		if m.confirm != nil {
			return m.updateConfirm(msg)
		}
		if m.editing {
			return m.updateInlineEdit(msg)
		}
		if m.mode != inputNone {
			return m.updateInput(msg)
		}
		return m.updateKeys(msg)
	}
	return m, nil
}

func (m Model) updateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		pending := m.confirm
		m.confirm = nil
		m.statusMsg = "syncing page to notion…"
		return m, m.replacePage(pending.page, pending.blocks)
	case "ctrl+c":
		return m, tea.Quit
	default:
		m.confirm = nil
		m.statusMsg = "edit discarded"
		return m, nil
	}
}

func (m Model) updateInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = inputNone
		m.input.Blur()
		return m, nil
	case "ctrl+c":
		return m, tea.Quit
	case "enter":
		value := m.input.Value()
		mode := m.mode
		m.mode = inputNone
		m.input.Blur()
		return m.submitInput(mode, value)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m Model) submitInput(mode inputMode, value string) (tea.Model, tea.Cmd) {
	switch mode {
	case inputTitle:
		return m.saveTitle(value)
	case inputSearch:
		m.selected = nil
		m.db = nil
		m.recent = nil
		m.history = nil
		m.focus = focusSidebar
		m.loading = true
		m.err = nil
		return m, m.loadPages(value)

	case inputAppend:
		if m.selected == nil || strings.TrimSpace(value) == "" {
			return m, nil
		}
		blocks := convert.ParseMarkdown(value)
		if len(blocks) == 0 {
			return m, nil
		}
		nodes, pendingIDs := m.mintPendingNodes(blocks)
		anchor := m.appendAnchor()
		existing := m.blockCache[m.selected.ID]
		inserted := make([]notion.BlockNode, 0, len(existing)+len(nodes))
		placed := false
		for _, b := range existing {
			inserted = append(inserted, b)
			if !placed && anchor != "" && b.Block.GetID().String() == anchor {
				inserted = append(inserted, nodes...)
				placed = true
			}
		}
		if !placed {
			inserted = append(inserted, nodes...)
		}
		m.blockCache[m.selected.ID] = inserted
		m.rebuildPage(true)
		m.statusMsg = "adding blocks…"
		m.enqueueCreate(m.selected.ID, pendingIDs, blocks)
		if !m.moveSyncing {
			if cmd := m.flushNextMove(); cmd != nil {
				return m, cmd
			}
		}
		return m, nil

	case inputFind:
		m.findQuery = value
		if strings.TrimSpace(value) == "" {
			return m, nil
		}
		return m.jumpToMatch(0)

	case inputIcon:
		if m.selected == nil || strings.TrimSpace(value) == "" {
			return m, nil
		}
		payload, newIcon, ok := parseIconInput(strings.TrimSpace(value))
		if !ok {
			m.statusMsg = `icon must be an emoji or "name color" (e.g. target red)`
			return m, nil
		}
		m.selected.Icon = newIcon
		var cmds []tea.Cmd
		for i, it := range m.sidebar.Items() {
			if pi, isPage := it.(pageItem); isPage && pi.page.ID == m.selected.ID {
				pi.page.Icon = newIcon
				cmds = append(cmds, m.sidebar.SetItem(i, pi))
			}
		}
		client := m.client
		pageID := m.selected.ID
		m.writesInFlight++
		cmds = append(cmds, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := client.SetPageIcon(ctx, pageID, payload); err != nil {
				return writeErrMsg{pageID: pageID, err: err}
			}
			return writeDoneMsg{pageID: pageID, status: "icon updated"}
		})
		return m, tea.Batch(cmds...)

	case inputNewPage:
		parent := m.newPageParent
		m.newPageParent = nil
		title := strings.TrimSpace(value)
		if parent == nil || title == "" {
			return m, nil
		}
		m.statusMsg = "creating page…"
		client := m.client
		parentID := parent.ID
		return m, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			page, err := client.CreatePage(ctx, parentID, title)
			if err != nil {
				return errMsg{err}
			}
			return pageCreatedMsg{page: page, parentID: parentID}
		}

	}
	return m, nil
}

func (m Model) startInlineEdit() (tea.Model, tea.Cmd) {
	unit, ok := m.pv.current()
	if !ok || m.selected == nil {
		return m, nil
	}
	if isPendingID(unit.ID()) && unit.ID() != draftBlockID {
		m.statusMsg = "block is still syncing — try again in a moment"
		return m, nil
	}
	text, editable := unit.EditableMarkdown()
	if !editable {
		m.statusMsg = fmt.Sprintf("%s blocks aren't editable inline", unit.Node.Block.GetType())
		return m, nil
	}
	m.editing = true
	m.editOrig = text
	m.editAnchor = -1
	m.editUndo = nil
	m.setEditValue(text)
	m.editArea.SetWidth(max(m.viewer.Width-gutterWidth-2*unit.Depth, 20))
	m.resizeEditArea()
	m.syncViewer()
	return m, m.editArea.Focus()
}

// yankSelection extracts the visual range (or just the cursor block) as
// markdown, one block per line group.
func (m Model) yankSelection() (string, int) {
	if len(m.pv.units) == 0 {
		return "", 0
	}
	lo, hi := m.pv.cursor, m.pv.cursor
	if m.visualAnchor >= 0 {
		lo = min(m.visualAnchor, m.pv.cursor)
		hi = max(m.visualAnchor, m.pv.cursor)
	}
	lo = clamp(lo, 0, len(m.pv.units)-1)
	hi = clamp(hi, 0, len(m.pv.units)-1)
	parts := make([]string, 0, hi-lo+1)
	for i := lo; i <= hi; i++ {
		parts = append(parts, blockYankText(m.pv.units[i]))
	}
	return strings.Join(parts, "\n"), hi - lo + 1
}

func blockYankText(u convert.Unit) string {
	if text, ok := u.EditableMarkdown(); ok {
		return text
	}
	// code blocks, tables, images: the fragment markdown, with display
	// hard-breaks normalized back to plain newlines
	return strings.ReplaceAll(u.Markdown, "  \n", "\n")
}

// saveAndContinue commits the current block and opens a fresh draft below,
// pre-seeded with a list marker when continuing a list.
func (m Model) saveAndContinue(seed string) (tea.Model, tea.Cmd) {
	next, cmd := m.saveInlineEdit()
	model := next.(Model)
	if model.editing {
		return model, cmd
	}
	nextModel, newCmd := model.newBlockBelow()
	continued := nextModel.(Model)
	if continued.editing && seed != "" {
		continued.setEditValue(seed)
		continued.editArea.CursorEnd()
		continued.resizeEditArea()
		continued.syncViewer()
	}
	return continued, tea.Batch(cmd, newCmd)
}

// Validation failures keep the editor open, including its selection and undo history.
func (m Model) saveInlineEdit() (tea.Model, tea.Cmd) {
	value, anchor, undo := m.editArea.Value(), m.editAnchor, m.editUndo
	m.stopInlineEdit()
	next, cmd := m.commitInlineEdit(value)
	model := next.(Model)
	if model.editing {
		model.editAnchor, model.editUndo = anchor, undo
		model.syncViewer()
	}
	return model, cmd
}

func (m *Model) stopInlineEdit() {
	m.editing = false
	m.editDragging = false
	m.editAnchor = -1
	m.editUndo = nil
	m.editArea.Blur()
	if m.paletteOpen {
		m.closePalette()
	}
}

func (m Model) commitInlineEdit(value string) (tea.Model, tea.Cmd) {
	unit, ok := m.pv.current()
	if !ok || m.selected == nil {
		m.syncViewer()
		return m, nil
	}
	if unit.ID() == draftBlockID {
		return m.commitDraft(unit, value)
	}
	if value == m.editOrig {
		m.syncViewer()
		return m, nil
	}
	block := unit.Node.Block
	if _, isTable := block.(*notionapi.TableBlock); isTable {
		if strings.TrimSpace(value) == "" {
			m.editing = true
			m.statusMsg = "table edit must remain one markdown table"
			m.syncViewer()
			return m, m.editArea.Focus()
		}
		return m.commitTableEdit(unit, value)
	}
	patch := convert.ParseEditPatch(value)
	origKind := string(block.GetType())

	// no marker typed on a marker-less block type (toggle, callout, …)
	// keeps that type; otherwise a changed marker means a conversion
	markerless := patch.Kind == "paragraph" && !markeredKind(origKind)
	if patch.Kind == origKind || markerless {
		return m.saveBlockPatch(unit, patch)
	}
	if unit.TopLevel && len(unit.Node.Children) == 0 {
		if ok, _ := notion.CanRecreate(unit.Node); ok {
			return m.retypeBlock(unit, patch)
		}
	}
	next, cmd := m.saveBlockPatch(unit, patch)
	model := next.(Model)
	model.statusMsg = fmt.Sprintf("kept as %s — this block can't be converted here", origKind)
	return model, cmd
}

func markeredKind(kind string) bool {
	switch kind {
	case "paragraph", "heading_1", "heading_2", "heading_3",
		"bulleted_list_item", "numbered_list_item", "to_do", "quote", "toggle":
		return true
	}
	return false
}

// saveBlockPatch updates a block's text (and checked state) in place.
func (m Model) saveBlockPatch(unit convert.Unit, patch convert.EditPatch) (tea.Model, tea.Cmd) {
	block := unit.Node.Block
	prev, _ := notion.LocalRichText(block)
	notion.SetLocalRichText(block, patch.RichText)

	client := m.client
	pageID := m.selected.ID
	blockID := unit.ID()

	if todo, isTodo := block.(*notionapi.ToDoBlock); isTodo {
		prevChecked := todo.ToDo.Checked
		todo.ToDo.Checked = patch.Checked
		m.rebuildPage(true)
		m.pushUndo("block edit", pageID, func(ctx context.Context) error {
			return client.SetToDo(ctx, blockID, prev, prevChecked)
		})
		rts, checked := patch.RichText, patch.Checked
		m.writesInFlight++
		return m, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := client.SetToDo(ctx, blockID, rts, checked); err != nil {
				return writeErrMsg{pageID: pageID, err: err}
			}
			return writeDoneMsg{pageID: pageID, status: "block updated"}
		}
	}

	m.rebuildPage(true)
	m.pushUndo("block edit", pageID, func(ctx context.Context) error {
		return client.SetBlockRichText(ctx, block, prev)
	})
	rts := patch.RichText
	m.writesInFlight++
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := client.SetBlockRichText(ctx, block, rts); err != nil {
			return writeErrMsg{pageID: pageID, err: err}
		}
		return writeDoneMsg{pageID: pageID, status: "block updated"}
	}
}

// commitTableEdit saves an edited table. Content-only changes update the
// changed rows in place (identity preserved); shape changes (rows/columns
// added or removed) rebuild the table, since the API fixes table_width at
// creation.
func (m Model) commitTableEdit(unit convert.Unit, value string) (tea.Model, tea.Cmd) {
	parsed := convert.ParseMarkdown(value)
	var newTable *notionapi.TableBlock
	if len(parsed) == 1 {
		newTable, _ = parsed[0].(*notionapi.TableBlock)
	}
	if newTable == nil {
		// don't lose the user's edit: explain and reopen the editor
		m.statusMsg = "a table edit must stay one markdown table — check the | rows"
		m.editing = true
		m.syncViewer()
		return m, m.editArea.Focus()
	}

	oldRows := unit.Node.Children
	newRows := newTable.Table.Children
	origTable := unit.Node.Block.(*notionapi.TableBlock)
	sameShape := len(oldRows) == len(newRows) && newTable.Table.TableWidth == origTable.Table.TableWidth

	client := m.client
	pageID := m.selected.ID
	if sameShape {
		type rowUpdate struct {
			rowID string
			cells [][]notionapi.RichText
			prev  [][]notionapi.RichText
		}
		var updates []rowUpdate
		for i := range oldRows {
			oldRow, okOld := oldRows[i].Block.(*notionapi.TableRowBlock)
			newRow, okNew := newRows[i].(*notionapi.TableRowBlock)
			if !okOld || !okNew {
				continue
			}
			if tableRowMarkdown(oldRow.TableRow.Cells) == tableRowMarkdown(newRow.TableRow.Cells) {
				continue
			}
			updates = append(updates, rowUpdate{
				rowID: oldRow.ID.String(),
				cells: newRow.TableRow.Cells,
				prev:  oldRow.TableRow.Cells,
			})
			oldRow.TableRow.Cells = newRow.TableRow.Cells // optimistic
		}
		if len(updates) == 0 {
			m.syncViewer()
			return m, nil
		}
		m.rebuildPage(true)
		restore := make([]rowUpdate, len(updates))
		copy(restore, updates)
		m.pushUndo("table edit", pageID, func(ctx context.Context) error {
			for _, u := range restore {
				if err := client.UpdateTableRow(ctx, u.rowID, u.prev); err != nil {
					return err
				}
			}
			return nil
		})
		m.writesInFlight++
		return m, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			for _, u := range updates {
				if err := client.UpdateTableRow(ctx, u.rowID, u.cells); err != nil {
					return writeErrMsg{pageID: pageID, err: err}
				}
			}
			return writeDoneMsg{pageID: pageID, status: "table updated"}
		}
	}

	// shape changed: rebuild the table in place (new IDs, like moves)
	origID := unit.ID()
	container := unit.ParentID
	if container == "" {
		container = pageID
	}
	blocks := m.blockCache[pageID]
	replacement := convert.NodesFromBlocks([]notionapi.Block{newTable})
	if replaced, ok := notion.ReplaceNode(blocks, origID, replacement); ok {
		m.blockCache[pageID] = replaced
	}
	m.rebuildPage(true)
	m.statusMsg = "rebuilding table…"
	m.writesInFlight++
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if _, err := client.AppendBlocks(ctx, container, origID, []notionapi.Block{newTable}); err != nil {
			return writeErrMsg{pageID: pageID, err: err}
		}
		if err := client.DeleteBlock(ctx, origID); err != nil {
			return writeErrMsg{pageID: pageID, err: err}
		}
		return writeDoneMsg{pageID: pageID, status: "table rebuilt", reload: true}
	}
}

func tableRowMarkdown(cells [][]notionapi.RichText) string {
	parts := make([]string, len(cells))
	for i, cell := range cells {
		parts[i] = convert.InlineMarkdown(cell)
	}
	return strings.Join(parts, "|")
}

// retypeBlock converts a block by recreating it as the new kind (append
// after the original, then archive the original — the API cannot change a
// block's type in place). Comments on the block are lost.
func (m Model) retypeBlock(unit convert.Unit, patch convert.EditPatch) (tea.Model, tea.Cmd) {
	newBlock := convert.BuildBlock(patch.Kind, patch.RichText, patch.Checked)
	pageID := m.selected.ID
	origID := unit.ID()

	blocks := m.blockCache[pageID]
	for i, b := range blocks {
		if b.Block.GetID().String() == origID {
			blocks[i] = convert.NodesFromBlocks([]notionapi.Block{newBlock})[0]
			break
		}
	}
	m.rebuildPage(true)
	m.statusMsg = "converting block…"

	client := m.client
	m.writesInFlight++
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if _, err := client.AppendBlocks(ctx, pageID, origID, []notionapi.Block{newBlock}); err != nil {
			return writeErrMsg{pageID: pageID, err: err}
		}
		if err := client.DeleteBlock(ctx, origID); err != nil {
			return writeErrMsg{pageID: pageID, err: err}
		}
		return writeDoneMsg{pageID: pageID, status: "block converted", reload: true}
	}
}

func (m *Model) editAreaRows() int {
	w := max(m.editArea.Width(), 1)
	rows := 0
	for _, line := range strings.Split(m.editArea.Value(), "\n") {
		rows += len(wrapEditorLine([]rune(line), w))
	}
	return rows
}

// maxEditRows lets the edit box grow to fill the pane; beyond that the
// textarea scrolls internally (the outer viewport can't track the cursor
// inside the box, so the box must always fit on screen).
func (m Model) maxEditRows() int {
	return max(m.viewer.Height-2, 10)
}

// resizeEditArea sizes the box exactly to its wrapped content, so the edit
// gutter matches the block's real height.
func (m *Model) resizeEditArea() {
	m.editArea.SetHeight(clamp(m.editAreaRows(), 1, m.maxEditRows()))
}

// preGrowEditArea runs before a keystroke reaches the textarea: one spare
// row means a new line (or a wrap) never triggers the textarea's internal
// scroll, which would permanently hide the first line. resizeEditArea snaps
// back to the exact height afterwards.
func (m *Model) preGrowEditArea() {
	m.editArea.SetHeight(clamp(m.editAreaRows()+1, 2, m.maxEditRows()))
}

// commitDraft syncs a `n`-created block. The typed markdown is parsed into
// real block types (headings, bullets, to-dos, quotes, ...) which stay
// visible until the reload swaps in the blocks Notion created.
func (m Model) commitDraft(unit convert.Unit, value string) (tea.Model, tea.Cmd) {
	blocks := convert.ParseMarkdown(value)
	if len(blocks) == 0 {
		// an empty draft still saves — an empty paragraph is a valid
		// (and useful) Notion block
		blocks = []notionapi.Block{convert.BuildBlock("paragraph", []notionapi.RichText{}, false)}
	}

	// created blocks get temporary pending-N IDs so the local tree stays
	// fully addressable (anchoring, cursor) while the sync is in flight;
	// the sync queue patches in the real IDs when the server responds
	nodes, pendingIDs := m.mintPendingNodes(blocks)
	existing := m.blockCache[m.selected.ID]
	// the draft may nest inside a toggle or list item; its parent becomes
	// the create op's append target
	parentID, _, _ := notion.FindPlacement(existing, draftBlockID)
	replaced, _ := notion.ReplaceNode(existing, draftBlockID, nodes)
	m.blockCache[m.selected.ID] = replaced
	m.rebuildPage(true)
	// land on the last created block — natural continuation point, and it
	// keeps shift+enter's next draft below the whole batch
	m.cursorToBlock(pendingIDs[len(pendingIDs)-1])

	m.statusMsg = "adding blocks…"
	for _, b := range blocks {
		if b.GetType() == "toggle" {
			m.statusMsg = "toggle added — press n on it to write inside"
		}
	}
	m.moveQueue = append(m.moveQueue, moveOp{
		pageID:     m.selected.ID,
		parentID:   parentID,
		pendingIDs: pendingIDs,
		payloads:   blocks,
	})
	if !m.moveSyncing {
		if cmd := m.flushNextMove(); cmd != nil {
			return m, cmd
		}
	}
	return m, nil
}

func (m *Model) mintPendingNodes(blocks []notionapi.Block) ([]notion.BlockNode, []string) {
	nodes := convert.NodesFromBlocks(blocks)
	pendingIDs := make([]string, len(nodes))
	for i := range nodes {
		m.pendingSeq++
		pendingIDs[i] = fmt.Sprintf("pending-%d", m.pendingSeq)
		nodes[i] = notion.ReplaceBlockID(nodes[i], pendingIDs[i])
	}
	return nodes, pendingIDs
}

// createEmptyBlockBelow inserts an empty paragraph as a sibling after the
// cursor block — without opening the editor.
func (m Model) createEmptyBlockBelow() (tea.Model, tea.Cmd) {
	if m.selected == nil {
		return m, nil
	}
	blocks, ok := m.blockCache[m.selected.ID]
	if !ok {
		return m, nil
	}
	empty := convert.BuildBlock("paragraph", []notionapi.RichText{}, false)
	nodes, pendingIDs := m.mintPendingNodes([]notionapi.Block{empty})

	placed := false
	if unit, hasCursor := m.pv.current(); hasCursor {
		blocks, placed = notion.InsertSibling(blocks, unit.ID(), nodes[0], false)
	}
	if !placed {
		blocks = append(blocks, nodes[0])
	}
	m.blockCache[m.selected.ID] = blocks
	parentID, _, _ := notion.FindPlacement(blocks, pendingIDs[0])
	m.rebuildPage(true)
	m.cursorToBlock(pendingIDs[0])

	m.moveQueue = append(m.moveQueue, moveOp{
		pageID:     m.selected.ID,
		parentID:   parentID,
		pendingIDs: pendingIDs,
		payloads:   []notionapi.Block{empty},
	})
	if !m.moveSyncing {
		if cmd := m.flushNextMove(); cmd != nil {
			return m, cmd
		}
	}
	return m, nil
}

func (m Model) newBlockBelow() (tea.Model, tea.Cmd) {
	return m.newBlockAt(false)
}

func (m Model) newBlockAbove() (tea.Model, tea.Cmd) {
	return m.newBlockAt(true)
}

func (m Model) newBlockAt(above bool) (tea.Model, tea.Cmd) {
	if m.selected == nil {
		return m, nil
	}
	blocks, ok := m.blockCache[m.selected.ID]
	if !ok {
		return m, nil
	}
	draft := notion.BlockNode{Block: &notionapi.ParagraphBlock{
		BasicBlock: notionapi.BasicBlock{Object: "block", ID: draftBlockID, Type: "paragraph"},
	}}

	placed := false
	if unit, hasCursor := m.pv.current(); hasCursor {
		switch {
		case !above && unit.NestTarget() && !unit.Collapsed:
			// n on an open toggle adds inside its body, notion-style
			blocks, placed = notion.AppendChild(blocks, unit.ID(), draft)
		default:
			// sibling next to the cursor block, at whatever nesting
			blocks, placed = notion.InsertSibling(blocks, unit.ID(), draft, above)
		}
	}
	if !placed {
		blocks = append(blocks, draft)
	}
	// landing above the first top-level block later requires the shuffle
	// that recreates the displaced block — refuse if it can't be
	if above && len(blocks) > 1 && blocks[0].Block.GetID().String() == draftBlockID {
		if ok, reason := notion.CanRecreate(blocks[1]); !ok {
			m.statusMsg = "can't insert above: " + reason
			blocks, _ = notion.ReplaceNode(blocks, draftBlockID, nil)
			m.blockCache[m.selected.ID] = blocks
			return m, nil
		}
	}
	m.blockCache[m.selected.ID] = blocks
	m.rebuildPage(true)
	for i, u := range m.pv.units {
		if u.ID() == draftBlockID {
			m.pv.cursor = i
			break
		}
	}
	return m.startInlineEdit()
}

func (m *Model) removeDraft() {
	m.removeLocalBlock(draftBlockID)
}

func (m *Model) removeLocalBlock(id string) {
	if m.selected == nil {
		return
	}
	m.blockCache[m.selected.ID] = removeBlockNode(m.blockCache[m.selected.ID], id)
}

func removeBlockNode(nodes []notion.BlockNode, id string) []notion.BlockNode {
	kept := make([]notion.BlockNode, 0, len(nodes))
	for _, n := range nodes {
		if n.Block.GetID().String() == id {
			continue
		}
		n.Children = removeBlockNode(n.Children, id)
		kept = append(kept, n)
	}
	return kept
}

func (m Model) deleteCurrentBlock() (tea.Model, tea.Cmd) {
	unit, ok := m.pv.current()
	if !ok || m.selected == nil {
		return m, nil
	}
	if isPendingID(unit.ID()) {
		m.statusMsg = "block is still syncing — try again in a moment"
		return m, nil
	}
	if _, isChild := unit.Node.Block.(*notionapi.ChildPageBlock); isChild {
		m.statusMsg = "refusing to delete a sub-page — do that in Notion"
		return m, nil
	}
	if _, isDB := unit.Node.Block.(*notionapi.ChildDatabaseBlock); isDB {
		m.statusMsg = "refusing to delete a database — do that in Notion"
		return m, nil
	}
	blockID := unit.ID()
	if blockID == draftBlockID {
		m.removeDraft()
		m.rebuildPage(true)
		return m, nil
	}

	// remember the position for undo before the block leaves the tree
	var anchorID string
	var deletedNode *notion.BlockNode
	if unit.TopLevel {
		if ok, _ := notion.CanRecreate(unit.Node); ok {
			node := unit.Node
			deletedNode = &node
			for _, b := range m.blockCache[m.selected.ID] {
				if b.Block.GetID().String() == blockID {
					break
				}
				anchorID = b.Block.GetID().String()
			}
		}
	}

	m.removeLocalBlock(blockID)
	m.rebuildPage(true)
	m.statusMsg = "block deleted (u to undo)"

	client := m.client
	pageID := m.selected.ID
	m.undoStack = append(m.undoStack, undoRecord{
		desc:   "block delete",
		pageID: pageID,
		apply: func(ctx context.Context) error {
			return client.RestoreBlock(ctx, blockID)
		},
		node:     deletedNode,
		anchorID: anchorID,
	})
	if len(m.undoStack) > maxUndoDepth {
		m.undoStack = m.undoStack[len(m.undoStack)-maxUndoDepth:]
	}
	m.writesInFlight++
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := client.DeleteBlock(ctx, blockID); err != nil {
			return writeErrMsg{pageID: pageID, err: err}
		}
		return writeDoneMsg{pageID: pageID, status: "block deleted (u to undo)"}
	}
}

func (m Model) updateKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "/":
		if m.focus == focusViewer && m.selected != nil && m.db == nil && m.recent == nil {
			return m.startInput(inputFind, "find in page: ", m.findQuery)
		}
		if m.selected != nil {
			// Recent and databases have no in-page text search. Return to the
			// visible root list before starting a global search so focus never
			// points at the hidden sidebar.
			m.selected = nil
			m.db = nil
			m.recent = nil
			m.history = nil
			m.focus = focusSidebar
			m.pv = pageView{}
			m.renderedID = ""
			m.pageLoading = false
			m.viewer.SetContent("")
			m.layout()
		}
		return m.startInput(inputSearch, "search: ", "")
	case "u":
		return m.undo()
	case "tab":
		if m.selected != nil {
			m.focus = focusViewer
			return m, nil
		}
		m.focus = focusSidebar
		return m, nil
	case "1":
		if m.selected != nil {
			m.focus = focusViewer
			return m, nil
		}
		m.focus = focusSidebar
		m.rebuildPage(false)
		return m, nil
	case "2":
		if m.selected == nil {
			m.focus = focusSidebar
			return m, nil
		}
		m.focus = focusViewer
		m.rebuildPage(false)
		return m, nil
	case "esc":
		if m.visualAnchor >= 0 && m.db == nil {
			m.visualAnchor = -1
			m.statusMsg = ""
			m.syncViewer()
			return m, nil
		}
		if m.focus == focusViewer && len(m.history) > 0 {
			entry := m.history[len(m.history)-1]
			m.history = m.history[:len(m.history)-1]
			if entry.recent != nil {
				m.recent = entry.recent
				m.selected = &recentPage
				m.db = nil
				m.focus = focusViewer
				m.layout()
				return m, nil
			}
			if entry.db != nil {
				// back into a database grid, exactly as it was left
				m.db = entry.db
				m.selected = &entry.db.ref
				m.pendingRestore = nil
				m.focus = focusViewer
				m.layout()
				return m, nil
			}
			m.db = nil
			m.recent = nil
			next, cmd := m.openPage(entry.page)
			model := next.(Model)
			model.layout()
			model.pendingRestore = &cursorRestore{
				pageID: entry.page.ID,
				cursor: entry.cursor,
				offset: entry.offset,
			}
			model.applyPendingRestore()
			return model, cmd
		}
		m.focus = focusSidebar
		if m.selected != nil {
			m.selected = nil
			m.db = nil
			m.recent = nil
			m.history = nil
			m.pv = pageView{}
			m.renderedID = ""
			m.pageLoading = false
			m.viewer.SetContent("")
			m.layout()
		}
		m.rebuildPage(false)
		return m, nil
	case "R":
		if m.recent != nil {
			return m, func() tea.Msg {
				m.recentLoader.requestDeep(m.client, m.store)
				return m.recentLoader.load(m.client, m.store, m.wsGen, true)
			}
		}
		return m, nil
	case "r":
		if m.focus == focusViewer && m.recent != nil {
			return m, m.loadRecent(true)
		}
		m.err = nil
		if m.focus == focusViewer && m.db != nil {
			return m.refreshDatabaseView()
		}
		// viewer: refresh just the current page, straight past all caches
		if m.focus == focusViewer && m.selected != nil {
			delete(m.blockCache, m.selected.ID)
			if m.store != nil {
				m.store.Invalidate(m.selected.ID)
			}
			m.pageLoading = true
			m.statusMsg = "refreshing page…"
			return m, m.loadPage(*m.selected, true)
		}
		// sidebar: full refresh — page list plus the open page
		m.loading = true
		m.blockCache = make(map[string][]notion.BlockNode)
		cmds := []tea.Cmd{m.fetchPages(m.lastQuery, true)}
		if m.recent != nil {
			cmds = append(cmds, m.loadRecent(true))
		} else if m.db != nil {
			cmds = append(cmds, m.fetchDatabaseView(m.db.ref, true))
		} else if m.selected != nil {
			m.pageLoading = true
			cmds = append(cmds, m.loadPage(*m.selected, true))
		}
		return m, tea.Batch(cmds...)
	case "ctrl+o":
		if m.recent != nil {
			if len(m.recent.pages) > 0 {
				return m, openInBrowser(m.recent.pages[m.recent.cursor].URL)
			}
			return m, nil
		}
		if m.selected != nil {
			return m, openInBrowser(m.selected.URL)
		}
		return m, nil
	case "n":
		if m.focus == focusSidebar {
			return m.startNewPageInput()
		}
		if m.recent != nil {
			return m, nil
		}
		if m.db != nil {
			m.statusMsg = "database view is read-only for now — enter opens the row"
			return m, nil
		}
		return m.newBlockBelow()
	case "w":
		if m.focus == focusSidebar {
			return m.switchWorkspace()
		}
	case "?":
		m.showHelp = true
		return m, nil
	case "v":
		if m.focus == focusViewer && m.selected != nil && m.db == nil && m.recent == nil && len(m.pv.units) > 0 {
			if m.visualAnchor >= 0 {
				m.visualAnchor = -1
				m.statusMsg = ""
			} else {
				m.visualAnchor = m.pv.cursor
				m.statusMsg = "visual: j/k extend · y yank · esc cancel"
			}
			m.syncViewer()
		}
		return m, nil
	case "y":
		if m.focus == focusViewer && m.recent != nil {
			if len(m.recent.pages) > 0 {
				return m, copyCmd(m.recent.pages[m.recent.cursor].URL, "page link copied")
			}
			return m, nil
		}
		if m.focus == focusSidebar {
			if item, ok := m.sidebar.SelectedItem().(pageItem); ok {
				return m, copyCmd(item.page.URL, "page link copied")
			}
			return m, nil
		}
		if m.selected == nil {
			return m, nil
		}
		if m.db != nil {
			return m, copyCmd(m.selected.URL, "database link copied")
		}
		text, count := m.yankSelection()
		m.visualAnchor = -1
		m.syncViewer()
		if strings.TrimSpace(text) == "" {
			return m, nil
		}
		label := "block copied"
		if count > 1 {
			label = fmt.Sprintf("%d blocks copied", count)
		}
		return m, copyCmd(text, label)
	case "I":
		if m.focus == focusViewer && m.selected != nil && m.db == nil && m.recent == nil {
			return m.startInput(inputIcon, `page icon (emoji or "name color"): `, "")
		}
		return m, nil
	case "Y":
		if m.recent != nil {
			return m, nil
		}
		if m.focus != focusViewer || m.selected == nil {
			return m, nil
		}
		if m.db != nil {
			return m, copyCmd(m.selected.URL, "database link copied")
		}
		if unit, ok := m.pv.current(); ok && unit.ID() != draftBlockID && !isPendingID(unit.ID()) {
			return m, copyCmd(blockLink(m.selected.URL, unit.ID()), "block link copied")
		}
		return m, copyCmd(m.selected.URL, "page link copied")
	case "enter":
		if m.focus == focusSidebar {
			return m.openSelectedPage()
		}
		if m.recent != nil {
			return m.openRecentItem()
		}
		if m.db != nil {
			return m.openDBRow()
		}
		return m.openChildPage()
	}

	if m.focus == focusViewer {
		if m.recent != nil {
			return m.updateRecentKeys(msg)
		}
		if m.db != nil {
			return m.updateDBKeys(msg)
		}
		return m.updateViewerKeys(msg)
	}
	var cmd tea.Cmd
	m.sidebar, cmd = m.sidebar.Update(msg)
	return m, cmd
}

func (m Model) updateViewerKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// in visual mode, movement extends the selection; anything except
	// movement/yank drops it first, vim-style
	if m.visualAnchor >= 0 {
		switch msg.String() {
		case "j", "down", "k", "up", "g", "G":
		default:
			m.visualAnchor = -1
			m.statusMsg = ""
		}
	}
	switch msg.String() {
	case "j", "down":
		m.pv.move(1)
		m.syncViewer()
		return m, nil
	case "k", "up":
		m.pv.move(-1)
		m.syncViewer()
		return m, nil
	case "g":
		m.pv.cursor = 0
		m.syncViewer()
		return m, nil
	case "G":
		m.pv.move(len(m.pv.units))
		m.syncViewer()
		return m, nil
	case " ":
		return m.toggleCurrentToDo()
	case "e", "i":
		return m.startInlineEdit()
	case "d":
		return m.deleteCurrentBlock()
	case "N":
		return m.newBlockAbove()
	case "J":
		return m.moveCurrentBlock(1)
	case "K":
		return m.moveCurrentBlock(-1)
	case "a":
		if m.selected == nil {
			return m, nil
		}
		return m.startInput(inputAppend, "append: ", "")
	case "E":
		return m.openInEditor()
	case "]":
		return m.jumpToMatch(1)
	case "[":
		return m.jumpToMatch(-1)
	}

	var cmd tea.Cmd
	m.viewer, cmd = m.viewer.Update(msg)
	return m, cmd
}

func (m Model) startNewPageInput() (tea.Model, tea.Cmd) {
	item, ok := m.sidebar.SelectedItem().(pageItem)
	if !ok {
		m.statusMsg = "highlight a parent page first — the API can't create workspace-root pages"
		return m, nil
	}
	if item.page.ID == recentID {
		m.statusMsg = "select a parent page to create a child"
		return m, nil
	}
	if item.page.Kind == notion.KindDataSource {
		m.statusMsg = "creating database rows isn't supported yet"
		return m, nil
	}
	parent := item.page
	m.newPageParent = &parent
	return m.startInput(inputNewPage, fmt.Sprintf("new page under %q: ", parent.Title), "")
}

func (m Model) startInput(mode inputMode, prompt, value string) (tea.Model, tea.Cmd) {
	m.mode = mode
	m.configureInputCursor(mode)
	m.input.Prompt = prompt
	m.input.SetValue(value)
	if mode == inputTitle {
		m.input.CursorStart()
	} else {
		m.input.CursorEnd()
	}
	return m, m.input.Focus()
}

func (m Model) openSelectedPage() (tea.Model, tea.Cmd) {
	item, ok := m.sidebar.SelectedItem().(pageItem)
	if !ok {
		return m, nil
	}
	m.history = nil
	m.startupPending = false
	if item.page.ID == recentID {
		return m.openRecent()
	}

	// opening a search result leaves search mode: restore the default
	// list right away (from the cached copy) and refresh it quietly
	var restore tea.Cmd
	if m.lastQuery != "" {
		m.lastQuery = ""
		if len(m.defaultPages) > 0 {
			items := make([]list.Item, len(m.defaultPages))
			highlight := 0
			for i, p := range m.defaultPages {
				items[i] = pageItem{page: p}
				if p.ID == item.page.ID {
					highlight = i
				}
			}
			restore = m.sidebar.SetItems(items)
			m.sidebar.Select(highlight)
		} else {
			m.loading = true
		}
		restore = tea.Batch(restore, m.loadPages(""))
	}

	if item.page.Kind == notion.KindDataSource {
		next, cmd := m.openDatabaseView(item.page)
		model := next.(Model)
		return model, tea.Batch(cmd, restore)
	}
	next, cmd := m.openPage(item.page)
	model := next.(Model)
	return model, tea.Batch(cmd, restore)
}

func (m Model) openChildPage() (tea.Model, tea.Cmd) {
	unit, ok := m.pv.current()
	if !ok || m.selected == nil {
		return m, nil
	}
	// enter on a toggle (or toggleable heading) folds it
	if unit.Foldable {
		id := unit.ID()
		m.collapsed[id] = !m.collapsed[id]
		m.rebuildPage(true)
		m.cursorToBlock(id)
		return m, nil
	}
	if dbID, dbTitle, isDB := unit.DatabaseRef(); isDB {
		if dbTitle == "" {
			dbTitle = "Untitled"
		}
		ref := notion.Page{
			ID:    dbID,
			Title: dbTitle,
			URL:   "https://www.notion.so/" + strings.ReplaceAll(dbID, "-", ""),
		}
		m.history = append(m.history, historyEntry{
			page:   *m.selected,
			cursor: m.pv.cursor,
			offset: m.viewer.YOffset,
		})
		return m.openDatabaseView(ref)
	}
	id, title, isRef := unit.PageRef()
	if !isRef {
		// plain blocks: enter stamps a new empty block underneath and
		// moves onto it, so repeated presses keep adding blocks
		return m.createEmptyBlockBelow()
	}
	child := notion.Page{
		ID:    id,
		Title: title,
		URL:   "https://www.notion.so/" + strings.ReplaceAll(id, "-", ""),
	}
	if t := unit.Node.Block.GetLastEditedTime(); t != nil {
		child.LastEdited = *t
	}
	m.history = append(m.history, historyEntry{
		page:   *m.selected,
		cursor: m.pv.cursor,
		offset: m.viewer.YOffset,
	})
	return m.openPage(child)
}

func (m Model) openPage(page notion.Page) (tea.Model, tea.Cmd) {
	m.pendingRestore = nil
	m.db = nil
	m.recent = nil
	m.selected = &page
	m.focus = focusViewer
	m.layout()
	m.pv.cursor = 0
	m.viewer.GotoTop()
	if _, cached := m.blockCache[page.ID]; cached {
		m.rebuildPage(true)
		return m, nil
	}
	m.pageLoading = true
	m.err = nil
	return m, m.loadPage(page, false)
}

func (m Model) toggleCurrentToDo() (tea.Model, tea.Cmd) {
	unit, ok := m.pv.current()
	if !ok || m.selected == nil {
		return m, nil
	}
	todo, isTodo := unit.Node.Block.(*notionapi.ToDoBlock)
	if !isTodo {
		return m, nil
	}
	if isPendingID(unit.ID()) {
		m.statusMsg = "block is still syncing — try again in a moment"
		return m, nil
	}
	newChecked := !todo.ToDo.Checked
	blockID := todo.ID.String()
	richText := todo.ToDo.RichText
	pageID := m.selected.ID

	todo.ToDo.Checked = newChecked
	m.rebuildPage(true)

	client := m.client
	m.pushUndo("to-do toggle", pageID, func(ctx context.Context) error {
		return client.SetToDo(ctx, blockID, richText, !newChecked)
	})
	m.writesInFlight++
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := client.SetToDo(ctx, blockID, richText, newChecked); err != nil {
			return writeErrMsg{pageID: pageID, err: err}
		}
		return writeDoneMsg{pageID: pageID, status: ""}
	}
}

// appendAnchor finds the nearest top-level block at or above the cursor so
// the new paragraph lands after it rather than at the bottom of the page.
func (m Model) appendAnchor() string {
	for i := m.pv.cursor; i >= 0 && i < len(m.pv.units); i-- {
		if m.pv.units[i].TopLevel {
			return m.pv.units[i].ID()
		}
	}
	return ""
}

func (m *Model) replacePage(page notion.Page, blocks []notionapi.Block) tea.Cmd {
	client := m.client
	m.writesInFlight++
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if err := client.ReplacePageBlocks(ctx, page.ID, blocks); err != nil {
			return writeErrMsg{pageID: page.ID, err: err}
		}
		return writeDoneMsg{pageID: page.ID, status: "page synced to notion", reload: true}
	}
}

// rebuildPage re-renders the viewer from cached blocks. rerender=false only
// reassembles (cursor/gutter changes); true re-renders fragments (width or
// content changes).
func (m *Model) rebuildPage(rerender bool) {
	if m.selected == nil {
		return
	}
	blocks, ok := m.blockCache[m.selected.ID]
	if !ok {
		return
	}
	if rerender || len(m.pv.units) == 0 {
		// the page title lives in the viewer pane's border; the cover
		// image (when present and downloaded) renders as the header
		m.pv.kitty = m.kittyImgs
		header := strings.Join(m.coverLines(), "\n")
		m.pv.setUnits(header, convert.FlattenFolded(blocks, m.collapsed), m.viewer.Width, m.images)
		m.refreshReadingMaps()
		m.renderedID = m.selected.ID
	}
	m.viewerSetContent()
	m.pv.ensureVisible(&m.viewer)
	m.applyPendingRestore()
}

// applyPendingRestore puts the cursor and scroll back after an esc-return,
// once the right page is actually rendered (it may load asynchronously).
func (m *Model) applyPendingRestore() {
	r := m.pendingRestore
	if r == nil || m.selected == nil || m.selected.ID != r.pageID || m.renderedID != r.pageID {
		return
	}
	m.pendingRestore = nil
	m.pv.cursor = clamp(r.cursor, 0, max(len(m.pv.units)-1, 0))
	m.viewerSetContent()
	m.viewer.SetYOffset(r.offset)
	m.pv.ensureVisible(&m.viewer)
}

// localBusy reports outstanding local mutations — editing, queued or
// in-flight syncs — during which background content refreshes must yield.
func (m Model) localBusy() bool {
	return m.editing || m.moveSyncing || len(m.moveQueue) > 0 || m.writesInFlight > 0
}

// syncing reports whether anything is talking to Notion right now: the
// create/move queue, tracked block writes, or page/sidebar loads.
func (m Model) syncing() bool {
	return m.moveSyncing || len(m.moveQueue) > 0 || m.writesInFlight > 0 ||
		m.pageLoading || m.loading || m.pagesQuery != nil || m.recentQuery != nil
}

// viewerSetContent pushes the assembled page into the viewport, padded
// with half a screen of blank lines so the view can overscroll — the last
// block can rest mid-screen instead of pinned to the bottom edge.
func (m *Model) viewerSetContent() {
	m.pv.visualOn = m.visualAnchor >= 0
	m.pv.visualAnchor = max(m.visualAnchor, 0)
	content := m.pv.assemble(m.focus == focusViewer, m.editLines())
	if m.readSelection != nil && !m.editing {
		content = m.highlightReadingSelection(content)
	}
	if pad := m.viewer.Height / 2; pad > 0 {
		content += strings.Repeat("\n", pad)
	}
	m.viewer.SetContent(content)
}

func (m *Model) syncViewer() {
	m.viewerSetContent()
	m.pv.ensureVisible(&m.viewer)
}

func (m *Model) editLines() []string {
	if !m.editing {
		return nil
	}
	view := m.editArea.View()
	if m.editAnchor >= 0 {
		view = highlightEditSelection(view, m.editArea.Value(), m.editArea.Width(), m.editAnchor, m.editCursorIndex(), m.editScroll)
	}
	return strings.Split(view, "\n")
}

func (m Model) footerHeight() int {
	if m.paletteOpen {
		return min(len(m.filteredPalette()), paletteMaxRows) + 1
	}
	return 1
}

func (m *Model) layout() {
	paneHeight := max(m.height-m.footerHeight(), 0)
	if m.selected == nil {
		m.sidebar.SetSize(max(m.width-2, 1), max(paneHeight-4, 0))
		m.viewer.Width = max(m.width-2, 1)
	} else {
		m.sidebar.SetSize(0, 0)
		m.viewer.Width = max(m.width-2, 1)
	}
	m.viewer.Height = max(paneHeight-2, 0)
	m.input.Width = m.width - 12
}

func relTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return t.Format("Jan 2, 2006")
	}
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
