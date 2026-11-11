package edit

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/ryo-aoki-pc/md-preview/internal/session"
)

// Range は data-sourcepos と同じ、1 起点で終端を含むバイトの範囲。
type Range string

var rangePattern = regexp.MustCompile(`^([1-9][0-9]*):([1-9][0-9]*)-([1-9][0-9]*):([0-9]+)$`)

func ParseRange(value string) (Range, *Error) {
	_, e := coordinates(Range(value))
	return Range(value), e
}
func coordinates(r Range) ([4]int, *Error) {
	var out [4]int
	m := rangePattern.FindStringSubmatch(string(r))
	if m == nil {
		return out, invalid()
	}
	for i := range out {
		n, e := strconv.ParseUint(m[i+1], 10, 31)
		if e != nil {
			return out, invalid()
		}
		out[i] = int(n)
	}
	// 終了列 0 は「その行の手前まで」。同じ行では開始より前になるので受け付けない。
	if out[2] < out[0] || out[2] == out[0] && out[3] < out[1] {
		return out, invalid()
	}
	return out, nil
}
func reasonText(reason string) string {
	switch reason {
	case "include":
		return "取り込んだファイルの中は編集できません。"
	case "directive":
		return "取り込みの指定を含む範囲は編集できません。"
	case "frontmatter":
		return "front matter は編集できません。"
	case "shift":
		return "単独の CR などで行の対応がずれる文書は編集できません。"
	default:
		return "原文の位置を確かめられません。"
	}
}
func offsets(src session.Source, r Range) (int, int, int, *Error) {
	c, e := coordinates(r)
	if e != nil {
		return 0, 0, 0, e
	}
	// 展開後の行数を入力から無制限に走査しない。
	maxLine := src.Lines.Expanded(strings.Count(src.Text, "\n") + 1)
	if c[0] > maxLine || c[2] > maxLine {
		return 0, 0, 0, fault(422, "range", "原文の行が範囲外です。")
	}
	first, last := 0, 0
	for line := c[0]; line <= c[2]; line++ {
		orig, reason := src.Lines.Original(line)
		if reason != "" {
			return 0, 0, 0, fault(422, reason, reasonText(reason))
		}
		if line == c[0] {
			first = orig
		}
		if line == c[2] {
			last = orig
		}
		if orig != first+line-c[0] {
			return 0, 0, 0, fault(422, "range", "原文の連続した範囲ではありません。")
		}
	}
	starts := []int{0}
	for i := 0; i < len(src.Text); i++ {
		if src.Text[i] == '\n' {
			starts = append(starts, i+1)
		}
	}
	position := func(line, col int, end bool) (int, bool) {
		if line < 1 || line > len(starts) {
			return 0, false
		}
		start := starts[line-1]
		if end && col == 0 {
			return start, true
		}
		limit := len(src.Text)
		if line < len(starts) {
			limit = starts[line] - 1
		}
		if limit > start && src.Text[limit-1] == '\r' {
			limit--
		}
		pos := start + col - 1
		if end {
			pos++
		}
		return pos, pos >= start && pos <= limit && (pos == len(src.Text) || utf8.RuneStart(src.Text[pos]))
	}
	a, okA := position(first, c[1], false)
	b, okB := position(last, c[3], true)
	if !okA || !okB || b < a {
		return 0, 0, 0, fault(422, "range", "原文の列が範囲外か、UTF-8 の文字の途中です。")
	}
	return a, b, first, nil
}
func removeCR(s string) string { return strings.ReplaceAll(s, "\r", "") }
func normalizeNewlines(s, ending string) string {
	return strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n"), "\n", ending)
}
func newlineAt(text string, line int) string {
	start := 0
	for i := 1; i < line; i++ {
		n := strings.IndexByte(text[start:], '\n')
		if n < 0 {
			break
		}
		start += n + 1
	}
	if n := strings.IndexByte(text[start:], '\n'); n >= 0 {
		if n > 0 && text[start+n-1] == '\r' {
			return "\r\n"
		}
		return "\n"
	}
	if n := strings.IndexByte(text, '\n'); n >= 0 && n > 0 && text[n-1] == '\r' {
		return "\r\n"
	}
	return "\n"
}
