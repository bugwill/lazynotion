package convert

import (
	"strings"
	"testing"

	"github.com/jomei/notionapi"
	"github.com/justinm35/lazynotion/internal/notion"
)

func TestHeading4NumberedTextRoundTripAndFolding(t *testing.T) {
	const md = "#### 1. **肥胖症长期组合与市场分层**"
	blocks := ParseMarkdown(md)
	if len(blocks) != 1 {
		t.Fatalf("blocks=%d", len(blocks))
	}
	h, ok := blocks[0].(*notion.Heading4Block)
	if !ok || h.Type != "heading_4" {
		t.Fatalf("heading downgraded: %T", blocks[0])
	}
	h.ID = "h4"
	nodes := NodesFromBlocks(blocks)
	if got := strings.TrimSpace(ToMarkdown(nodes)); got != md {
		t.Fatalf("roundtrip=%q", got)
	}
	u := Flatten(nodes)[0]
	edit, ok := u.EditableMarkdown()
	if !ok || edit != md {
		t.Fatalf("edit=%q", edit)
	}
	patch := ParseEditPatch(edit)
	if patch.Kind != "heading_4" || plain(patch.RichText) != "1. 肥胖症长期组合与市场分层" || !patch.RichText[1].Annotations.Bold {
		t.Fatalf("bad patch: %+v", patch)
	}
	if BuildBlock(patch.Kind, patch.RichText, false).GetType() != "heading_4" {
		t.Fatal("editing downgraded heading")
	}
	h.Heading4.IsToggleable = true
	nodes[0].Children = []notion.BlockNode{{Block: &notionapi.ParagraphBlock{Paragraph: notionapi.Paragraph{RichText: rt("child")}}}}
	if len(Flatten(nodes)) != 2 || len(FlattenFolded(nodes, map[string]bool{"h4": true})) != 1 || !Flatten(nodes)[0].Foldable {
		t.Fatal("heading4 folding failed")
	}
}
