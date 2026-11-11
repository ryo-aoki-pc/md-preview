package table

import "strings"

// ReplaceHTMLCellSource はセルのタグの間の原文だけを差し替える。
func ReplaceHTMLCellSource(r *Region, cell *HTMLCell, source string) ([]string, error) {
	source = strings.ReplaceAll(strings.ReplaceAll(source, "\r\n", "\n"), "\r", "\n")
	return applyHTMLEdits(r, r.HTML, []htmlEdit{{cell.ContentStart, cell.ContentEnd, source}})
}

// Locate は HTML の本文のバイト位置を文書の行とバイト列に戻す。
func (r *Region) Locate(pos int) (line, col int) {
	start := 0
	for i, b := range r.Bodies {
		if pos <= start+len(b) || i == len(r.Bodies)-1 {
			return r.Start + i, len(r.Prefixes[i]) + pos - start
		}
		start += len(b) + 1
	}
	return r.Start, 0
}
