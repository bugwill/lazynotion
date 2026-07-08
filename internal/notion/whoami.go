package notion

import (
	"context"
	"encoding/json"
)

// BotInfo identifies the integration a token belongs to.
type BotInfo struct {
	Name          string
	WorkspaceName string
}

// WhoAmI validates the client's token against the API and reports which
// integration and workspace it belongs to.
func (c *Client) WhoAmI(ctx context.Context) (BotInfo, error) {
	data, err := c.rawRequest(ctx, "GET", "/users/me", nil)
	if err != nil {
		return BotInfo{}, err
	}
	var raw struct {
		Name string `json:"name"`
		Bot  struct {
			WorkspaceName string `json:"workspace_name"`
		} `json:"bot"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return BotInfo{}, err
	}
	return BotInfo{Name: raw.Name, WorkspaceName: raw.Bot.WorkspaceName}, nil
}
