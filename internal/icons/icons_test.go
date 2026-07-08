package icons

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/jomei/notionapi"
	"github.com/muesli/termenv"
)

func TestFromNotionEmoji(t *testing.T) {
	emoji := notionapi.Emoji("🚀")
	ic := FromNotion(&notionapi.Icon{Type: "emoji", Emoji: &emoji})
	if ic.Emoji != "🚀" || ic.Glyph() != "🚀" || ic.Render() != "🚀" {
		t.Errorf("emoji icon = %+v", ic)
	}
}

func TestFromNotionBuiltin(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })
	ic := FromNotion(&notionapi.Icon{
		Type:     "external",
		External: &notionapi.FileObject{URL: "https://www.notion.so/icons/checklist_green.svg"},
	})
	if ic.Name != "checklist" || ic.Color != "green" {
		t.Fatalf("builtin icon = %+v", ic)
	}
	if ic.Glyph() != "✓" {
		t.Errorf("glyph = %q, want ✓", ic.Glyph())
	}
	if !strings.Contains(ic.Render(), "✓") || !strings.Contains(ic.Render(), "\x1b[") {
		t.Errorf("render should be a colored glyph: %q", ic.Render())
	}
}

func TestFromNotionCustomUpload(t *testing.T) {
	ic := FromNotion(&notionapi.Icon{
		Type: "file",
		File: &notionapi.FileObject{URL: "https://s3.amazonaws.com/x/logo.png"},
	})
	if !ic.Custom || ic.Glyph() != "◆" {
		t.Errorf("custom icon = %+v glyph %q", ic, ic.Glyph())
	}
}

func TestGlyphPrefixFallback(t *testing.T) {
	if g := glyphFor("arrow-up-line"); g != "↑" {
		t.Errorf("arrow-up-line = %q, want ↑ via prefix fallback", g)
	}
	if g := glyphFor("completely-unknown-thing"); g != "◆" {
		t.Errorf("unknown icon = %q, want fallback ◆", g)
	}
}

func TestFromNotionNil(t *testing.T) {
	if ic := FromNotion(nil); !ic.IsZero() || ic.Render() != "" {
		t.Errorf("nil icon should be zero, got %+v", ic)
	}
}

func TestFromNotionUnderscoreName(t *testing.T) {
	ic := FromNotion(&notionapi.Icon{
		Type:     "external",
		External: &notionapi.FileObject{URL: "https://www.notion.so/icons/arrow_up_gray.svg?mode=dark"},
	})
	if ic.Name != "arrow-up" || ic.Color != "gray" {
		t.Fatalf("underscore icon = %+v", ic)
	}
	if ic.Glyph() != "↑" {
		t.Errorf("glyph = %q, want ↑", ic.Glyph())
	}
}

func TestFromNotionUnknownTypeGetsFallback(t *testing.T) {
	// custom_emoji payloads decode with only Type set
	ic := FromNotion(&notionapi.Icon{Type: "custom_emoji"})
	if !ic.Custom || ic.Glyph() != "◆" {
		t.Errorf("unknown icon type should fall back to ◆, got %+v", ic)
	}
}
