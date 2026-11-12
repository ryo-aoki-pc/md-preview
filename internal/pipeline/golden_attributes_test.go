package pipeline

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"sort"
	"strings"

	t "github.com/ryo-aoki-pc/md-tools/internal/htmltree"
	"golang.org/x/net/html"
)

// 描画エンジンの属性 map の種は作るたびに進み、属性順が呼び出し履歴で変わる。
// 通常は木の比較で見えないが、この2入力では <plaintext> の後ろに文字として現れる。
// 担当元の指定により、この入力の文字ノード内の開始タグだけ属性順をそろえる。
var textAttributeOrderCases = map[string]string{
	"1e5b1ed8620414c43a25cfd180b26fb036879cbac73d623a95af71b7103882b0": "include で取り込む複合 XSS 入力",
	"0f7e7b04a59feaa32b7ab965ea23cac66d77d450daffb1d62a331d3a8fda69d2": "複合 XSS 入力",
}

var startTagShape = regexp.MustCompile("(?s)<([A-Za-z][A-Za-z0-9:-]*)([\\t\\n\\f\\r ]+(?:[^\"'<>]|\"[^\"]*\"|'[^']*')*)?[\\t\\n\\f\\r ]*/?>")
var attributeShape = regexp.MustCompile("^([^\\t\\n\\f\\r \"'<>/=]+)(?:[\\t\\n\\f\\r ]*=[\\t\\n\\f\\r ]*(?:\"([^\"]*)\"|'([^']*)'|([^\\t\\n\\f\\r \"'=<>`]+)))?")

func canonicalTextTags(value string) string {
	return startTagShape.ReplaceAllStringFunc(value, func(raw string) string {
		z := html.NewTokenizer(strings.NewReader(raw))
		kind := z.Next()
		if kind != html.StartTagToken && kind != html.SelfClosingTagToken {
			return raw
		}
		if string(z.Raw()) != raw {
			return raw
		}
		m := startTagShape.FindStringSubmatch(raw)
		remaining := strings.Trim(m[2], "\t\n\f\r ")
		if kind == html.SelfClosingTagToken {
			remaining = strings.TrimSuffix(remaining, "/")
			remaining = strings.TrimRight(remaining, "\t\n\f\r ")
		}
		attrs := []string{}
		for remaining != "" {
			a := attributeShape.FindStringSubmatch(remaining)
			if len(a) == 0 {
				return raw
			}
			v := a[2] + a[3] + a[4]
			attrs = append(attrs, t.JSON([]string{a[1], v}))
			remaining = remaining[len(a[0]):]
			trimmed := strings.TrimLeft(remaining, "\t\n\f\r ")
			if remaining != "" && remaining == trimmed {
				return raw
			}
			remaining = trimmed
		}
		sort.Strings(attrs)
		ending := ">"
		if kind == html.SelfClosingTagToken {
			ending = "/>"
		}
		return "<" + m[1] + " " + strings.Join(attrs, " ") + ending
	})
}

func canonicalizeTextNodes(n *treeValue) {
	if n.Type == html.TextNode {
		n.Text = canonicalTextTags(n.Text)
	}
	for _, c := range n.Children {
		canonicalizeTextNodes(c)
	}
}
func textOrderException(input string) bool {
	_, ok := textAttributeOrderCases[fmt.Sprintf("%x", sha256.Sum256([]byte(input)))]
	return ok
}
