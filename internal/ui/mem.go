package ui

import (
	"fmt"
	"runtime"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const memRefreshInterval = 10 * time.Second

type memMsg struct{ heapBytes uint64 }

// measureMem reports live heap only. Without the forced collection the
// reading includes garbage the GC hasn't bothered with yet — Go collects
// lazily, so the number would climb steadily even at idle (each ticker wake
// re-renders the UI, allocating transient strings) and read like a leak.
// A GC cycle on a heap this size costs well under a millisecond.
func measureMem() tea.Msg {
	runtime.GC()
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return memMsg{heapBytes: ms.HeapAlloc}
}

func nextMemTick() tea.Cmd {
	return tea.Tick(memRefreshInterval, func(time.Time) tea.Msg {
		return measureMem()
	})
}

// The sync glyph "pulses" by cycling brightness — the closest a terminal
// gets to opacity. The ticker only runs while something is syncing.
const pulseInterval = 200 * time.Millisecond

type pulseMsg struct{}

func nextPulseTick() tea.Cmd {
	return tea.Tick(pulseInterval, func(time.Time) tea.Msg {
		return pulseMsg{}
	})
}

var pulseShades = []string{"94", "130", "172", "214", "172", "130"}

func formatBytes(b uint64) string {
	switch {
	case b < 1<<10:
		return fmt.Sprintf("%d B", b)
	case b < 1<<20:
		return fmt.Sprintf("%.0f KB", float64(b)/(1<<10))
	case b < 1<<30:
		return fmt.Sprintf("%.1f MB", float64(b)/(1<<20))
	default:
		return fmt.Sprintf("%.2f GB", float64(b)/(1<<30))
	}
}
