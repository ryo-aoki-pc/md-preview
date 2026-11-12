package edit

import (
	"context"
	"fmt"
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
				if got["range"] != Range(c.actual) || got["text"] != c.source || got["kind"] != blockKind(c.tag) {
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
	} {
		for _, ending := range []string{"\n", "\r\n"} {
			t.Run(c.public+fmt.Sprintf("/%q", ending), func(t *testing.T) {
				s, h := testService(t, e, strings.ReplaceAll(c.text, "\n", ending), nil)
				// 今のエンジンでは終了列 0 を再現しないため、指定の公開位置を木へ直接渡す。
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
				if _, err := s.Text(context.Background(), nil, h.src.Render, tag, "1:1-1:5"); err == nil || err.Status != 400 || err.Code != "invalid" {
					t.Fatal(tag, err)
				}
			}
			for _, tag := range []string{"h1", "p"} {
				if _, err := s.Text(context.Background(), nil, h.src.Render, tag, "1:1-1:1"); err == nil || err.Code != "target" {
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
	if _, err := s.Text(context.Background(), nil, h.src.Render, "p", "1:1-1:5"); err == nil || err.Code != "ambiguous" {
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
	for _, tag := range []string{"p", "h1", "h2", "h3", "h4", "h5", "h6", "li", "dt", "dd", "blockquote", "pre"} {
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
