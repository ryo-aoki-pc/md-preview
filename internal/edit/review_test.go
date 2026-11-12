package edit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/ryo-aoki-pc/md-tools/internal/pipeline"
	"github.com/ryo-aoki-pc/md-tools/internal/session"
)

func TestReviewBlocks(t *testing.T) {
	e := testEngine(t)
	for _, c := range []struct {
		name, source, tag, public, kind string
		first, last                     int
	}{
		{"段落", "元の文\n続き", "p", "1:1-2:6", "段落", 1, 2},
		{"見出し", "# 見出し", "h1", "1:1-1:11", "見出し", 1, 1},
		{"項目", "- 項目", "li", "1:1-1:8", "リストの項目", 1, 1},
		{"リスト", "- a\n- b", "ul", "1:1-2:3", "リスト", 1, 2},
		{"説明リスト", "Apple\n: red", "dl", "1:1-2:5", "説明リスト", 1, 2},
		{"用語の補正", "Apple\n: red", "dt", "1:1-1:1", "用語", 1, 1},
		{"説明", "Apple\n: red", "dd", "2:1-2:5", "説明", 2, 2},
		{"引用", "> 引用", "blockquote", "1:1-1:8", "引用", 1, 1},
		{"コード", "```go\nx := 1\n```", "pre", "1:1-3:3", "コード", 1, 3},
		{"数式", "```math\nx^2\n```", "pre", "1:1-3:3", "数式", 1, 3},
		{"図", "```mermaid\ngraph TD; A-->B\n```", "pre", "1:1-3:3", "図", 1, 3},
		{"注記の補正", ">>> [!note]\n本文\n>>>", "div", "1:1-2:6", "注記", 1, 3},
	} {
		for _, ending := range []string{"\n", "\r\n"} {
			t.Run(c.name+ending, func(t *testing.T) {
				text := strings.ReplaceAll(c.source+"\n", "\n", ending)
				s, h := testService(t, e, text, nil)
				document, items, err := s.Review(context.Background(), nil, h.src.Render, []ReviewTarget{{Kind: "block", Tag: c.tag, Range: Range(c.public)}})
				if err != nil || len(items) != 1 {
					t.Fatalf("取得=%+v、誤り=%v\n%s", items, err, h.src.HTML)
				}
				wantSource := c.source
				if c.tag == "dt" {
					wantSource = "Apple"
				}
				if c.tag == "dd" {
					wantSource = ": red"
				}
				want := ReviewItem{Kind: c.kind, StartLine: c.first, EndLine: c.last, Excerpt: wantSource}
				if !reflect.DeepEqual(items[0], want) {
					t.Fatalf("項目=%+v、期待=%+v", items[0], want)
				}
				sum := sha256.Sum256([]byte(text))
				if document != (ReviewDocument{Root: h.src.Root, RelPath: "main.md", Render: h.src.Render, Hash: hex.EncodeToString(sum[:])}) || disk(t, h) != text {
					t.Fatal("文書の照合値またはファイルが違う", document)
				}
			})
		}
	}
}

func TestReviewLineMapping(t *testing.T) {
	e := testEngine(t)
	for _, ending := range []string{"\n", "\r\n"} {
		text := strings.ReplaceAll("---\ntitle: 題\n---\n\n::include{file=part.md}\n\n- a\n- b\n\n# h\n", "\n", ending)
		s, h := testService(t, e, text, map[string]string{"part.md": "一\n二\n三"})
		_, items, err := s.Review(context.Background(), nil, h.src.Render, []ReviewTarget{{Kind: "block", Tag: "li", Range: "10:1-11:0"}})
		if err != nil || len(items) != 1 || items[0].StartLine != 8 || items[0].EndLine != 8 || items[0].Excerpt != "- b" {
			t.Fatalf("項目=%+v、誤り=%v\n%s", items, err, h.src.HTML)
		}
	}
}

func TestReviewHTMLAndCells(t *testing.T) {
	e := testEngine(t)
	for _, ending := range []string{"\n", "\r\n"} {
		text := "---\ntitle: 題\n---\n\n前\n\n<div>a</div>\n\n<div>b</div>\n\n後\n\n| 列 |\n| --- |\n| 値 |\n\n<table>\n<tr><td>原文<b>太字</b></td></tr>\n</table>\n"
		s, h := testService(t, e, strings.ReplaceAll(text, "\n", ending), nil)
		prev, next := htmlTarget(t, s, h, 0)
		_, items, err := s.Review(context.Background(), nil, h.src.Render, []ReviewTarget{
			{Kind: "html", Prev: prev, Next: next}, {Kind: "cell", Table: 0, Row: 1, Col: 0}, {Kind: "cell", Table: 1, Row: 0, Col: 0},
		})
		want := []ReviewItem{
			{Kind: "HTML のブロック", StartLine: 7, EndLine: 9, Excerpt: "<div>a</div>\n\n<div>b</div>"},
			{Kind: "表のセル", StartLine: 13, EndLine: 15, Excerpt: "値", Table: 1, Row: 2, Col: 1},
			{Kind: "表のセル", StartLine: 17, EndLine: 19, Excerpt: "原文<b>太字</b>", Table: 2, Row: 1, Col: 1},
		}
		if err != nil || !reflect.DeepEqual(items, want) {
			t.Fatalf("項目=%+v、誤り=%v、期待=%+v\n%s", items, err, want, h.src.HTML)
		}
	}
}

func TestReviewCellNumberAfterExcludedTable(t *testing.T) {
	s, h := testService(t, testEngine(t), "```json:table\n{\"markdown\":true,\"items\":[{\"列\":\"除外\"}]}\n```\n\n| 列 |\n| --- |\n| 値 |\n", nil)
	_, items, e := s.Review(context.Background(), nil, h.src.Render, []ReviewTarget{{Kind: "cell", Table: 1, Row: 1, Col: 0}})
	want := []ReviewItem{{Kind: "表のセル", StartLine: 5, EndLine: 7, Excerpt: "値", Table: 2, Row: 2, Col: 1}}
	if e != nil || !reflect.DeepEqual(items, want) {
		t.Fatal(items, e)
	}
}

func TestReviewRejectedTargets(t *testing.T) {
	e := testEngine(t)
	for _, c := range []struct {
		name, text, code string
		target           ReviewTarget
		files            map[string]string
	}{
		{"include", "::include{file=part.md}\n", "include", ReviewTarget{Kind: "block", Tag: "p", Range: "1:1-1:9"}, map[string]string{"part.md": "取込文"}},
		{"front matter", "---\ntitle: 題\n---\n\n本文\n", "target", ReviewTarget{Kind: "block", Tag: "p", Range: "2:1-2:10"}, nil},
		{"一覧に無い範囲", "本文\n", "target", ReviewTarget{Kind: "block", Tag: "p", Range: "1:1-1:3"}, nil},
		{"利用者の位置", "本文\n\n<p data-sourcepos=\"1:1-1:3\">偽物</p>\n", "untrusted", ReviewTarget{Kind: "block", Tag: "p", Range: "1:1-1:3"}, nil},
		{"JSON の表", "```json:table\n{\"markdown\":true,\"items\":[{\"列\":\"値\"}]}\n```\n", "excluded", ReviewTarget{Kind: "cell"}, nil},
		{"入れ子の表", "<table><tr><td><table><tr><td>内</td></tr></table></td></tr></table>\n", "excluded", ReviewTarget{Kind: "cell", Table: 1}, nil},
		{"include の表", "::include{file=part.md}\n", "excluded", ReviewTarget{Kind: "cell"}, map[string]string{"part.md": "| 列 |\n| --- |\n| 値 |\n"}},
		{"覆われたセル", "<table><tr><td colspan=\"2\">結合</td></tr></table>\n", "merged", ReviewTarget{Kind: "cell", Col: 1}, nil},
		{"セル範囲外", "| 列 |\n| --- |\n| 値 |\n", "range", ReviewTarget{Kind: "cell", Row: 2}, nil},
		{"表範囲外", "本文\n", "excluded", ReviewTarget{Kind: "cell", Table: 3}, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, h := testService(t, e, c.text, c.files)
			if c.name == "JSON の表" {
				tables := getTables(t, s, h).Tables
				if len(tables) != 1 || tables[0].Editable || tables[0].Reason != "JSON から作った表" {
					t.Fatal("JSON の表を生成できていない", tables)
				}
			}
			_, _, err := s.Review(context.Background(), nil, h.src.Render, []ReviewTarget{c.target})
			if err == nil || err.Status != 422 || err.Code != c.code || disk(t, h) != c.text {
				t.Fatalf("誤り=%+v、期待=%s\n%s", err, c.code, h.src.HTML)
			}
		})
	}
}

func TestReviewDocumentGuards(t *testing.T) {
	e := testEngine(t)
	targets := []ReviewTarget{{Kind: "block", Tag: "p", Range: "1:1-1:6"}}
	for _, c := range []struct {
		name, code string
		status     int
		change     func(*Service, *testHost)
	}{
		{"古い描画", "stale", 409, func(_ *Service, h *testHost) { h.src.Render++ }},
		{"再描画中", "stale", 409, func(_ *Service, h *testHost) { h.src.Stale = true }},
		{"無名", "unnamed", 422, func(_ *Service, h *testHost) { h.src.Path = "" }},
		{"ファイルの変更", "changed", 409, func(_ *Service, h *testHost) {
			if err := os.WriteFile(h.src.Path, []byte("変更\n"), 0644); err != nil {
				t.Fatal(err)
			}
		}},
		{"試しの描画中の変更", "changed", 409, func(_ *Service, h *testHost) {
			h.unposition = func() (pipeline.Result, error) {
				if err := os.WriteFile(h.src.Path, []byte("変更\n"), 0644); err != nil {
					t.Fatal(err)
				}
				c := session.FileContext(h.src.Root, h.src.File, h.src.Dir, false)
				c.NoSourcepos = true
				return h.renderer.Render(context.Background(), h.src.Text, c)
			}
			h.src.HasSourcepos = true
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, h := testService(t, e, "本文\n", nil)
			c.change(s, h)
			_, _, err := s.Review(context.Background(), nil, 1, targets)
			if err == nil || err.Code != c.code || err.Status != c.status {
				t.Fatalf("誤り=%+v", err)
			}
		})
	}
	s, h := testService(t, e, "本文\n", nil)
	for _, c := range []struct {
		render  uint64
		targets []ReviewTarget
	}{{0, targets}, {h.src.Render, nil}} {
		if _, _, err := s.Review(context.Background(), nil, c.render, c.targets); err == nil || err.Status != 400 || err.Code != "invalid" {
			t.Fatal(err)
		}
	}
}

// include の行は展開後に 3 行へ増えるが、表のセルの行は原文のファイルの行で返す。
func TestReviewCellLinesAfterInclude(t *testing.T) {
	e := testEngine(t)
	for _, ending := range []string{"\n", "\r\n"} {
		text := strings.ReplaceAll("::include{file=part.md}\n\n| 列 |\n| --- |\n| 値 |\n", "\n", ending)
		s, h := testService(t, e, text, map[string]string{"part.md": "一\n\n二\n\n三"})
		_, items, err := s.Review(context.Background(), nil, h.src.Render, []ReviewTarget{{Kind: "cell", Table: 0, Row: 1, Col: 0}})
		want := []ReviewItem{{Kind: "表のセル", StartLine: 3, EndLine: 5, Excerpt: "値", Table: 1, Row: 2, Col: 1}}
		if err != nil || !reflect.DeepEqual(items, want) {
			t.Fatalf("項目=%+v、誤り=%v、期待=%+v\n%s", items, err, want, h.src.HTML)
		}
	}
}
