package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/BurntSushi/toml"
)

var ErrNoToken = errors.New("no Notion token found")

type Workspace struct {
	Name  string
	Token string
	// RootOnly limits the sidebar to workspace-level pages; nested pages
	// are reached by navigating into their parents (or via search).
	RootOnly bool
}

type Config struct {
	Workspaces   []Workspace
	DefaultIndex int
	// Images is "auto" (sharp images on kitty/ghostty, mosaic elsewhere),
	// "pixels" (force the kitty graphics protocol) or "mosaic" (force the
	// half-block fallback).
	Images string
	// Accent recolors the UI chrome (borders, cursor, selections); any
	// lipgloss color: ANSI-256 index ("205") or hex ("#ff79c6").
	Accent string
	// Style picks the glamour markdown theme: a built-in name (dark,
	// light, dracula, tokyo-night, pink, ascii, notty) or a path to a
	// glamour style JSON file.
	Style string
}

func Path() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "lazynotion", "config.toml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "lazynotion", "config.toml"), nil
}

// Load reads workspaces from the config file. Three forms compose:
//
//	token = "ntn_..."             # single workspace named "default"
//
//	default = "personal"          # starting workspace
//	[workspaces.personal]
//	token = "ntn_..."
//	[workspaces.work]
//	token = "ntn_..."
//
// A NOTION_TOKEN env var adds an "env" workspace and takes precedence as
// the starting workspace, matching the old single-token behavior.
func Load() (Config, error) {
	var raw struct {
		Token         string `toml:"token"`
		Default       string `toml:"default"`
		Images        string `toml:"images"`
		Accent        string `toml:"accent"`
		Style         string `toml:"style"`
		RootPagesOnly bool   `toml:"root_pages_only"`
		Workspaces    map[string]struct {
			Token         string `toml:"token"`
			RootPagesOnly *bool  `toml:"root_pages_only"`
		} `toml:"workspaces"`
	}
	if path, err := Path(); err == nil {
		if _, err := toml.DecodeFile(path, &raw); err != nil && !os.IsNotExist(err) {
			return Config{}, err
		}
	}

	var cfg Config
	names := make([]string, 0, len(raw.Workspaces))
	for name := range raw.Workspaces {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		w := raw.Workspaces[name]
		if w.Token == "" {
			continue
		}
		rootOnly := raw.RootPagesOnly
		if w.RootPagesOnly != nil {
			rootOnly = *w.RootPagesOnly
		}
		cfg.Workspaces = append(cfg.Workspaces, Workspace{Name: name, Token: w.Token, RootOnly: rootOnly})
	}
	if raw.Token != "" {
		cfg.Workspaces = append(cfg.Workspaces, Workspace{Name: "default", Token: raw.Token, RootOnly: raw.RootPagesOnly})
	}
	if env := os.Getenv("NOTION_TOKEN"); env != "" {
		cfg.Workspaces = append([]Workspace{{Name: "env", Token: env, RootOnly: raw.RootPagesOnly}}, cfg.Workspaces...)
	}
	if len(cfg.Workspaces) == 0 {
		return cfg, ErrNoToken
	}

	cfg.Accent = raw.Accent
	cfg.Style = raw.Style

	switch raw.Images {
	case "":
		cfg.Images = "auto"
	case "auto", "pixels", "mosaic":
		cfg.Images = raw.Images
	default:
		return cfg, fmt.Errorf("images must be \"auto\", \"pixels\" or \"mosaic\", got %q", raw.Images)
	}

	if raw.Default != "" && os.Getenv("NOTION_TOKEN") == "" {
		found := false
		for i, w := range cfg.Workspaces {
			if w.Name == raw.Default {
				cfg.DefaultIndex = i
				found = true
				break
			}
		}
		if !found {
			return cfg, fmt.Errorf("default workspace %q is not defined", raw.Default)
		}
	}
	return cfg, nil
}
