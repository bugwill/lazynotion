package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/lipgloss"
)

// glamourStyle overrides the auto dark/light markdown theme when the config
// names a style or points at a JSON style file.
var glamourStyle *ansi.StyleConfig

// ApplyTheme applies config theming. It must run before the first render:
// the derived styles are rebuilt here, and the glamour renderer reads
// glamourStyle lazily.
func ApplyTheme(accent, style string) {
	if accent != "" {
		accentColor = lipgloss.Color(accent)
		accentDimColor = lipgloss.Color(accent)
		rebuildAccentStyles()
	}
	if style != "" {
		if cfg := resolveGlamourStyle(style); cfg != nil {
			glamourStyle = cfg
		}
	}
}

// resolveGlamourStyle accepts a built-in name (dark, light, dracula,
// tokyo-night, pink, ascii, notty) or a path to a glamour style JSON.
func resolveGlamourStyle(style string) *ansi.StyleConfig {
	if strings.ContainsAny(style, "/.") {
		data, err := os.ReadFile(expandHome(style))
		if err != nil {
			return nil
		}
		var cfg ansi.StyleConfig
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil
		}
		return &cfg
	}
	if cfg, ok := styles.DefaultStyles[strings.ToLower(style)]; ok {
		clone := *cfg
		return &clone
	}
	return nil
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

// rebuildAccentStyles refreshes every style derived from the accent colors —
// they are package vars built at init from the defaults.
func rebuildAccentStyles() {
	borderTitleFocused = lipgloss.NewStyle().Bold(true).Foreground(accentColor)
	focusedPaneStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accentColor)
	selectedTitleStyle = lipgloss.NewStyle().Foreground(accentColor).Bold(true)
	selectedDescStyle = lipgloss.NewStyle().Foreground(accentDimColor)
	paletteQueryStyle = lipgloss.NewStyle().Foreground(accentColor).Bold(true).Padding(0, 1)
	paletteSelectedStyle = lipgloss.NewStyle().Foreground(accentColor).Bold(true).Padding(0, 1)
	helpBoxStyle = helpBoxStyle.BorderForeground(accentColor)
	helpSectionStyle = lipgloss.NewStyle().Bold(true).Foreground(accentColor)
}
