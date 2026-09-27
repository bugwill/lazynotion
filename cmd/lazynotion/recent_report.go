package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/justinm35/lazynotion/internal/cache"
	"github.com/justinm35/lazynotion/internal/config"
	"github.com/justinm35/lazynotion/internal/notion"
)

// A read-only application diagnostic: configuration stays inside the normal
// application loader, and output contains only API metadata and query metrics.
func recentReport(cfg config.Config, args []string) error {
	flags := flag.NewFlagSet("recent-report", flag.ContinueOnError)
	days := flags.Int("days", 14, "number of days of modified pages to extract")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *days < 1 || *days > 3650 || flags.NArg() != 0 {
		return fmt.Errorf("use recent-report --days N (1 through 3650)")
	}
	workspace := cfg.Workspaces[cfg.DefaultIndex]
	client := notion.NewClient(workspace.Token)
	var cached []notion.Page
	if store, err := cache.Open(); err == nil {
		var index notion.RecentIndex
		if store.LoadMetadata("recent-index-v1:"+client.CacheKey(), &index) {
			for _, p := range index.Pages {
				if p.Kind == notion.KindDataSource {
					cached = append(cached, p)
				}
			}
		}
	}
	until := time.Now().UTC()
	since := until.Add(-time.Duration(*days) * 24 * time.Hour)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	result := client.RecentWindow(ctx, since, until, cached, func(p notion.QueryProgress) {
		if p.Total > 0 {
			fmt.Fprintf(os.Stderr, "%s %d/%d\n", p.Stage, p.Done, p.Total)
		} else {
			fmt.Fprintf(os.Stderr, "%s %d\n", p.Stage, p.Done)
		}
	})
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		return err
	}
	if len(result.Errors) > 0 {
		return fmt.Errorf("extraction partly failed; see the report's errors")
	}
	return nil
}
