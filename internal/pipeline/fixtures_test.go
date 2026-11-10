package pipeline

// 許可する正規化は属性順、遅延画像、脚注の乱数接尾辞、絵文字の囲み、配色の字句の5つだけ。
// 行の span・包み・文字、解決された参照、そのほかの要素と属性は厳密に比べる。
import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/ryo-aoki-pc/md-preview/engine"
	ht "github.com/ryo-aoki-pc/md-preview/internal/htmltree"
	"golang.org/x/net/html"
)

const fixtureDirectory = "../../test/fixtures"

type fixtureNode struct {
	Type      string            `json:"type"`
	Tag       string            `json:"tag,omitempty"`
	Namespace string            `json:"namespace,omitempty"`
	Text      string            `json:"text,omitempty"`
	Attrs     map[string]string `json:"attrs,omitempty"`
	Children  []*fixtureNode    `json:"children,omitempty"`
}

type fixtureKnownDifference struct {
	Fixture    string `json:"fixture"`
	Kind       string `json:"kind"`
	Path       []int  `json:"path"`
	OraclePath []int  `json:"oraclePath,omitempty"`
	Reason     string `json:"reason"`
	Values     map[string]struct {
		Local  *string `json:"local"`
		Oracle *string `json:"oracle"`
	} `json:"values,omitempty"`
	Local  *fixtureNode `json:"local,omitempty"`
	Oracle *fixtureNode `json:"oracle,omitempty"`
}

func readFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(fixtureDirectory, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func fixtureDifferences(t *testing.T) []fixtureKnownDifference {
	t.Helper()
	data, err := os.ReadFile("testdata/known_differences.json")
	if err != nil {
		t.Fatal(err)
	}
	var differences []fixtureKnownDifference
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&differences); err != nil {
		t.Fatal(err)
	}
	return differences
}

func fixtureRenderer(t *testing.T) *Renderer {
	t.Helper()
	e, err := engine.New(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Close(context.Background()) })
	return New(e)
}

func renderFixture(t *testing.T, r *Renderer, name string, c Context) string {
	t.Helper()
	result, err := r.Render(t.Context(), readFixture(t, name+".md"), c)
	if err != nil {
		t.Fatal(err)
	}
	return result.HTML
}

func fixtureMergeText(nodes []*fixtureNode) []*fixtureNode {
	merged := make([]*fixtureNode, 0, len(nodes))
	for _, n := range nodes {
		if len(merged) > 0 && n.Type == "text" && merged[len(merged)-1].Type == "text" {
			merged[len(merged)-1].Text += n.Text
		} else {
			merged = append(merged, n)
		}
	}
	return merged
}

var fixtureFootnoteSuffix = regexp.MustCompile(`-\d+$`)
var fixtureFootnoteID = regexp.MustCompile(`^fn(?:ref)?-`)
var fixtureFootnoteLink = regexp.MustCompile(`^#fn(?:ref)?-`)

func normalizeFixtureHTML(source string, oracle bool) (*fixtureNode, error) {
	root, err := ht.Fragment(context.Background(), source)
	if err != nil {
		return nil, err
	}
	elements, err := ht.Elements(context.Background(), root, nil)
	if err != nil {
		return nil, err
	}
	ids := map[string]bool{}
	for _, n := range elements {
		if ht.Has(n, "id") {
			ids[ht.Get(n, "id")] = true
		}
	}
	for _, n := range elements {
		if n.Data == "a" && (ht.Has(n, "data-footnote-ref") || ht.Has(n, "data-footnote-backref")) {
			href := ht.Get(n, "href")
			if !strings.HasPrefix(href, "#") || !ids[strings.TrimPrefix(href, "#")] {
				return nil, fmt.Errorf("脚注の参照先が存在しない: %s", href)
			}
		}
	}
	suffix := ""
	if oracle {
		for _, n := range elements {
			id := ht.Get(n, "id")
			if n.Data != "li" || !strings.HasPrefix(id, "fn-") || !ht.Ancestor(n, func(p *html.Node) bool { return p.Data == "section" && ht.Has(p, "data-footnotes") }) {
				continue
			}
			if suffix == "" {
				suffix = fixtureFootnoteSuffix.FindString(id)
				if suffix == "" {
					return nil, fmt.Errorf("GitLab の脚注に乱数の接尾辞がない: %s", id)
				}
			}
			if !strings.HasSuffix(id, suffix) {
				return nil, fmt.Errorf("脚注の乱数が文書内で一致していない: %s", id)
			}
		}
	}
	var convert func(*html.Node, bool, bool) ([]*fixtureNode, error)
	convert = func(n *html.Node, inHighlight, inLine bool) ([]*fixtureNode, error) {
		switch n.Type {
		case html.TextNode:
			return []*fixtureNode{{Type: "text", Text: n.Data}}, nil
		case html.CommentNode:
			return []*fixtureNode{{Type: "comment", Text: n.Data}}, nil
		case html.DoctypeNode:
			return []*fixtureNode{{Type: "doctype"}}, nil
		}
		highlighted := inHighlight || n.Data == "pre" && ht.HasClass(n, "code") && ht.HasClass(n, "highlight")
		line := inLine || highlighted && n.Data == "span" && ht.HasClass(n, "line")
		children := []*fixtureNode{}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			converted, err := convert(c, highlighted, line)
			if err != nil {
				return nil, err
			}
			children = append(children, converted...)
		}
		children = fixtureMergeText(children)
		if n.Data == "gl-emoji" {
			return []*fixtureNode{{Type: "text", Text: ht.Content(n)}}, nil
		}
		if inLine && n.Data == "span" {
			return children, nil
		}
		if n.Type == html.DocumentNode {
			return []*fixtureNode{{Type: "root", Children: children}}, nil
		}
		attrs := map[string]string{}
		for _, a := range n.Attr {
			name := a.Key
			if a.Namespace != "" {
				name = a.Namespace + ":" + name
			}
			attrs[name] = a.Val
		}
		if n.Data == "img" && ht.HasClass(n, "lazy") && ht.Has(n, "data-src") {
			if attrs["src"] != "data:image/gif;base64,R0lGODlhAQABAAAAACH5BAEKAAEALAAAAAABAAEAAAICTAEAOw==" {
				return nil, fmt.Errorf("遅延画像の仮画像が変わった: %s", attrs["src"])
			}
			attrs["src"] = attrs["data-src"]
			delete(attrs, "data-src")
			classes := []string{}
			for _, class := range ht.Classes(n) {
				if class != "lazy" {
					classes = append(classes, class)
				}
			}
			delete(attrs, "class")
			if len(classes) > 0 {
				attrs["class"] = strings.Join(classes, " ")
			}
			if attrs["decoding"] == "async" {
				delete(attrs, "decoding")
			}
		}
		if suffix != "" {
			if (n.Data == "li" || ht.Has(n, "data-footnote-ref")) && fixtureFootnoteID.MatchString(attrs["id"]) {
				attrs["id"] = strings.TrimSuffix(attrs["id"], suffix)
			}
			if (ht.Has(n, "data-footnote-ref") || ht.Has(n, "data-footnote-backref")) && fixtureFootnoteLink.MatchString(attrs["href"]) {
				attrs["href"] = strings.TrimSuffix(attrs["href"], suffix)
			}
		}
		return []*fixtureNode{{Type: "element", Tag: n.Data, Namespace: n.Namespace, Attrs: attrs, Children: children}}, nil
	}
	nodes, err := convert(root, false, false)
	if err != nil {
		return nil, err
	}
	return nodes[0], nil
}

func mustNormalizeFixture(t *testing.T, source string, oracle bool) *fixtureNode {
	t.Helper()
	n, err := normalizeFixtureHTML(source, oracle)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func fixtureAt(root *fixtureNode, path []int) (*fixtureNode, error) {
	n := root
	for _, index := range path {
		if n == nil || index < 0 || index >= len(n.Children) {
			return nil, fmt.Errorf("既知の差の場所が存在しない: %v", path)
		}
		n = n.Children[index]
	}
	return n, nil
}

func fixtureAttribute(n *fixtureNode, name string) *string {
	if value, ok := n.Attrs[name]; ok {
		return &value
	}
	return nil
}

func maskFixtureDifferences(name string, local, oracle *fixtureNode, differences []fixtureKnownDifference) error {
	for _, difference := range differences {
		if difference.Fixture != name {
			continue
		}
		rightPath := difference.OraclePath
		if rightPath == nil {
			rightPath = difference.Path
		}
		right, err := fixtureAt(oracle, rightPath)
		if err != nil {
			return fmt.Errorf("%s: %w", difference.Reason, err)
		}
		if difference.Kind == "oracle-only" {
			if !reflect.DeepEqual(right, difference.Oracle) {
				return fmt.Errorf("既知の差の GitLab の要素が変わった: %s\n実際=%s\n固定値=%s", difference.Reason, ht.JSON(right), ht.JSON(difference.Oracle))
			}
			parent, err := fixtureAt(oracle, rightPath[:len(rightPath)-1])
			if err != nil {
				return err
			}
			index := rightPath[len(rightPath)-1]
			parent.Children = fixtureMergeText(append(parent.Children[:index], parent.Children[index+1:]...))
			continue
		}
		left, err := fixtureAt(local, difference.Path)
		if err != nil {
			return fmt.Errorf("%s: %w", difference.Reason, err)
		}
		switch difference.Kind {
		case "attributes":
			for key, values := range difference.Values {
				if !reflect.DeepEqual(fixtureAttribute(left, key), values.Local) || !reflect.DeepEqual(fixtureAttribute(right, key), values.Oracle) {
					return fmt.Errorf("既知の差の属性が変わった: %s / %s\n手元=%s\nGitLab=%s\n固定値=%s", difference.Reason, key, ht.JSON(fixtureAttribute(left, key)), ht.JSON(fixtureAttribute(right, key)), ht.JSON(values))
				}
				left.Attrs[key], right.Attrs[key] = "[既知の差]", "[既知の差]"
			}
		case "node":
			if !reflect.DeepEqual(left, difference.Local) || !reflect.DeepEqual(right, difference.Oracle) {
				return fmt.Errorf("既知の差の要素が変わった: %s\n手元=%s\nGitLab=%s", difference.Reason, ht.JSON(left), ht.JSON(right))
			}
			*left, *right = fixtureNode{Type: "known", Text: difference.Reason}, fixtureNode{Type: "known", Text: difference.Reason}
		default:
			return fmt.Errorf("不明な既知の差の種類: %s", difference.Kind)
		}
	}
	return nil
}

func fixtureFirstDifference(left, right *fixtureNode, path string) string {
	if left.Type != right.Type || left.Tag != right.Tag || left.Namespace != right.Namespace || left.Text != right.Text || !reflect.DeepEqual(left.Attrs, right.Attrs) {
		return fmt.Sprintf("%s: 手元=%s\nGitLab=%s", path, ht.JSON(left), ht.JSON(right))
	}
	for i := 0; i < len(left.Children) && i < len(right.Children); i++ {
		if diff := fixtureFirstDifference(left.Children[i], right.Children[i], fmt.Sprintf("%s/%d", path, i)); diff != "" {
			return diff
		}
	}
	if len(left.Children) != len(right.Children) {
		return fmt.Sprintf("%s/子: 手元=%s\nGitLab=%s", path, ht.JSON(left.Children), ht.JSON(right.Children))
	}
	return ""
}

func TestFixtures(t *testing.T) {
	var metadata struct{ Fixtures map[string]json.RawMessage }
	if err := json.Unmarshal([]byte(readFixture(t, "fixtures.json")), &metadata); err != nil {
		t.Fatal(err)
	}
	if len(metadata.Fixtures) == 0 {
		t.Fatal("見本がありません")
	}
	names := make([]string, 0, len(metadata.Fixtures))
	for name := range metadata.Fixtures {
		names = append(names, name)
	}
	sort.Strings(names)
	differences := fixtureDifferences(t)
	for _, difference := range differences {
		if _, ok := metadata.Fixtures[difference.Fixture]; !ok {
			t.Fatalf("既知の差に見本がありません: %s", difference.Fixture)
		}
	}
	r := fixtureRenderer(t)
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			local := mustNormalizeFixture(t, renderFixture(t, r, name, Context{}), false)
			oracle := mustNormalizeFixture(t, readFixture(t, name+".gitlab.html"), true)
			if err := maskFixtureDifferences(name, local, oracle, differences); err != nil {
				t.Fatal(err)
			}
			if diff := fixtureFirstDifference(local, oracle, "本文"); diff != "" {
				t.Fatalf("最初の相違: %s", diff)
			}
		})
	}
}

func TestFixtureKnownDifferenceReasons(t *testing.T) {
	// 移行中は Node.js 版の件数・順序・理由・子の番号と照合し、表の無断変更を検知する。
	source, err := os.ReadFile("../../test/fixtures.test.mjs")
	if err != nil {
		t.Fatal(err)
	}
	_, table, found := strings.Cut(string(source), "export const KNOWN_DIFFERENCES = ")
	if !found {
		t.Fatal("Node.js 版の既知の差の表がありません")
	}
	table, _, found = strings.Cut(table, "\n];")
	if !found {
		t.Fatal("Node.js 版の既知の差の終端がありません")
	}
	var original []struct {
		Fixture, Kind, Reason string
		Path, OraclePath      []any
	}
	if err := json.Unmarshal([]byte(table+"\n]"), &original); err != nil {
		t.Fatal(err)
	}
	actual := fixtureDifferences(t)
	if len(actual) != len(original) {
		t.Fatalf("既知の差の件数: Go=%d Node.js=%d", len(actual), len(original))
	}
	indices := func(path []any) []int {
		if path == nil {
			return nil
		}
		out := []int{}
		for _, value := range path {
			if index, ok := value.(float64); ok {
				out = append(out, int(index))
			}
		}
		return out
	}
	for i, a := range actual {
		b := original[i]
		if a.Fixture != b.Fixture || a.Kind != b.Kind || a.Reason != b.Reason || !reflect.DeepEqual(a.Path, indices(b.Path)) || !reflect.DeepEqual(a.OraclePath, indices(b.OraclePath)) {
			t.Errorf("既知の差 %d の対応が違います: %s", i+1, a.Reason)
		}
	}
}

func TestFixtureHighlightTokens(t *testing.T) {
	r := fixtureRenderer(t)
	root, err := ht.Fragment(t.Context(), renderFixture(t, r, "code", Context{}))
	if err != nil {
		t.Fatal(err)
	}
	for _, language := range []string{"javascript", "ruby", "python", "shell", "json", "yaml", "diff", "html"} {
		blocks, err := ht.Elements(t.Context(), root, func(n *html.Node) bool { return n.Data == "pre" && ht.HasClass(n, "language-"+language) })
		if err != nil || len(blocks) == 0 {
			t.Fatalf("コードブロックがない: %s (%v)", language, err)
		}
		tokens, err := ht.Elements(t.Context(), blocks[0], func(n *html.Node) bool { return n.Data == "span" && !ht.HasClass(n, "line") })
		if err != nil || len(tokens) == 0 {
			t.Errorf("字句の span がない: %s (%v)", language, err)
		}
	}
}

func TestFixturePlantuml(t *testing.T) {
	r := fixtureRenderer(t)
	root := mustNormalizeFixture(t, renderFixture(t, r, "code", Context{Plantuml: true}), false)
	var actual *fixtureNode
	var visit func(*fixtureNode)
	visit = func(n *fixtureNode) {
		if n.Tag == "a" && n.Attrs["data-diagram"] == "plantuml" && actual == nil {
			actual = n
		}
		for _, c := range n.Children {
			visit(c)
		}
	}
	visit(root)
	var expected *fixtureNode
	for _, difference := range fixtureDifferences(t) {
		if strings.HasPrefix(difference.Reason, "61 行の PlantUML") {
			expected = difference.Oracle
		}
	}
	if actual == nil || expected == nil || len(actual.Children) != 1 || len(expected.Children) != 1 {
		t.Fatal("PlantUML の図の見本がありません")
	}
	if actual.Children[0].Attrs["data-sourcepos"] != "61:1-63:3" {
		t.Fatal("PlantUML の行の位置が違います")
	}
	delete(actual.Attrs, "href")
	delete(expected.Attrs, "href")
	delete(actual.Attrs, "rel")
	delete(expected.Attrs, "rel")
	delete(actual.Children[0].Attrs, "src")
	delete(expected.Children[0].Attrs, "src")
	delete(actual.Children[0].Attrs, "data-sourcepos")
	if diff := fixtureFirstDifference(actual, expected, "PlantUML"); diff != "" {
		t.Fatal(diff)
	}
}

func TestFixtureComparatorStrict(t *testing.T) {
	samples := [][2]string{
		{"<pre><code> x\n</code></pre>", "<pre><code>x\n</code></pre>"},
		{`<p class="a b">x</p>`, `<p class="b a">x</p>`},
		{`<a href="/a">x</a>`, `<a href="/b">x</a>`},
		{`<input type="checkbox" checked>`, `<input type="checkbox">`},
		{`<p data-sourcepos="1:1-1:2">x</p>`, `<p data-sourcepos="2:1-2:2">x</p>`},
		{"<p><span>x</span></p>", "<p>x</p>"},
		{`<a class="gfm">@name</a>`, "@name"},
		{"<p>x\n y</p>", "<p>x y</p>"},
		{`<img src="a" decoding="async">`, `<img src="a">`},
		{`<img src="a" alt="x">`, `<img src="a" alt="y">`},
	}
	for _, pair := range samples {
		if fixtureFirstDifference(mustNormalizeFixture(t, pair[0], false), mustNormalizeFixture(t, pair[1], false), "本文") == "" {
			t.Errorf("区別していない: %s / %s", pair[0], pair[1])
		}
	}
	left := mustNormalizeFixture(t, `<p title="x" dir="auto">x</p>`, false)
	right := mustNormalizeFixture(t, `<p dir="auto" title="x">x</p>`, false)
	if diff := fixtureFirstDifference(left, right, "本文"); diff != "" {
		t.Fatal(diff)
	}
}

func TestFixtureComparatorHighlightLines(t *testing.T) {
	block := func(content string) string {
		return `<pre class="code highlight"><code><span id="LC1" class="line" data-lang="javascript">` + content + `</span></code></pre>`
	}
	plain := mustNormalizeFixture(t, block("x "), false)
	if diff := fixtureFirstDifference(plain, mustNormalizeFixture(t, block(`<span class="k">x</span> `), false), "本文"); diff != "" {
		t.Fatal(diff)
	}
	for _, changed := range []string{block("x"), strings.ReplaceAll(block("x "), "LC1", "LC2"), strings.ReplaceAll(block("x "), "javascript", "python"), strings.ReplaceAll(block("x "), `class="line"`, `class="line extra"`), strings.ReplaceAll(block("x "), "</code>", "</code><copy-code></copy-code>")} {
		if fixtureFirstDifference(plain, mustNormalizeFixture(t, changed, false), "本文") == "" {
			t.Errorf("行番号・言語・空白・行の属性・包みの変更を区別していない: %s", changed)
		}
	}
}

func TestFixtureComparatorFootnotes(t *testing.T) {
	valid := `<sup><a data-footnote-ref id="fnref-1-2-4567" href="#fn-1-4567">1</a></sup><section data-footnotes><ol><li id="fn-1-4567"><a data-footnote-backref href="#fnref-1-2-4567">戻る</a></li></ol></section>`
	for _, source := range []string{`<sup><a data-footnote-ref href="#fn-missing">1</a></sup>`, strings.ReplaceAll(valid, `href="#fnref-1-2-4567"`, `href="#fnref-missing"`)} {
		if _, err := normalizeFixtureHTML(source, true); err == nil || !strings.Contains(err.Error(), "参照先が存在しない") {
			t.Fatalf("参照先を検査していない: %v", err)
		}
	}
	n := mustNormalizeFixture(t, valid, true).Children[0].Children[0]
	if n.Attrs["id"] != "fnref-1-2" || n.Attrs["href"] != "#fn-1" {
		t.Fatal(n.Attrs)
	}
	for _, source := range []string{`<section data-footnotes><li id="fn-name"></li></section>`, valid + `<section data-footnotes><li id="fn-2-9999"></li></section>`} {
		if _, err := normalizeFixtureHTML(source, true); err == nil {
			t.Fatal("脚注の乱数接尾辞を検査していない")
		}
	}
}

func TestFixtureComparatorAllowedNormalizations(t *testing.T) {
	local := `<p>😄<img class="keep" src="image.png" alt="図"></p>`
	oracle := `<p><gl-emoji data-name="smile">😄</gl-emoji><img alt="図" class="lazy keep" data-src="image.png" decoding="async" src="data:image/gif;base64,R0lGODlhAQABAAAAACH5BAEKAAEALAAAAAABAAEAAAICTAEAOw=="></p>`
	if diff := fixtureFirstDifference(mustNormalizeFixture(t, local, false), mustNormalizeFixture(t, oracle, true), "本文"); diff != "" {
		t.Fatal(diff)
	}
	if _, err := normalizeFixtureHTML(strings.ReplaceAll(oracle, "data:image/gif;base64,R0lGODlhAQABAAAAACH5BAEKAAEALAAAAAABAAEAAAICTAEAOw==", "wrong.gif"), true); err == nil {
		t.Fatal("遅延画像の仮画像を検査していない")
	}
}
