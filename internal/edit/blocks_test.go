package edit

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/ryo-aoki-pc/md-tools/internal/session"
)

func TestBlockRangesAndWrites(t *testing.T) {
	e := testEngine(t)
	for _, c := range []struct {
		name, text, tag, public, actual, source, replacement, want string
	}{
		{"英字の用語", "Apple\n: red\n", "dt", "1:1-1:1", "1:1-1:5", "Apple", "Pear", "Pear\n: red\n"},
		{"日本語の用語", "りんご\n: 赤\n", "dt", "1:1-1:1", "1:1-1:9", "りんご", "みかん", "みかん\n: 赤\n"},
		{"二行の用語", "Apple\nPear\n: red\n", "dt", "1:1-1:1", "1:1-2:4", "Apple\nPear", "Orange\nGrape", "Orange\nGrape\n: red\n"},
		{"引用の用語", "> T3\n> : d3\n", "dt", "1:3-1:3", "1:3-1:4", "T3", "T4", "> T4\n> : d3\n"},
		// 引用やリストの中の 2 行目は、段落と同じく、行頭の印やインデントを含む原文のまま直す。
		{"引用の二行の用語", "> Apple\n> Pear\n> : red\n", "dt", "1:3-1:3", "1:3-2:6", "Apple\n> Pear", "Orange\n> Grape", "> Orange\n> Grape\n> : red\n"},
		{"リストの二行の用語", "- Apple\n  Pear\n  : red\n", "dt", "1:3-1:3", "1:3-2:6", "Apple\n  Pear", "Orange\n  Grape", "- Orange\n  Grape\n  : red\n"},
		{"リストの用語", "- Apple\n  : red\n", "dt", "1:3-1:3", "1:3-1:7", "Apple", "Pear", "- Pear\n  : red\n"},
		{"チルダの定義", "Apple\n~ red\n", "dt", "1:1-1:1", "1:1-1:5", "Apple", "Pear", "Pear\n~ red\n"},
		{"一文字と空行", "A\n\n: d\n", "dt", "1:1-1:1", "1:1-1:1", "A", "B", "B\n\n: d\n"},
		{"正しい用語", "Term2\n\n: def2\n", "dt", "1:1-1:5", "1:1-1:5", "Term2", "Term3", "Term3\n\n: def2\n"},
		{"引用の空行", "> A\n>\n> : d\n", "dt", "1:3-1:3", "1:3-1:3", "A", "B", "> B\n>\n> : d\n"},
		{"水平線で終わる項目", "- item\n\n  ---\n", "li", "1:1-3:5", "1:1-3:5", "- item\n\n  ---", "- changed", "- changed\n"},
		{"空行で終わる項目", "1. one\n2. two\n\n次", "li", "2:1-2:6", "2:1-2:6", "2. two", "2. changed", "1. one\n2. changed\n\n次"},
		// 終了列 0 は、リストの後ろに見出しや別のリストが続くときに出る。
		{"見出しの前の最後の項目", "- a\n- b\n\n# h\n", "li", "2:1-3:0", "2:1-2:3", "- b", "- c", "- a\n- c\n\n# h\n"},
		{"別のリストの前の最後の項目", "1. one\n2. two\n\n- task\n", "li", "2:1-3:0", "2:1-2:6", "2. two", "2. changed", "1. one\n2. changed\n\n- task\n"},
		{"水平線で終わり見出しが続く項目", "- a\n\n  ---\n\n# h\n", "li", "1:1-4:0", "1:1-3:5", "- a\n\n  ---", "- x", "- x\n\n# h\n"},
		{"説明リスト全体の終了列0", "Apple\n: red\n\n次\n", "dl", "1:1-3:0", "1:1-2:5", "Apple\n: red", "Pear\n: green", "Pear\n: green\n\n次\n"},
		{"リスト全体", "- a\n- b\n\n# h\n", "ul", "1:1-2:3", "1:1-2:3", "- a\n- b", "- c\n- d", "- c\n- d\n\n# h\n"},
		{"番号のリスト全体", "1. a\n2. b\n\n# h\n", "ol", "1:1-2:4", "1:1-2:4", "1. a\n2. b", "1. c\n2. d", "1. c\n2. d\n\n# h\n"},
		{"引用全体", "前\n\n>>>\n- ab cd\n- ef gh\n>>>\n\n後\n", "blockquote", "3:1-6:3", "3:1-6:3", ">>>\n- ab cd\n- ef gh\n>>>", "> 新しい引用", "前\n\n> 新しい引用\n\n後\n"},
		{"注記全体", ">>> [!note]\nnote body\n>>>\n", "div", "1:1-2:9", "1:1-3:3", ">>> [!note]\nnote body\n>>>", ">>> [!warning]\nchanged\n>>>", ">>> [!warning]\nchanged\n>>>\n"},
		{"題名とリストの注記", ">>> [!note] 題\n- a\n- b\n>>>\n\n次\n", "div", "1:1-3:3", "1:1-4:3", ">>> [!note] 題\n- a\n- b\n>>>", ">>> [!tip] 新しい題\nchanged\n>>>", ">>> [!tip] 新しい題\nchanged\n>>>\n\n次\n"},
		{"引用の形の注記", "> [!warning] 題名\n> 本文\n", "div", "1:1-2:8", "1:1-2:8", "> [!warning] 題名\n> 本文", "> [!note] 題\n> 変更", "> [!note] 題\n> 変更\n"},
		{"閉じない注記", ">>> [!note]\nbody\n", "div", "1:1-2:4", "1:1-2:4", ">>> [!note]\nbody", ">>> [!tip]\nchanged", ">>> [!tip]\nchanged\n"},
		{"閉じる前の空行と終了列0", ">>> [!note]\nbody\n\n>>>\n\n次\n", "div", "1:1-3:0", "1:1-4:3", ">>> [!note]\nbody\n\n>>>", ">>> [!note]\nchanged\n>>>", ">>> [!note]\nchanged\n>>>\n\n次\n"},
		{"注記で終わる項目", "- item\n\n  >>> [!note]\n  body\n  >>>\n\n次\n", "li", "1:1-4:6", "1:1-5:5", "- item\n\n  >>> [!note]\n  body\n  >>>", "- changed", "- changed\n\n次\n"},
		{"注記で終わるリスト", "- item\n\n  >>> [!note]\n  body\n  >>>\n\n次\n", "ul", "1:1-4:6", "1:1-5:5", "- item\n\n  >>> [!note]\n  body\n  >>>", "- changed", "- changed\n\n次\n"},
		{"リストの中の注記", "- item\n\n  >>> [!note]\n  body\n  >>>\n\n次\n", "div", "3:3-4:6", "3:3-5:5", ">>> [!note]\n  body\n  >>>", ">>> [!tip]\n  changed\n  >>>", "- item\n\n  >>> [!tip]\n  changed\n  >>>\n\n次\n"},
		{"引用の中の注記", "> >>> [!note]\n> body\n> >>>\n", "div", "1:3-2:6", "1:3-3:5", ">>> [!note]\n> body\n> >>>", ">>> [!tip]\n> changed\n> >>>", "> >>> [!tip]\n> changed\n> >>>\n"},
	} {
		for _, ending := range []string{"\n", "\r\n"} {
			t.Run(c.name+fmt.Sprintf("/%q", ending), func(t *testing.T) {
				text := strings.ReplaceAll(c.text, "\n", ending)
				s, h := testService(t, e, text, nil)
				originalHTML := h.src.HTML
				value, err := s.Text(context.Background(), nil, h.src.Render, c.tag, Range(c.public))
				if err != nil {
					t.Fatalf("取得: %v\n%s", err, h.src.HTML)
				}
				got := value.(map[string]any)
				kind := blockKind(c.tag)
				if c.tag == "div" {
					kind = "alert"
				}
				if got["range"] != Range(c.actual) || got["text"] != c.source || got["kind"] != kind {
					t.Fatalf("取得=%+v、期待=%s %q", got, c.actual, c.source)
				}
				if h.src.HTML != originalHTML || !strings.Contains(h.src.HTML, `data-sourcepos="`+c.public+`"`) {
					t.Fatal("公開の HTML と位置が変わった")
				}
				if _, err := s.Apply(context.Background(), nil, map[string]any{"render": h.src.Render, "range": c.actual, "text": c.replacement}); err != nil {
					t.Fatal(err)
				}
				if got, want := disk(t, h), strings.ReplaceAll(c.want, "\n", ending); got != want {
					t.Fatalf("原文=%q、期待=%q", got, want)
				}
			})
		}
	}
}

func TestBlockEndColumnZero(t *testing.T) {
	e := testEngine(t)
	for _, c := range []struct{ text, public, actual, source, want string }{
		{"- item\n\n  ---\n", "1:1-4:0", "1:1-3:5", "- item\n\n  ---", "changed\n"},
		{"1. one\n2. two\n\n次", "2:1-3:0", "2:1-2:6", "2. two", "1. one\nchanged\n\n次"},
		{"1. one\n2. two\n \t\n\n次", "2:1-5:0", "2:1-2:6", "2. two", "1. one\nchanged\n \t\n\n次"},
		// 終了列 0 が、最後の行の次の行を指す形。
		{"- a\n- b", "2:1-3:0", "2:1-2:3", "- b", "- a\nchanged"},
	} {
		for _, ending := range []string{"\n", "\r\n"} {
			t.Run(c.public+fmt.Sprintf("/%q", ending), func(t *testing.T) {
				s, h := testService(t, e, strings.ReplaceAll(c.text, "\n", ending), nil)
				// 本物の出力の例は TestBlockRangesAndWrites にある。ここでは、空白だけの行や
				// 文書の末尾との組み合わせを、位置を木へ直接渡して確かめる。
				h.src.HTML = `<ul><li data-sourcepos="` + c.public + `">item</li></ul>`
				value, err := s.Text(context.Background(), nil, h.src.Render, "li", Range(c.public))
				if err != nil {
					t.Fatal(err)
				}
				got := value.(map[string]any)
				if got["range"] != Range(c.actual) || got["text"] != c.source {
					t.Fatal(got)
				}
				if _, err := applyJSON(t, s, h, `{"range":"`+c.public+`","text":"changed"}`); err == nil || err.Code != "target" {
					t.Fatal("公開の終了列 0 を書き込みで許した", err)
				}
				if _, err := applyJSON(t, s, h, `{"range":"`+c.actual+`","text":"changed"}`); err != nil {
					t.Fatal(err)
				}
				if got, want := disk(t, h), strings.ReplaceAll(c.want, "\n", ending); got != want {
					t.Fatalf("原文=%q、期待=%q", got, want)
				}
			})
		}
	}
}

func TestBlockAfterInclude(t *testing.T) {
	e := testEngine(t)
	// 取り込みで行がずれた後ろでも、終了列 0 の補正と置き換えが原文の行に当たる。
	text := "::include{file=part.md}\n\n- a\n- b\n\n# h\n"
	s, h := testService(t, e, text, map[string]string{"part.md": "一\n二\n三"})
	value, err := s.Text(context.Background(), nil, h.src.Render, "li", "6:1-7:0")
	if err != nil {
		t.Fatalf("取得: %v\n%s", err, h.src.HTML)
	}
	if got := value.(map[string]any); got["range"] != Range("6:1-6:3") || got["text"] != "- b" {
		t.Fatal(got)
	}
	if _, err := applyJSON(t, s, h, `{"range":"6:1-6:3","text":"- c"}`); err != nil {
		t.Fatal(err)
	}
	if got, want := disk(t, h), "::include{file=part.md}\n\n- a\n- c\n\n# h\n"; got != want {
		t.Fatalf("原文=%q、期待=%q", got, want)
	}
}

func TestBlockBoundariesAndTargets(t *testing.T) {
	e := testEngine(t)
	s, h := testService(t, e, ">>>\n- ab cd\n- ef gh\n>>>\n", nil)
	if _, err := s.Text(context.Background(), nil, h.src.Render, "li", "3:1-3:3"); err == nil || err.Code != "boundary" {
		t.Fatalf("最後の項目=%v\n%s", err, h.src.HTML)
	}
	// 水平線で終わる項目の後ろに段落が続くと、項目の終端が水平線の開始位置で切れる。
	s, h = testService(t, e, "- item\n\n  ---\n\n次\n", nil)
	if _, err := s.Text(context.Background(), nil, h.src.Render, "li", "1:1-3:3"); err == nil || err.Code != "boundary" {
		t.Fatalf("水平線で終わる項目=%v\n%s", err, h.src.HTML)
	}
	if _, err := s.Text(context.Background(), nil, h.src.Render, "ul", "1:1-3:3"); err == nil || err.Code != "boundary" || err.Outer == nil || len(err.Outer) != 0 {
		t.Fatalf("水平線で終わるリスト=%v\n%s", err, h.src.HTML)
	}
	if _, err := applyJSON(t, s, h, `{"range":"1:1-3:3","text":"- x"}`); err == nil || err.Code != "target" || disk(t, h) != h.src.Text {
		t.Fatal("範囲を確かめられない項目を書き込みで許した", err)
	}
	for _, mode := range []Mode{File, Buffer} {
		t.Run(fmt.Sprint(mode), func(t *testing.T) {
			var sent *[]BufferEdit
			if mode == File {
				s, h = testService(t, e, "Apple\n\n[ ]\n\n- [ ] task\n", nil)
			} else {
				s, h, sent = bufferService(t, e, "Apple\n\n[ ]\n\n- [ ] task\n")
			}
			for _, body := range []string{
				`{"range":"1:1-1:1","text":"X"}`,
				`{"range":"1:1-2:0","text":"X"}`,
				`{"checkbox":"3:2-3:2","checked":true}`,
			} {
				if _, err := applyJSON(t, s, h, body); err == nil || err.Status != 422 || err.Code != "target" {
					t.Fatalf("%s: %v", body, err)
				}
			}
			for _, tag := range []string{"", "P", "p-", "p "} {
				if _, err := s.Text(context.Background(), nil, h.src.Render, tag, "1:1-1:5"); err == nil || err.Status != 400 || err.Code != "invalid" || err.Outer != nil {
					t.Fatal(tag, err)
				}
			}
			for _, tag := range []string{"h1", "p"} {
				if _, err := s.Text(context.Background(), nil, h.src.Render, tag, "1:1-1:1"); err == nil || err.Code != "target" || err.Outer != nil {
					t.Fatal(tag, err)
				}
			}
			if mode == File && disk(t, h) != h.src.Text || sent != nil && len(*sent) != 0 {
				t.Fatal("断った編集を反映した")
			}
		})
	}
}

func TestBlockAmbiguous(t *testing.T) {
	e := testEngine(t)
	s, h := testService(t, e, "<p data-sourcepos=\"1:1-1:5\">first</p>\n\n<p data-sourcepos=\"1:1-1:5\">second</p>\n", nil)
	if _, err := s.Text(context.Background(), nil, h.src.Render, "p", "1:1-1:5"); err == nil || err.Code != "ambiguous" || err.Outer != nil {
		t.Fatalf("重複=%v\n%s", err, h.src.HTML)
	}
}

func TestBlockTermRejections(t *testing.T) {
	e := testEngine(t)
	for _, c := range []struct{ text, html string }{
		{"Apple\n: red", `<dt data-sourcepos="1:1-1:1">Apple</dt>`},
		{"Apple\n: red", `<dt data-sourcepos="1:1-1:1">Apple</dt><p data-sourcepos="2:1-2:5">red</p>`},
		{"Apple\n: red", `<dt data-sourcepos="1:1-1:1">Apple</dt><dd data-sourcepos="bad">red</dd>`},
		{"Apple\n: red", `<dt data-sourcepos="1:1-1:1">Apple</dt><dd data-sourcepos="1:1-1:5">red</dd>`},
		{"Apple\nx red", `<dt data-sourcepos="1:1-1:1">Apple</dt><dd data-sourcepos="2:1-2:5">red</dd>`},
		{"Apple\n\nPear\n: red", `<dt data-sourcepos="1:1-1:1">Apple</dt><dd data-sourcepos="4:1-4:5">red</dd>`},
	} {
		s, h := testService(t, e, c.text, nil)
		h.src.HTML = "<dl>" + c.html + "</dl>"
		if _, err := s.Text(context.Background(), nil, h.src.Render, "dt", "1:1-1:1"); err == nil || err.Code != "term" {
			t.Fatal(c, err)
		}
	}
}

func TestBlockExclusionsAndKinds(t *testing.T) {
	e := testEngine(t)
	s, h := testService(t, e, "Apple", nil)
	for _, wrapper := range []string{
		`<table><tr><td>%s</td></tr></table>`,
		`<section data-footnotes>%s</section>`,
		`<div class="section-nav">%s</div>`,
		`<div class="js-render-math">%s</div>`,
		`<div data-diagram>%s</div>`,
	} {
		h.src.Render++
		h.src.HTML = fmt.Sprintf(wrapper, `<p data-sourcepos="1:1-1:5">Apple</p>`)
		if _, err := s.Text(context.Background(), nil, h.src.Render, "p", "1:1-1:5"); err == nil || err.Code != "target" {
			t.Fatal(wrapper, err)
		}
	}
	for _, tag := range []string{"p", "h1", "h2", "h3", "h4", "h5", "h6", "li", "dt", "dd", "blockquote", "pre", "ul", "ol", "dl"} {
		h.src.Render++
		h.src.HTML = fmt.Sprintf(`<%s data-sourcepos="1:1-1:5">Apple</%s>`, tag, tag)
		value, err := s.Text(context.Background(), nil, h.src.Render, tag, "1:1-1:5")
		if err != nil || value.(map[string]any)["kind"] != blockKind(tag) {
			t.Fatal(tag, value, err)
		}
	}
	h.src.Render++
	h.src.HTML = `<pre data-sourcepos="1:1-1:5"><code class="js-render-mermaid">Apple</code></pre>`
	if _, err := s.Text(context.Background(), nil, h.src.Render, "pre", "1:1-1:5"); err == nil || err.Code != "target" {
		t.Fatal(err)
	}
}

func TestBlockCache(t *testing.T) {
	e := testEngine(t)
	s, h := testService(t, e, "Apple", nil)
	for i := 0; i < historyDocuments+5; i++ {
		src := h.src
		src.Path = fmt.Sprint(i)
		if _, err := s.blocks(context.Background(), src); err != nil {
			t.Fatal(err)
		}
	}
	if len(s.blockCache) != historyDocuments {
		t.Fatal(len(s.blockCache))
	}
	if _, err := s.Text(context.Background(), nil, h.src.Render, "p", "1:1-1:5"); err != nil {
		t.Fatal(err)
	}
	if _, err := applyJSON(t, s, h, `{"range":"1:1-1:5","text":"Pear"}`); err != nil {
		t.Fatal(err)
	}
	if s.blockCache[pathKey(h.src.Path)] != nil {
		t.Fatal("書き込み後に対応が残った")
	}
	if _, err := s.Text(context.Background(), nil, h.src.Render, "p", "1:1-1:5"); err == nil || err.Code != "target" {
		t.Fatal("古い描画の範囲を許した", err)
	}
}

func TestOuterBlockChains(t *testing.T) {
	e := testEngine(t)
	for _, c := range []struct {
		name, text, tag, public, code string
		outer                         []OuterBlock
	}{
		// 項目が 1 つだけのリストは項目と同じ範囲なので、外側の列に入れない。
		{"入れ子のリスト", "- parent\n  - child\n", "li", "2:3-2:9", "", []OuterBlock{{"li", "1:1-2:9", "item"}}},
		{"項目が 2 つの入れ子のリスト", "- parent\n  - child\n  - second\n- next\n", "li", "2:3-2:9", "", []OuterBlock{{"ul", "2:3-3:10", "list"}, {"li", "1:1-3:10", "item"}, {"ul", "1:1-4:6", "list"}}},
		{"引用のリスト", "> - a\n> - b\n", "li", "1:3-1:5", "", []OuterBlock{{"ul", "1:3-2:5", "list"}, {"blockquote", "1:1-2:5", "quote"}}},
		{"注記の段落", ">>> [!note]\nbody\n>>>\n", "p", "2:1-2:4", "", []OuterBlock{{"div", "1:1-2:4", "alert"}}},
		{"断った項目", ">>>\n- ab cd\n- ef gh\n>>>\n", "li", "3:1-3:3", "boundary", []OuterBlock{{"ul", "2:1-3:7", "list"}, {"blockquote", "1:1-4:3", "quote"}}},
		{"外側のない段落", "body\n", "p", "1:1-1:4", "", []OuterBlock{}},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, h := testService(t, e, c.text, nil)
			value, err := s.Text(context.Background(), nil, h.src.Render, c.tag, Range(c.public))
			var outer []OuterBlock
			if c.code != "" {
				if err == nil || err.Code != c.code {
					t.Fatalf("誤り=%v\n%s", err, h.src.HTML)
				}
				outer = err.Outer
			} else {
				if err != nil {
					t.Fatalf("取得=%v\n%s", err, h.src.HTML)
				}
				outer = value.(map[string]any)["outer"].([]OuterBlock)
			}
			if !reflect.DeepEqual(outer, c.outer) {
				t.Fatalf("外側=%+v、期待=%+v\n%s", outer, c.outer, h.src.HTML)
			}
			for _, target := range outer {
				if _, err := s.Text(context.Background(), nil, h.src.Render, target.Tag, target.Range); err != nil {
					t.Fatal("外側の取得", target, err)
				}
			}
		})
	}
}

func TestAlertBoundaryAndPropagation(t *testing.T) {
	e := testEngine(t)
	for _, suffix := range []string{"wrong", " >>>", ">>> extra"} {
		s, h := testService(t, e, ">>> [!note]\nbody\n"+suffix+"\n", nil)
		// 補正の規則から外れる閉じの行は、エンジンの公開の終端を固定して確かめる。
		h.src.HTML = `<div class="markdown-alert" data-sourcepos="1:1-2:4"><p data-sourcepos="2:1-2:4">body</p></div>`
		if _, err := s.Text(context.Background(), nil, h.src.Render, "div", "1:1-2:4"); err == nil || err.Code != "boundary" || err.Outer == nil {
			t.Fatal(suffix, err)
		}
	}
	for _, c := range []struct{ name, html, public, actual string }{
		{"説明の祖先", `<dl data-sourcepos="1:1-4:6"><dd data-sourcepos="1:1-4:6"><div class="markdown-alert" data-sourcepos="3:3-4:6"><p data-sourcepos="4:3-4:6">body</p></div></dd></dl>`, "1:1-4:6", "1:1-5:5"},
		{"終端が異なる祖先", `<dl data-sourcepos="1:1-4:7"><dd data-sourcepos="1:1-4:6"><div class="markdown-alert" data-sourcepos="3:3-4:6"><p data-sourcepos="4:3-4:6">body</p></div></dd></dl>`, "1:1-4:7", ""},
		{"最後ではない注記", `<dl data-sourcepos="1:1-4:6"><dd data-sourcepos="1:1-4:6"><div class="markdown-alert" data-sourcepos="3:3-4:6"><p data-sourcepos="4:3-4:6">body</p></div><p>after</p></dd></dl>`, "1:1-4:6", "1:1-4:6"},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, h := testService(t, e, "- item\n\n  >>> [!note]\n  body\n  >>> \t\n\n次\n", nil)
			h.src.HTML = c.html
			value, err := s.Text(context.Background(), nil, h.src.Render, "dl", Range(c.public))
			if c.actual == "" {
				if err == nil {
					t.Fatal(value)
				}
				return
			}
			if err != nil || value.(map[string]any)["range"] != Range(c.actual) {
				t.Fatal(value, err)
			}
		})
	}
}

func TestAlertRangeAlreadyClosed(t *testing.T) {
	e := testEngine(t)
	// 公開の範囲が閉じの行まで含む形（上流の位置が直ったとき）は、延ばさずにそのまま使う。
	s, h := testService(t, e, ">>> [!note]\nbody\n>>>\n\n次\n", nil)
	h.src.HTML = `<div class="markdown-alert" data-sourcepos="1:1-3:3"><p data-sourcepos="2:1-2:4">body</p></div>`
	value, err := s.Text(context.Background(), nil, h.src.Render, "div", "1:1-3:3")
	if err != nil {
		t.Fatal(err)
	}
	if got := value.(map[string]any); got["range"] != Range("1:1-3:3") || got["text"] != ">>> [!note]\nbody\n>>>" {
		t.Fatal(got)
	}
}

func TestDeniedOuterBlocks(t *testing.T) {
	e := testEngine(t)
	s, h := testService(t, e, ">>>\n- ab cd\n- ef gh\n>>>\n", nil)
	h.src.HTML = `<blockquote data-sourcepos="1:1-4:3"><ul data-sourcepos="2:1-3:3"><li data-sourcepos="3:1-3:3">ef gh</li></ul></blockquote>`
	_, err := s.Text(context.Background(), nil, h.src.Render, "li", "3:1-3:3")
	if err == nil || !reflect.DeepEqual(err.Outer, []OuterBlock{{"blockquote", "1:1-4:3", "quote"}}) {
		t.Fatal("断ったリストは飛ばす", err)
	}
	s, h = testService(t, e, "Apple\nx red\n", nil)
	h.src.HTML = `<dl data-sourcepos="1:1-2:5"><dt data-sourcepos="1:1-1:1">Apple</dt><dd data-sourcepos="2:1-2:5">red</dd></dl>`
	_, err = s.Text(context.Background(), nil, h.src.Render, "dt", "1:1-1:1")
	if err == nil || err.Code != "term" || !reflect.DeepEqual(err.Outer, []OuterBlock{{"dl", "1:1-2:5", "deflist"}}) {
		t.Fatal("用語の外側の説明リスト", err)
	}
}

func TestBlockBufferTerm(t *testing.T) {
	e := testEngine(t)
	s, h, sent := bufferService(t, e, "りんご\n: 赤\n")
	value, err := s.Text(context.Background(), nil, 1, "dt", "1:1-1:1")
	if err != nil || value.(map[string]any)["range"] != Range("1:1-1:9") {
		t.Fatal(value, err)
	}
	if _, err := applyJSON(t, s, h, `{"range":"1:1-1:9","text":"みかん"}`); err != nil {
		t.Fatal(err)
	}
	if len(*sent) != 1 || (*sent)[0].Start != 0 || (*sent)[0].End != 1 || strings.Join((*sent)[0].Lines, "\n") != "みかん" {
		t.Fatal(*sent)
	}
}

func TestBlockEmptyBoundary(t *testing.T) {
	_, err := blockRange(session.Source{Text: " \n"}, []string{" ", ""}, []int{0, 2}, nil, "1:1-2:0")
	if err == nil || err.Code != "boundary" {
		t.Fatal(err)
	}
}
