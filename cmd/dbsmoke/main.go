// Read-only smoke test for the new database layer against the live API.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/justinm35/lazynotion/internal/config"
	"github.com/justinm35/lazynotion/internal/notion"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Println("config:", err)
		os.Exit(1)
	}
	client := notion.NewClient(cfg.Workspaces[0].Token)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pages, err := client.Search(ctx, "", false)
	if err != nil {
		fmt.Println("search:", err)
		os.Exit(1)
	}
	var ds []notion.Page
	for _, p := range pages {
		if p.Kind == notion.KindDataSource {
			ds = append(ds, p)
		}
	}
	fmt.Printf("search: %d results, %d data sources\n", len(pages), len(ds))
	for i, d := range ds {
		if i >= 5 {
			break
		}
		fmt.Printf("  🗃 %q (ds=%s db=%s)\n", d.Title, d.ID[:8], d.DatabaseID[:8])
	}
	if len(ds) == 0 {
		fmt.Println("no data sources visible to this integration — grant it access to a database to test further")
		return
	}

	target := ds[0]
	schema, err := client.GetDataSource(ctx, target.ID)
	if err != nil {
		fmt.Println("get data source:", err)
		os.Exit(1)
	}
	fmt.Printf("schema %q: %d columns, title=%q\n", schema.Title, len(schema.Properties), schema.TitleProperty())
	for _, p := range schema.Properties {
		fmt.Printf("  - %s (%s)\n", p.Name, p.Type)
	}

	db, err := client.GetDatabase(ctx, target.DatabaseID)
	if err != nil {
		fmt.Println("get database:", err)
		os.Exit(1)
	}
	fmt.Printf("database %q: %d data source(s)\n", db.Title, len(db.DataSources))

	rows, err := client.QueryDataSource(ctx, target.ID, "", 10)
	if err != nil {
		fmt.Println("query:", err)
		os.Exit(1)
	}
	fmt.Printf("query: %d rows (hasMore=%v)\n", len(rows.Rows), rows.HasMore)
	for _, r := range rows.Rows {
		fmt.Printf("  • %-30q", r.Title(schema.TitleProperty()))
		for _, p := range schema.Properties {
			if p.Type == "title" {
				continue
			}
			if v := r.Properties[p.Name].Display(); v != "" {
				fmt.Printf("  %s=%s", p.Name, v)
			}
		}
		fmt.Println()
	}
}
