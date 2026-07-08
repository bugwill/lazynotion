package notion

import (
	"context"
	"fmt"

	"github.com/justinm35/lazynotion/internal/icons"
)

// IconReport dumps the raw icon JSON of recent pages next to what the
// parser made of it — a diagnostic for icon shapes we don't handle yet.
func (c *Client) IconReport(ctx context.Context) ([]string, error) {
	resp, err := c.rawSearch(ctx, "", "", 30)
	if err != nil {
		return nil, err
	}
	var lines []string
	for _, p := range resp.Results {
		raw := string(p.Icon)
		if raw == "" {
			raw = "null"
		}
		parsed := icons.FromRaw(p.Icon)
		lines = append(lines, fmt.Sprintf("%-30.30s raw=%s\n%31s→ name=%q color=%q emoji=%q custom=%v renders as %q",
			p.title(), raw, "", parsed.Name, parsed.Color, parsed.Emoji, parsed.Custom, parsed.Glyph()))
	}
	return lines, nil
}
