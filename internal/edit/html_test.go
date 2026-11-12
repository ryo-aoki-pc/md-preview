package edit

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func htmlTarget(t *testing.T, s *Service, h *testHost, index int) (*HTMLBoundary, *HTMLBoundary) {
	t.Helper()
	m, e := s.blocks(context.Background(), h.src)
	if e != nil {
		t.Fatal(e)
	}
	runs, e := m.htmlRuns(context.Background(), h.src)
	if e != nil || index >= len(runs) {
		t.Fatalf("連なり=%d、誤り=%v\n%s", len(runs), e, h.src.HTML)
	}
	return runs[index].prev, runs[index].next
}
func htmlSource(t *testing.T, s *Service, h *testHost, index int) map[string]any {
	t.Helper()
	prev, next := htmlTarget(t, s, h, index)
	value, e := s.HTMLText(context.Background(), nil, h.src.Render, prev, next)
	if e != nil {
		t.Fatalf("取得=%v\n%s", e, h.src.HTML)
	}
	return value.(map[string]any)
}

func TestHTMLRangesAndWrites(t *testing.T) {
	engine := testEngine(t)
	details := "<details>\n<summary>題</summary>\n\n中\n\n- 項目\n\n~~~js\ncode\n~~~\n\n</details>"
	for _, c := range []struct{ name, before, raw, after string }{
		{"詳細", "前\n\n", details, "\n\n後\n"},
		{"後ろの文字", "前\n\n", "<div align=\"center\">\n中央\n</div>\n後ろの行", "\n\n後\n"},
		{"二つの div", "前\n\n", "<div>a</div>\n\n<div>b</div>", "\n\n後\n"},
		{"段落", "前\n\n", "<p>段落</p>", "\n\n後\n"},
		{"改行", "前\n\n", "<br>", "\n\n後\n"},
		{"画像", "前\n\n", "<img src=\"a.png\" alt=\"絵\">", "\n\n後\n"},
		{"先頭", "", "<div>先頭</div>", "\n\n後\n"},
		{"末尾", "前\n\n", "<div>末尾</div>", "\n"},
		{"全体", "", "<div>全体</div>", ""},
		{"空行なしの段落", "前\n", "<div>\nx\n</div>", ""},
		{"空行なしの見出し", "# 前\n", "<div>\nx\n</div>", ""},
		{"コードの後", "~~~js\ncode\n~~~\n\n", "<div>x</div>", "\n\n~~~\n後\n~~~\n"},
		{"表の後", "| 列 |\n| --- |\n| 値 |\n\n", "<div>x</div>", "\n\n後\n"},
		{"リストの後", "- 項目\n\n", "<div>x</div>", "\n\n後\n"},
		{"水平線の後", "- 項目\n\n  ---\n\n", "<div>x</div>", "\n\n後\n"},
		{"注記の後", ">>> [!note]\n注記\n>>>\n\n", "<div>x</div>", "\n\n後\n"},
		{"脚注の節を飛ばす", "本文[^1]\n\n[^1]: 注\n\n前\n\n", "<div>x</div>", "\n"},
		{"別の連なりを残す", "<div>残す</div>\n\n前\n\n", "<div>x</div>", "\n\n後\n"},
		{"コメントを含む", "前\n\n", "<!-- コメント -->\n\n<div>x</div>", "\n\n後\n"},
		// 両端の、リンクの参照や脚注の定義だけの段は、連なりを作らないので候補から外す。
		{"脚注の定義の後", "本文[^1]\n\n[^1]: 注\n\n", "<details>題</details>", "\n"},
		{"リンクの参照の定義の前", "前 [r]\n\n", "<div>x</div>", "\n\n[r]: http://example.com\n"},
		{"定義に挟まれる", "[a]: http://example.com/a\n[b]: http://example.com/b\n\n", "<div>x</div>", "\n\n[^1]: 注\n\n本文[^1] [a] [b]\n"},
		// 定義の形の行が連なりの中の文字として描かれているときは、候補から外さない。
		{"閉じていない pre の中の定義の形の行", "前\n\n", "<pre>\ntext\n\n[x]: y", "\n"},
	} {
		for _, mode := range []Mode{File, Buffer} {
			for _, ending := range []string{"\n", "\r\n"} {
				t.Run(fmt.Sprintf("%s/%v/%q", c.name, mode, ending), func(t *testing.T) {
					text := strings.ReplaceAll(c.before+c.raw+c.after, "\n", ending)
					s, h, updated := footnoteService(t, engine, text, mode)
					originalHTML := h.src.HTML
					index := 0
					if c.name == "別の連なりを残す" {
						index = 1
					}
					got := htmlSource(t, s, h, index)
					if got["kind"] != "html" || got["text"] != c.raw || !reflect.DeepEqual(got["outer"], []OuterBlock{}) {
						t.Fatalf("応答=%+v", got)
					}
					b, e := s.htmlWritable(context.Background(), h.src, s.blockCache[pathKey(h.src.Path)], got["range"].(Range))
					if e != nil {
						t.Fatal(e)
					}
					if got["anchor"] != nil && *got["anchor"].(*InsertAnchor) != (InsertAnchor{"html", b.actual, b.actual}) {
						t.Fatalf("追加先=%+v", got["anchor"])
					}
					if h.src.HTML != originalHTML {
						t.Fatal("公開の HTML が変わった")
					}
					if h.trials != 1 {
						t.Fatalf("取得と確かめ済みの再利用=%d", h.trials)
					}
					replacement := "<div>変更</div>"
					if _, e := s.Apply(context.Background(), nil, map[string]any{"render": h.src.Render, "range": string(b.actual), "text": replacement}); e != nil {
						t.Fatal(e)
					}
					want := strings.Replace(text, strings.ReplaceAll(c.raw, "\n", ending), replacement, 1)
					// バッファの本文には末尾の改行が付く。
					if mode == Buffer && !strings.HasSuffix(want, "\n") {
						want += "\n"
					}
					if got := updated(); got != want {
						t.Fatalf("前後のバイト=%q、期待=%q", got, want)
					}
				})
			}
		}
	}
}

func TestHTMLRejectsBoundaries(t *testing.T) {
	engine := testEngine(t)
	for _, c := range []struct {
		name, text string
		files      map[string]string
	}{
		{"目次", "[[_TOC_]]\n\n<div>x</div>\n\n# 後\n", nil},
		{"取り込み", "::include{file=part.md}\n\n後\n", map[string]string{"part.md": "<div>x</div>\n"}},
		{"front matter", "---\ntitle: 題\n---\n\n<div>x</div>\n", nil},
		// 続きの行のある定義は、定義だけの段と見分けないので候補に残り、試しの描画で脚注が消えて断る。
		{"続きの行のある脚注の定義", "本文[^1]\n\n[^1]: 注\n    続き\n\n<details>題</details>\n", nil},
		{"手書きの前の位置", "<p data-sourcepos=\"1:1-1:3\">偽</p>\n\n<div>x</div>\n", nil},
		{"手書きの後の位置", "<div>x</div>\n\n<p data-sourcepos=\"5:1-5:3\">偽</p>\n", nil},
		{"内側の手書きの位置", "<details><summary>題</summary><p data-sourcepos=\"1:1-1:3\">偽</p></details>\n", nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, h := testService(t, engine, c.text, c.files)
			m, e := s.blocks(context.Background(), h.src)
			if e != nil {
				t.Fatal(e)
			}
			runs, e := m.htmlRuns(context.Background(), h.src)
			if e != nil {
				t.Fatal(e)
			}
			if c.name != "目次" && len(runs) == 0 {
				t.Fatalf("拒否を確かめる連なりがない: %s", h.src.HTML)
			}
			for _, run := range runs {
				if _, e := s.HTMLText(context.Background(), nil, h.src.Render, run.prev, run.next); e == nil || e.Code != "boundary" {
					t.Fatalf("拒否=%v\n%s", e, h.src.HTML)
				}
			}
			if got := disk(t, h); got != c.text {
				t.Fatal("拒否後にファイルが変わった")
			}
		})
	}
}

func TestHTMLInnerAndTargets(t *testing.T) {
	engine := testEngine(t)
	text := "前\n\n<details>\n<summary>題</summary>\n\n中\n\n</details>\n\n後\n"
	s, h := testService(t, engine, text, nil)
	value, e := s.Text(context.Background(), nil, h.src.Render, "p", "6:1-6:3")
	if e != nil {
		t.Fatal(e)
	}
	got := value.(map[string]any)
	if !reflect.DeepEqual(got["outer"], []OuterBlock{{"html", "3:1-8:10", "html"}}) {
		t.Fatalf("外側=%+v", got["outer"])
	}
	if *got["anchor"].(*InsertAnchor) != (InsertAnchor{"p", "6:1-6:3", "6:1-6:3"}) {
		t.Fatalf("内側の追加先=%+v", got["anchor"])
	}
	prev, next := htmlTarget(t, s, h, 0)
	for _, pair := range [][2]*HTMLBoundary{{nil, next}, {prev, nil}, {next, prev}, {prev, prev}} {
		if _, e := s.HTMLText(context.Background(), nil, h.src.Render, pair[0], pair[1]); e == nil || e.Code != "target" {
			t.Fatalf("境の食い違い=%v", e)
		}
	}
	nested := "- 項目\n\n  <details>\n  <summary>題</summary>\n\n  中\n\n  </details>\n\n後\n"
	s, h = testService(t, engine, nested, nil)
	m, _ := s.blocks(context.Background(), h.src)
	runs, _ := m.htmlRuns(context.Background(), h.src)
	if len(runs) != 0 {
		t.Fatal("リストの内側を最上位の連なりにした")
	}
}

func TestHTMLInsertHistoryAndBOM(t *testing.T) {
	engine := testEngine(t)
	raw := "<div>x</div>"
	for _, side := range []string{"before", "after"} {
		for _, addition := range []string{"追加", "<div>追加</div>"} {
			for _, mode := range []Mode{File, Buffer} {
				t.Run(fmt.Sprintf("%s/%s/%v", side, addition, mode), func(t *testing.T) {
					text := "前\n\n" + raw + "\n\n後\n"
					s, h, updated := footnoteService(t, engine, text, mode)
					value := htmlSource(t, s, h, 0)
					if _, e := s.Apply(context.Background(), nil, map[string]any{"render": h.src.Render, "insert": map[string]any{"anchor": string(value["range"].(Range)), "side": side}, "text": addition}); e != nil {
						t.Fatal(e)
					}
					replacement := addition + "\n\n" + raw
					if side == "after" {
						replacement = raw + "\n\n" + addition
					}
					if got, want := updated(), strings.Replace(text, raw, replacement, 1); got != want {
						t.Fatalf("追加=%q、期待=%q", got, want)
					}
					if mode == File {
						if _, e := applyJSON(t, s, h, `{"undo":true}`); e != nil {
							t.Fatal(e)
						}
						if disk(t, h) != text {
							t.Fatal("取り消しで戻らない")
						}
						if _, e := applyJSON(t, s, h, `{"redo":true}`); e != nil {
							t.Fatal(e)
						}
						if disk(t, h) != strings.Replace(text, raw, replacement, 1) {
							t.Fatal("やり直しで戻らない")
						}
					}
				})
			}
		}
	}
	for _, ending := range []string{"\n", "\r\n"} {
		text := "\ufeff前" + ending + ending + raw + ending
		s, h := testService(t, engine, text, nil)
		// 取得を先にしなくても、確定時にその描画で検証する。
		if _, e := s.Apply(context.Background(), nil, map[string]any{"render": h.src.Render, "range": "3:1-3:12", "text": "<div>変更</div>"}); e != nil {
			t.Fatal(e)
		}
		if got, want := disk(t, h), strings.Replace(text, raw, "<div>変更</div>", 1); got != want {
			t.Fatalf("BOM と改行=%q、期待=%q", got, want)
		}
	}
}

// 位置を持たない節点は、要素名だけでなく形（入れ子・文字）でも比べる。
func TestHTMLInsertKeepsRawNodes(t *testing.T) {
	engine := testEngine(t)
	// 文書の最後の連なりの前に閉じない要素を足すと、既存の節点がその中へ取り込まれる。後ろに位置を持つ要素が無くても断る。
	for _, c := range []struct{ text, side, added, code string }{
		{"前\n\n<div>a</div>\n", "before", "<div>", "insert"},
		{"<div>a</div>\n\n<div>b</div>\n", "before", "<section>", "insert"},
		{"前\n\n<div>a</div>\n", "after", "<div>", ""},
		{"前\n\n<div>a</div>\n\n後\n", "after", "<div>b</div>", ""},
		{"前\n\n<div>a</div>\n\n後\n", "before", "<div>b</div>", ""},
	} {
		t.Run(fmt.Sprint(c), func(t *testing.T) {
			s, h := testService(t, engine, c.text, nil)
			value := htmlSource(t, s, h, 0)
			_, e := s.Apply(context.Background(), nil, map[string]any{"render": h.src.Render, "insert": map[string]any{"anchor": string(value["range"].(Range)), "side": c.side}, "text": c.added})
			if c.code == "" && e != nil || c.code != "" && (e == nil || e.Code != c.code) {
				t.Fatalf("追加=%v\n%s", e, h.src.HTML)
			}
			if c.code != "" && disk(t, h) != c.text {
				t.Fatal("断った追加を書いた")
			}
		})
	}
	// 生の HTML の中の段落は、それ自身が追加先。中へ足すと包みの中身が増えるが、最上位の並びは変わらないので受ける。
	text := "前\n\n<details>\n<summary>題</summary>\n\n中\n\n</details>\n\n<div>後ろの連なり</div>\n"
	for _, added := range []string{"追加", "~~~js\ncode\n~~~", "<div>中の HTML</div>"} {
		s, h := testService(t, engine, text, nil)
		if _, e := s.Apply(context.Background(), nil, map[string]any{"render": h.src.Render, "insert": map[string]any{"anchor": "6:1-6:3", "side": "after"}, "text": added}); e != nil {
			t.Fatalf("中への追加 %q=%v", added, e)
		}
		if want := strings.Replace(text, "中\n", "中\n\n"+added+"\n", 1); disk(t, h) != want {
			t.Fatalf("中への追加=%q、期待=%q", disk(t, h), want)
		}
	}
}

func TestHTMLWhitespaceAndMissingTargets(t *testing.T) {
	engine := testEngine(t)
	for _, ending := range []string{"\n", "\r\n"} {
		text := "前" + ending + ending + "  <div>日本語</div>  \t" + ending + " \t" + ending + "後" + ending
		s, h := testService(t, engine, text, nil)
		got := htmlSource(t, s, h, 0)
		if got["text"] != "  <div>日本語</div>" || got["range"] != Range("3:1-3:22") {
			t.Fatalf("空白とバイトの範囲=%+v", got)
		}
		if _, e := s.Apply(context.Background(), nil, map[string]any{"render": h.src.Render, "range": string(got["range"].(Range)), "text": "<div>変更</div>"}); e != nil {
			t.Fatal(e)
		}
		if want := strings.Replace(text, "  <div>日本語</div>", "<div>変更</div>", 1); disk(t, h) != want {
			t.Fatalf("末尾の空白が変わった: %q", disk(t, h))
		}
	}
	for _, text := range []string{"前\n\n後\n", "<!-- コメント -->\n", "- 項目\n\n  <details>題</details>\n"} {
		s, h := testService(t, engine, text, nil)
		if _, e := s.HTMLText(context.Background(), nil, h.src.Render, nil, nil); e == nil || e.Code != "target" {
			t.Fatalf("最上位の連なりではない=%v", e)
		}
	}
}
