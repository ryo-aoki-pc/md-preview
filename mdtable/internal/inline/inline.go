// Package inline はインラインのまとまりとエスケープを扱う。
package inline

import (
	"github.com/ryo-aoki-pc/md-table-excel/internal/pycompat"
	"regexp"
	"strings"
	"unicode/utf8"
)

const ASCIIPunctuation = "!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~"
const tagName = `[A-Za-z][A-Za-z0-9-]*`
const ws = `(?:[ \t]*\n?[ \t]*)`
const ws1 = `(?:(?:[ \t]+\n?[ \t]*)|(?:[ \t]*\n[ \t]*))`
const attrValue = `(?:[^"'=<>` + "`" + `\x00-\x20]+|'[^']*'|"[^"]*")`
const attribute = ws1 + `[a-zA-Z_:][a-zA-Z0-9_.:-]*(?:` + ws + `=` + ws + attrValue + `)?`

var openTag = regexp.MustCompile(`^<(` + tagName + `)((?:` + attribute + `)*)` + ws + `(/?)>`)
var closeTag = regexp.MustCompile(`^</(` + tagName + `)` + ws + `>`)
var specialHTML = []*regexp.Regexp{regexp.MustCompile(`^<!-->|^<!--->|^<!--[\s\S]*?-->`), regexp.MustCompile(`^<\?[\s\S]*?\?>`), regexp.MustCompile(`^<!\[CDATA\[[\s\S]*?\]\]>`), regexp.MustCompile(`^<![A-Za-z][^>]*>`)}
var uriAutolink = regexp.MustCompile(`^<[A-Za-z][A-Za-z0-9+.-]{1,31}:[^<>\x00-\x20]*>`)
var emailAutolink = regexp.MustCompile("^<[a-zA-Z0-9.!#$%&'*+/=?^_`{|}~-]+@[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(?:\\.[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*>")

type Token struct {
	Kind       string
	Start, End int
	Text, Tag  string
	Closing    bool
}

// inline.match_raw_html に対応する HTML の一致。
func MatchRawHTML(s string, pos int) (int, string, bool) {
	if pos >= len(s) || s[pos] != '<' {
		return -1, "", false
	}
	tail := s[pos:]
	for _, r := range specialHTML {
		if m := r.FindString(tail); m != "" {
			return pos + len(m), "", false
		}
	}
	if m := openTag.FindStringSubmatch(tail); m != nil {
		return pos + len(m[0]), pycompat.Lower(m[1]), false
	}
	if m := closeTag.FindStringSubmatch(tail); m != nil {
		return pos + len(m[0]), pycompat.Lower(m[1]), true
	}
	return -1, "", false
}

// inline.match_autolink に対応する自動リンク。
func MatchAutolink(s string, pos int) int {
	if pos >= len(s) || s[pos] != '<' {
		return -1
	}
	for _, r := range []*regexp.Regexp{uriAutolink, emailAutolink} {
		if m := r.FindString(s[pos:]); m != "" {
			return pos + len(m)
		}
	}
	return -1
}

// inline._backtick_run に対応する連なり。
func backtickRun(s string, pos int) int {
	end := pos
	for end < len(s) && s[end] == '`' {
		end++
	}
	return end - pos
}

// inline._find_code_span_end に対応するコードスパンの終端。
func codeSpanEnd(s string, pos, length int) int {
	search := pos + length
	for search <= len(s) {
		found := strings.Index(s[search:], strings.Repeat("`", length))
		if found < 0 {
			return -1
		}
		found += search
		run := backtickRun(s, found)
		if run == length {
			return found + length
		}
		search = found + run
	}
	return -1
}

// inline._match_math に対応する数式の終端。
func matchMath(s string, pos int) int {
	if strings.HasPrefix(s[pos:], "$`") {
		end := strings.Index(s[pos+2:], "`$")
		if end >= 0 {
			return pos + 2 + end + 2
		}
		return -1
	}
	if strings.HasPrefix(s[pos:], "$$") {
		end := strings.Index(s[pos+2:], "$$")
		if end > 0 {
			return pos + 2 + end + 2
		}
		return -1
	}
	if s[pos] != '$' || pos+1 >= len(s) {
		return -1
	}
	next, _ := utf8.DecodeRuneInString(s[pos+1:])
	if pycompat.IsSpace(next) || next == '$' {
		return -1
	}
	search := pos + 1
	for search < len(s) {
		found := strings.IndexByte(s[search:], '$')
		if found < 0 {
			return -1
		}
		found += search
		before, _ := utf8.DecodeLastRuneInString(s[:found])
		after := byte(0)
		if found+1 < len(s) {
			after = s[found+1]
		}
		if pycompat.IsSpace(before) || before == '\\' || after >= '0' && after <= '9' {
			search = found + 1
			continue
		}
		return found + 1
	}
	return -1
}

// inline.tokenize に対応するトークンの分割。
func Tokenize(s string, math bool) []Token {
	var tokens []Token
	pos, start := 0, 0
	flush := func(upto int) {
		if upto > start {
			tokens = append(tokens, Token{Kind: "text", Start: start, End: upto, Text: s[start:upto]})
		}
	}
	for pos < len(s) {
		ch := s[pos]
		if ch == '\\' && pos+1 < len(s) && strings.ContainsRune(ASCIIPunctuation, rune(s[pos+1])) {
			flush(pos)
			tokens = append(tokens, Token{Kind: "escape", Start: pos, End: pos + 2, Text: s[pos : pos+2]})
			pos += 2
			start = pos
			continue
		}
		if ch == '`' {
			run := backtickRun(s, pos)
			end := codeSpanEnd(s, pos, run)
			if end >= 0 {
				flush(pos)
				tokens = append(tokens, Token{Kind: "code", Start: pos, End: end, Text: s[pos:end]})
				pos = end
				start = pos
			} else {
				pos += run
			}
			continue
		}
		if ch == '<' {
			end := MatchAutolink(s, pos)
			if end >= 0 {
				flush(pos)
				tokens = append(tokens, Token{Kind: "autolink", Start: pos, End: end, Text: s[pos:end]})
				pos = end
				start = pos
				continue
			}
			end, tag, closing := MatchRawHTML(s, pos)
			if end >= 0 {
				flush(pos)
				tokens = append(tokens, Token{Kind: "html", Start: pos, End: end, Text: s[pos:end], Tag: tag, Closing: closing})
				pos = end
				start = pos
				continue
			}
		}
		if math && ch == '$' {
			end := matchMath(s, pos)
			if end >= 0 {
				flush(pos)
				tokens = append(tokens, Token{Kind: "math", Start: pos, End: end, Text: s[pos:end]})
				pos = end
				start = pos
				continue
			}
		}
		pos++
	}
	flush(len(s))
	return tokens
}

type Span struct{ Start, End int }

// inline.atom_spans に対応する範囲。
func AtomSpans(s string) []Span {
	var out []Span
	for _, t := range Tokenize(s, true) {
		if t.Kind == "code" || t.Kind == "html" || t.Kind == "autolink" || t.Kind == "math" {
			out = append(out, Span{t.Start, t.End})
		}
	}
	return out
}

// inline.position_in_spans に対応する範囲内判定。
func PositionInSpans(pos int, spans []Span) bool {
	for _, s := range spans {
		if s.Start < pos && pos < s.End {
			return true
		}
	}
	return false
}

// inline.is_unicode_whitespace に対応する CommonMark の空白。
func IsUnicodeWhitespace(ch rune) bool {
	return ch == 0 || strings.ContainsRune("\t\n\v\f\r ", ch) || pycompat.Category(ch) == "Zs"
}

// inline.is_punctuation に対応する約物の判定。
func IsPunctuation(ch rune, symbols bool) bool {
	if ch == 0 {
		return false
	}
	if strings.ContainsRune(ASCIIPunctuation, ch) {
		return true
	}
	c := pycompat.Category(ch)
	return strings.HasPrefix(c, "P") || symbols && strings.HasPrefix(c, "S")
}

// inline._flanking に対応する左右の判定。
func flanking(before, after rune, symbols bool) (bool, bool) {
	left := !IsUnicodeWhitespace(after) && (!IsPunctuation(after, symbols) || IsUnicodeWhitespace(before) || IsPunctuation(before, symbols))
	right := !IsUnicodeWhitespace(before) && (!IsPunctuation(before, symbols) || IsUnicodeWhitespace(after) || IsPunctuation(after, symbols))
	return left, right
}

// inline.delimiters_work に対応する強調の成立判定。
func DelimitersWork(before rune, inner string, after rune, marker string) bool {
	r := []rune(inner)
	if len(r) == 0 || strings.ContainsRune(" \t\n", r[0]) || strings.ContainsRune(" \t\n", r[len(r)-1]) {
		return false
	}
	first, last := r[0], r[len(r)-1]
	for _, ch := range []rune{first, last, before, after} {
		if ch != 0 && strings.ContainsRune("*_~", ch) {
			return false
		}
	}
	for _, symbols := range []bool{false, true} {
		ol, or := flanking(before, first, symbols)
		cl, cr := flanking(last, after, symbols)
		co, cc := ol, cr
		if marker[0] == '_' {
			co = ol && (!or || IsPunctuation(before, symbols))
			cc = cr && (!cl || IsPunctuation(after, symbols))
		}
		if !co || !cc {
			return false
		}
	}
	return true
}

var lineStart = regexp.MustCompile(`^(?:[-+*](?:[ \t]|$)|[` + pycompat.ReDigit + `]{1,9}[.)](?:[ \t]|$)|#{1,6}(?:[ \t]|$)|>|[:~][ \t]|=+[ \t]*$|-+[ \t]*$|(?:\*[ \t]*){3,}$|(?:_[ \t]*){3,}$|(?:-[ \t]*){3,}$|` + "`{3,}" + `|~{3,}|\[\^?[^\]]+\]:)`)
var digits = regexp.MustCompile(`^[` + pycompat.ReDigit + `]+`)
var unescapeDigits = regexp.MustCompile(`^([` + pycompat.ReDigit + `]{1,9})\\([.)])`)

// inline.escape_line_start に対応する行頭のエスケープ。
func EscapeLineStart(s string) string {
	if !lineStart.MatchString(strings.TrimSuffix(s, "\n")) {
		return s
	}
	if s[0] >= '0' && s[0] <= '9' {
		d := digits.FindString(s)
		return d + "\\" + s[len(d):]
	}
	return "\\" + s
}

// inline.unescape_line_start に対応する行頭の解除。
func UnescapeLineStart(s string) string {
	if strings.HasPrefix(s, "\\") && len(s) > 1 {
		c := s[1:]
		if EscapeLineStart(c) == s {
			return c
		}
	}
	if m := unescapeDigits.FindStringSubmatch(s); m != nil {
		c := m[1] + s[len(m[1])+1:]
		if EscapeLineStart(c) == s {
			return c
		}
	}
	return s
}
