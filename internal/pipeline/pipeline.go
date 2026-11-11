package pipeline

// GitLab 19.5.1 の GfmPipeline の順で、消毒の境界を挟んで処理する。
// 省略するフィルターと理由:
// IframeToImg・Kroki・IframeLink: 外部の図サービスや埋め込みの設定を持たない。
// Plantuml は手元で描ける Context.Plantuml がある場合だけ処理する。
// GollumTags・WikiLinkGollum: Wiki のリポジトリとパスを解決しない（目次だけは処理する）。
// AssetProxy: 手元のプレビューにはプロキシがない。
// HeadingLocalization: 見出しはエンジンが作った ID をそのまま使う。
// Suggestion: Merge Request の差分と権限の文脈を持たない。
// 参照の16個（UserReference・ProjectReference・DesignReference・IssueReference・WorkItemReference・
// ExternalIssueReference・MergeRequestReference・SnippetReference・PersonalSnippetReference・
// CommitRangeReference・LabelReference・MilestoneReference・AlertReference・FeatureFlagReference・
// CommitReference・WikiPageReference）: GitLab のデータベースと権限を解決しない。
// ImageLazyLoad: 手元では画像をそのまま読み込む。
// Emoji: gl-emoji の代わりにエンジンの gemojis: true で Unicode の文字にする。
// CustomEmoji: GitLab の独自絵文字の設定を持たない。
// PostProcessPipeline: GitLab の権限・添付・リポジトリ・通知の文脈を持たない。
// Include: Markdown の前に処理し、読み込みは Context.ReadInclude に任せる。

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/ryo-aoki-pc/md-preview/internal/filters"
	t "github.com/ryo-aoki-pc/md-preview/internal/htmltree"
	"github.com/ryo-aoki-pc/md-preview/internal/include"
	"github.com/ryo-aoki-pc/md-preview/internal/notices"
	"github.com/ryo-aoki-pc/md-preview/internal/sanitize"
	"golang.org/x/net/html"
)

// Ruby の行頭は LF の直後だけなので、Go の局所的な m フラグで同じ条件を表す。
// 空白は ASCII に限る。言語名の貪欲さと、異なる終了区切りも元の式のままにする。
const rubyLineBreak = `(?:\r\n|[\n\v\f\r\x{0085}\x{2028}\x{2029}])`

var frontMatterPattern = regexp.MustCompile(`\A([^\r\n]*coding:[^\r\n]*` + rubyLineBreak + `)?([\t\n\v\f\r ]*)(?m:^)(---|\+\+\+|;;;)[ \t]*([^\t\n\v\f\r ]*)` + rubyLineBreak + `(?s:(.*?))(?m:^)(?:---|\+\+\+|;;;|\.\.\.)[\t\v\f ]*(?:` + rubyLineBreak + `|\z)`)

// FrontMatter は front matter を、行数を保つコードブロックへ変える。
func FrontMatter(text string) string {
	converted, _ := frontMatter(text)
	return converted
}

// frontMatter は変換した本文と、変換前の一致の終わりを返す。
func frontMatter(text string) (string, int) {
	m := frontMatterPattern.FindStringSubmatchIndex(text)
	if m == nil {
		return text, 0
	}
	group := func(i int) string {
		if m[2*i] < 0 {
			return ""
		}
		return text[m[2*i]:m[2*i+1]]
	}
	language := group(4)
	if language == "" {
		language = map[string]string{"---": "yaml", "+++": "toml", ";;;": "json"}[group(3)]
	}
	prefix := ""
	if group(1) != "" {
		prefix = "\n"
	}
	return prefix + group(2) + "```" + language + ":frontmatter\n" + group(5) + "```\n" + text[m[1]:], m[1]
}
func stringValue(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
func (r *Renderer) render(ctx context.Context, text string, c Context) (result Result, err error) {
	defer func() {
		if ctx.Err() != nil {
			result = Result{}
			err = ctx.Err()
		} else if errors.Is(err, t.ErrDepth) {
			result = Result{}
			err = &DocumentError{Message: t.ErrDepth.Error()}
		}
	}()
	if err = ctx.Err(); err != nil {
		return Result{}, err
	}
	original := strings.ReplaceAll(text, "\r", "")
	var reader func(string) include.Read
	if c.ReadInclude != nil {
		reader = func(path string) include.Read {
			v := c.ReadInclude(path)
			return include.Read{Kind: include.Kind(v.Kind), Text: v.Text}
		}
	}
	converted, frontEnd := frontMatter(original)
	expansion, err := include.Expand(ctx, converted, stringValue(c.File), reader)
	if err != nil {
		return Result{}, err
	}
	raw, err := r.engine.Render(ctx, strings.ReplaceAll(expansion.Text, "\r", ""), map[string]any{"gemojis": true, "placeholder_detection": false})
	if err != nil {
		return Result{}, err
	}
	tree, err := t.Fragment(ctx, strings.TrimRightFunc(raw, t.Space))
	if err != nil {
		return Result{}, err
	}
	state := &filters.State{Plantuml: c.Plantuml, Markdown: r.engine.Render}
	if err = filters.Before(ctx, tree, state); err != nil {
		return Result{}, err
	}
	tree, err = sanitize.Document(ctx, tree)
	if err != nil {
		return Result{}, err
	}
	tree, err = sanitize.Links(ctx, tree)
	if err != nil {
		return Result{}, err
	}
	if err = filters.After(ctx, tree, state); err != nil {
		return Result{}, err
	}
	result = Result{Source: sourceMap(text, original, converted, frontEnd, expansion), Headings: []Heading{}, Includes: []Include{}, Notices: []Notice{}, Diagrams: []Diagram{}}
	headings, err := t.Elements(ctx, tree, func(n *html.Node) bool {
		return len(n.Data) == 2 && n.Data[0] == 'h' && n.Data[1] >= '1' && n.Data[1] <= '6'
	})
	if err != nil {
		return Result{}, err
	}
	for _, n := range headings {
		var b strings.Builder
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			if child.Data == "a" && t.HasClass(child, "anchor") {
				continue
			}
			b.WriteString(t.ContentContext(ctx, child))
		}
		result.Headings = append(result.Headings, Heading{Level: int(n.Data[1] - '0'), ID: strings.TrimPrefix(t.Get(n, "id"), "user-content-"), Text: b.String()})
	}
	for _, v := range expansion.Includes {
		result.Includes = append(result.Includes, Include{Line: v.Line, Path: v.Path, Lines: v.Lines, OK: v.OK})
	}
	for _, v := range state.Diagrams {
		result.Diagrams = append(result.Diagrams, Diagram{ID: v.ID, Source: v.Source})
	}
	found, err := notices.Collect(ctx, tree, original, expansion, notices.Context{Dir: stringValue(c.Dir), Exists: c.Exists})
	if err != nil {
		return Result{}, err
	}
	for _, v := range found {
		result.Notices = append(result.Notices, Notice{Kind: v.Kind, Line: v.Line, Text: v.Text})
	}
	result.HTML, err = t.HTMLContext(ctx, tree)
	return result, err
}
