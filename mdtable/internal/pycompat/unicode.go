// Package pycompat は Python 3 の標準ライブラリに合わせた処理を提供する。
// 生成し直すには、リポジトリの直下で go generate ./internal/pycompat/ を実行する。
// 生成元 Python の Unicode データ、文字メソッド、HTML の表を固定して使う。
//
//go:generate python -B gen_tables.py
//go:generate python -B gen_testdata.py
//go:generate gofmt -w tables_generated.go
package pycompat

import (
	"sort"
	"strings"
	"unicode/utf8"
)

type propertyRange struct {
	lo, hi          rune
	category, width string
	combining       int
	flags           uint8
	decimal         int
}
type lowerRange struct {
	lo, hi rune
	delta  rune
}

const (
	spaceFlag uint8 = 1 << iota
	alphaFlag
	digitFlag
	wordFlag
	casedFlag
	ignorableFlag
)

func properties(r rune) propertyRange {
	i := sort.Search(len(propertyRanges), func(i int) bool { return propertyRanges[i].hi >= r })
	if i == len(propertyRanges) || r < propertyRanges[i].lo {
		return propertyRange{category: "Cn", width: "N", decimal: -1}
	}
	return propertyRanges[i]
}

// Category は生成元 Python の Unicode 一般カテゴリを返す。
func Category(r rune) string { return properties(r).category }

// EastAsianWidth は未割り当ての文字も含めて Python の幅分類を返す。
func EastAsianWidth(r rune) string { return properties(r).width }

// Combining は正準結合クラスを返す。
func Combining(r rune) int { return properties(r).combining }

// IsSpace は Python の空白判定を行う。
func IsSpace(r rune) bool { return properties(r).flags&spaceFlag != 0 }

// IsReDigit は Python の正規表現の数字判定を行う。
func IsReDigit(r rune) bool { return properties(r).flags&digitFlag != 0 }

// IsReSpace は Python の正規表現の空白判定を行う。
func IsReSpace(r rune) bool { return IsSpace(r) }

// IsReWord は Python の正規表現の単語文字判定を行う。
func IsReWord(r rune) bool { return properties(r).flags&wordFlag != 0 }

// Len は UTF-8 のバイト数ではなくコードポイント数を返す。
func Len(s string) int { return utf8.RuneCountInString(s) }

// IsAlpha は空でなく全ての文字が Python の英字であるかを返す。
func IsAlpha(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if properties(r).flags&alphaFlag == 0 {
			return false
		}
	}
	return true
}

// Strip は両端の空白を除く。
func Strip(s string) string { return strings.TrimFunc(s, IsSpace) }

// LStrip は左端の空白を除く。
func LStrip(s string) string { return strings.TrimLeftFunc(s, IsSpace) }

// RStrip は右端の空白を除く。
func RStrip(s string) string { return strings.TrimRightFunc(s, IsSpace) }

// Split は連続する空白で分割し、空の要素を返さない。
func Split(s string) []string {
	result := strings.FieldsFunc(s, IsSpace)
	if result == nil {
		return []string{}
	}
	return result
}

// SplitLines は CRLF を一つの区切りとして Python の全ての行区切りを扱う。
func SplitLines(s string, keepends bool) []string {
	result := []string{}
	start := 0
	for i, r := range s {
		if i < start {
			continue
		}
		if r != '\n' && r != '\r' && r != '\v' && r != '\f' && r != 0x1c && r != 0x1d && r != 0x1e && r != 0x85 && r != 0x2028 && r != 0x2029 {
			continue
		}
		end := i + utf8.RuneLen(r)
		if r == '\r' && end < len(s) && s[end] == '\n' {
			end++
		}
		if keepends {
			result = append(result, s[start:end])
		} else {
			result = append(result, s[start:i])
		}
		start = end
	}
	if start < len(s) {
		result = append(result, s[start:])
	}
	return result
}

// ExpandTabs はコードポイントで桁を数え、CR と LF だけで桁を戻す。
func ExpandTabs(s string, tabsize int) string {
	var out strings.Builder
	column := 0
	for _, r := range s {
		if r == '\t' {
			if tabsize > 0 {
				n := tabsize - column%tabsize
				out.WriteString(strings.Repeat(" ", n))
				column += n
			}
		} else {
			out.WriteRune(r)
			if r == '\r' || r == '\n' {
				column = 0
			} else {
				column++
			}
		}
	}
	return out.String()
}

// Lower は拡張小文字写像と文脈に依存するギリシャ文字の語末シグマを扱う。
func Lower(s string) string {
	rs := []rune(s)
	var out strings.Builder
	for i, r := range rs {
		if r == 0x3a3 && finalSigma(rs, i) {
			out.WriteRune(0x3c2)
			continue
		}
		if value, ok := lowerExpansions[r]; ok {
			out.WriteString(value)
			continue
		}
		j := sort.Search(len(lowerRanges), func(j int) bool { return lowerRanges[j].hi >= r })
		if j < len(lowerRanges) && r >= lowerRanges[j].lo {
			r += lowerRanges[j].delta
		}
		out.WriteRune(r)
	}
	return out.String()
}

func finalSigma(rs []rune, i int) bool {
	before := false
	for j := i - 1; j >= 0; j-- {
		f := properties(rs[j]).flags
		if f&ignorableFlag != 0 {
			continue
		}
		before = f&casedFlag != 0
		break
	}
	if !before {
		return false
	}
	for j := i + 1; j < len(rs); j++ {
		f := properties(rs[j]).flags
		if f&ignorableFlag != 0 {
			continue
		}
		return f&casedFlag == 0
	}
	return true
}

// Atoi は Unicode の十進数字を、実行環境の int の範囲まで読み取る。
func Atoi(s string) (n int, ok bool) {
	if s == "" {
		return 0, false
	}
	if pythonIntMaxStrDigits > 0 && Len(s) > pythonIntMaxStrDigits {
		return 0, false
	}
	limit := int(^uint(0) >> 1)
	for _, r := range s {
		p := properties(r)
		if p.flags&digitFlag == 0 {
			return 0, false
		}
		d := int(r-p.lo) + p.decimal
		if n > (limit-d)/10 {
			return 0, false
		}
		n = n*10 + d
	}
	return n, true
}
