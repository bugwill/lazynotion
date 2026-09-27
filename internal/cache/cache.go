// Package cache persists fetched page blocks so revisited pages render
// instantly; a cheap last_edited_time check decides whether to refetch.
package cache

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jomei/notionapi"

	"github.com/justinm35/lazynotion/internal/notion"
)

type Store struct {
	dir string
}

// SavePages persists browse/search metadata separately from page block trees.
func (s *Store) SavePages(key string, pages []notion.Page) error {
	return s.SaveMetadata("pages:"+key, pages)
}

func (s *Store) SaveMetadata(key string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	path := s.pagesPath(key)
	f, err := os.CreateTemp(s.dir, "list-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func (s *Store) pagesPath(key string) string {
	return filepath.Join(s.dir, fmt.Sprintf("list-%x.json", sha256.Sum256([]byte(key))))
}

func (s *Store) LoadPages(key string) ([]notion.Page, bool) {
	var pages []notion.Page
	ok := s.LoadMetadata("pages:"+key, &pages)
	return pages, ok
}

func (s *Store) LoadMetadata(key string, value any) bool {
	data, err := os.ReadFile(s.pagesPath(key))
	if err != nil {
		return false
	}
	return json.Unmarshal(data, value) == nil
}

func Open() (*Store, error) {
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		base = filepath.Join(home, ".cache")
	}
	dir := filepath.Join(base, "lazynotion", "pages")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

func OpenAt(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

type diskNode struct {
	Block    json.RawMessage `json:"block"`
	Children []diskNode      `json:"children,omitempty"`
}

type entry struct {
	LastEdited time.Time  `json:"last_edited"`
	SavedAt    time.Time  `json:"saved_at"`
	Nodes      []diskNode `json:"nodes"`
}

func (s *Store) path(pageID string) string {
	return filepath.Join(s.dir, pageID+".json")
}

func (s *Store) Has(pageID string) bool {
	_, err := os.Stat(s.path(pageID))
	return err == nil
}

func (s *Store) Save(pageID string, lastEdited time.Time, nodes []notion.BlockNode) error {
	disk, err := encodeNodes(nodes)
	if err != nil {
		return err
	}
	data, err := json.Marshal(entry{
		LastEdited: lastEdited,
		SavedAt:    time.Now(),
		Nodes:      disk,
	})
	if err != nil {
		return err
	}
	tmp := s.path(pageID) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path(pageID))
}

// Load returns the cached tree and the last_edited_time it was saved
// against. Any decode problem reads as a miss — the page just refetches.
func (s *Store) Load(pageID string) ([]notion.BlockNode, time.Time, bool) {
	data, err := os.ReadFile(s.path(pageID))
	if err != nil {
		return nil, time.Time{}, false
	}
	var e entry
	if err := json.Unmarshal(data, &e); err != nil {
		return nil, time.Time{}, false
	}
	nodes, err := decodeNodes(e.Nodes)
	if err != nil {
		return nil, time.Time{}, false
	}
	return nodes, e.LastEdited, true
}

func (s *Store) Invalidate(pageID string) {
	os.Remove(s.path(pageID))
}

func encodeNodes(nodes []notion.BlockNode) ([]diskNode, error) {
	out := make([]diskNode, 0, len(nodes))
	for _, n := range nodes {
		raw, err := json.Marshal(n.Block)
		if err != nil {
			return nil, err
		}
		children, err := encodeNodes(n.Children)
		if err != nil {
			return nil, err
		}
		out = append(out, diskNode{Block: raw, Children: children})
	}
	return out, nil
}

func decodeNodes(disk []diskNode) ([]notion.BlockNode, error) {
	out := make([]notion.BlockNode, 0, len(disk))
	for _, d := range disk {
		// notionapi.Blocks owns the type-dispatching unmarshal, so each
		// block rides through it as a single-element array
		var blocks notionapi.Blocks
		if err := json.Unmarshal(append(append([]byte("["), d.Block...), ']'), &blocks); err != nil {
			return nil, err
		}
		if len(blocks) != 1 {
			continue
		}
		children, err := decodeNodes(d.Children)
		if err != nil {
			return nil, err
		}
		out = append(out, notion.BlockNode{Block: blocks[0], Children: children})
	}
	return out, nil
}
