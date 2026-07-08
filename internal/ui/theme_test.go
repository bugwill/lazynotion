package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func TestApplyThemeAccent(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() {
		lipgloss.SetColorProfile(termenv.Ascii)
		accentColor = lipgloss.Color("117")
		accentDimColor = lipgloss.Color("110")
		rebuildAccentStyles()
	})

	ApplyTheme("205", "")
	rendered := borderTitleFocused.Render("x")
	if !strings.Contains(rendered, "205") {
		t.Errorf("focused title should use the custom accent: %q", rendered)
	}
	rendered = paletteSelectedStyle.Render("x")
	if !strings.Contains(rendered, "205") {
		t.Errorf("palette selection should use the custom accent: %q", rendered)
	}
}

func TestResolveGlamourStyle(t *testing.T) {
	if resolveGlamourStyle("dracula") == nil {
		t.Error("built-in dracula style should resolve")
	}
	if resolveGlamourStyle("DRACULA") == nil {
		t.Error("style names should be case-insensitive")
	}
	if resolveGlamourStyle("not-a-style") != nil {
		t.Error("unknown style must not resolve")
	}

	path := filepath.Join(t.TempDir(), "mine.json")
	os.WriteFile(path, []byte(`{"document": {"color": "252"}}`), 0o600)
	if resolveGlamourStyle(path) == nil {
		t.Error("style JSON file should resolve")
	}
	if resolveGlamourStyle(filepath.Join(t.TempDir(), "missing.json")) != nil {
		t.Error("missing file must not resolve")
	}
}

func TestBlockLinkAndOSC52(t *testing.T) {
	link := blockLink("https://www.notion.so/My-Page-abc123", "0d9f5a8e-1234-5678-9abc-def012345678")
	if link != "https://www.notion.so/My-Page-abc123#0d9f5a8e123456789abcdef012345678" {
		t.Errorf("block link = %q", link)
	}
	seq := osc52("hello")
	if !strings.HasPrefix(seq, "\x1b]52;c;") || !strings.HasSuffix(seq, "\x07") {
		t.Errorf("osc52 = %q", seq)
	}
	if !strings.Contains(seq, "aGVsbG8=") {
		t.Errorf("osc52 payload should be base64: %q", seq)
	}
}
