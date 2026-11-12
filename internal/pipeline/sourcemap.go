package pipeline

import (
	"strings"

	"github.com/ryo-aoki-pc/md-tools/internal/include"
)

// SourceMap は描画エンジンへ渡した文字列の行と、CR を除く前の原文の行の対応。
// ゼロ値は恒等の対応になる。
type SourceMap struct {
	expansion  include.Expansion
	directives map[int]bool
	frontLines int
	shift      bool
}

// Original は原文の行と、その場所を直接直せない理由を返す。
func (m SourceMap) Original(line int) (orig int, reason string) {
	orig = m.expansion.OriginalLine(line)
	switch {
	case m.shift:
		reason = "shift"
	case orig > 0 && orig <= m.frontLines:
		reason = "frontmatter"
	case m.expansion.IsIncludedLine(line):
		reason = "include"
	case m.directives[orig]:
		reason = "directive"
	}
	return
}

// Expanded は原文の行に対応する、描画エンジンへ渡した文字列の先頭の行を返す。
// 行数がずれる文書では、Original が返す理由を先に確かめる。
func (m SourceMap) Expanded(orig int) int { return m.expansion.ExpandedLine(orig) }

func standaloneCR(text string) bool {
	for i := 0; i < len(text); i++ {
		if text[i] == '\r' && (i+1 == len(text) || text[i+1] != '\n') {
			return true
		}
	}
	return false
}

func sourceMap(text, original, converted string, frontEnd int, expansion include.Expansion) SourceMap {
	m := SourceMap{shift: standaloneCR(text) || standaloneCR(expansion.Text)}
	if frontEnd > 0 {
		end := len(converted) - len(original[frontEnd:])
		m.frontLines = strings.Count(converted[:end], "\n")
		if frontEnd < len(original) && m.frontLines != strings.Count(original[:frontEnd], "\n") {
			m.shift = true
		}
	}
	m.directives = make(map[int]bool, len(expansion.Includes))
	for _, inc := range expansion.Includes {
		m.directives[inc.Line] = true
	}
	// 対応には展開後の本文そのものを保持する必要がない。
	expansion.Text = ""
	m.expansion = expansion
	return m
}
