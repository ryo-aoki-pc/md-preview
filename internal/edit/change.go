package edit

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"

	"github.com/ryo-aoki-pc/md-tools/internal/pipeline"
	"github.com/ryo-aoki-pc/md-tools/internal/session"
)

// lineKeep は変わらなかった行の対応 [旧の行, 新の行, 行数]。行は 1 始まり。
type lineKeep [3]int

type textChange struct {
	Before string     `json:"before"`
	After  string     `json:"after"`
	Keep   []lineKeep `json:"keep"`
}

func (s *Service) version(src session.Source, text string) string {
	key := pathKey(src.Path)
	if src.Path == "" {
		key = "buffer:" + fmt.Sprint(src.Buf)
	}
	h := hmac.New(sha256.New, s.secret[:])
	_, _ = h.Write([]byte("md-preview text version\x00" + key + "\x00"))
	_, _ = h.Write([]byte(strings.TrimPrefix(text, "\ufeff")))
	return hex.EncodeToString(h.Sum(nil)[:16])
}

// LF・CRLF も行に含めて比べる。末尾の LF の後に架空の空行を数えず、
// 改行のない最後の行は、そのまま一致したときだけ残す。
func commonKeep(before, after string) []lineKeep {
	return limitedKeep(before, after, -1, -1)
}

// rangeKeep は、書き換えた範囲 before[start:end] が分かる編集の行の対応。共通の先頭と末尾を
// その範囲の外の行に限り、同じ行が続く文書で、書き換えた行を残った行と取り違えないようにする。
func rangeKeep(before, after string, start, end int) []lineKeep {
	if start < 0 || end < start || end > len(before) {
		return []lineKeep{}
	}
	suffix := len(headingLines(before[end:]))
	if end > 0 && before[end-1] != '\n' && suffix > 0 {
		suffix--
	}
	return limitedKeep(before, after, strings.Count(before[:start], "\n"), suffix)
}

// editKeep は、書き換えた範囲が分かれば rangeKeep、分からなければ共通の先頭と末尾で行の対応を作る。
func (s *Service) editKeep(before, after string, start, end int) []lineKeep {
	if start >= 0 {
		return rangeKeep(before, after, start, end)
	}
	return commonKeep(before, after)
}

// limitedKeep の maxFirst・maxLast は、共通の先頭・末尾の行数の上限（負なら上限なし）。
func limitedKeep(before, after string, maxFirst, maxLast int) []lineKeep {
	a, b := headingLines(before), headingLines(after)
	first := 0
	for first < len(a) && first < len(b) && (maxFirst < 0 || first < maxFirst) && a[first] == b[first] {
		first++
	}
	last := 0
	for last < len(a)-first && last < len(b)-first && (maxLast < 0 || last < maxLast) && a[len(a)-1-last] == b[len(b)-1-last] {
		last++
	}
	keep := []lineKeep{}
	if first > 0 {
		keep = append(keep, lineKeep{1, 1, first})
	}
	if last > 0 {
		keep = append(keep, lineKeep{len(a) - last + 1, len(b) - last + 1, last})
	}
	return keep
}

func orderedKeep(keep []lineKeep, side int) []lineKeep {
	out := append([]lineKeep{}, keep...)
	slices.SortFunc(out, func(a, b lineKeep) int { return a[side] - b[side] })
	return out
}

func reverseKeep(keep []lineKeep) []lineKeep {
	out := make([]lineKeep, len(keep))
	for i, k := range keep {
		out[i] = lineKeep{k[1], k[0], k[2]}
	}
	return orderedKeep(out, 0)
}

// include.Expand と同じく、行頭から始まる指定だけを検出する。
// 書式が途中で切れた指定も、編集で有効になり得るので安全側に倒す。
func includeLine(line string) bool { return strings.HasPrefix(line, "::include{file=") }

func changedInclude(lines []string, keep []lineKeep, side int) int {
	index := 0
	for i, line := range lines {
		if !includeLine(line) {
			continue
		}
		n := i + 1
		for index < len(keep) && keep[index][side]+keep[index][2] <= n {
			index++
		}
		if index == len(keep) || n < keep[index][side] {
			return n
		}
	}
	return len(lines) + 1
}

// publicKeep は描画済みの旧の対応だけから公開の行へ変換する純粋な関数。
// 取り込みを変えた区間の後ろは新しい展開が不明なので返さない。
func publicKeep(before, after string, keep []lineKeep, source pipeline.SourceMap) []lineKeep {
	out := []lineKeep{}
	if _, reason := source.Original(1); reason == "shift" || standaloneChangeCR(before) || standaloneChangeCR(after) {
		return out
	}
	a, b := headingLines(before), headingLines(after)
	oldOrder, newOrder := orderedKeep(keep, 0), orderedKeep(keep, 1)
	oldLimit, newLimit := changedInclude(a, oldOrder, 0), changedInclude(b, newOrder, 1)
	type expansion struct{ line, extra int }
	expansions := []expansion{}
	for _, k := range newOrder {
		for i := 0; i < k[2]; i++ {
			old := k[0] + i
			if includeLine(a[old-1]) {
				extra := source.Expanded(old+1) - source.Expanded(old) - 1
				if extra != 0 {
					expansions = append(expansions, expansion{k[1] + i, extra})
				}
			}
		}
	}
	for _, k := range oldOrder {
		if k[0] >= oldLimit || k[1] >= newLimit {
			continue
		}
		part := lineKeep{}
		flush := func() {
			if part[2] > 0 {
				out = append(out, part)
				part = lineKeep{}
			}
		}
		for i := 0; i < k[2]; i++ {
			old, next := k[0]+i, k[1]+i
			public := source.Expanded(old)
			_, reason := source.Original(public)
			if reason == "shift" {
				return []lineKeep{}
			}
			if reason == "frontmatter" {
				flush()
				continue
			}
			for _, e := range expansions {
				if e.line >= k[1]+i {
					break
				}
				next += e.extra
			}
			if includeLine(a[old-1]) {
				flush()
				out = append(out, lineKeep{public, next, source.Expanded(old+1) - public})
			} else {
				if part[2] == 0 {
					part = lineKeep{public, next, 0}
				}
				part[2]++
			}
		}
		flush()
	}
	return out
}

func standaloneChangeCR(text string) bool {
	for i := 0; i < len(text); i++ {
		if text[i] == '\r' && (i+1 == len(text) || text[i+1] != '\n') {
			return true
		}
	}
	return false
}

func (s *Service) textChange(src session.Source, before, after string, keep []lineKeep) textChange {
	result := textChange{Before: s.version(src, before), After: s.version(src, after), Keep: []lineKeep{}}
	// 連続した取り消しで描画が追い付いていない場合、旧の公開の位置を信用しない。
	if src.Text == before {
		result.Keep = publicKeep(before, after, keep, src.Lines)
	}
	return result
}
