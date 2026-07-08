package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestFormatBytes(t *testing.T) {
	cases := []struct {
		in   uint64
		want string
	}{
		{512, "512 B"},
		{8 << 10, "8 KB"},
		{14<<20 + 400<<10, "14.4 MB"},
		{3 << 30, "3.00 GB"},
	}
	for _, c := range cases {
		if got := formatBytes(c.in); got != c.want {
			t.Errorf("formatBytes(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFooterLineRightAlignsMemory(t *testing.T) {
	m := New([]Workspace{{Name: "test"}}, 0, nil, "mosaic")
	m.width, m.height = 120, 24
	m.loading = false
	m.memUsage = "12.3 MB"

	line := m.footerLine()
	if w := lipgloss.Width(line); w != 120 {
		t.Errorf("footer width = %d, want 120", w)
	}
	plain := strings.TrimRight(stripAnsi(line), " ")
	if !strings.HasSuffix(plain, "12.3 MB") {
		t.Errorf("footer should end with the memory readout: %q", plain)
	}
	if !strings.Contains(plain, "pages") {
		t.Errorf("footer should keep the status content on the left: %q", plain)
	}
}

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
