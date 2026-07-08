package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/justinm35/lazynotion/internal/cache"
	"github.com/justinm35/lazynotion/internal/config"
	"github.com/justinm35/lazynotion/internal/notion"
	"github.com/justinm35/lazynotion/internal/ui"
)

const setupHelp = `lazynotion needs at least one Notion integration token.

Run "lazynotion auth" for a guided setup. Or by hand:

  1. Create an internal integration at https://www.notion.so/my-integrations
     (capabilities: Read, Update and Insert content)
  2. In Notion, share the pages you want to browse with that integration
     (page menu → Connections → your integration)
  3. Put the token(s) in %s:

     one workspace:
       token = "ntn_..."

     several workspaces (switch with w in the sidebar):
       default = "personal"

       [workspaces.personal]
       token = "ntn_..."

       [workspaces.work]
       token = "ntn_..."

     (or export NOTION_TOKEN=ntn_... for a quick start)
`

func main() {
	if len(os.Args) > 1 && os.Args[1] == "debug-graphics" {
		ui.DebugGraphics()
		return
	}

	if len(os.Args) > 1 && os.Args[1] == "auth" {
		if err := runAuth(); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	}

	cfg, err := config.Load()
	if err != nil {
		if errors.Is(err, config.ErrNoToken) {
			path, _ := config.Path()
			fmt.Fprintf(os.Stderr, setupHelp, path)
		} else {
			fmt.Fprintln(os.Stderr, "config error:", err)
		}
		os.Exit(1)
	}

	if len(os.Args) > 1 && os.Args[1] == "debug-icons" {
		debugIcons(cfg)
		return
	}

	workspaces := make([]ui.Workspace, len(cfg.Workspaces))
	for i, w := range cfg.Workspaces {
		workspaces[i] = ui.Workspace{Name: w.Name, Client: notion.NewClient(w.Token), RootOnly: w.RootOnly}
	}

	ui.ApplyTheme(cfg.Accent, cfg.Style)

	store, err := cache.Open()
	if err != nil {
		store = nil // caching disabled, everything still works
	}

	program := tea.NewProgram(ui.New(workspaces, cfg.DefaultIndex, store, cfg.Images), tea.WithAltScreen())
	if _, err := program.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func debugIcons(cfg config.Config) {
	for _, w := range cfg.Workspaces {
		fmt.Printf("── workspace %s ──\n", w.Name)
		lines, err := notion.NewClient(w.Token).IconReport(context.Background())
		if err != nil {
			fmt.Println("error:", err)
			continue
		}
		for _, l := range lines {
			fmt.Println(l)
		}
	}
}
