package ui

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	_ "golang.org/x/image/webp"

	"github.com/justinm35/lazynotion/internal/convert"
)

const maxImageBytes = 20 << 20

type imageMsg struct {
	pageID  string
	blockID string
	img     image.Image
	err     error
}

// fetchImages starts a download for every image on the page that isn't in
// memory yet. Notion's file URLs are signed and expire after ~1 hour, so
// downloads happen eagerly on page load.
func (m Model) fetchImages(pageID string, units []convert.Unit) tea.Cmd {
	var cmds []tea.Cmd
	for _, u := range units {
		url, _, ok := u.Image()
		if !ok || url == "" {
			continue
		}
		if _, cached := m.images[u.ID()]; cached {
			continue
		}
		cmds = append(cmds, downloadImage(pageID, u.ID(), url))
	}
	return tea.Batch(cmds...)
}

func downloadImage(pageID, blockID, url string) tea.Cmd {
	return func() tea.Msg {
		client := &http.Client{Timeout: 30 * time.Second}
		resp, err := client.Get(url)
		if err != nil {
			return imageMsg{pageID: pageID, blockID: blockID, err: err}
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return imageMsg{pageID: pageID, blockID: blockID, err: fmt.Errorf("image download: %s", resp.Status)}
		}
		raw, err := io.ReadAll(io.LimitReader(resp.Body, maxImageBytes))
		if err != nil {
			return imageMsg{pageID: pageID, blockID: blockID, err: err}
		}
		img, format, err := image.Decode(bytes.NewReader(raw))
		if err != nil {
			return imageMsg{pageID: pageID, blockID: blockID, err: err}
		}
		img = normalizeOrientation(img, format, raw)
		return imageMsg{pageID: pageID, blockID: blockID, img: img}
	}
}

const (
	coverKeyPrefix = "cover|"
	maxCoverCols   = 100
	maxCoverRows   = 6
)

func coverKey(pageID string) string { return coverKeyPrefix + pageID }

// coverLines renders the page cover as header lines: sharp placement when
// transmitted, mosaic otherwise, nothing while the download is in flight.
func (m Model) coverLines() []string {
	if m.selected == nil || m.selected.Cover == "" {
		return nil
	}
	key := coverKey(m.selected.ID)
	width := max(m.viewer.Width-gutterWidth, 10)
	if placement, ok := m.kittyImgs[key]; ok {
		if lines := kittyPlaceholderLines(placement, width); lines != nil {
			return lines
		}
	}
	if img := m.images[key]; img != nil {
		initTheme()
		return ansiMosaic(img, min(width, maxCoverCols), maxCoverRows, darkBG)
	}
	return nil
}
