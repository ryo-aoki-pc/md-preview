package edit

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/ryo-aoki-pc/md-tools/internal/pipeline"
	"github.com/ryo-aoki-pc/md-tools/internal/session"
)

func moveTargetAt(t *testing.T, s *Service, h *testHost, index int) blockMoveTarget {
	t.Helper()
	m, e := s.blocks(context.Background(), h.src)
	if e != nil {
		t.Fatal(e)
	}
	tables, e := s.tables(context.Background(), h.src)
	if e != nil {
		t.Fatal(e)
	}
	moves, e := s.blockMoves(context.Background(), h.src, m, tables)
	if e != nil || index >= len(moves.entries) {
		t.Fatalf("移動の対象=%+v、誤り=%v、描画=%s", moves, e, h.src.HTML)
	}
	return moves.entries[index].target
}

func moveBlockRequest(t *testing.T, s *Service, h *testHost, target blockMoveTarget, direction string) (any, *Error) {
	t.Helper()
	return s.Apply(context.Background(), nil, map[string]any{"render": h.src.Render, "op": map[string]any{
		"type": "moveBlock", "target": jsonObject(t, target), "direction": direction,
	}})
}

func TestMoveBlockKinds(t *testing.T) {
	engine := testEngine(t)
	for _, c := range []struct{ name, block string }{
		{"段落", "段落の本文。"},
		{"リスト", "- 一\n- 二"},
		{"説明リスト", "用語\n: 定義"},
		{"引用", "> 引用\n> 続き"},
		{"コード", "```js\nconst a = 1;\n```"},
		{"数式", "```math\nx^2\n```"},
		{"図", "```mermaid\ngraph TD; A-->B;\n```"},
		{"PlantUML", "```plantuml\nA -> B\n```"},
		{"表", strings.TrimSuffix(pipeTable, "\n")},
		{"HTML の表", strings.TrimSuffix(htmlTable, "\n")},
		{"注記", ">>> [!note]\n注記\n>>>"},
		{"水平線", "---"},
		{"HTML の連なり", "<div>一</div>\n\n<div>二</div>"},
	} {
		for _, direction := range []string{"up", "down"} {
			t.Run(c.name+"/"+direction, func(t *testing.T) {
				before := "前。\n\n" + c.block + "\n\n後。\n"
				s, h := testService(t, engine, before, nil)
				if c.name == "PlantUML" {
					h.trial = func(text string) (pipeline.Result, error) {
						return h.renderer.Render(context.Background(), text, session.FileContext(h.src.Root, h.src.File, h.src.Dir, true))
					}
					result, err := h.trial(before)
					if err != nil {
						t.Fatal(err)
					}
					h.src.HTML = result.HTML
				}
				v, e := moveBlockRequest(t, s, h, moveTargetAt(t, s, h, 1), direction)
				want := c.block + "\n\n前。\n\n後。\n"
				if direction == "down" {
					want = "前。\n\n後。\n\n" + c.block + "\n"
				}
				if e != nil || disk(t, h) != want {
					t.Fatalf("結果=%q、期待=%q、応答=%+v、誤り=%v、描画=%s", disk(t, h), want, v, e, h.src.HTML)
				}
				responseChange(t, v)
				if _, e := applyJSON(t, s, h, `{"undo":true}`); e != nil || disk(t, h) != before {
					t.Fatalf("1 手の取り消し=%v、本文=%q", e, disk(t, h))
				}
				if _, e := applyJSON(t, s, h, `{"redo":true}`); e != nil || disk(t, h) != want {
					t.Fatalf("やり直し=%v、本文=%q", e, disk(t, h))
				}
			})
		}
	}
}

// 空行なしで段落に続けて書けるブロックとの入れ替えでは、空行を 1 個補って別のブロックのまま残す。
func TestMoveBlockWithoutBlankLine(t *testing.T) {
	for _, c := range []struct{ name, before, want string }{
		{"段落の直後のコードを上へ", "段落\n```\nコード\n```\n", "```\nコード\n```\n\n段落\n"},
		{"段落の直後のリストを上へ", "段落\n- 項目\n", "- 項目\n\n段落\n"},
		{"段落の直後の引用を上へ", "段落\n> 引用\n", "> 引用\n\n段落\n"},
		{"コードの直後の段落を上へ", "```\nコード\n```\n段落\n", "段落\n\n```\nコード\n```\n"},
		{"段落の直後のコードを上へ（後ろに段落）", "段落\n```\nコード\n```\n後\n", "```\nコード\n```\n\n段落\n\n後\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, h := testService(t, testEngine(t), c.before, nil)
			_, e := moveBlockRequest(t, s, h, moveTargetAt(t, s, h, 1), "up")
			if e != nil || disk(t, h) != c.want {
				t.Fatalf("移動=%v、結果=%q、期待=%q、描画=%s", e, disk(t, h), c.want, h.src.HTML)
			}
			if _, e := applyJSON(t, s, h, `{"undo":true}`); e != nil || disk(t, h) != c.before {
				t.Fatalf("1 手の取り消し=%v、本文=%q", e, disk(t, h))
			}
		})
	}
}

// 脚注は名前で対応づけて比べるので、参照の順が変わって番号が付け直される移動も受ける。
func TestMoveBlockFootnotes(t *testing.T) {
	text := "前[^a]\n\n後[^b]\n\n[^a]: 一\n[^b]: 二\n"
	s, h := testService(t, testEngine(t), text, nil)
	if _, e := moveBlockRequest(t, s, h, moveTargetAt(t, s, h, 1), "up"); e != nil || disk(t, h) != "後[^b]\n\n前[^a]\n\n[^a]: 一\n[^b]: 二\n" {
		t.Fatalf("番号が変わる移動=%v、本文=%q", e, disk(t, h))
	}
	if !strings.Contains(h.src.HTML, `href="#fn-b" id="fnref-b">1</a>`) || !strings.Contains(h.src.HTML, `href="#fn-a" id="fnref-a">2</a>`) {
		t.Fatalf("番号の付け直し=%s", h.src.HTML)
	}
	// 脚注の定義の中身が変わる描画は断る。
	text = "前[^a]\n\n後\n\n[^a]: 一\n"
	s, h = testService(t, testEngine(t), text, nil)
	h.trial = func(text string) (result pipeline.Result, err error) {
		result, err = h.renderer.Render(context.Background(), text, session.FileContext(h.src.Root, h.src.File, h.src.Dir, false))
		result.HTML = strings.Replace(result.HTML, "一", "変化", 1)
		return
	}
	if _, e := moveBlockRequest(t, s, h, moveTargetAt(t, s, h, 1), "up"); e == nil || e.Message != blockMoveCompareReason || disk(t, h) != text {
		t.Fatalf("脚注の中身の変化=%v、本文=%q", e, disk(t, h))
	}
}

func TestMoveBlockBoundaries(t *testing.T) {
	for _, c := range []struct {
		name, text, direction, reason string
		index                         int
	}{
		{"文書の先頭", "前\n\n後\n", "up", "入れ替えるブロック", 0},
		{"文書の末尾", "前\n\n後\n", "down", "入れ替えるブロック", 1},
		{"見出しを上へまたぐ", "前\n\n## 題\n\n後\n", "up", "見出しはまたげません", 2},
		{"見出しを下へまたぐ", "前\n\n## 題\n\n後\n", "down", "見出しはまたげません", 0},
		{"見出し自体", "前\n\n## 題\n\n後\n", "down", "見出しはまたげません", 1},
		{"つながるリスト", "- 前\n\n間\n\n- 後\n", "down", blockMoveCompareReason, 1},
		{"定義を運ばない", "前\n\n[x]: https://example.com\n\n後\n", "down", "別の原文", 0},
		{"取り込みをまたぐ", "前\n\n::include{file=part.md}\n\n後\n", "down", "取り込んだ", 0},
		{"取り込んだブロック", "::include{file=part.md}\n\n後\n", "down", "取り込んだ", 0},
		{"front matter 自体", "---\ntitle: 題\n---\n\n本文\n", "down", "front matter", 0},
		{"front matter をまたぐ", "---\ntitle: 題\n---\n\n本文\n", "up", "front matter", 1},
		{"閉じない HTML が前後を巻き込む", "前\n\n<div>HTML</div>\n\n<!--", "up", blockMoveCompareReason, 1},
		{"HTML の見出し", "前\n\n<h2>題</h2>\n\n後\n", "down", "見出しはまたげません", 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, h := testService(t, testEngine(t), c.text, map[string]string{"part.md": "取り込んだ段落\n"})
			_, e := moveBlockRequest(t, s, h, moveTargetAt(t, s, h, c.index), c.direction)
			if e == nil || !strings.Contains(e.Message, c.reason) || disk(t, h) != c.text {
				t.Fatalf("拒否=%v、本文=%q、描画=%s", e, disk(t, h), h.src.HTML)
			}
		})
	}
	// 内側の段落を指しても、リスト全体へ勝手に広げない。
	s, h := testService(t, testEngine(t), "前\n\n- 内側\n\n後\n", nil)
	m, _ := s.blocks(context.Background(), h.src)
	for _, b := range m.blocks {
		if b.outer >= 0 {
			_, e := moveBlockRequest(t, s, h, blockMoveTarget{Tag: b.tag, Range: b.public}, "up")
			if e == nil || e.Message != blockMoveOuterReason {
				t.Fatal(e)
			}
		}
	}
}

func TestMoveHTMLRunContainingTable(t *testing.T) {
	for _, raw := range []string{strings.TrimSuffix(htmlTable, "\n"), htmlTable + "\n<div>表の後ろの HTML</div>"} {
		for _, direction := range []string{"up", "down"} {
			before := "前\n\n" + raw + "\n\n後\n"
			s, h := testService(t, testEngine(t), before, nil)
			source := htmlSource(t, s, h, 0)
			info := source["move"].(*blockMoveInfo)
			if info.Up != "" || info.Down != "" {
				t.Fatal(info)
			}
			_, e := moveBlockRequest(t, s, h, info.Target, direction)
			want := raw + "\n\n前\n\n後\n"
			if direction == "down" {
				want = "前\n\n後\n\n" + raw + "\n"
			}
			if e != nil || disk(t, h) != want {
				t.Fatalf("移動=%v、結果=%q、期待=%q", e, disk(t, h), want)
			}
		}
	}
}

func TestMoveBlockNewlinesAndKeep(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n"} {
		for _, bom := range []string{"", "\ufeff"} {
			for _, tail := range []string{"", "\n", "\n\n"} {
				before := bom + strings.ReplaceAll("前\n\n\n後"+tail, "\n", ending)
				s, h := testService(t, testEngine(t), before, nil)
				v, e := moveBlockRequest(t, s, h, moveTargetAt(t, s, h, 0), "down")
				want := bom + strings.ReplaceAll("後\n\n\n前"+tail, "\n", ending)
				if e != nil || disk(t, h) != want {
					t.Fatal(e, disk(t, h), want)
				}
				keep := responseChange(t, v).Keep
				if len(keep) < 2 || !reflect.DeepEqual(keep[:2], []lineKeep{{1, 4, 1}, {4, 1, 1}}) {
					t.Fatal(keep)
				}
				undo, e := applyJSON(t, s, h, `{"undo":true}`)
				if e != nil || disk(t, h) != before {
					t.Fatal(e, disk(t, h))
				}
				if !reflect.DeepEqual(responseChange(t, undo).Keep, reverseKeep(keep)) {
					t.Fatal("取り消しの行対応", responseChange(t, undo).Keep, keep)
				}
			}
		}
	}
}

func TestMoveBlockBuffer(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n"} {
		text := strings.ReplaceAll("前\n\n後\n", "\n", ending)
		s, h, sent := bufferService(t, testEngine(t), text)
		v, e := moveBlockRequest(t, s, h, moveTargetAt(t, s, h, 1), "up")
		if e != nil || len(*sent) != 1 {
			t.Fatal(e, sent)
		}
		cr := strings.TrimSuffix(ending, "\n")
		want := BufferEdit{Path: h.src.DocPath, Buf: h.src.Buf, Rev: h.src.Rev, Start: 0, End: 3, Lines: []string{"後" + cr, cr, "前" + cr}}
		if !reflect.DeepEqual((*sent)[0], want) {
			t.Fatal(*sent, want)
		}
		checkKeep(t, responseChange(t, v).Keep, []lineKeep{{1, 3, 1}, {3, 1, 1}})
	}
}

func TestMoveBlockInformationAndFailure(t *testing.T) {
	s, h := testService(t, testEngine(t), "## 題\n\n前\n\n"+pipeTable+"\n後\n", nil)
	info := getTables(t, s, h)
	if len(info.BlockMoves) != 1 || info.BlockMoves[0].Up != "" || info.BlockMoves[0].Down != "" {
		t.Fatal(info.BlockMoves)
	}
	target := moveTargetAt(t, s, h, 1)
	v, e := s.Text(context.Background(), nil, h.src.Render, target.Tag, target.Range)
	if e != nil {
		t.Fatal(e)
	}
	move := v.(map[string]any)["move"].(*blockMoveInfo)
	if move.Up != blockMoveHeadingReason || move.Down != "" {
		t.Fatal(move)
	}
	// 試しの描画で隣の本文を巻き込む・表を変える・描画に失敗する場合は、履歴もファイルも作り替えない。
	for _, failure := range []string{"周囲", "表", "失敗"} {
		before := disk(t, h)
		h.trial = func(text string) (pipeline.Result, error) {
			if failure == "失敗" {
				return pipeline.Result{}, errors.New("描画失敗")
			}
			result, err := h.renderer.Render(context.Background(), text, pipeline.Context{})
			if failure == "周囲" {
				result.HTML = strings.ReplaceAll(result.HTML, "後", "変わった後")
			} else {
				result.HTML = strings.ReplaceAll(result.HTML, "見出し", "変わった表")
			}
			return result, err
		}
		_, e := moveBlockRequest(t, s, h, target, "down")
		if e == nil || e.Code != "render" || disk(t, h) != before || len(s.history.documents) != 0 {
			t.Fatal(e, disk(t, h), s.history)
		}
	}
	for _, body := range []string{
		`{"op":{"type":"moveBlock","target":{"tag":"p","range":"3:1-3:3"},"direction":"left"}}`,
		`{"op":{"type":"moveBlock","target":{"table":-1},"direction":"up"}}`,
		`{"op":{"type":"moveBlock","target":{"tag":"p","range":"3:1-3:3","extra":true},"direction":"up"}}`,
	} {
		var request map[string]any
		if err := json.Unmarshal([]byte(body), &request); err != nil {
			t.Fatal(err)
		}
		request["render"] = h.src.Render
		if _, e := s.Apply(context.Background(), nil, request); e == nil || e.Status != 400 {
			t.Fatal(e)
		}
	}
}
