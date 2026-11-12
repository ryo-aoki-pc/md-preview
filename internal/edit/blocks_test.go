package edit

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/ryo-aoki-pc/md-tools/engine"
	"github.com/ryo-aoki-pc/md-tools/internal/session"
)

func footnoteService(t *testing.T, e *engine.Engine, text string, mode Mode) (*Service, *testHost, func() string) {
	t.Helper()
	if mode == File {
		s, h := testService(t, e, text, nil)
		return s, h, func() string { return disk(t, h) }
	}
	s, h, sent := bufferService(t, e, text)
	return s, h, func() string {
		t.Helper()
		if len(*sent) != 1 {
			t.Fatalf("バッファへの置き換え=%+v", *sent)
		}
		r := (*sent)[0]
		if r.Path != h.src.DocPath || r.Buf != h.src.Buf || r.Rev != h.src.Rev {
			t.Fatalf("バッファの対応=%+v", r)
		}
		lines := bufferLines(text)
		lines = append(append(append([]string{}, lines[:r.Start]...), r.Lines...), lines[r.End:]...)
		return strings.Join(lines, "\n") + "\n"
	}
}

func TestFootnoteRangesAndWrites(t *testing.T) {
	e := testEngine(t)
	const multiple = "本文[^a] と[^b]\n\n[^a]: 一つ目\n    続き\n\n[^b]: 二つ目\n\n    段落\n\n次\n"
	for _, c := range []struct{ name, text, public, paragraph, source, replacement string }{
		{"一行", "本文[^1]\n\n[^1]: 脚注の定義\n", "3:1-3:21", "3:7-3:21", "[^1]: 脚注の定義", "[^1]: 変更した定義"},
		{"続きの行", multiple, "3:1-4:10", "3:7-4:10", "[^a]: 一つ目\n    続き", "[^a]: 一つ目の変更\n    続きの変更"},
		{"二つの段落", multiple, "6:1-8:10", "8:5-8:10", "[^b]: 二つ目\n\n    段落", "[^b]: 二つ目の変更\n\n    段落の変更"},
		{"参照の前の定義", "[^a]: 定義\n\n本文[^a]\n", "1:1-1:12", "1:7-1:12", "[^a]: 定義", "[^a]: 定義の変更"},
		{"空行なしで続く定義", "本文[^1] と[^2]\n\n[^1]: 一つ目\n[^2]: 二つ目\n", "4:1-4:15", "4:7-4:15", "[^2]: 二つ目", "[^2]: 二つ目の変更"},
		{"日本語のラベル", "本文[^注1] と[^note-1]\n\n[^注1]: 日本語のラベル\n\n[^note-1]: ハイフンのラベル\n", "3:1-3:30", "3:10-3:30", "[^注1]: 日本語のラベル", "[^注1]: 変更"},
		{"ハイフンのラベル", "本文[^注1] と[^note-1]\n\n[^注1]: 日本語のラベル\n\n[^note-1]: ハイフンのラベル\n", "5:1-5:35", "5:12-5:35", "[^note-1]: ハイフンのラベル", "[^note-1]: 変更"},
		{"字下げした定義", "本文[^1]\n\n  [^1]: 字下げした定義\n", "3:3-3:29", "3:9-3:29", "[^1]: 字下げした定義", "[^1]: 変更"},
	} {
		for _, mode := range []Mode{File, Buffer} {
			for _, ending := range []string{"\n", "\r\n"} {
				t.Run(fmt.Sprintf("%s/%v/%q", c.name, mode, ending), func(t *testing.T) {
					text := strings.ReplaceAll(c.text, "\n", ending)
					s, h, updated := footnoteService(t, e, text, mode)
					originalHTML := h.src.HTML
					value, err := s.Text(context.Background(), nil, h.src.Render, "li", Range(c.public))
					if err != nil {
						t.Fatalf("取得=%v\n%s", err, h.src.HTML)
					}
					got := value.(map[string]any)
					if got["kind"] != "footnote" || got["range"] != Range(c.public) || got["text"] != c.source || !reflect.DeepEqual(got["outer"], []OuterBlock{}) {
						t.Fatalf("定義の取得=%+v", got)
					}
					if _, err := s.Text(context.Background(), nil, h.src.Render, "p", Range(c.paragraph)); err == nil || err.Code != "target" {
						t.Fatalf("脚注の中の段落=%v", err)
					}
					if h.src.HTML != originalHTML || !strings.Contains(h.src.HTML, `data-sourcepos="`+c.public+`"`) {
						t.Fatal("公開の HTML と位置が変わった")
					}
					result, err := s.Apply(context.Background(), nil, map[string]any{"render": h.src.Render, "range": string(got["range"].(Range)), "text": c.replacement})
					if err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(result.(map[string]any)["warnings"], []string{}) {
						t.Fatalf("ラベルを変えない編集の注意=%+v", result)
					}
					want := strings.Replace(text, strings.ReplaceAll(c.source, "\n", ending), strings.ReplaceAll(c.replacement, "\n", ending), 1)
					if got := updated(); got != want {
						t.Fatalf("前後のバイトを含む原文=%q、期待=%q", got, want)
					}
					if h.trials != 1 {
						t.Fatalf("試しの描画=%d", h.trials)
					}
				})
			}
		}
	}
}

func TestFootnoteLabelWarnings(t *testing.T) {
	e := testEngine(t)
	for _, mode := range []Mode{File, Buffer} {
		for _, replacement := range []string{"[^a]: 変更", "[^b]: 変更", "定義ではない段落", "", "\n[^a]: 変更", "[^]: 変更", "[^a b]: 変更", "[^a\u3000b]: 変更", "[^a]x: 変更"} {
			t.Run(fmt.Sprintf("%v/%q", mode, replacement), func(t *testing.T) {
				const text = "本文[^a]\n\n[^a]: 定義\n\n後\n"
				s, h, updated := footnoteService(t, e, text, mode)
				result, err := s.Apply(context.Background(), nil, map[string]any{"render": h.src.Render, "range": "3:1-3:12", "text": replacement})
				if err != nil {
					t.Fatal(err)
				}
				want := []string{}
				if replacement != "[^a]: 変更" {
					want = append(want, "脚注のラベルが変わりました。本文の参照を確かめてください。")
				}
				if !reflect.DeepEqual(result.(map[string]any)["warnings"], want) {
					t.Fatalf("注意=%+v、期待=%q", result, want)
				}
				if got, want := updated(), "本文[^a]\n\n"+replacement+"\n\n後\n"; got != want {
					t.Fatalf("原文=%q、期待=%q", got, want)
				}
			})
		}
	}
}

func TestFootnoteBoundariesAndExclusions(t *testing.T) {
	e := testEngine(t)
	for _, mode := range []Mode{File, Buffer} {
		t.Run(fmt.Sprintf("%v/信用しない位置", mode), func(t *testing.T) {
			text := "plain\n\n<section data-footnotes><ol><li data-sourcepos=\"1:1-1:4\">偽の定義</li></ol></section>\n"
			s, h, _ := footnoteService(t, e, text, mode)
			if _, err := s.Text(context.Background(), nil, h.src.Render, "li", "1:1-1:4"); err == nil || err.Code != "untrusted" {
				t.Fatalf("信用しない位置の取得=%v\n%s", err, h.src.HTML)
			}
			if _, err := applyJSON(t, s, h, `{"range":"1:1-1:4","text":"[^a]: 変更"}`); err == nil || err.Code != "untrusted" {
				t.Fatalf("信用しない位置の確定=%v", err)
			}
		})
		for _, text := range []string{"定義ではない", "[^]: 空", "[^a b]: 空白", "[^a\tb]: タブ", "[^a\u3000b]: 空白", "[^a]x: コロンなし"} {
			t.Run(fmt.Sprintf("%v/%q", mode, text), func(t *testing.T) {
				s, h, _ := footnoteService(t, e, text+"\n", mode)
				r := Range(fmt.Sprintf("1:1-1:%d", len(text)))
				h.src.HTML = `<section data-footnotes><ol><li data-sourcepos="` + string(r) + `">定義</li></ol></section>`
				if _, err := s.Text(context.Background(), nil, h.src.Render, "li", r); err == nil || err.Code != "boundary" || !reflect.DeepEqual(err.Outer, []OuterBlock{}) {
					t.Fatalf("定義の形でない範囲=%v", err)
				}
				if _, err := s.Apply(context.Background(), nil, map[string]any{"render": h.src.Render, "range": string(r), "text": "[^a]: 変更"}); err == nil || err.Code != "target" {
					t.Fatalf("断った範囲の確定=%v", err)
				}
			})
		}
		t.Run(fmt.Sprintf("%v/終了列0と内側の対象", mode), func(t *testing.T) {
			s, h, updated := footnoteService(t, e, "本文[^a]\n\n[^a]: 定義\n\n次\n", mode)
			h.src.HTML = `<blockquote data-sourcepos="1:1-5:3"><section data-footnotes><ol data-sourcepos="3:1-4:0"><li data-sourcepos="3:1-4:0"><p data-sourcepos="3:7-3:12">定義</p><ul data-sourcepos="3:1-3:12"><li data-sourcepos="3:1-3:12">内側</li></ul><input class="task-list-item-checkbox" data-checkbox-sourcepos="3:4-3:4"></li></ol></section></blockquote>`
			value, err := s.Text(context.Background(), nil, h.src.Render, "li", "3:1-4:0")
			if err != nil || value.(map[string]any)["range"] != Range("3:1-3:12") || value.(map[string]any)["text"] != "[^a]: 定義" || !reflect.DeepEqual(value.(map[string]any)["outer"], []OuterBlock{}) {
				t.Fatal(value, err)
			}
			for _, c := range []struct {
				tag string
				r   Range
			}{{"p", "3:7-3:12"}, {"ul", "3:1-3:12"}, {"li", "3:1-3:12"}, {"ol", "3:1-4:0"}} {
				if _, err := s.Text(context.Background(), nil, h.src.Render, c.tag, c.r); err == nil || err.Code != "target" {
					t.Fatalf("内側の対象=%+v、結果=%v", c, err)
				}
			}
			if _, err := applyJSON(t, s, h, `{"checkbox":"3:4-3:4","checked":true}`); err == nil || err.Code != "target" {
				t.Fatalf("脚注のチェックボックス=%v", err)
			}
			if _, err := applyJSON(t, s, h, `{"range":"3:1-4:0","text":"[^a]: 変更"}`); err == nil || err.Code != "target" {
				t.Fatalf("公開の終了列0の確定=%v", err)
			}
			if _, err := applyJSON(t, s, h, `{"range":"3:1-3:12","text":"[^a]: 変更"}`); err != nil {
				t.Fatal(err)
			}
			if got := updated(); got != "本文[^a]\n\n[^a]: 変更\n\n次\n" {
				t.Fatalf("終了列0の確定後=%q", got)
			}
		})
	}
}

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
		// 項目や説明の 1 行目から始まる注記は、閉じの行の前が、印と同じ幅のインデントになる。
		{"項目の一行目の注記", "- >>> [!note]\n  body\n  >>>\n\n次\n", "div", "1:3-2:6", "1:3-3:5", ">>> [!note]\n  body\n  >>>", ">>> [!tip]\n  changed\n  >>>", "- >>> [!tip]\n  changed\n  >>>\n\n次\n"},
		{"一行目が注記の項目", "- >>> [!note]\n  body\n  >>>\n\n次\n", "li", "1:1-2:6", "1:1-3:5", "- >>> [!note]\n  body\n  >>>", "- changed", "- changed\n\n次\n"},
		{"番号の項目の一行目の注記", "1. >>> [!note]\n   body\n   >>>\n", "div", "1:4-2:7", "1:4-3:6", ">>> [!note]\n   body\n   >>>", ">>> [!tip]\n   changed\n   >>>", "1. >>> [!tip]\n   changed\n   >>>\n"},
		{"一行目が注記の説明", "Term\n: >>> [!note]\n  body\n  >>>\n\n次\n", "dd", "2:1-3:6", "2:1-4:5", ": >>> [!note]\n  body\n  >>>", ": changed", "Term\n: changed\n\n次\n"},
		{"段落の後ろで閉じる注記", ">>> [!note]\nbody\n\n次\n\n>>>\nquote\n>>>\n", "div", "1:1-5:0", "1:1-6:3", ">>> [!note]\nbody\n\n次\n\n>>>", ">>> [!note]\nchanged\n>>>", ">>> [!note]\nchanged\n>>>\nquote\n>>>\n"},
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
	// 閉じの行を決められない注記で終わる項目は、項目の終端も決められない（次の項目の行を取り込まない）。
	s, h := testService(t, e, "- >>> [!note]\n  body\n- >>>\n", nil)
	for _, c := range []struct{ tag, r string }{{"div", "1:3-2:6"}, {"li", "1:1-2:6"}} {
		if _, err := s.Text(context.Background(), nil, h.src.Render, c.tag, Range(c.r)); err == nil || err.Code != "boundary" {
			t.Fatalf("%v=%v\n%s", c, err, h.src.HTML)
		}
	}
	if value, err := s.Text(context.Background(), nil, h.src.Render, "li", "3:1-3:5"); err != nil || value.(map[string]any)["text"] != "- >>>" {
		t.Fatal("次の項目", value, err)
	}
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
