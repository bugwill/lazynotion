package notion

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/justinm35/lazynotion/internal/icons"
)

// Databases go through the raw API under Notion-Version 2025-09-03: that
// version replaced the legacy /databases/{id}/query endpoint with data
// sources (a database holds one or more data sources; rows hang off the
// data source). The client library predates all of this.

// DataSourceRef identifies one data source inside a database.
type DataSourceRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Database is the container object — usually holding exactly one data source.
type Database struct {
	ID          string
	Title       string
	Icon        icons.Icon
	DataSources []DataSourceRef
	LastEdited  time.Time
}

// PropertyConfig is one column of a data source's schema.
type PropertyConfig struct {
	ID   string
	Name string
	Type string
}

// DataSource carries the schema needed to lay out a table of rows.
type DataSource struct {
	ID         string
	DatabaseID string
	Title      string
	Icon       icons.Icon
	Properties []PropertyConfig // title column first, then alphabetical
	LastEdited time.Time
}

// TitleProperty returns the name of the title column ("" if none).
func (ds DataSource) TitleProperty() string {
	for _, p := range ds.Properties {
		if p.Type == "title" {
			return p.Name
		}
	}
	return ""
}

// SelectOption is a select/multi-select/status value with its Notion color.
type SelectOption struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

// DateValue holds a date property's ISO-8601 start/end strings.
type DateValue struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

// PropertyValue is one cell: the property type plus whichever field that
// type populates. Unknown types keep Type set and everything else zero.
type PropertyValue struct {
	ID       string
	Type     string
	Text     string         // title, rich_text, url, email, phone_number, unique_id, string formula
	Number   *float64       // number, number formula/rollup
	Checkbox bool           // checkbox, boolean formula
	Options  []SelectOption // select, status (one), multi_select (many)
	Date     *DateValue     // date, created_time, last_edited_time, date formula
	People   []string       // people, created_by, last_edited_by
	Files    []string       // file names
	Relation int            // count of related pages
	HasValue bool           // false when the cell is empty
}

// Display renders the cell as plain text for the grid; the UI layer adds
// color for selects.
func (v PropertyValue) Display() string {
	if !v.HasValue {
		return ""
	}
	switch {
	case v.Type == "checkbox" || (v.Type == "formula" && v.Number == nil && v.Text == "" && v.Date == nil):
		if v.Checkbox {
			return "✓"
		}
		return ""
	case v.Number != nil:
		return strconv.FormatFloat(*v.Number, 'f', -1, 64)
	case len(v.Options) > 0:
		names := make([]string, len(v.Options))
		for i, o := range v.Options {
			names[i] = o.Name
		}
		return strings.Join(names, ", ")
	case v.Date != nil:
		if v.Date.End != "" {
			return humanDate(v.Date.Start) + " → " + humanDate(v.Date.End)
		}
		return humanDate(v.Date.Start)
	case len(v.People) > 0:
		return strings.Join(v.People, ", ")
	case len(v.Files) > 0:
		return strings.Join(v.Files, ", ")
	case v.Type == "relation":
		if v.Relation == 1 {
			return "1 linked"
		}
		return fmt.Sprintf("%d linked", v.Relation)
	}
	return v.Text
}

// humanDate trims ISO timestamps down to the date (Notion sends either
// bare dates or full timestamps).
func humanDate(s string) string {
	if len(s) > 10 {
		return s[:10]
	}
	return s
}

// Row is one data-source row — in Notion terms, a page whose properties
// follow the data source's schema.
type Row struct {
	ID         string
	Icon       icons.Icon
	URL        string
	LastEdited time.Time
	Properties map[string]PropertyValue // keyed by property name
}

// Title returns the row's title-property text.
func (r Row) Title(titleProp string) string {
	if v, ok := r.Properties[titleProp]; ok && strings.TrimSpace(v.Text) != "" {
		return strings.TrimSpace(v.Text)
	}
	return "Untitled"
}

// RowPage is one page of query results.
type RowPage struct {
	Rows       []Row
	HasMore    bool
	NextCursor string
}

// --- raw JSON shapes -------------------------------------------------------

type rawRichText struct {
	PlainText string `json:"plain_text"`
}

func joinPlain(rt []rawRichText) string {
	var b strings.Builder
	for _, t := range rt {
		b.WriteString(t.PlainText)
	}
	return b.String()
}

type rawPropertyValue struct {
	ID   string          `json:"id"`
	Type string          `json:"type"`
	Rest map[string]json.RawMessage
}

func (p *rawPropertyValue) UnmarshalJSON(data []byte) error {
	var head struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return err
	}
	p.ID, p.Type = head.ID, head.Type
	return json.Unmarshal(data, &p.Rest)
}

// parsePropertyValue converts one raw cell payload into a PropertyValue.
func parsePropertyValue(raw rawPropertyValue) PropertyValue {
	v := PropertyValue{ID: raw.ID, Type: raw.Type}
	payload, ok := raw.Rest[raw.Type]
	if !ok || string(payload) == "null" {
		return v
	}
	switch raw.Type {
	case "title", "rich_text":
		var rt []rawRichText
		json.Unmarshal(payload, &rt)
		v.Text = joinPlain(rt)
		v.HasValue = v.Text != ""
	case "number":
		var n float64
		if json.Unmarshal(payload, &n) == nil {
			v.Number = &n
			v.HasValue = true
		}
	case "checkbox":
		json.Unmarshal(payload, &v.Checkbox)
		v.HasValue = true
	case "select", "status":
		var o SelectOption
		if json.Unmarshal(payload, &o) == nil && o.Name != "" {
			v.Options = []SelectOption{o}
			v.HasValue = true
		}
	case "multi_select":
		json.Unmarshal(payload, &v.Options)
		v.HasValue = len(v.Options) > 0
	case "date", "created_time", "last_edited_time":
		if raw.Type == "date" {
			var d DateValue
			if json.Unmarshal(payload, &d) == nil && d.Start != "" {
				v.Date = &d
				v.HasValue = true
			}
		} else {
			var s string
			if json.Unmarshal(payload, &s) == nil && s != "" {
				v.Date = &DateValue{Start: s}
				v.HasValue = true
			}
		}
	case "url", "email", "phone_number":
		json.Unmarshal(payload, &v.Text)
		v.HasValue = v.Text != ""
	case "people", "created_by", "last_edited_by":
		var users []struct {
			Name string `json:"name"`
		}
		single := raw.Type != "people"
		if single {
			var u struct {
				Name string `json:"name"`
			}
			if json.Unmarshal(payload, &u) == nil && u.Name != "" {
				users = append(users, u)
			}
		} else {
			json.Unmarshal(payload, &users)
		}
		for _, u := range users {
			if u.Name != "" {
				v.People = append(v.People, u.Name)
			}
		}
		v.HasValue = len(v.People) > 0
	case "files":
		var files []struct {
			Name string `json:"name"`
		}
		json.Unmarshal(payload, &files)
		for _, f := range files {
			v.Files = append(v.Files, f.Name)
		}
		v.HasValue = len(v.Files) > 0
	case "relation":
		var rel []struct {
			ID string `json:"id"`
		}
		json.Unmarshal(payload, &rel)
		v.Relation = len(rel)
		v.HasValue = v.Relation > 0
	case "unique_id":
		var u struct {
			Prefix string   `json:"prefix"`
			Number *float64 `json:"number"`
		}
		if json.Unmarshal(payload, &u) == nil && u.Number != nil {
			num := strconv.FormatFloat(*u.Number, 'f', -1, 64)
			if u.Prefix != "" {
				v.Text = u.Prefix + "-" + num
			} else {
				v.Text = num
			}
			v.HasValue = true
		}
	case "formula", "rollup":
		parseComputed(payload, &v)
	}
	return v
}

// parseComputed handles formula and rollup payloads, which nest a second
// typed value: {"type":"number","number":5}.
func parseComputed(payload json.RawMessage, v *PropertyValue) {
	var inner struct {
		Type    string          `json:"type"`
		String  *string         `json:"string"`
		Number  *float64        `json:"number"`
		Boolean *bool           `json:"boolean"`
		Date    *DateValue      `json:"date"`
		Array   json.RawMessage `json:"array"`
	}
	if json.Unmarshal(payload, &inner) != nil {
		return
	}
	switch inner.Type {
	case "string":
		if inner.String != nil {
			v.Text = *inner.String
			v.HasValue = v.Text != ""
		}
	case "number":
		if inner.Number != nil {
			v.Number = inner.Number
			v.HasValue = true
		}
	case "boolean":
		if inner.Boolean != nil {
			v.Checkbox = *inner.Boolean
			v.HasValue = true
		}
	case "date":
		if inner.Date != nil && inner.Date.Start != "" {
			v.Date = inner.Date
			v.HasValue = true
		}
	case "array":
		var items []rawPropertyValue
		if json.Unmarshal(inner.Array, &items) == nil && len(items) > 0 {
			var parts []string
			for _, item := range items {
				if s := parsePropertyValue(item).Display(); s != "" {
					parts = append(parts, s)
				}
			}
			v.Text = strings.Join(parts, ", ")
			v.HasValue = v.Text != ""
		}
	}
}

type rawDataSource struct {
	ID     string          `json:"id"`
	Title  []rawRichText   `json:"title"`
	Name   []rawRichText   `json:"name"`
	Icon   json.RawMessage `json:"icon"`
	Parent struct {
		DatabaseID string `json:"database_id"`
	} `json:"parent"`
	LastEditedTime time.Time `json:"last_edited_time"`
	Properties     map[string]struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	} `json:"properties"`
}

func (r rawDataSource) toDataSource() DataSource {
	ds := DataSource{
		ID:         r.ID,
		DatabaseID: r.Parent.DatabaseID,
		Title:      strings.TrimSpace(joinPlain(r.Title)),
		Icon:       icons.FromRaw(r.Icon),
		LastEdited: r.LastEditedTime,
	}
	if ds.Title == "" {
		ds.Title = strings.TrimSpace(joinPlain(r.Name))
	}
	if ds.Title == "" {
		ds.Title = "Untitled"
	}
	for name, cfg := range r.Properties {
		ds.Properties = append(ds.Properties, PropertyConfig{ID: cfg.ID, Name: name, Type: cfg.Type})
	}
	// The API returns properties as an unordered object; pin the title
	// column first and sort the rest so the layout is deterministic.
	sort.Slice(ds.Properties, func(i, j int) bool {
		pi, pj := ds.Properties[i], ds.Properties[j]
		if (pi.Type == "title") != (pj.Type == "title") {
			return pi.Type == "title"
		}
		return pi.Name < pj.Name
	})
	return ds
}

type rawRowPage struct {
	Object         string                      `json:"object"`
	ID             string                      `json:"id"`
	URL            string                      `json:"url"`
	Icon           json.RawMessage             `json:"icon"`
	LastEditedTime time.Time                   `json:"last_edited_time"`
	Properties     map[string]rawPropertyValue `json:"properties"`
}

func (r rawRowPage) toRow() Row {
	row := Row{
		ID:         r.ID,
		URL:        r.URL,
		Icon:       icons.FromRaw(r.Icon),
		LastEdited: r.LastEditedTime,
		Properties: make(map[string]PropertyValue, len(r.Properties)),
	}
	for name, raw := range r.Properties {
		row.Properties[name] = parsePropertyValue(raw)
	}
	return row
}

// --- endpoints -------------------------------------------------------------

// GetDatabase fetches the database container, mainly to learn its data
// sources.
func (c *Client) GetDatabase(ctx context.Context, databaseID string) (*Database, error) {
	data, err := c.rawRequestV(ctx, http.MethodGet, "/databases/"+databaseID, nil, versionDataSources)
	if err != nil {
		return nil, err
	}
	var raw struct {
		ID             string          `json:"id"`
		Title          []rawRichText   `json:"title"`
		Icon           json.RawMessage `json:"icon"`
		LastEditedTime time.Time       `json:"last_edited_time"`
		DataSources    []DataSourceRef `json:"data_sources"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	db := &Database{
		ID:          raw.ID,
		Title:       strings.TrimSpace(joinPlain(raw.Title)),
		Icon:        icons.FromRaw(raw.Icon),
		DataSources: raw.DataSources,
		LastEdited:  raw.LastEditedTime,
	}
	if db.Title == "" {
		db.Title = "Untitled"
	}
	return db, nil
}

// GetDataSource fetches a data source's schema.
func (c *Client) GetDataSource(ctx context.Context, dataSourceID string) (*DataSource, error) {
	data, err := c.rawRequestV(ctx, http.MethodGet, "/data_sources/"+dataSourceID, nil, versionDataSources)
	if err != nil {
		return nil, err
	}
	var raw rawDataSource
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	ds := raw.toDataSource()
	return &ds, nil
}

// QueryDataSource fetches one page of rows. An empty cursor starts from the
// beginning.
func (c *Client) QueryDataSource(ctx context.Context, dataSourceID, cursor string, pageSize int) (*RowPage, error) {
	req := map[string]any{"page_size": pageSize}
	if cursor != "" {
		req["start_cursor"] = cursor
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	data, err := c.rawRequestV(ctx, http.MethodPost, "/data_sources/"+dataSourceID+"/query", body, versionDataSources)
	if err != nil {
		return nil, err
	}
	var raw struct {
		Results    []rawRowPage `json:"results"`
		HasMore    bool         `json:"has_more"`
		NextCursor string       `json:"next_cursor"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	page := &RowPage{HasMore: raw.HasMore, NextCursor: raw.NextCursor}
	for _, r := range raw.Results {
		page.Rows = append(page.Rows, r.toRow())
	}
	return page, nil
}
