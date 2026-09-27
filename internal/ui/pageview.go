package ui

import (
	"bytes"
	"image"
	"strings"
	"sync"
	"unicode"

	"github.com/charmbracelet/bubbles/viewport"
	glamouransi "github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/jomei/notionapi"
	"github.com/muesli/termenv"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"

	"github.com/justinm35/lazynotion/internal/convert"
)

const gutterWidth = 2

// pageView owns the block cursor: each convert.Unit renders to its own
// glamour fragment so cursor position maps cleanly onto content lines.
type pageView struct {
	units    []convert.Unit
	rendered [][]string
	starts   []int
	heights  []int
	header   []string
	cursor   int
	// kitty maps block IDs to transmitted virtual placements; those image
	// units render placeholder cells instead of the mosaic
	kitty map[string]kittyPlacement
	// visualOn extends the gutter highlight from visualAnchor to the
	// cursor (vim visual-line); the zero value is off
	visualOn     bool
	visualAnchor int
}

func (pv *pageView) current() (convert.Unit, bool) {
	if pv.cursor < 0 || pv.cursor >= len(pv.units) {
		return convert.Unit{}, false
	}
	return pv.units[pv.cursor], true
}

func (pv *pageView) move(delta int) {
	if len(pv.units) == 0 {
		return
	}
	pv.cursor = clamp(pv.cursor+delta, 0, len(pv.units)-1)
}

// setUnits takes the header pre-styled (icon colors and all) — it is shown
// verbatim rather than routed through glamour, which would garble ANSI.
func (pv *pageView) setUnits(header string, units []convert.Unit, width int, images map[string]image.Image) {
	pv.units = units
	if pv.cursor >= len(units) {
		pv.cursor = max(len(units)-1, 0)
	}
	pv.render(header, width, images)
}

const (
	maxImageCellsW = 72
	maxImageCellsH = 22
)

func (pv *pageView) render(header string, width int, images map[string]image.Image) {
	renderers := map[int]*fragmentRenderer{}
	renderAt := func(md string, w int) []string {
		r, ok := renderers[w]
		if !ok {
			r = newFragmentRenderer(w)
			renderers[w] = r
		}
		if r == nil {
			return strings.Split(md, "\n")
		}
		out, err := r.Render(md)
		if err != nil {
			return strings.Split(md, "\n")
		}
		return splitTrimmed(out)
	}

	contentWidth := max(width-gutterWidth, 10)
	if header != "" {
		pv.header = strings.Split(header, "\n")
	} else {
		pv.header = nil
	}
	pv.rendered = make([][]string, len(pv.units))
	for i, u := range pv.units {
		indent := strings.Repeat("  ", u.Depth)
		w := max(contentWidth-len(indent), 10)
		lines := pv.renderImage(u, images, w)
		if lines == nil {
			// glamour collapses hard line breaks inside paragraphs, so
			// in-block newlines are rendered segment by segment instead.
			for si, seg := range hardBreakSegments(u) {
				segW := w
				continuation := si > 0 && listish(u)
				if continuation {
					// budget for the alignment prefix, or these lines
					// end up 2 columns too wide and the viewport clips
					// them instead of wrapping
					segW = max(w-2, 10)
				}
				segLines := renderAt(seg, segW)
				if continuation {
					for j := range segLines {
						segLines[j] = "  " + segLines[j]
					}
				}
				lines = append(lines, segLines...)
			}
		}
		// Wrap once, after rendering, so Chinese text is not first moved
		// around as oversized words by glamour's space-based wrapper.
		lines = hardWrapOverflow(lines, w)
		// empty blocks still occupy a line so the cursor can reach them
		if len(lines) == 0 {
			lines = []string{""}
		}
		for j := range lines {
			lines[j] = indent + lines[j]
		}
		pv.rendered[i] = lines
	}
	pv.starts = make([]int, len(pv.units))
	pv.heights = make([]int, len(pv.units))
}

// hardBreakSegments splits a fragment on the hard-break markers ToMarkdown
// emits for in-block newlines. Code, tables and images pass through whole;
// quote continuations keep their quote prefix.
func hardBreakSegments(u convert.Unit) []string {
	switch u.Node.Block.(type) {
	case *notionapi.CodeBlock, *notionapi.TableBlock, *notionapi.ImageBlock, *notionapi.EquationBlock:
		return []string{u.Markdown}
	}
	segments := strings.Split(u.Markdown, "  \n")
	_, isQuote := u.Node.Block.(*notionapi.QuoteBlock)
	_, isCallout := u.Node.Block.(*notionapi.CalloutBlock)
	if isQuote || isCallout {
		for i := 1; i < len(segments); i++ {
			if !strings.HasPrefix(segments[i], ">") {
				segments[i] = "> " + segments[i]
			}
		}
	}
	return segments
}

// renderImage returns mosaic lines for a downloaded image unit, or nil to
// fall back to the markdown link (not an image, or still downloading).
func (pv *pageView) renderImage(u convert.Unit, images map[string]image.Image, width int) []string {
	_, caption, ok := u.Image()
	if !ok {
		return nil
	}
	if placement, ok := pv.kitty[u.ID()]; ok {
		if lines := kittyPlaceholderLines(placement, width); lines != nil {
			if caption != "" {
				lines = append(lines, pageMetaStyle.Render(caption))
			}
			return lines
		}
	}
	img := images[u.ID()]
	if img == nil {
		return nil
	}
	initTheme()
	lines := ansiMosaic(img, min(width, maxImageCellsW), maxImageCellsH, darkBG)
	if lines == nil {
		return nil
	}
	if caption != "" {
		lines = append(lines, pageMetaStyle.Render(caption))
	}
	return lines
}

// assemble builds the viewport content. When editLines is non-nil the
// cursor unit's rendered lines are replaced by them (the inline editor).
func (pv *pageView) assemble(focused bool, editLines []string) string {
	lines := make([]string, 0, len(pv.header)+len(pv.rendered)*2)
	if len(pv.header) > 0 {
		lines = append(lines, pv.header...)
		lines = append(lines, "")
	}
	selLo, selHi := pv.cursor, pv.cursor
	if pv.visualOn {
		selLo = min(pv.visualAnchor, pv.cursor)
		selHi = max(pv.visualAnchor, pv.cursor)
	}
	for i, unitLines := range pv.rendered {
		pv.starts[i] = len(lines)
		gutter := "  "
		if i == pv.cursor && editLines != nil {
			unitLines = editLines
			gutter = editGutter()
		} else if i >= selLo && i <= selHi && focused {
			gutter = cursorGutter()
		}
		pv.heights[i] = len(unitLines)
		for _, l := range unitLines {
			lines = append(lines, gutter+l)
		}
		if i < len(pv.rendered)-1 && !(listish(pv.units[i]) && listish(pv.units[i+1])) {
			lines = append(lines, "")
		}
	}
	return strings.Join(lines, "\n")
}

func (pv *pageView) ensureVisible(vp *viewport.Model) {
	if pv.cursor >= len(pv.starts) || pv.cursor >= len(pv.heights) {
		return
	}
	start := pv.starts[pv.cursor]
	end := start + pv.heights[pv.cursor]
	if start < vp.YOffset {
		vp.SetYOffset(start)
	} else if end > vp.YOffset+vp.Height {
		vp.SetYOffset(end - vp.Height)
	}
}

func listish(u convert.Unit) bool {
	switch u.Node.Block.(type) {
	case *notionapi.BulletedListItemBlock, *notionapi.NumberedListItemBlock, *notionapi.ToDoBlock:
		return true
	}
	return false
}

// hardWrapOverflow ANSI-aware-wraps lines wider than w; fitting lines pass
// through untouched (placeholder and mosaic lines are pre-capped).
func hardWrapOverflow(lines []string, w int) []string {
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		if lipgloss.Width(l) <= w {
			out = append(out, l)
			continue
		}
		wrapped := ansi.Wrap(l, w, "")
		if strings.ContainsFunc(ansi.Strip(l), isCJKEmphasisRune) {
			wrapped = ansi.Hardwrap(l, w, true)
		}
		out = append(out, strings.Split(wrapped, "\n")...)
	}
	return selfContainedANSILines(out)
}

// Every display row must restore its own style: gutters, viewport clipping,
// and terminal redraws can all reset the style between rows.
func selfContainedANSILines(lines []string) []string {
	active := ""
	out := make([]string, len(lines))
	for i, line := range lines {
		prefix := active
		var state byte
		for rest := line; rest != ""; {
			seq, _, n, next := ansi.DecodeSequence(rest, state, nil)
			if n == 0 {
				break
			}
			state = next
			if strings.HasPrefix(seq, "\x1b[") && strings.HasSuffix(seq, "m") {
				if seq == "\x1b[0m" || seq == "\x1b[m" {
					active = ""
				} else {
					active += seq
				}
			}
			rest = rest[n:]
		}
		out[i] = prefix + line
		if active != "" {
			out[i] += "\x1b[0m"
		}
	}
	return out
}

func splitTrimmed(out string) []string {
	lines := strings.Split(strings.TrimRight(out, " \n"), "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	return lines
}

var (
	gutterOnce   sync.Once
	gutterMarker string
	editMarker   string
	darkBG       bool
)

func cursorGutter() string {
	initTheme()
	return gutterMarker
}

func editGutter() string {
	initTheme()
	return editMarker
}

func initTheme() {
	gutterOnce.Do(func() {
		darkBG = lipgloss.HasDarkBackground()
		gutterMarker = lipgloss.NewStyle().Foreground(accentColor).Render("▌") + " "
		editMarker = lipgloss.NewStyle().Foreground(warnColor).Render("▌") + " "
	})
}

type fragmentRenderer struct{ md goldmark.Markdown }

func (r *fragmentRenderer) Render(source string) (string, error) {
	var b bytes.Buffer
	if err := r.md.Convert([]byte(source), &b); err != nil {
		return "", err
	}
	return b.String(), nil
}

func newFragmentRenderer(width int) *fragmentRenderer {
	initTheme()
	style := styles.DarkStyleConfig
	if !darkBG {
		style = styles.LightStyleConfig
	}
	if glamourStyle != nil {
		style = *glamourStyle
	}
	zero := uint(0)
	style.Document.Margin = &zero
	style.Document.BlockPrefix = ""
	style.Document.BlockSuffix = ""
	md := goldmark.New(
		goldmark.WithExtensions(extension.GFM, extension.DefinitionList),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
			parser.WithInlineParsers(util.Prioritized(cjkEmphasisParser{}, 450)),
		),
	)
	ar := glamouransi.NewRenderer(glamouransi.Options{
		// The page view wraps the rendered text, with CJK-aware widths and
		// self-contained styles on every row. Disable the earlier word wrap.
		WordWrap:     0,
		ColorProfile: termenv.TrueColor,
		Styles:       style,
	})
	md.SetRenderer(renderer.NewRenderer(renderer.WithNodeRenderers(util.Prioritized(ar, 1000))))
	return &fragmentRenderer{md: md}
}

type cjkEmphasisParser struct{}

func (cjkEmphasisParser) Trigger() []byte { return []byte{'*'} }

func (cjkEmphasisParser) Parse(parent ast.Node, block text.Reader, pc parser.Context) ast.Node {
	before := block.PrecendingCharacter()
	line, segment := block.PeekLine()
	delimiter := parser.ScanDelimiter(line, before, 1, emphasisDelimiterProcessor{})
	if delimiter == nil {
		return nil
	}
	after := rune(' ')
	if delimiter.OriginalLength < len(line) {
		after = util.ToRune(line, delimiter.OriginalLength)
	}
	if isCJKEmphasisRune(before) && unicode.IsPunct(after) {
		delimiter.CanOpen = true
	}
	if unicode.IsPunct(before) && isCJKEmphasisRune(after) {
		delimiter.CanClose = true
	}
	delimiter.Segment = segment.WithStop(segment.Start + delimiter.OriginalLength)
	block.Advance(delimiter.OriginalLength)
	pc.PushDelimiter(delimiter)
	return delimiter
}

type emphasisDelimiterProcessor struct{}

func (emphasisDelimiterProcessor) IsDelimiter(b byte) bool { return b == '*' }
func (emphasisDelimiterProcessor) CanOpenCloser(opener, closer *parser.Delimiter) bool {
	return opener.Char == closer.Char
}
func (emphasisDelimiterProcessor) OnMatch(consumes int) ast.Node { return ast.NewEmphasis(consumes) }

func isCJKEmphasisRune(r rune) bool {
	return unicode.Is(unicode.Han, r) ||
		unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) ||
		unicode.Is(unicode.Hangul, r)
}
