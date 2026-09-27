package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/justinm35/lazynotion/internal/icons"
	"github.com/justinm35/lazynotion/internal/notion"
)

// dbState is the read-only database view: one data source's schema plus the
// rows loaded so far. It lives behind a pointer so history entries can stash
// the whole view and esc restores it — cursor, scroll and rows intact.
type dbState struct {
	ref         notion.Page // the entry that was opened (title, URL)
	dsID        string
	ds          *notion.DataSource
	rows        []notion.Row
	cursor      int
	scroll      int // first visible row
	colOff      int // first visible column after the title column
	hasMore     bool
	nextCursor  string
	loadingMore bool
	sources     int // data sources in the database (>1 shows a note)
}

const (
	dbPageSize      = 50
	dbLoadMoreNear  = 10 // fetch the next page this many rows before the end
	dbMinColWidth   = 6
	dbMaxColWidth   = 32
	dbMaxTitleWidth = 40
)

type dbLoadedMsg struct {
	key       string // ref.ID, to drop results for a view navigated away from
	ds        *notion.DataSource
	rows      *notion.RowPage
	sources   int
	wsGen     int
	fromCache bool
}

type cachedDatabase struct {
	Source  *notion.DataSource
	Rows    *notion.RowPage
	Sources int
}

type dbRowsMsg struct {
	dsID  string
	page  *notion.RowPage
	wsGen int
}

// openDatabaseView enters the database view for a sidebar data source or a
// child_database block (whose ID is the database, resolved to its first
// data source while loading).
func (m Model) openDatabaseView(ref notion.Page) (tea.Model, tea.Cmd) {
	m.recent = nil
	m.selected = &ref
	m.focus = focusViewer
	m.db = &dbState{ref: ref}
	m.pageLoading = true
	m.err = nil
	m.layout()
	return m, m.loadDatabaseView(ref)
}

func (m Model) loadDatabaseView(ref notion.Page) tea.Cmd {
	return m.fetchDatabaseView(ref, false)
}

func (m Model) fetchDatabaseView(ref notion.Page, force bool) tea.Cmd {
	client, gen := m.client, m.wsGen
	store := m.store
	return func() tea.Msg {
		key := "database:" + client.CacheKey() + ":" + ref.ID
		if !force && store != nil {
			var cached cachedDatabase
			if store.LoadMetadata(key, &cached) && cached.Source != nil && cached.Rows != nil {
				return dbLoadedMsg{key: ref.ID, ds: cached.Source, rows: cached.Rows, sources: cached.Sources, wsGen: gen, fromCache: true}
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		dsID, sources := ref.ID, 1
		if ref.Kind != notion.KindDataSource {
			// a child_database block carries the database ID; resolve it
			db, err := client.GetDatabase(ctx, ref.ID)
			if err != nil {
				return errMsg{err}
			}
			if len(db.DataSources) == 0 {
				return errMsg{fmt.Errorf("database %q has no data sources", db.Title)}
			}
			sources = len(db.DataSources)
			dsID = db.DataSources[0].ID
		}
		ds, err := client.GetDataSource(ctx, dsID)
		if err != nil {
			return errMsg{err}
		}
		rows, err := client.QueryDataSource(ctx, dsID, "", dbPageSize)
		if err != nil {
			return errMsg{err}
		}
		if store != nil {
			_ = store.SaveMetadata(key, cachedDatabase{Source: ds, Rows: rows, Sources: sources})
		}
		return dbLoadedMsg{key: ref.ID, ds: ds, rows: rows, sources: sources, wsGen: gen}
	}
}

func (m Model) loadMoreRows() tea.Cmd {
	client, gen := m.client, m.wsGen
	dsID, cursor := m.db.dsID, m.db.nextCursor
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		page, err := client.QueryDataSource(ctx, dsID, cursor, dbPageSize)
		if err != nil {
			return errMsg{err}
		}
		return dbRowsMsg{dsID: dsID, page: page, wsGen: gen}
	}
}

func (m Model) handleDBLoaded(msg dbLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.wsGen != m.wsGen || m.db == nil || m.db.ref.ID != msg.key {
		return m, nil
	}
	m.pageLoading = false
	m.db.ds = msg.ds
	m.db.dsID = msg.ds.ID
	m.db.rows = msg.rows.Rows
	m.db.hasMore = msg.rows.HasMore
	m.db.nextCursor = msg.rows.NextCursor
	m.db.sources = msg.sources
	if msg.sources > 1 {
		m.statusMsg = fmt.Sprintf("this database has %d data sources — showing the first", msg.sources)
	}
	if msg.fromCache {
		return m, m.fetchDatabaseView(m.db.ref, true)
	}
	return m, nil
}

func (m Model) handleDBRows(msg dbRowsMsg) (tea.Model, tea.Cmd) {
	if msg.wsGen != m.wsGen || m.db == nil || m.db.dsID != msg.dsID {
		return m, nil
	}
	m.db.loadingMore = false
	m.db.rows = append(m.db.rows, msg.page.Rows...)
	m.db.hasMore = msg.page.HasMore
	m.db.nextCursor = msg.page.NextCursor
	return m, nil
}

// updateDBKeys handles viewer keys while the database grid is on screen —
// the view is read-only, so edit keys fall through to nothing.
func (m Model) updateDBKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	db := m.db
	if db.ds == nil {
		return m, nil // still loading
	}
	switch msg.String() {
	case "j", "down":
		db.cursor = clamp(db.cursor+1, 0, max(len(db.rows)-1, 0))
	case "k", "up":
		db.cursor = clamp(db.cursor-1, 0, max(len(db.rows)-1, 0))
	case "g":
		db.cursor = 0
	case "G":
		db.cursor = max(len(db.rows)-1, 0)
	case "ctrl+d":
		db.cursor = clamp(db.cursor+m.dbVisibleRows()/2, 0, max(len(db.rows)-1, 0))
	case "ctrl+u":
		db.cursor = clamp(db.cursor-m.dbVisibleRows()/2, 0, max(len(db.rows)-1, 0))
	case "h", "left":
		db.colOff = max(db.colOff-1, 0)
	case "l", "right":
		// keep at least one column beyond the title on screen
		db.colOff = clamp(db.colOff+1, 0, max(len(db.ds.Properties)-2, 0))
	case "e", "i", "d", "n", "N", "a", "E", " ", "J", "K":
		m.statusMsg = "database view is read-only for now — enter opens the row"
		return m, nil
	default:
		return m, nil
	}
	// nearing the bottom of what's loaded: quietly fetch the next page
	if db.hasMore && !db.loadingMore && db.cursor >= len(db.rows)-dbLoadMoreNear {
		db.loadingMore = true
		return m, m.loadMoreRows()
	}
	return m, nil
}

// openDBRow drills into the row under the cursor — rows are pages, so the
// regular page view takes over; esc restores the grid as it was.
func (m Model) openDBRow() (tea.Model, tea.Cmd) {
	db := m.db
	if db == nil || db.ds == nil || len(db.rows) == 0 {
		return m, nil
	}
	row := db.rows[clamp(db.cursor, 0, len(db.rows)-1)]
	page := notion.Page{
		ID:         row.ID,
		Title:      row.Title(db.ds.TitleProperty()),
		Icon:       row.Icon,
		URL:        row.URL,
		LastEdited: row.LastEdited,
	}
	m.history = append(m.history, historyEntry{db: db})
	m.db = nil
	return m.openPage(page)
}

// refreshDatabaseView reloads schema and rows from scratch.
func (m Model) refreshDatabaseView() (tea.Model, tea.Cmd) {
	ref := m.db.ref
	m.db = &dbState{ref: ref}
	m.pageLoading = true
	m.statusMsg = "refreshing database…"
	return m, m.fetchDatabaseView(ref, true)
}

// --- rendering ---------------------------------------------------------

var (
	dbHeaderStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("252"))
	dbSepStyle     = lipgloss.NewStyle().Foreground(dimColor)
	dbCellStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("250"))
	dbCheckStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("77"))
	dbDimCellStyle = lipgloss.NewStyle().Foreground(dimColor)
	dbCursorBar    = lipgloss.NewStyle().Foreground(accentColor).Render("▎")
	dbCursorStyle  = lipgloss.NewStyle().Bold(true)
	dbCountStyle   = lipgloss.NewStyle().Foreground(dimColor)
)

// dbVisibleRows is how many data rows fit under the header and count line.
func (m Model) dbVisibleRows() int {
	return max(m.viewer.Height-3, 1)
}

// dbGridView renders the whole grid pane: header, separator, a window of
// rows, and a count line.
func (m *Model) dbGridView() string {
	db := m.db
	width, height := m.viewer.Width, m.viewer.Height
	if db.ds == nil {
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center,
			pageMetaStyle.Render("loading database…"))
	}
	if len(db.rows) == 0 {
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center,
			pageMetaStyle.Render("no rows"))
	}

	cols := m.dbLayoutColumns(width - 2) // 1 cell of cursor gutter + right margin
	visible := m.dbVisibleRows()
	db.cursor = clamp(db.cursor, 0, len(db.rows)-1)
	if db.cursor < db.scroll {
		db.scroll = db.cursor
	}
	if db.cursor >= db.scroll+visible {
		db.scroll = db.cursor - visible + 1
	}
	db.scroll = clamp(db.scroll, 0, max(len(db.rows)-1, 0))

	var b strings.Builder
	// header
	b.WriteString("  ")
	for i, col := range cols {
		if i > 0 {
			b.WriteString(dbSepStyle.Render(" │ "))
		}
		b.WriteString(dbHeaderStyle.Render(padCell(col.name, col.width)))
	}
	b.WriteString("\n  ")
	for i, col := range cols {
		if i > 0 {
			b.WriteString(dbSepStyle.Render("─┼─"))
		}
		b.WriteString(dbSepStyle.Render(strings.Repeat("─", col.width)))
	}
	b.WriteString("\n")

	end := min(db.scroll+visible, len(db.rows))
	for r := db.scroll; r < end; r++ {
		row := db.rows[r]
		selected := r == db.cursor
		if selected {
			b.WriteString(dbCursorBar + " ")
		} else {
			b.WriteString("  ")
		}
		for i, col := range cols {
			if i > 0 {
				b.WriteString(dbSepStyle.Render(" │ "))
			}
			b.WriteString(renderDBCell(row.Properties[col.name], col, selected))
		}
		b.WriteString("\n")
	}

	count := fmt.Sprintf("%d rows", len(db.rows))
	if db.hasMore {
		count += " · more available"
	}
	if db.loadingMore {
		count += " · loading…"
	}
	if db.colOff > 0 || len(cols) < len(db.ds.Properties)-db.colOff {
		count += fmt.Sprintf(" · %d/%d cols shown (h/l scrolls)", len(cols), len(db.ds.Properties))
	}
	b.WriteString("  " + dbCountStyle.Render(count))
	return b.String()
}

type dbColumn struct {
	name    string
	kind    string
	width   int
	stretch bool
}

// dbLayoutColumns picks which columns fit and how wide each one is: the
// title column is pinned first, h/l slides the rest, and the final visible
// column absorbs slack width.
func (m Model) dbLayoutColumns(width int) []dbColumn {
	db := m.db
	props := db.ds.Properties
	if len(props) == 0 {
		return nil
	}

	// natural width per column: widest of header and (loaded) cells
	natural := make([]int, len(props))
	for i, p := range props {
		w := lipgloss.Width(p.Name)
		for r, row := range db.rows {
			if r >= 100 {
				break // enough of a sample; keeps wide tables cheap
			}
			if cw := lipgloss.Width(row.Properties[p.Name].Display()); cw > w {
				w = cw
			}
		}
		maxW := dbMaxColWidth
		if p.Type == "title" {
			maxW = dbMaxTitleWidth
		}
		natural[i] = clamp(w, dbMinColWidth, maxW)
	}

	// visible set: title first, then props[1+colOff:], as many as fit
	order := []int{0}
	for i := 1 + db.colOff; i < len(props); i++ {
		order = append(order, i)
	}
	var cols []dbColumn
	used := 0
	for n, idx := range order {
		sep := 0
		if n > 0 {
			sep = 3 // " │ "
		}
		w := natural[idx]
		if used+sep+w > width {
			// squeeze a partial last column in if at least a sliver fits
			if rest := width - used - sep; rest >= dbMinColWidth {
				cols = append(cols, dbColumn{name: props[idx].Name, kind: props[idx].Type, width: rest})
			}
			break
		}
		cols = append(cols, dbColumn{name: props[idx].Name, kind: props[idx].Type, width: w})
		used += sep + w
	}
	if len(cols) == 0 {
		cols = append(cols, dbColumn{name: props[0].Name, kind: props[0].Type, width: max(width, dbMinColWidth)})
	}
	// the last column absorbs leftover width so the grid fills the pane
	if slack := width - used; slack > 0 && len(cols) > 0 {
		cols[len(cols)-1].width += slack
	}
	return cols
}

// renderDBCell truncates, pads and colors one cell.
func renderDBCell(v notion.PropertyValue, col dbColumn, selected bool) string {
	text := padCell(v.Display(), col.width)
	style := dbCellStyle
	switch {
	case len(v.Options) > 0:
		style = lipgloss.NewStyle().Foreground(icons.TermColor(v.Options[0].Color))
	case v.Type == "checkbox" || (v.Checkbox && v.HasValue):
		style = dbCheckStyle
	case v.Date != nil || v.Type == "relation" || len(v.People) > 0:
		style = dbDimCellStyle
	}
	if selected {
		return dbCursorStyle.Inherit(style).Render(text)
	}
	return style.Render(text)
}

// padCell truncates plain text to width and pads it back out to width.
func padCell(s string, width int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = truncateText(s, width)
	if pad := width - lipgloss.Width(s); pad > 0 {
		s += strings.Repeat(" ", pad)
	}
	return s
}
