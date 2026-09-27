package ui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jomei/notionapi"

	"github.com/justinm35/lazynotion/internal/notion"
)

func TestEditorSelectionAfterTabKeepsCJKOffsets(t *testing.T) {
	m := interactionEditor(t, "\t中文：后文")
	value := m.editArea.Value() // Bubbles expands the tab to four editor spaces.
	start := strings.Index(value, "中文：")
	if start < 0 {
		t.Fatalf("textarea value lost CJK text after tab: %q", value)
	}
	prefix := utf8.RuneCountInString(value[:start])
	m.moveEditCursor(prefix)
	m = interactionKey(t, m, tea.KeyMsg{Type: tea.KeyCtrlV})
	for range 3 {
		m = interactionKey(t, m, tea.KeyMsg{Type: tea.KeyShiftRight})
	}
	if m.editAnchor != prefix || m.editCursorIndex() != prefix+3 || m.editSelectionLength() != 3 {
		t.Fatalf("selection after tab = anchor %d, cursor %d, length %d", m.editAnchor, m.editCursorIndex(), m.editSelectionLength())
	}
	visible, reverse := ansiAttributeShape(t, strings.Join(m.editLines(), "\n"), 7, 27)
	runes := []rune(visible)
	visibleStart := strings.Index(visible, "中文：")
	if visibleStart < 0 {
		t.Fatalf("selected CJK text is absent after tab: %q", visible)
	}
	first := utf8.RuneCountInString(visible[:visibleStart])
	for i := first; i < first+3; i++ {
		if i >= len(runes) || !reverse[i] {
			t.Errorf("CJK rune after tab at visible position %d is not selected: %q", i, visible)
		}
	}

	m = interactionKey(t, m, interactionRune('b'))
	if got := m.editArea.Value(); got != value[:start]+"**中文：**"+value[start+len("中文："):] {
		t.Fatalf("bold after tab = %q", got)
	}
	m = interactionKey(t, m, interactionRune('u'))
	if m.editArea.Value() != value || m.editAnchor != prefix || m.editCursorIndex() != prefix+3 {
		t.Fatalf("undo after tab lost text or selection: value=%q anchor=%d cursor=%d", m.editArea.Value(), m.editAnchor, m.editCursorIndex())
	}
}

func TestInvalidTableSaveRetainsUndoAndShiftEnterDoesNotAddDraft(t *testing.T) {
	t.Run("Escape keeps undo after validation reopens editor", func(t *testing.T) {
		m := tableModel(t)
		next, _ := m.startInlineEdit()
		m = next.(Model)
		m.editArea.SetValue("not a markdown table")
		m.editArea.CursorEnd()
		m = interactionKey(t, m, interactionRune('x'))
		invalidWithEdit := m.editArea.Value()

		m = interactionKey(t, m, tea.KeyMsg{Type: tea.KeyEsc})
		if !m.editing || m.editArea.Value() != invalidWithEdit || m.statusMsg == "" {
			t.Fatalf("invalid Escape did not preserve and explain the editor: editing=%v value=%q status=%q", m.editing, m.editArea.Value(), m.statusMsg)
		}
		m = interactionKey(t, m, tea.KeyMsg{Type: tea.KeyCtrlZ})
		if !m.editing || m.editArea.Value() != "not a markdown table" {
			t.Fatalf("Ctrl+Z after invalid Escape did not undo the last edit: editing=%v value=%q", m.editing, m.editArea.Value())
		}
		if len(m.blockCache["page-1"]) != 1 || m.blockCache["page-1"][0].Block.GetID().String() != "tbl" {
			t.Fatalf("invalid table save changed the cached table: %+v", m.blockCache["page-1"])
		}
	})

	t.Run("Shift+Enter does not create a draft after validation failure", func(t *testing.T) {
		m := tableModel(t)
		next, _ := m.startInlineEdit()
		m = next.(Model)
		m.editArea.SetValue("not a markdown table")
		m.editArea.CursorEnd()
		// Bubble Tea represents the continuation gesture as Alt+Enter in a
		// KeyMsg; the editor also accepts this as its Shift+Enter alias.
		next, _ = m.updateInlineEdit(tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
		m = next.(Model)
		if !m.editing || len(m.blockCache["page-1"]) != 1 {
			t.Fatalf("invalid Shift+Enter left editor or added a block: editing=%v blocks=%d", m.editing, len(m.blockCache["page-1"]))
		}
		if m.blockCache["page-1"][0].Block.GetID().String() != "tbl" {
			t.Fatalf("invalid Shift+Enter replaced the table with %q", m.blockCache["page-1"][0].Block.GetID())
		}
	})
}

func TestAppCtrlCDiscardsEditWithoutQuitting(t *testing.T) {
	m := interactionEditor(t, "original")
	m.editArea.SetValue("changed")
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = next.(Model)
	if cmd != nil {
		if msg := cmd(); msg != nil {
			if _, quit := msg.(tea.QuitMsg); quit {
				t.Fatal("Ctrl+C in the inline editor returned a quit message")
			}
		}
	}
	text, _ := notion.LocalRichText(m.blockCache["page-1"][0].Block)
	if m.editing || len(text) != 1 || text[0].PlainText != "original" || m.statusMsg != "edit discarded" {
		t.Fatalf("Ctrl+C did not discard only the editor: editing=%v text=%+v status=%q", m.editing, text, m.statusMsg)
	}
}

func TestNestedRecentAndDatabaseNavigationRestorePaneFocus(t *testing.T) {
	assertViewer := func(t *testing.T, m Model, wantID string) {
		t.Helper()
		if m.selected == nil || m.selected.ID != wantID || m.focus != focusViewer || m.sidebar.Width() != 0 || m.viewer.Width != m.width-2 {
			t.Fatalf("viewer pane not restored for %q: selected=%+v focus=%v sidebar=%d viewer=%d", wantID, m.selected, m.focus, m.sidebar.Width(), m.viewer.Width)
		}
	}
	assertRoot := func(t *testing.T, m Model) {
		t.Helper()
		if m.selected != nil || m.focus != focusSidebar || m.sidebar.Width() != m.width-2 || m.viewer.Width != m.width-2 {
			t.Fatalf("root pane not restored: selected=%+v focus=%v sidebar=%d viewer=%d", m.selected, m.focus, m.sidebar.Width(), m.viewer.Width)
		}
	}

	t.Run("nested page to parent to root", func(t *testing.T) {
		m := New([]Workspace{{Name: "test"}}, 0, nil, "mosaic")
		m.width, m.height = 92, 30
		m.layout()
		next, _ := m.Update(pagesMsg{pages: []notion.Page{{ID: "home", Title: "Home"}}})
		m = next.(Model)
		child := &notionapi.ChildPageBlock{BasicBlock: notionapi.BasicBlock{ID: "nested", Type: "child_page"}}
		child.ChildPage.Title = "Nested"
		m.blockCache["home"] = []notion.BlockNode{{Block: child}}
		m.blockCache["nested"] = []notion.BlockNode{para("inside", "nested body")}
		m.sidebar.Select(1)
		next, _ = m.updateKeys(key("enter"))
		m = next.(Model)
		assertViewer(t, m, "home")
		next, _ = m.updateKeys(key("enter"))
		m = next.(Model)
		assertViewer(t, m, "nested")
		next, _ = m.updateKeys(key("esc"))
		m = next.(Model)
		assertViewer(t, m, "home")
		next, _ = m.updateKeys(key("esc"))
		m = next.(Model)
		assertRoot(t, m)
	})

	t.Run("Recent item to Recent to root", func(t *testing.T) {
		m := New([]Workspace{{Name: "test"}}, 0, nil, "mosaic")
		m.width, m.height = 92, 30
		m.layout()
		next, _ := m.Update(pagesMsg{pages: []notion.Page{{ID: "home", Title: "Home"}}})
		m = next.(Model)
		m.sidebar.Select(0)
		next, _ = m.updateKeys(key("enter"))
		m = next.(Model)
		next, _ = m.Update(recentMsg{pages: []notion.Page{{ID: "recent-item", Title: "Recent item"}}})
		m = next.(Model)
		m.blockCache["recent-item"] = []notion.BlockNode{para("recent-block", "from Recent")}
		next, _ = m.updateKeys(key("enter"))
		m = next.(Model)
		assertViewer(t, m, "recent-item")
		next, _ = m.updateKeys(key("esc"))
		m = next.(Model)
		assertViewer(t, m, recentID)
		next, _ = m.updateKeys(key("esc"))
		m = next.(Model)
		assertRoot(t, m)
	})

	t.Run("database row to database to root", func(t *testing.T) {
		m := New([]Workspace{{Name: "test"}}, 0, nil, "mosaic")
		m.width, m.height = 92, 30
		m.layout()
		next, _ := m.Update(pagesMsg{pages: []notion.Page{
			{ID: "home", Title: "Home"},
			{ID: "ds-1", Title: "Tasks", Kind: notion.KindDataSource},
		}})
		m = next.(Model)
		m.sidebar.Select(2)
		next, _ = m.updateKeys(key("enter"))
		m = next.(Model)
		next, _ = m.Update(dbLoadedMsg{
			key: "ds-1", ds: testDataSource(), rows: &notion.RowPage{Rows: []notion.Row{testRow("row-1", "First row", "", false)}}, wsGen: m.wsGen,
		})
		m = next.(Model)
		m.blockCache["row-1"] = []notion.BlockNode{para("row-body", "row content")}
		next, _ = m.updateKeys(key("enter"))
		m = next.(Model)
		assertViewer(t, m, "row-1")
		next, _ = m.updateKeys(key("esc"))
		m = next.(Model)
		assertViewer(t, m, "ds-1")
		if m.db == nil || len(m.db.rows) != 1 || m.db.cursor != 0 {
			t.Fatalf("database view was not restored: %+v", m.db)
		}
		next, _ = m.updateKeys(key("esc"))
		m = next.(Model)
		assertRoot(t, m)
	})
}

func TestEditorEscapePersistsBoldSelectionToFakeNotionAPI(t *testing.T) {
	var request struct {
		method string
		path   string
		body   struct {
			Paragraph struct {
				RichText []struct {
					Text struct {
						Content string `json:"content"`
					} `json:"text"`
					Annotations struct {
						Bold bool `json:"bold"`
					} `json:"annotations"`
				} `json:"rich_text"`
			} `json:"paragraph"`
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request.method, request.path = r.Method, r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&request.body); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"block","id":"edit-block","type":"paragraph","paragraph":{"rich_text":[]}}`))
	}))
	defer server.Close()
	client := notionClientForTestServer(t, server)

	m := interactionEditor(t, "中文：后文")
	m.client = client
	m.workspaces[m.wsIndex].Client = client
	m.moveEditCursor(0)
	m = interactionKey(t, m, tea.KeyMsg{Type: tea.KeyCtrlV})
	for range 3 {
		m = interactionKey(t, m, tea.KeyMsg{Type: tea.KeyShiftRight})
	}
	m = interactionKey(t, m, interactionRune('b'))
	next, cmd := m.updateInlineEdit(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)
	if m.editing || cmd == nil {
		t.Fatalf("Escape did not save and close formatted edit: editing=%v cmd=%v", m.editing, cmd != nil)
	}
	if msg := cmd(); msg == nil {
		t.Fatal("fake API write returned no completion message")
	} else if _, ok := msg.(writeDoneMsg); !ok {
		t.Fatalf("fake API completion = %T (%v), want writeDoneMsg", msg, msg)
	}
	if request.method != http.MethodPatch || request.path != "/v1/blocks/edit-block" {
		t.Fatalf("fake API request = %s %s", request.method, request.path)
	}
	var visible, bold strings.Builder
	for _, run := range request.body.Paragraph.RichText {
		visible.WriteString(run.Text.Content)
		for range run.Text.Content {
			if run.Annotations.Bold {
				bold.WriteByte('1')
			} else {
				bold.WriteByte('0')
			}
		}
	}
	if visible.String() != "中文：后文" || bold.String() != "11100" {
		t.Fatalf("fake API rich_text content/bold mask = %q/%q, want 中文：后文/11100; payload %+v", visible.String(), bold.String(), request.body.Paragraph.RichText)
	}
}

type redirectRoundTripper struct {
	target *url.URL
	base   http.RoundTripper
}

func (rt redirectRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	copy := req.Clone(req.Context())
	target := *rt.target
	target.Path = req.URL.Path
	target.RawQuery = req.URL.RawQuery
	copy.URL = &target
	copy.Host = target.Host
	return rt.base.RoundTrip(copy)
}

func notionClientForTestServer(t *testing.T, server *httptest.Server) *notion.Client {
	t.Helper()
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	base := server.Client().Transport
	if base == nil {
		base = http.DefaultTransport
	}
	previous := http.DefaultClient
	http.DefaultClient = &http.Client{Transport: redirectRoundTripper{target: target, base: base}}
	client := notion.NewClient("dummy-notion-test-token")
	http.DefaultClient = previous
	return client
}
