package edit

import (
	"strings"

	"github.com/ryo-aoki-pc/md-preview/internal/session"
)

// 末尾の改行だけではバッファの行は変わらない。CR は行の文字として残す。
func bufferLines(text string) []string {
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
}

// 同じ先頭と末尾を除き、Lua に渡す置き換えを必ず 1 個にまとめる。
func bufferEdit(src session.Source, before, after string) BufferEdit {
	a, b := bufferLines(before), bufferLines(after)
	start := 0
	for start < len(a) && start < len(b) && a[start] == b[start] {
		start++
	}
	endA, endB := len(a), len(b)
	for endA > start && endB > start && a[endA-1] == b[endB-1] {
		endA--
		endB--
	}
	return BufferEdit{Path: src.DocPath, Buf: src.Buf, Rev: src.Rev, Start: start, End: endA, Lines: append([]string{}, b[start:endB]...)}
}
