package notion

import (
	"encoding/json"
	"fmt"

	"github.com/jomei/notionapi"
)

// Heading4Block covers the heading level missing from the upstream SDK.
type Heading4Block struct {
	notionapi.BasicBlock
	Heading4 notionapi.Heading `json:"heading_4"`
}

// DecodeBlock is shared by API responses, disk caches and cloned blocks.
func DecodeBlock(data []byte) (notionapi.Block, error) {
	var basic notionapi.BasicBlock
	if err := json.Unmarshal(data, &basic); err != nil {
		return nil, err
	}
	if basic.Type == "" {
		return nil, fmt.Errorf("block has no type")
	}
	if basic.Type == "heading_4" {
		var block Heading4Block
		if err := json.Unmarshal(data, &block); err != nil {
			return nil, err
		}
		return &block, nil
	}
	var blocks notionapi.Blocks
	if err := json.Unmarshal(append(append([]byte("["), data...), ']'), &blocks); err != nil {
		return nil, err
	}
	if len(blocks) != 1 {
		return nil, fmt.Errorf("expected one block")
	}
	// The SDK discards metadata for unknown types; keep it for diagnostics
	// and to ensure an unrecognized block still retains its identity.
	if block, ok := blocks[0].(*notionapi.UnsupportedBlock); ok {
		block.BasicBlock = basic
	}
	return blocks[0], nil
}
