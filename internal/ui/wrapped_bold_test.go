package ui

import (
	"strings"
	"testing"
	"unicode"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/x/ansi"
	"github.com/jomei/notionapi"

	"github.com/justinm35/lazynotion/internal/convert"
	"github.com/justinm35/lazynotion/internal/notion"
)

func TestWrappedCJKBoldSurvivesGuttersAndScrolling(t *testing.T) {
	text := "整体信息：管理层将 Credo 定位为纯粹的高速连接解决方案公司，核心使命是让 AI 集群中的数据在 GPU、网卡和交换机之间可靠、高效地传输，公司强调可靠性与功耗效率，产品覆盖毫米级至公里级连接，介质覆盖铜和光。"
	for _, width := range []int{30, 48, 82} {
		block := &notionapi.ParagraphBlock{Paragraph: notionapi.Paragraph{RichText: []notionapi.RichText{{PlainText: text, Annotations: &notionapi.Annotations{Bold: true}}}}}
		var pv pageView
		pv.setUnits("", convert.Flatten([]notion.BlockNode{{Block: block}}), width, nil)
		content := pv.assemble(true, nil)
		rows := strings.Split(content, "\n")
		if len(rows) < 2 {
			t.Fatal("fixture must span multiple display rows")
		}
		var visible strings.Builder
		for i, row := range rows {
			plain := ansi.Strip(row)
			if ansi.StringWidth(plain) > width {
				t.Fatalf("width %d: row %d overflows: %q", width, i, plain)
			}
			visible.WriteString(string([]rune(plain)[gutterWidth:]))
			if i < len(rows)-1 && ansi.StringWidth(plain) < width-1 {
				t.Fatalf("width %d: premature CJK break on row %d: %q", width, i, plain)
			}
		}
		if visible.String() != text {
			t.Fatalf("wrapped text changed: %q", visible.String())
		}
		// Start halfway through the block: no style may depend on a hidden row.
		vp := viewport.New(width, 2)
		vp.SetContent(content)
		for offset := 0; offset < len(rows)-1; offset++ {
			vp.SetYOffset(offset)
			plain, bold := ansiAttributeShape(t, vp.View(), 1, 22)
			for i, r := range []rune(plain) {
				if !unicode.IsSpace(r) && r != '▌' && !bold[i] {
					t.Fatalf("width %d offset %d: %q lost bold", width, offset, r)
				}
			}
		}
	}
}

func TestANSIRowsPreserveSelectiveStyleChanges(t *testing.T) {
	lines := hardWrapOverflow([]string{"\x1b[1;3m中文中文\x1b[23m后续后续\x1b[22m普通普通\x1b[0m"}, 8)
	wantBold := []bool{true, true, false}
	wantItalic := []bool{true, false, false}
	if len(lines) != len(wantBold) {
		t.Fatalf("got %d rows, want %d", len(lines), len(wantBold))
	}
	for row, line := range lines {
		_, bold := ansiAttributeShape(t, line, 1, 22)
		_, italic := ansiAttributeShape(t, line, 3, 23)
		for i := range bold {
			if bold[i] != wantBold[row] || italic[i] != wantItalic[row] {
				t.Fatalf("row %d rune %d: bold=%v italic=%v", row, i, bold[i], italic[i])
			}
		}
	}
}
