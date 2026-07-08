package ui

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"os"
	"os/exec"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/sys/unix"
)

// Sharp inline images via the kitty graphics protocol's unicode
// placeholders: the image is transmitted once as a "virtual placement",
// then anchored to placeholder characters that live in the normal cell
// grid — so images scroll, clip and repaint like text, and Bubble Tea
// never needs to know. Supported by kitty and Ghostty; everyone else
// keeps the half-block mosaic.

type kittyPlacement struct {
	id   int
	cols int
	rows int
}

type kittyMsg struct {
	pageID    string
	blockID   string
	placement kittyPlacement
	err       error
}

// underlyingKittyTerminal detects a placeholder-capable terminal even
// through tmux, where TERM is rewritten but the emulator's env survives.
func underlyingKittyTerminal() bool {
	term := os.Getenv("TERM")
	if strings.Contains(term, "kitty") || strings.Contains(term, "ghostty") {
		return true
	}
	return os.Getenv("KITTY_WINDOW_ID") != "" || os.Getenv("GHOSTTY_RESOURCES_DIR") != ""
}

func inTmux() bool { return os.Getenv("TMUX") != "" }

// tmuxAllowsPassthrough is a var so tests can stub the tmux binary call.
var tmuxAllowsPassthrough = func() bool {
	out, err := exec.Command("tmux", "show", "-Ap", "allow-passthrough").Output()
	if err != nil {
		return false
	}
	value := strings.TrimSpace(string(out))
	return strings.HasSuffix(value, " on") || strings.HasSuffix(value, " all")
}

var (
	tmuxPassOnce sync.Once
	tmuxPassOK   bool
)

func tmuxPassthroughEnabled() bool {
	tmuxPassOnce.Do(func() { tmuxPassOK = tmuxAllowsPassthrough() })
	return tmuxPassOK
}

// supportsKittyGraphics reports whether sharp images will actually reach
// the terminal. Inside tmux that additionally requires allow-passthrough,
// which is checked against the live tmux config rather than assumed.
func supportsKittyGraphics() bool {
	if !underlyingKittyTerminal() {
		return false
	}
	if inTmux() {
		return tmuxPassthroughEnabled()
	}
	return true
}

// kittySupportHint explains the one fixable gap: a capable terminal behind
// a tmux that drops our escapes.
func kittySupportHint() string {
	if underlyingKittyTerminal() && inTmux() && !tmuxPassthroughEnabled() {
		return "sharp images need tmux passthrough: tmux set -g allow-passthrough on (then restart the pane)"
	}
	return ""
}

// wrapForTmux wraps an escape sequence in tmux's passthrough envelope so it
// reaches the outer terminal; ESC bytes inside must be doubled.
func wrapForTmux(seq string) string {
	if !inTmux() {
		return seq
	}
	return "\x1bPtmux;" + strings.ReplaceAll(seq, "\x1b", "\x1b\x1b") + "\x1b\\"
}

func (m Model) kittyEnabled() bool {
	switch m.imagesMode {
	case "mosaic":
		return false
	case "pixels":
		return true
	default:
		return supportsKittyGraphics()
	}
}

var (
	cellPxOnce sync.Once
	cellPxW    int
	cellPxH    int
)

// cellPixelSize asks the terminal how many pixels one cell occupies —
// kitty and Ghostty report it via TIOCGWINSZ. Without it, sizing falls
// back to the ~1:2 approximation.
func cellPixelSize() (int, int, bool) {
	cellPxOnce.Do(func() {
		tty := terminalTTY()
		if tty == nil {
			return
		}
		ws, err := unix.IoctlGetWinsize(int(tty.Fd()), unix.TIOCGWINSZ)
		if err != nil || ws.Col == 0 || ws.Row == 0 || ws.Xpixel == 0 || ws.Ypixel == 0 {
			return
		}
		cellPxW = int(ws.Xpixel) / int(ws.Col)
		cellPxH = int(ws.Ypixel) / int(ws.Row)
	})
	return cellPxW, cellPxH, cellPxW > 0 && cellPxH > 0
}

// kittyCellDims sizes the placement box using real cell pixel metrics:
// exact aspect (the terminal scales the image to fill the box, so a wrong
// box aspect smears it) and never larger than the image's native pixels
// (upscaling is what reads as "blurry").
func kittyCellDims(srcW, srcH, maxCols, maxRows int) (int, int) {
	cw, ch, ok := cellPixelSize()
	if !ok {
		return imageCellDims(srcW, srcH, maxCols, maxRows)
	}
	return kittyCellDimsWith(srcW, srcH, maxCols, maxRows, cw, ch)
}

func kittyCellDimsWith(srcW, srcH, maxCols, maxRows, cw, ch int) (int, int) {
	if srcW <= 0 || srcH <= 0 || cw <= 0 || ch <= 0 {
		return 0, 0
	}
	cols := min(maxCols, max((srcW+cw-1)/cw, 1))
	pxW := cols * cw
	pxH := srcH * pxW / srcW
	rows := max((pxH+ch-1)/ch, 1)
	if rows > maxRows {
		rows = maxRows
		pxH = rows * ch
		pxW = srcW * pxH / srcH
		cols = max(min(cols, (pxW+cw-1)/cw), 1)
	}
	return cols, rows
}

// imageCellDims sizes an image into terminal cells, aspect-preserving with
// the usual ~1:2 cell shape — the same math the mosaic uses.
func imageCellDims(srcW, srcH, maxCols, maxRows int) (cols, rows int) {
	if srcW <= 0 || srcH <= 0 {
		return 0, 0
	}
	cols = min(maxCols, srcW)
	if cols < 1 {
		cols = 1
	}
	pxH := srcH * cols / srcW
	rows = max((pxH+1)/2, 1)
	if rows > maxRows {
		rows = maxRows
		cols = min(cols, max(srcW*rows*2/srcH, 1))
	}
	return cols, rows
}

// kittyPlaceholderLines builds the cell-grid anchor for a transmitted
// image: each cell is U+10EEEE plus row/column diacritics, with the image
// ID encoded in the foreground color.
func kittyPlaceholderLines(p kittyPlacement, maxCols int) []string {
	cols := min(p.cols, maxCols)
	if cols < 1 || p.rows < 1 || p.rows > len(rowColDiacritics) || cols > len(rowColDiacritics) {
		return nil
	}
	// 256-color encoding for small IDs: legal per spec and more widely
	// supported than truecolor-encoded IDs
	fg := fmt.Sprintf("\x1b[38;5;%dm", p.id&0xff)
	if p.id > 0xff {
		fg = fmt.Sprintf("\x1b[38;2;%d;%d;%dm", (p.id>>16)&0xff, (p.id>>8)&0xff, p.id&0xff)
	}
	lines := make([]string, 0, p.rows)
	for r := 0; r < p.rows; r++ {
		var b strings.Builder
		b.WriteString(fg)
		for c := 0; c < cols; c++ {
			b.WriteRune('\U0010EEEE')
			b.WriteRune(rowColDiacritics[r])
			b.WriteRune(rowColDiacritics[c])
		}
		b.WriteString("\x1b[39m")
		lines = append(lines, b.String())
	}
	return lines
}

var (
	ttyOnce  sync.Once
	ttyFile  *os.File
	ttyMutex sync.Mutex
)

func terminalTTY() *os.File {
	ttyOnce.Do(func() {
		f, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
		if err == nil {
			ttyFile = f
		}
	})
	return ttyFile
}

const kittyChunkSize = 4096

// transmitKittyImage sends the PNG to the terminal as a virtual placement.
// It writes straight to the tty: the payload must not pass through Bubble
// Tea's renderer. Chunks of one image must not interleave with another's
// (continuation escapes are stateful), hence the mutex.
func transmitKittyImage(pageID, blockID string, img image.Image, placement kittyPlacement) tea.Cmd {
	return func() tea.Msg {
		tty := terminalTTY()
		if tty == nil {
			return kittyMsg{pageID: pageID, blockID: blockID, err: fmt.Errorf("no tty")}
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			return kittyMsg{pageID: pageID, blockID: blockID, err: err}
		}
		payload := base64.StdEncoding.EncodeToString(buf.Bytes())

		ttyMutex.Lock()
		defer ttyMutex.Unlock()
		first := true
		for len(payload) > 0 {
			chunk := payload
			if len(chunk) > kittyChunkSize {
				chunk = payload[:kittyChunkSize]
			}
			payload = payload[len(chunk):]
			more := 0
			if len(payload) > 0 {
				more = 1
			}
			var header string
			if first {
				// a=T transmit+display, U=1 virtual placement anchored to
				// placeholders, q=2 suppress responses (they'd land in
				// Bubble Tea's input stream), f=100 PNG
				header = fmt.Sprintf("\x1b_Ga=T,U=1,q=2,f=100,i=%d,c=%d,r=%d,m=%d;",
					placement.id, placement.cols, placement.rows, more)
				first = false
			} else {
				header = fmt.Sprintf("\x1b_Gm=%d;", more)
			}
			if _, err := tty.WriteString(wrapForTmux(header + chunk + "\x1b\\")); err != nil {
				return kittyMsg{pageID: pageID, blockID: blockID, err: err}
			}
		}
		return kittyMsg{pageID: pageID, blockID: blockID, placement: placement}
	}
}
