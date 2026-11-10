package assets

import (
	"errors"
	"fmt"
	"net/url"
	"path"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

func quotedEnd(css string, start int) (int, error) {
	quote := css[start]
	for end := start + 1; end < len(css); end++ {
		if css[end] == '\\' {
			end++
		} else if css[end] == quote {
			return end + 1, nil
		}
	}
	return 0, fmt.Errorf("CSS文字列が閉じていません（位置 %d）。", start)
}
func cssSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == '\f' }
func cssWord(c byte) bool {
	return c == '_' || c == '-' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}
func isHex(c byte) bool { return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' }

func unescapeCSS(value string) (string, error) {
	var out strings.Builder
	for i := 0; i < len(value); {
		if value[i] != '\\' || i+1 == len(value) {
			out.WriteByte(value[i])
			i++
			continue
		}
		i++
		if isHex(value[i]) {
			start := i
			for i < len(value) && i-start < 6 && isHex(value[i]) {
				i++
			}
			n, _ := strconv.ParseUint(value[start:i], 16, 32)
			if n > utf8.MaxRune {
				return "", errors.New("CSSのエスケープが不正です。")
			}
			out.WriteRune(rune(n))
			if i < len(value) {
				r, size := utf8.DecodeRuneInString(value[i:])
				if unicode.IsSpace(r) {
					i += size
				}
			}
		} else if value[i] == '\n' {
			i++
		} else if value[i] == '\r' && i+1 < len(value) && value[i+1] == '\n' {
			i += 2
		} else {
			out.WriteByte(value[i])
			i++
		}
	}
	return out.String(), nil
}

// RewriteCSS は、コメントと文字列を保ち、固定された資産への url() だけを相対パスにする。
// visit があれば、参照した資産名を渡す。
func RewriteCSS(css, name string, files map[string]Record, visit func(string)) (string, error) {
	var result strings.Builder
	for at := 0; at < len(css); {
		if strings.HasPrefix(css[at:], "/*") {
			end := strings.Index(css[at+2:], "*/")
			if end < 0 {
				return "", errors.New("CSSのコメントが閉じていません。")
			}
			end += at + 4
			result.WriteString(css[at:end])
			at = end
			continue
		}
		if css[at] == '\\' {
			end := min(len(css), at+2)
			result.WriteString(css[at:end])
			at = end
			continue
		}
		if css[at] == '"' || css[at] == '\'' {
			end, err := quotedEnd(css, at)
			if err != nil {
				return "", fmt.Errorf("%s の%s", name, err)
			}
			result.WriteString(css[at:end])
			at = end
			continue
		}
		cursor := at + 3
		match := cursor <= len(css) && strings.EqualFold(css[at:cursor], "url") && (at == 0 || !cssWord(css[at-1]))
		if match {
			for cursor < len(css) && cssSpace(css[cursor]) {
				cursor++
			}
			match = cursor < len(css) && cursor-at < 16 && css[cursor] == '('
		}
		if !match {
			result.WriteByte(css[at])
			at++
			continue
		}
		cursor++
		for cursor < len(css) && cssSpace(css[cursor]) {
			cursor++
		}
		var value string
		if cursor < len(css) && (css[cursor] == '"' || css[cursor] == '\'') {
			end, err := quotedEnd(css, cursor)
			if err != nil {
				return "", fmt.Errorf("%s の%s", name, err)
			}
			value = css[cursor+1 : end-1]
			cursor = end
		} else {
			start := cursor
			for cursor < len(css) && css[cursor] != ')' {
				if css[cursor] == '\\' {
					cursor++
				}
				cursor++
			}
			value = strings.TrimSpace(css[start:min(cursor, len(css))])
		}
		for cursor < len(css) && cssSpace(css[cursor]) {
			cursor++
		}
		if cursor >= len(css) || css[cursor] != ')' {
			return "", errors.New("CSSのurl()が閉じていません。")
		}
		end := cursor + 1
		var err error
		value, err = unescapeCSS(value)
		if err != nil {
			return "", err
		}
		if strings.HasPrefix(strings.ToLower(value), "data:") || strings.HasPrefix(value, "#") {
			result.WriteString(css[at:end])
			at = end
			continue
		}
		source := strings.TrimPrefix(files[name].Archive, "assets/")
		if source == "" {
			source = name
		}
		base := &url.URL{Scheme: "https", Host: "asset.invalid", Path: "/" + source}
		// WHATWG URL と同じく、HTTPS の URL にある逆斜線を区切りとして扱う。
		ref, err := url.Parse(strings.ReplaceAll(value, "\\", "/"))
		if err != nil {
			return "", errors.New("CSSのURLが不正です。")
		}
		u := base.ResolveReference(ref)
		if u.Scheme != "https" || u.Host != "asset.invalid" || u.User != nil {
			return "", fmt.Errorf("%s に外部資産への参照があります。", name)
		}
		decoded, err := url.PathUnescape(normalizeURLDots(u.EscapedPath()))
		if err != nil {
			return "", errors.New("CSSのURLが不正です。")
		}
		target := strings.TrimPrefix(strings.TrimPrefix(decoded, "/"), "assets/")
		if err := relativePath(target); err != nil {
			return "", err
		}
		if visit != nil {
			visit(target)
		}
		if _, ok := files[target]; !ok {
			return "", fmt.Errorf("%s が参照する資産がロックにありません: %s", name, target)
		}
		relative := relativeURLPath(path.Dir(name), target)
		result.WriteString(`url("` + relative)
		if u.RawQuery != "" || u.ForceQuery {
			result.WriteString("?" + u.RawQuery)
		}
		if u.Fragment != "" {
			result.WriteString("#" + u.EscapedFragment())
		}
		result.WriteString(`")`)
		at = end
	}
	return result.String(), nil
}

// WHATWG URL は、パーセント表記の点だけの区間も、通常の .・.. と同じように畳む。
func normalizeURLDots(value string) string {
	var parts []string
	for _, part := range strings.Split(value, "/") {
		decoded, err := url.PathUnescape(part)
		if err == nil && decoded == "." {
			continue
		}
		if err == nil && decoded == ".." {
			if len(parts) > 0 {
				parts = parts[:len(parts)-1]
			}
			continue
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, "/")
}

func relativeURLPath(from, to string) string {
	a, b := strings.Split(path.Clean(from), "/"), strings.Split(path.Clean(to), "/")
	if len(a) == 1 && a[0] == "." {
		a = nil
	}
	i := 0
	for i < len(a) && i < len(b) && a[i] == b[i] {
		i++
	}
	rel := strings.Repeat("../", len(a)-i) + strings.Join(b[i:], "/")
	if !strings.HasPrefix(rel, ".") {
		rel = "./" + rel
	}
	return rel
}
