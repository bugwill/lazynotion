package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestBorderTitleKeepsPaneGeometry(t *testing.T) {
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Width(30).Height(3).Render("content")

	titled := withBorderTitle(box, "Pages · work", true)
	tLines := strings.Split(titled, "\n")
	bLines := strings.Split(box, "\n")
	if len(tLines) != len(bLines) {
		t.Fatalf("line count changed: %d -> %d", len(bLines), len(tLines))
	}
	for i, l := range tLines {
		if w := lipgloss.Width(l); w != 32 {
			t.Errorf("line %d width = %d, want 32", i, w)
		}
	}
	if !strings.Contains(stripAnsi(tLines[0]), "Pages · work") {
		t.Errorf("title missing from border: %q", stripAnsi(tLines[0]))
	}

	long := withBorderTitle(box, strings.Repeat("x", 100), false)
	if w := lipgloss.Width(strings.Split(long, "\n")[0]); w != 32 {
		t.Errorf("overlong title should truncate, width = %d", w)
	}
}
