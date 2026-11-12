package edit

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestInsertBlocks(t *testing.T) {
	e := testEngine(t)
	for _, c := range []struct{ name, old, tag, public, side, added, want string }{
		{"段落の後ろ", "前\n\n後\n", "p", "1:1-1:3", "after", "\n追加\n\n", "前\n\n追加\n\n後\n"},
		{"段落の前", "前\n\n後\n", "p", "3:1-3:3", "before", "追加", "前\n\n追加\n\n後\n"},
		{"見出しの前で空行なし", "前\n# 題\n後\n", "h1", "2:1-2:5", "before", "追加", "前\n\n追加\n\n# 題\n後\n"},
		{"見出しの後ろで空行なし", "前\n# 題\n後\n", "h1", "2:1-2:5", "after", "追加", "前\n# 題\n\n追加\n\n後\n"},
		{"リストのすぐ次の見出し", "- a\n# 題\n", "h1", "2:1-2:5", "before", "追加", "- a\n\n追加\n\n# 題\n"},
		{"改行のない末尾でCRLF", "前\n\n本文", "p", "3:1-3:6", "after", "追加", "前\n\n本文\n\n追加"},
		{"リスト全体の前", "- a\n- b\n", "li", "2:1-2:3", "before", "追加", "追加\n\n- a\n- b\n"},
		{"リスト全体の後ろ", "- a\n- b\n", "li", "1:1-1:3", "after", "追加", "- a\n- b\n\n追加\n"},
		{"先頭", "本文\n", "p", "1:1-1:6", "before", "追加", "追加\n\n本文\n"},
		{"末尾", "本文\n", "p", "1:1-1:6", "after", "追加", "本文\n\n追加\n"},
		{"改行のない末尾", "本文", "p", "1:1-1:6", "after", "追加", "本文\n\n追加"},
		{"改行のない末尾の前", "本文", "p", "1:1-1:6", "before", "追加", "追加\n\n本文"},
		{"既存の空行を保存", "前\n \t\n\n後\n", "p", "4:1-4:3", "before", "追加", "前\n \t\n\n追加\n\n後\n"},
		{"引用", "> 本文\n", "blockquote", "1:1-1:8", "after", "追加", "> 本文\n\n追加\n"},
		{"コード", "```\na\n```\n", "pre", "1:1-3:3", "before", "追加", "追加\n\n```\na\n```\n"},
		{"水平線の祖先", "- item\n\n  ---\n\n後\n", "hr", "3:3-3:3", "after", "追加", "- item\n\n  ---\n\n追加\n\n後\n"},
		{"水平線の前", "前\n\n---\n\n後\n", "hr", "3:1-3:3", "before", "追加", "前\n\n追加\n\n---\n\n後\n"},
		{"水平線の後ろ", "前\n\n---\n\n後\n", "hr", "3:1-3:3", "after", "追加", "前\n\n---\n\n追加\n\n後\n"},
		// 複数行の追加は、増えた行数だけ後ろの位置をずらして比べる。
		{"複数行のコード", "前\n\n後\n", "p", "1:1-1:3", "after", "```\ncode\n```", "前\n\n```\ncode\n```\n\n後\n"},
		{"複数行の段落と項目", "前\n\n後\n", "p", "3:1-3:3", "before", "一行目\n二行目\n\n- a\n- b", "前\n\n一行目\n二行目\n\n- a\n- b\n\n後\n"},
		{"説明リストの前", "前\n\nTerm\n: def\n\n後\n", "dl", "3:1-5:0", "before", "追加", "前\n\n追加\n\nTerm\n: def\n\n後\n"},
		// 上流の公開の終端は、後ろに続く内容で変わる（終了列 0、入れ物の中の水平線）。補正後の範囲で比べるので断らない。
		{"次が見出しのリストの後ろ", "- a\n- b\n\n# 題\n", "li", "2:1-3:0", "after", "追加", "- a\n- b\n\n追加\n\n# 題\n"},
		{"リストの後ろへ見出し", "- a\n- b\n\n後\n", "p", "4:1-4:3", "before", "# 追加", "- a\n- b\n\n# 追加\n\n後\n"},
		{"見出しの前へリスト", "前\n\n# 題\n", "p", "1:1-1:3", "after", "- 追加", "前\n\n- 追加\n\n# 題\n"},
		{"説明リストの後ろ", "Term\n: def\n\n後\n", "dl", "1:1-3:0", "after", "追加", "Term\n: def\n\n追加\n\n後\n"},
		{"水平線で終わる末尾のリストの後ろ", "- item\n\n  ---\n", "ul", "1:1-3:5", "after", "追加", "- item\n\n  ---\n\n追加\n"},
		{"水平線で終わるリストの前", "前\n\n- item\n\n  ---\n\n後\n", "ul", "3:1-5:3", "before", "追加", "前\n\n追加\n\n- item\n\n  ---\n\n後\n"},
		{"注記の後ろ", "前\n\n>>> [!note]\n本文\n>>>\n\n後\n", "div", "3:1-4:6", "after", "追加", "前\n\n>>> [!note]\n本文\n>>>\n\n追加\n\n後\n"},
		{"信用しない位置を保存", "前\n\n<p data-sourcepos=\"1:1-1:1\">偽</p>\n", "p", "1:1-1:3", "before", "追加", "追加\n\n前\n\n<p data-sourcepos=\"1:1-1:1\">偽</p>\n"},
	} {
		for _, mode := range []Mode{File, Buffer} {
			for _, ending := range []string{"\n", "\r\n"} {
				t.Run(fmt.Sprintf("%s/%v/%q", c.name, mode, ending), func(t *testing.T) {
					old := strings.ReplaceAll(c.old, "\n", ending)
					s, h, updated := footnoteService(t, e, old, mode)
					value, err := s.Text(context.Background(), nil, h.src.Render, c.tag, Range(c.public))
					if err != nil {
						t.Fatalf("取得=%v\n%s", err, h.src.HTML)
					}
					anchor := value.(map[string]any)["anchor"].(*InsertAnchor)
					if anchor == nil {
						t.Fatal("追加先がない")
					}
					body := map[string]any{"render": h.src.Render, "insert": map[string]any{"anchor": string(anchor.Actual), "side": c.side}, "text": c.added}
					if _, err := s.Apply(context.Background(), nil, body); err != nil {
						t.Fatalf("追加=%v\n%s", err, h.src.HTML)
					}
					want := strings.ReplaceAll(c.want, "\n", ending)
					if !strings.Contains(old, "\n") {
						// 改行が一つもない文書には、CRLF を選ぶ根拠がないので LF を使う。
						want = c.want
					}
					// バッファの取り決めは、末尾に改行を一つ付ける。
					if mode == Buffer && !strings.HasSuffix(want, "\n") {
						want += "\n"
					}
					if got := updated(); got != want {
						t.Fatalf("原文=%q、期待=%q", got, want)
					}
					if mode == File {
						if _, err := applyJSON(t, s, h, `{"undo":true}`); err != nil || disk(t, h) != old {
							t.Fatal("追加の取り消し", err, disk(t, h))
						}
						if _, err := applyJSON(t, s, h, `{"redo":true}`); err != nil || disk(t, h) != want {
							t.Fatal("追加のやり直し", err, disk(t, h))
						}
					}
				})
			}
		}
	}
}

func TestInsertRejects(t *testing.T) {
	e := testEngine(t)
	for _, mode := range []Mode{File, Buffer} {
		for _, c := range []struct{ old, anchor, side, text, code string }{
			{"前\n\n後\n", "1:1-1:3", "after", "```", "insert"},
			{"前\n\n後\n", "1:1-1:3", "after", ">>>", "insert"},
			{"前\n\n後\n", "1:1-1:3", "after", "<details>", "insert"},
			{"前\n\n後\n", "3:1-3:3", "before", "```", "insert"},
			// リストに取り込まれる文と、項目を足す文は、リストの範囲が変わるので断る。
			{"- a\n\n後\n", "1:1-1:3", "after", "  続き", "insert"},
			{"- a\n\n後\n", "1:1-1:3", "after", "- b", "insert"},
			{"前\n\n<p data-sourcepos=\"1:1-1:1\">偽</p>\n\n後\n", "1:1-1:3", "after", "```", "insert"},
			{"前\n\n後\n", "1:1-1:3", "after", " \n\t", "invalid"},
			{"- a\n- b\n", "1:1-1:3", "after", "追加", "target"},
			// 脚注の定義は、画面の位置と原文の位置が対応しないので、追加先にしない。
			{"本文[^1]\n\n[^1]: 注\n\n後\n", "3:1-3:9", "after", "追加", "target"},
			{"前\n\n" + pipeTable, "3:1-5:17", "before", "追加", "target"},
		} {
			t.Run(fmt.Sprint(mode, c), func(t *testing.T) {
				s, h, _ := footnoteService(t, e, c.old, mode)
				_, err := s.Apply(context.Background(), nil, map[string]any{"render": h.src.Render,
					"insert": map[string]any{"anchor": c.anchor, "side": c.side}, "text": c.text})
				if err == nil || err.Status != 422 || err.Code != c.code {
					t.Fatalf("拒否=%v\n%s", err, h.src.HTML)
				}
				if mode == File && disk(t, h) != c.old {
					t.Fatal("拒否した追加を書いた")
				}
			})
		}
	}
}

func TestInsertFootnoteHasNoAnchor(t *testing.T) {
	e := testEngine(t)
	s, h := testService(t, e, "本文[^1]\n\n[^1]: 注\n\n後\n", nil)
	// 脚注の定義とその中の段落からは、追加先を返さない。本文の段落は今までどおり。
	value, err := s.Text(context.Background(), nil, h.src.Render, "li", "3:1-3:9")
	if err != nil || value.(map[string]any)["anchor"].(*InsertAnchor) != nil {
		t.Fatal("脚注の定義の追加先", value, err)
	}
	value, err = s.Text(context.Background(), nil, h.src.Render, "p", "1:1-1:10")
	if err != nil || value.(map[string]any)["anchor"].(*InsertAnchor) == nil {
		t.Fatal("本文の段落の追加先", value, err)
	}
}

func TestInsertDeniedOuterAndPrefix(t *testing.T) {
	e := testEngine(t)
	s, h := testService(t, e, "x body\n", nil)
	h.src.HTML = `<blockquote data-sourcepos="1:1-1:1"><p data-sourcepos="1:3-1:6">body</p></blockquote>`
	value, err := s.Text(context.Background(), nil, h.src.Render, "p", "1:3-1:6")
	if err != nil || value.(map[string]any)["anchor"].(*InsertAnchor) != nil {
		t.Fatal("断った外側の追加先", value, err)
	}
	h.src.Render++
	h.src.HTML = `<p data-sourcepos="1:3-1:6">body</p>`
	_, err = applyJSON(t, s, h, `{"insert":{"anchor":"1:3-1:6","side":"before"},"text":"追加"}`)
	if err == nil || err.Code != "boundary" || disk(t, h) != "x body\n" {
		t.Fatal("行頭の文字をまたぐ追加", err)
	}
}

func TestInsertRequestShapes(t *testing.T) {
	for _, body := range []map[string]any{
		{"render": 1, "insert": nil, "text": "a"},
		{"render": 1, "insert": map[string]any{"document": "middle"}, "text": "a"},
		{"render": 1, "insert": map[string]any{"document": ""}, "text": "a"},
		{"render": 1, "insert": map[string]any{"document": true}, "text": "a"},
		{"render": 1, "insert": map[string]any{"document": "start", "anchor": "1:1-1:3", "side": "before"}, "text": "a"},
		{"render": 1, "insert": map[string]any{"document": "start", "anchor": "1:1-1:3"}, "text": "a"},
		{"render": 1, "insert": map[string]any{"document": "end", "side": "after"}, "text": "a"},
		{"render": 1, "insert": map[string]any{"document": "end", "extra": true}, "text": "a"},
		{"render": 1, "insert": map[string]any{"anchor": "1:1-1:3"}, "text": "a"},
		{"render": 1, "insert": map[string]any{"anchor": "1:1-1:3", "side": "after", "extra": true}, "text": "a"},
		{"render": 1, "insert": map[string]any{"anchor": "1:1-1:3", "side": "above"}, "text": "a"},
		{"render": 1, "insert": map[string]any{"anchor": "bad", "side": "after"}, "text": "a"},
		{"render": 1, "insert": map[string]any{"anchor": "1:3-1:1", "side": "after"}, "text": "a"},
		{"render": 1, "insert": map[string]any{"anchor": 3, "side": "after"}, "text": "a"},
		{"render": 1, "insert": map[string]any{"anchor": "1:1-1:3", "side": "after"}, "text": true},
		{"render": 1, "insert": map[string]any{"anchor": "1:1-1:3", "side": "after"}},
		{"render": 1, "insert": map[string]any{"anchor": "1:1-1:3", "side": "after"}, "text": "a", "range": "1:1-1:3"},
	} {
		if _, err := parseRequest(body); err == nil || err.Status != 400 {
			t.Fatal(body, err)
		}
	}
}

func TestInsertDocument(t *testing.T) {
	e := testEngine(t)
	for _, c := range []struct{ name, old, side, want string }{
		{"先頭", "本文\n", "start", "追加\n\n本文\n"},
		{"末尾", "本文\n", "end", "本文\n\n追加\n"},
		{"末尾改行なし", "前\n\n本文", "end", "前\n\n本文\n\n追加"},
		{"先頭改行なし", "本文", "start", "追加\n\n本文"},
		{"末尾だけ改行なし", "本文", "end", "本文\n\n追加"},
		{"YAMLの後ろ", "---\ntitle: x\n---\n本文\n", "start", "---\ntitle: x\n---\n\n追加\n\n本文\n"},
		{"TOMLの後ろ", "+++\ntitle = 'x'\n+++\n\n本文\n", "start", "+++\ntitle = 'x'\n+++\n\n追加\n\n本文\n"},
		{"frontmatterだけの先頭", "---\ntitle: x\n---\n", "start", "---\ntitle: x\n---\n\n追加\n"},
		{"frontmatterだけの末尾", "+++\ntitle = 'x'\n+++\n", "end", "+++\ntitle = 'x'\n+++\n\n追加\n"},
		{"frontmatterだけで改行なし", "---\ntitle: x\n---", "start", "---\ntitle: x\n---\n\n追加"},
		{"脚注の定義の後ろ", "本文[^a]\n\n[^a]: 定義\n", "end", "本文[^a]\n\n[^a]: 定義\n\n追加\n"},
		{"参照の定義の後ろ", "[本文][a]\n\n[a]: ./part.md\n", "end", "[本文][a]\n\n[a]: ./part.md\n\n追加\n"},
		{"脚注だけの後ろ", "[^a]: 定義\n", "end", "[^a]: 定義\n\n追加\n"},
		{"参照だけの後ろ", "[a]: ./part.md\n", "end", "[a]: ./part.md\n\n追加\n"},
		{"位置のないHTMLの先頭", "<details><summary>題</summary>本文</details>\n", "start", "追加\n\n<details><summary>題</summary>本文</details>\n"},
		{"水平線で終わるリストの先頭", "- item\n\n  ---\n\n後\n", "start", "追加\n\n- item\n\n  ---\n\n後\n"},
		{"BOMの後ろ", "\ufeff本文\n", "start", "\ufeff追加\n\n本文\n"},
		{"BOMを保つ末尾", "\ufeff本文\n", "end", "\ufeff本文\n\n追加\n"},
	} {
		for _, mode := range []Mode{File, Buffer} {
			for _, ending := range []string{"\n", "\r\n"} {
				t.Run(fmt.Sprintf("%s/%v/%q", c.name, mode, ending), func(t *testing.T) {
					old := strings.ReplaceAll(c.old, "\n", ending)
					s, h, updated := footnoteService(t, e, old, mode)
					if _, err := s.Apply(context.Background(), nil, map[string]any{"render": h.src.Render,
						"insert": map[string]any{"document": c.side}, "text": "\n追加\n"}); err != nil {
						t.Fatalf("追加=%v\n%s", err, h.src.HTML)
					}
					want := strings.ReplaceAll(c.want, "\n", ending)
					if !strings.Contains(old, "\n") {
						want = c.want
					}
					if mode == Buffer && !strings.HasSuffix(want, "\n") {
						want += "\n"
					}
					if got := updated(); got != want {
						t.Fatalf("原文=%q、期待=%q", got, want)
					}
					if mode == File {
						if _, err := applyJSON(t, s, h, `{"undo":true}`); err != nil || disk(t, h) != old {
							t.Fatal("取り消し", err)
						}
						if _, err := applyJSON(t, s, h, `{"redo":true}`); err != nil || disk(t, h) != want {
							t.Fatal("やり直し", err)
						}
					}
				})
			}
		}
	}
}

func TestInsertDocumentStartLine(t *testing.T) {
	e := testEngine(t)
	for _, c := range []struct {
		old string
		cut int
	}{
		{"本文\n", 1}, {"---\ntitle: x\n---\n本文\n", 4}, {"+++\ntitle = 'x'\n+++\n", 4},
	} {
		s, h := testService(t, e, c.old, nil)
		if got := getTables(t, s, h).InsertStart; got != c.cut {
			t.Fatalf("下書きの移動の先頭行=%d、期待=%d", got, c.cut)
		}
	}
}

func TestInsertEmptyDocument(t *testing.T) {
	e := testEngine(t)
	for _, old := range []string{"", " \t", "\n\n\n", " \t\r\n\r\n", "\ufeff\n\n"} {
		for _, mode := range []Mode{File, Buffer} {
			for _, side := range []string{"start", "end"} {
				t.Run(fmt.Sprintf("%q/%v/%s", old, mode, side), func(t *testing.T) {
					s, h, updated := footnoteService(t, e, old, mode)
					if _, err := s.Apply(context.Background(), nil, map[string]any{"render": h.src.Render,
						"insert": map[string]any{"document": side}, "text": " \n追加\r\n二行目\r\n\t"}); err != nil {
						t.Fatal(err)
					}
					want := "追加\n二行目\n"
					if strings.HasPrefix(old, "\ufeff") {
						want = "\ufeff" + want
					}
					if got := updated(); got != want {
						t.Fatalf("原文=%q、期待=%q", got, want)
					}
				})
			}
		}
	}
}

func TestInsertDocumentRejects(t *testing.T) {
	e := testEngine(t)
	for _, mode := range []Mode{File, Buffer} {
		for _, c := range []struct{ old, side, added, code string }{
			{"前\n\n<!-- 閉じない\n", "end", "追加", "insert"},
			{"前\n\n<details>\n本文\n", "end", "追加", "insert"},
			{"前\n\n```\ncode\n", "end", "追加", "insert"},
			{"- a\n\n  続き\n", "end", "    取り込まれる", "insert"},
			{"本文\n", "start", "<details>", "insert"},
			{"本文\n", "start", "<!--", "insert"},
			{"本文\n", "start", "```", "insert"},
			{"<div>元のHTML</div>\n", "start", "<details>", "insert"},
			{"本文\n", "start", " \n\t", "invalid"},
			{"", "end", "", "invalid"},
		} {
			t.Run(fmt.Sprint(mode, c), func(t *testing.T) {
				s, h, _ := footnoteService(t, e, c.old, mode)
				_, err := s.Apply(context.Background(), nil, map[string]any{"render": h.src.Render,
					"insert": map[string]any{"document": c.side}, "text": c.added})
				if err == nil || err.Status != 422 || err.Code != c.code {
					t.Fatalf("拒否=%v", err)
				}
				if mode == File && disk(t, h) != c.old {
					t.Fatal("拒否した追加を書いた")
				}
			})
		}
	}
}

func TestInsertDocumentEndProbe(t *testing.T) {
	e := testEngine(t)
	for _, old := range []string{"<!-- 閉じない\n", "<!-- 閉じた -->\n"} {
		s, h := testService(t, e, old, nil)
		_, err := applyJSON(t, s, h, `{"insert":{"document":"end"},"text":"追加"}`)
		// コメントに文字が消える場合も既存の要素は変わらないので、印による二度目の描画で断る。
		if h.trials != 2 {
			t.Fatalf("試しの描画=%d、期待=2", h.trials)
		}
		if strings.Contains(old, "-->") {
			if err != nil || disk(t, h) != old+"\n追加\n" {
				t.Fatal("閉じたコメントの後ろ", err)
			}
		} else if err == nil || err.Code != "insert" || disk(t, h) != old {
			t.Fatal("閉じないコメントの後ろ", err)
		}
	}
}

func TestInsertDocumentAfterInclude(t *testing.T) {
	e := testEngine(t)
	for _, mode := range []Mode{File, Buffer} {
		for _, side := range []string{"start", "end"} {
			for _, ending := range []string{"\n", "\r\n"} {
				for _, finalNewline := range []bool{false, true} {
					old := "本文\n\n::include{file=part.md}"
					if finalNewline {
						old += "\n"
					}
					old = strings.ReplaceAll(old, "\n", ending)
					s, h := testService(t, e, old, map[string]string{"part.md": "取り込み\n\n二つ目\n"})
					var sent []BufferEdit
					if mode == Buffer {
						s.opts.Mode = Buffer
						h.src.Buf, h.src.Rev = 7, 12
						s.opts.Send = func(_ context.Context, edit BufferEdit) *Error { sent = append(sent, edit); return nil }
					}
					if _, err := s.Apply(context.Background(), nil, map[string]any{"render": h.src.Render,
						"insert": map[string]any{"document": side}, "text": "追加"}); err != nil {
						t.Fatal(side, err)
					}
					want := "追加" + ending + ending + old
					if side == "end" {
						if finalNewline {
							want = old + ending + "追加" + ending
						} else {
							want = old + ending + ending + "追加"
						}
					}
					got := disk(t, h)
					if mode == Buffer {
						if len(sent) != 1 {
							t.Fatalf("バッファへの置き換え=%+v", sent)
						}
						edit, lines := sent[0], bufferLines(old)
						lines = append(append(append([]string{}, lines[:edit.Start]...), edit.Lines...), lines[edit.End:]...)
						got = strings.Join(lines, "\n") + "\n"
						if !strings.HasSuffix(want, "\n") {
							want += "\n"
						}
						if disk(t, h) != old {
							t.Fatal("バッファの追加でファイルへ書いた")
						}
					}
					if got != want {
						t.Fatalf("原文=%q、期待=%q", got, want)
					}
				}
			}
		}
	}
}

func TestInsertWithIncludeAndFrontMatter(t *testing.T) {
	e := testEngine(t)
	for _, side := range []string{"before", "after"} {
		text := "前\n\n::include{file=part.md}\n\n後\n"
		s, h := testService(t, e, text, map[string]string{"part.md": "取り込み\n\n二つ目\n"})
		if _, err := applyJSON(t, s, h, `{"insert":{"anchor":"1:1-1:3","side":"`+side+`"},"text":"追加"}`); err != nil {
			t.Fatal("展開された行もずらして比較する", side, err)
		}
		want := "追加\n\n" + text
		if side == "after" {
			want = "前\n\n追加\n\n::include{file=part.md}\n\n後\n"
		}
		if disk(t, h) != want {
			t.Fatalf("原文=%q、期待=%q", disk(t, h), want)
		}
	}
	// 除外する範囲を直接追加先にしても書かない。原文取得では従来の理由を返す。
	for _, text := range []string{"::include{file=part.md}\n", "---\ntitle: x\n---\n\n本文\n"} {
		s, h := testService(t, e, text, map[string]string{"part.md": "取り込み\n"})
		if _, err := applyJSON(t, s, h, `{"insert":{"anchor":"1:1-1:3","side":"before"},"text":"追加"}`); err == nil || err.Code != "target" || disk(t, h) != text {
			t.Fatal("除外する範囲への追加", err)
		}
	}
}
