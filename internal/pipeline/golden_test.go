package pipeline

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/ryo-aoki-pc/md-tools/engine"
	t "github.com/ryo-aoki-pc/md-tools/internal/htmltree"
	"github.com/ryo-aoki-pc/md-tools/internal/sanitize"
	"golang.org/x/net/html"
)

type golden struct {
	Text    string `json:"text"`
	Context struct {
		File        *string
		Dir         *string
		Plantuml    bool
		ReadInclude bool
		Exists      bool
	} `json:"context"`
	Reads    map[string]json.RawMessage      `json:"reads"`
	Existing map[string]json.RawMessage      `json:"existing"`
	Result   *Result                         `json:"result"`
	Error    *struct{ Name, Message string } `json:"error"`
}

func goldenRows(tb testing.TB) []golden {
	tb.Helper()
	f, err := os.Open("testdata/golden/pipeline.jsonl.gz")
	if err != nil {
		tb.Fatal(err)
	}
	defer f.Close()
	z, err := gzip.NewReader(f)
	if err != nil {
		tb.Fatal(err)
	}
	defer z.Close()
	scanner := bufio.NewScanner(z)
	scanner.Buffer(make([]byte, 65536), 128*1024*1024)
	rows := []golden{}
	for scanner.Scan() {
		var row golden
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			tb.Fatal(err)
		}
		rows = append(rows, row)
	}
	if err := scanner.Err(); err != nil {
		tb.Fatal(err)
	}
	return rows
}

type treeValue struct {
	Type                 html.NodeType
	Tag, Namespace, Text string
	Attrs                []html.Attribute
	Children             []*treeValue
}

// 行の包みと BiDi の警告を残し、字句の span だけを比較から外す。
func normalized(n *html.Node, inLine bool) *treeValue {
	v := &treeValue{Type: n.Type, Tag: n.Data, Namespace: n.Namespace, Attrs: append([]html.Attribute(nil), n.Attr...)}
	if n.Type == html.TextNode {
		v.Text = n.Data
		v.Tag = ""
	}
	sort.Slice(v.Attrs, func(i, j int) bool {
		if v.Attrs[i].Namespace != v.Attrs[j].Namespace {
			return v.Attrs[i].Namespace < v.Attrs[j].Namespace
		}
		return v.Attrs[i].Key < v.Attrs[j].Key
	})
	line := n.Data == "span" && t.HasClass(n, "line")
	var appendChild func(*html.Node)
	appendChild = func(c *html.Node) {
		lexical := (inLine || line) && c.Data == "span" && len(c.Attr) == 1 && c.Attr[0].Key == "class" && !t.HasClass(c, "line")
		if lexical {
			for child := c.FirstChild; child != nil; child = child.NextSibling {
				appendChild(child)
			}
			return
		}
		child := normalized(c, inLine || line)
		if child.Type == html.TextNode && len(v.Children) > 0 && v.Children[len(v.Children)-1].Type == html.TextNode {
			v.Children[len(v.Children)-1].Text += child.Text
		} else {
			v.Children = append(v.Children, child)
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		appendChild(c)
	}
	return v
}
func treeDiff(a, b *treeValue, path string) string {
	if a.Type != b.Type || a.Tag != b.Tag || a.Namespace != b.Namespace || a.Text != b.Text {
		return fmt.Sprintf("%s: 見本=%s Go=%s", path, t.JSON([]any{a.Type, a.Tag, a.Namespace, a.Text}), t.JSON([]any{b.Type, b.Tag, b.Namespace, b.Text}))
	}
	if !reflect.DeepEqual(a.Attrs, b.Attrs) {
		return fmt.Sprintf("%s/属性: 見本=%s Go=%s", path, t.JSON(a.Attrs), t.JSON(b.Attrs))
	}
	if len(a.Children) != len(b.Children) {
		return fmt.Sprintf("%s/子: 見本=%d Go=%d\n見本=%s\nGo=%s", path, len(a.Children), len(b.Children), t.JSON(a.Children), t.JSON(b.Children))
	}
	for i := range a.Children {
		if diff := treeDiff(a.Children[i], b.Children[i], fmt.Sprintf("%s/%s[%d]", path, a.Children[i].Tag, i)); diff != "" {
			return diff
		}
	}
	return ""
}
func compareHTML(expected, actual string, textAttributeOrder bool) (string, error) {
	a, err := t.Fragment(context.Background(), expected)
	if err != nil {
		return "", err
	}
	b, err := t.Fragment(context.Background(), actual)
	if err != nil {
		return "", err
	}
	x, y := normalized(a, false), normalized(b, false)
	if textAttributeOrder {
		canonicalizeTextNodes(x)
		canonicalizeTextNodes(y)
	}
	return treeDiff(x, y, "本文"), nil
}
func goldenContext(tb testing.TB, row golden) Context {
	c := Context{File: row.Context.File, Dir: row.Context.Dir, Plantuml: row.Context.Plantuml}
	if row.Context.ReadInclude {
		c.ReadInclude = func(path string) IncludeRead {
			raw, ok := row.Reads[path]
			if !ok {
				tb.Errorf("未記録の取り込み: %q", path)
				return IncludeRead{Kind: IncludeMissing}
			}
			var value any
			if json.Unmarshal(raw, &value) != nil {
				tb.Fatalf("取り込みの記録が不正: %s", raw)
			}
			switch v := value.(type) {
			case nil:
				return IncludeRead{Kind: IncludeMissing}
			case string:
				return IncludeRead{Kind: IncludeText, Text: v}
			case map[string]any:
				if v["thrown"] == true {
					return IncludeRead{Kind: IncludeNotReadable}
				}
				if v["error"] == "not readable" {
					return IncludeRead{Kind: IncludeNotReadable}
				}
				if v["error"] == "too large" {
					return IncludeRead{Kind: IncludeTooLarge}
				}
			}
			return IncludeRead{Kind: IncludeInvalid}
		}
	}
	if row.Context.Exists {
		c.Exists = func(path string) bool {
			raw, ok := row.Existing[path]
			if !ok {
				tb.Errorf("未記録の存在確認: %q", path)
			}
			var v any
			if err := json.Unmarshal(raw, &v); err != nil {
				tb.Errorf("存在確認の記録が不正: %s", raw)
			}
			return v == true
		}
	}
	return c
}
func TestGolden(tst *testing.T) {
	e, err := engine.New(context.Background())
	if err != nil {
		tst.Fatal(err)
	}
	defer e.Close(context.Background())
	renderer := New(e)
	rows := goldenRows(tst)
	var report strings.Builder
	failed, passed := 0, 0
	for i, row := range rows {
		result, err := renderer.Render(context.Background(), row.Text, goldenContext(tst, row))
		diffs := []string{}
		if row.Error != nil {
			var doc *DocumentError
			if !errors.As(err, &doc) || doc.Message != row.Error.Message {
				diffs = append(diffs, fmt.Sprintf("誤り: 見本=%s Go=%T %v", row.Error.Message, err, err))
			}
		} else if err != nil {
			diffs = append(diffs, fmt.Sprintf("描画の誤り: %T %v", err, err))
		} else {
			for _, pair := range []struct {
				name string
				a, b any
			}{{"headings", row.Result.Headings, result.Headings}, {"includes", row.Result.Includes, result.Includes}, {"notices", row.Result.Notices, result.Notices}, {"diagrams", row.Result.Diagrams, result.Diagrams}} {
				a, b := t.JSON(pair.a), t.JSON(pair.b)
				if a != b {
					diffs = append(diffs, fmt.Sprintf("%s: 見本=%s Go=%s", pair.name, a, b))
				}
			}
			diff, err := compareHTML(row.Result.HTML, result.HTML, false)
			if textOrderException(row.Text) && err == nil {
				if diff == "" {
					tst.Errorf("件 %d: 属性順の例外が不要になりました。表から除いてください。", i+1)
				} else {
					diff, err = compareHTML(row.Result.HTML, result.HTML, true)
				}
			}
			if err != nil {
				diffs = append(diffs, fmt.Sprintf("木の比較: %v", err))
			} else if diff != "" {
				diffs = append(diffs, diff)
			}
		}
		if len(diffs) > 0 {
			failed++
			prefix := []rune(row.Text)
			if len(prefix) > 160 {
				prefix = prefix[:160]
			}
			fmt.Fprintf(&report, "件 %d 入力=%s\n%s\n\n", i+1, t.JSON(string(prefix)), strings.Join(diffs, "\n"))
		} else {
			passed++
		}
	}
	if len(rows) != 2715 {
		tst.Errorf("見本の件数: %d", len(rows))
	}
	fmt.Fprintf(&report, "一致 %d / %d、不一致 %d\n", passed, len(rows), failed)
	dir := filepath.Join("..", "..", ".tests")
	if err := os.MkdirAll(dir, 0755); err != nil {
		tst.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "golden-latest.txt"), []byte(report.String()), 0644); err != nil {
		tst.Fatal(err)
	}
	tst.Logf("一致 %d / %d、不一致 %d", passed, len(rows), failed)
	if failed > 0 {
		lines := strings.Split(report.String(), "\n")
		if len(lines) > 100 {
			lines = lines[:100]
		}
		tst.Errorf("%s\n全差分は .tests/golden-latest.txt", strings.Join(lines, "\n"))
	}
}

// 読み直しで境界が変わっても、許可しない要素・属性・URL が現れないことを全見本で確かめる。
var chipStyle = regexp.MustCompile(`^background-color: [#A-Za-z0-9(),.% ]+;$`)

func safetyProblems(root *html.Node) []string {
	out := []string{}
	_ = t.Walk(context.Background(), root, func(n *html.Node) error {
		if n.Type == html.CommentNode {
			out = append(out, "コメント")
		}
		if n.Type != html.ElementNode {
			return nil
		}
		if !sanitize.Tags[n.Data] && n.Data != "video" && n.Data != "audio" && n.Data != "copy-code" && n.Data != "insert-code-snippet" {
			out = append(out, "要素 "+n.Data)
		}
		for _, a := range n.Attr {
			key := strings.ToLower(a.Key)
			if strings.HasPrefix(key, "on") || containsString([]string{"srcdoc", "formaction", "action", "data", "xlink:href", "ping", "background", "manifest"}, key) {
				out = append(out, "属性 "+a.Key)
			}
			if key == "style" {
				valid := (n.Data == "th" || n.Data == "td") && (a.Val == "text-align: center" || a.Val == "text-align: left" || a.Val == "text-align: right")
				if n.Data == "span" && n.Parent != nil && n.Parent.Data == "span" && t.HasClass(n.Parent, "gfm-color_chip") && chipStyle.MatchString(a.Val) {
					valid = true
				}
				if !valid {
					out = append(out, "style="+a.Val)
				}
			}
			if containsString([]string{"href", "src", "cite", "longdesc", "data-src", "data-canonical-src", "poster", "action", "formaction", "data", "xlink:href"}, key) {
				var b strings.Builder
				for _, r := range strings.ToLower(a.Val) {
					if r > 32 {
						b.WriteRune(r)
					}
				}
				s := b.String()
				for _, scheme := range []string{"javascript:", "vbscript:", "data:", "livescript:", "mocha:", "jar:", "blob:", "filesystem:"} {
					if strings.HasPrefix(s, scheme) {
						out = append(out, key+"="+a.Val)
					}
				}
			}
		}
		return nil
	})
	return out
}
func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
func TestGoldenSafety(tst *testing.T) {
	e, err := engine.New(context.Background())
	if err != nil {
		tst.Fatal(err)
	}
	defer e.Close(context.Background())
	r := New(e)
	for i, row := range goldenRows(tst) {
		if row.Error != nil {
			continue
		}
		result, err := r.Render(context.Background(), row.Text, goldenContext(tst, row))
		if err != nil {
			tst.Errorf("件 %d: %v", i+1, err)
			continue
		}
		s := result.HTML
		for j := 0; j < 2; j++ {
			root, err := t.Fragment(context.Background(), s)
			if err != nil {
				tst.Errorf("件 %d: %v", i+1, err)
				break
			}
			if problems := safetyProblems(root); len(problems) > 0 {
				tst.Errorf("件 %d 読み直し %d: %v", i+1, j+1, problems)
			}
			serialized, renderErr := t.HTML(root)
			if j > 0 && serialized != s {
				tst.Errorf("件 %d: 2 回目の読み直しで木が変わりました。", i+1)
			}
			s, err = serialized, renderErr
			if err != nil {
				tst.Fatal(err)
			}
		}
	}
}
