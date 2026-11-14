// Package links は、完成した描画の本文のリンクを、外へ通信せずに点検する。
package links

import (
	"context"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ryo-aoki-pc/md-tools/internal/documents"
	t "github.com/ryo-aoki-pc/md-tools/internal/htmltree"
	"github.com/ryo-aoki-pc/md-tools/internal/paths"
	"github.com/ryo-aoki-pc/md-tools/internal/pipeline"
	"github.com/ryo-aoki-pc/md-tools/internal/resolve"
	"golang.org/x/net/html"
)

const (
	MaxDocuments = 32
	MaxBytes     = 10 * 1024 * 1024
	Timeout      = 10 * time.Second
)

type Item struct {
	Line   *int   `json:"line"`
	Href   string `json:"href"`
	Reason string `json:"reason"`
}

type Result struct {
	Render    uint64 `json:"render"`
	Broken    []Item `json:"broken"`
	Unchecked []Item `json:"unchecked"`
}

type Renderer interface {
	Render(context.Context, string, pipeline.Context) (pipeline.Result, error)
}

var scheme = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*:`)
var position = regexp.MustCompile(`^(\d+):\d+-\d+:\d+$`)

// candidates は公開 HTML の位置を原文の行へ戻す。取り込んだ本文は include の行を指す。
func candidates(ctx context.Context, rendered pipeline.Result) ([]Item, map[string]bool, error) {
	tree, err := t.Fragment(ctx, rendered.HTML)
	if err != nil {
		return nil, nil, err
	}
	ids := map[string]bool{}
	for _, h := range rendered.Headings {
		ids[h.ID] = true
	}
	items := []Item{}
	err = t.Walk(ctx, tree, func(n *html.Node) error {
		if n.Type != html.ElementNode {
			return nil
		}
		for _, attr := range []string{"id", "name"} {
			if id := t.Get(n, attr); id != "" {
				ids[id] = true
			}
		}
		if n.Data != "a" || !t.Has(n, "href") || t.HasClass(n, "anchor") ||
			t.Has(n, "data-footnote-ref") || t.HasClass(n, "footnote-backref") ||
			t.Has(n, "data-diagram") || t.Ancestor(n, func(p *html.Node) bool {
			return p.Data == "ul" && t.HasClass(p, "section-nav")
		}) {
			return nil
		}
		// 画像フィルターが作った、画像そのものを開くリンクは既存のメディアの知らせに任せる。
		if child := n.FirstChild; child != nil && child == n.LastChild && child.Data == "img" &&
			(t.Get(n, "href") == t.Get(child, "src") || t.Get(n, "href") == t.Get(child, "data-src")) {
			return nil
		}
		href := t.Get(n, "href")
		if strings.HasPrefix(href, "//") || scheme.MatchString(href) {
			return nil
		}
		item := Item{Href: href}
		offset := 0
		for p := n; p != nil; p = p.Parent {
			if m := position.FindStringSubmatch(t.Get(p, "data-sourcepos")); m != nil {
				line, _ := strconv.Atoi(m[1])
				line += offset
				if line, _ = rendered.Source.Original(line); line > 0 {
					item.Line = &line
				}
				break
			}
			for sibling := p.PrevSibling; sibling != nil; sibling = sibling.PrevSibling {
				offset += strings.Count(t.ContentContext(ctx, sibling), "\n")
			}
		}
		items = append(items, item)
		return nil
	})
	return items, ids, err
}

// Check は別文書を再帰せず一度ずつ描く。読み取りの関数は FileContext と同じ検査を持つものに限る。
func Check(ctx context.Context, rendered pipeline.Result, root string, c pipeline.Context, renderer Renderer) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	out := Result{Broken: []Item{}, Unchecked: []Item{}}
	items, ids, err := candidates(ctx, rendered)
	if err != nil {
		return out, err
	}
	type target struct {
		ids    map[string]bool
		reason string
	}
	cache := map[string]target{}
	drawn := 0
	dir := ""
	if c.Dir != nil {
		dir = *c.Dir
	}
	add := func(item Item, reason string, unchecked bool) {
		item.Reason = reason
		if unchecked {
			out.Unchecked = append(out.Unchecked, item)
		} else {
			out.Broken = append(out.Broken, item)
		}
	}
	for _, item := range items {
		if ctx.Err() != nil {
			add(item, "期限を超えたため点検していません", true)
			continue
		}
		file, fragment, hasFragment := strings.Cut(item.Href, "#")
		id, err := url.PathUnescape(fragment)
		if err != nil {
			add(item, "行き先の符号化が正しくありません", false)
			continue
		}
		file, _, _ = strings.Cut(file, "?")
		if file == "" {
			if hasFragment && id != "" && !ids[id] && !ids["user-content-"+id] {
				add(item, "行き先の ID がありません", false)
			}
			continue
		}
		decoded, err := url.PathUnescape(file)
		if err != nil {
			add(item, "行き先の符号化が正しくありません", false)
			continue
		}
		resolved := paths.Resolve(decoded, dir)
		if !resolved.Safe {
			add(item, "ルートの外", true)
			continue
		}
		// 同じ文書へのファイル名付きのリンクにも、未保存の本文の見出しを使う。
		if c.File != nil && resolved.Path == *c.File {
			if hasFragment && id != "" && !ids[id] && !ids["user-content-"+id] {
				add(item, "行き先の ID がありません", false)
			}
			continue
		}
		if root == "" || c.Exists == nil {
			add(item, "文書のルートがないため点検していません", true)
			continue
		}
		located := resolve.Resolve(root, documents.Encode(resolved.Path))
		if located.Status == 403 {
			add(item, "ルートの外、または読み取りを許可していない場所です", true)
			continue
		}
		if located.Status != 200 || !c.Exists(resolved.Path) {
			add(item, "行き先のファイルがありません", false)
			continue
		}
		if !hasFragment || id == "" || !documents.IsMarkdown(resolved.Path) {
			continue
		}
		// 実パスを鍵にして、別名やシンボリックリンクからの重複した描画も避ける。
		key := located.Path
		if target, ok := cache[key]; ok {
			if target.reason != "" {
				add(item, target.reason, true)
			} else if !target.ids[id] {
				add(item, "行き先の見出しの ID がありません", false)
			}
			continue
		}
		value := target{}
		switch {
		case drawn >= MaxDocuments:
			value.reason = "文書数の上限を超えたため点検していません"
		case c.ReadInclude == nil || renderer == nil:
			value.reason = "文書を読み取れないため点検していません"
		default:
			read := c.ReadInclude(resolved.Path)
			switch {
			case read.Kind == pipeline.IncludeTooLarge || len(read.Text) > MaxBytes:
				value.reason = "文書が 10 MiB の上限を超えたため点検していません"
			case read.Kind != pipeline.IncludeText:
				value.reason = "文書を読み取れないため点検していません"
			default:
				drawn++
				targetContext := c
				file, dir := resolved.Path, path.Dir(resolved.Path)
				targetContext.File, targetContext.Dir = &file, &dir
				result, err := renderer.Render(ctx, read.Text, targetContext)
				if err != nil || ctx.Err() != nil {
					value.reason = "文書を描画できないため点検していません"
					if ctx.Err() != nil {
						value.reason = "期限を超えたため点検していません"
					}
				} else {
					value.ids = map[string]bool{}
					for _, h := range result.Headings {
						value.ids[h.ID] = true
						value.ids["user-content-"+h.ID] = true
					}
				}
			}
		}
		cache[key] = value
		if value.reason != "" {
			add(item, value.reason, true)
		} else if !value.ids[id] {
			add(item, "行き先の見出しの ID がありません", false)
		}
	}
	return out, nil
}
