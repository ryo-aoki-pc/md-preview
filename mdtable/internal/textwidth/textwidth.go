// Package textwidth は文字の表示幅を求める。
package textwidth

import (
	"github.com/ryo-aoki-pc/md-table-excel/internal/pycompat"
	"strings"
)

// textwidth.char_width に対応する文字幅。
func CharWidth(ch rune) int {
	if pycompat.Combining(ch) != 0 {
		return 0
	}
	w := pycompat.EastAsianWidth(ch)
	if w == "W" || w == "F" {
		return 2
	}
	return 1
}

// textwidth.text_width に対応する文字列の幅。
func TextWidth(s string) int {
	n := 0
	for _, ch := range s {
		n += CharWidth(ch)
	}
	return n
}

// textwidth.wrapped_line_count に対応する折り返しの行数。
func WrappedLineCount(s string, width int) int {
	if width <= 0 {
		return max(1, strings.Count(s, "\n")+1)
	}
	total := 0
	for _, line := range strings.Split(s, "\n") {
		w := TextWidth(pycompat.ExpandTabs(line, 4))
		total += max(1, (w+width-1)/width)
	}
	return total
}
