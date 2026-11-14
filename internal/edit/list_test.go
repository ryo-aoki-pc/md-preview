package edit

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/ryo-aoki-pc/md-tools/internal/htmltree"
	"github.com/ryo-aoki-pc/md-tools/internal/pipeline"
	"golang.org/x/net/html"
)

func listTargetAt(t *testing.T, s *Service, h *testHost, index int) blockMoveTarget {
	t.Helper()
	m, e := s.blocks(context.Background(), h.src)
	if e != nil {
		t.Fatal(e)
	}
	for _, b := range m.blocks {
		if b.kind == "item" {
			if index == 0 {
				return blockMoveTarget{Tag: "li", Range: b.public}
			}
			index--
		}
	}
	t.Fatalf("項目がありません: %s", h.src.HTML)
	return blockMoveTarget{}
}

func listRequest(t *testing.T, s *Service, h *testHost, target blockMoveTarget, direction string) (any, *Error) {
	t.Helper()
	return s.Apply(context.Background(), nil, map[string]any{"render": h.src.Render, "op": map[string]any{"type": "listIndent", "target": jsonObject(t, target), "direction": direction}})
}

func TestListIndent(t *testing.T) {
	for _, c := range []struct {
		name, before, want, direction string
		index                         int
	}{
		{"箇条を下げる", "- 前\n- 対象\n- 後\n", "- 前\n  - 対象\n- 後\n", "indent", 1},
		{"印を保つ", "* 前\n* 対象\n", "* 前\n  * 対象\n", "indent", 1},
		{"番号を下げる", "1. 前\n1. 対象\n", "1. 前\n   1. 対象\n", "indent", 1},
		{"番号を上げる", "1. 前\n   1. 対象\n", "1. 前\n1. 対象\n", "outdent", 1},
		{"タスクを下げる", "- [ ] 前\n- [x] 対象\n", "- [ ] 前\n  - [x] 対象\n", "indent", 1},
		{"タスクを上げる", "- [ ] 前\n  - [x] 対象\n", "- [ ] 前\n- [x] 対象\n", "outdent", 1},
		{"混在するタスクを下げる", "- 前\n- [x] 対象\n", "- 前\n  - [x] 対象\n", "indent", 1},
		{"混在するタスクを上げる", "- 前\n  - [x] 対象\n", "- 前\n- [x] 対象\n", "outdent", 1},
		{"既存の子の最後へ", "- 前\n  - 子\n- 対象\n", "- 前\n  - 子\n  - 対象\n", "indent", 2},
		{"番号を既存の子の最後へ", "1. 前\n   1. 子\n2. 対象\n", "1. 前\n   1. 子\n   2. 対象\n", "indent", 2},
		{"続く本文も下げる", "- 前\n- 対象\n  続き\n", "- 前\n  - 対象\n    続き\n", "indent", 1},
		{"脚注を含む文書", "- 前[^a]\n- 対象[^b]\n\n[^a]: 前の注\n[^b]: 対象の注\n", "- 前[^a]\n  - 対象[^b]\n\n[^a]: 前の注\n[^b]: 対象の注\n", "indent", 1},
		{"front matter の後ろ", "---\ntitle: 題\n---\n\n- 前\n- 対象\n", "---\ntitle: 題\n---\n\n- 前\n  - 対象\n", "indent", 1},
		{"取り込みの後ろ", "::include{file=part.md}\n\n- 前\n- 対象\n", "::include{file=part.md}\n\n- 前\n  - 対象\n", "indent", 1},
		{"子も下げる", "- 前\n- 対象\n  - 子\n    - 孫\n", "- 前\n  - 対象\n    - 子\n      - 孫\n", "indent", 1},
		{"子も上げる", "- 前\n  - 対象\n    - 子\n      - 孫\n", "- 前\n- 対象\n  - 子\n    - 孫\n", "outdent", 1},
		{"引用で下げる", "> - 前\n> - 対象\n", "> - 前\n>   - 対象\n", "indent", 1},
		{"引用で上げる", "> - 前\n>   - 対象\n", "> - 前\n> - 対象\n", "outdent", 1},
		{"本文の引用も下げる", "- 前\n- 対象\n  > 本文の引用\n", "- 前\n  - 対象\n    > 本文の引用\n", "indent", 1},
		{"本文の引用も上げる", "- 前\n  - 対象\n    > 本文の引用\n", "- 前\n- 対象\n  > 本文の引用\n", "outdent", 1},
		{"外側と本文の引用", "> - 前\n> - 対象\n>   > 本文の引用\n", "> - 前\n>   - 対象\n>     > 本文の引用\n", "indent", 1},
		{"親の後ろに上げる", "- 親\n  - 先\n  - 対象\n- 後\n", "- 親\n  - 先\n- 対象\n- 後\n", "outdent", 2},
		{"本文の桁まで下げる", "-   前\n- 対象\n", "-   前\n    - 対象\n", "indent", 1},
		{"括弧の番号を下げる", "1) 前\n1) 対象\n", "1) 前\n   1) 対象\n", "indent", 1},
		{"括弧の番号を上げる", "1) 前\n   1) 対象\n", "1) 前\n1) 対象\n", "outdent", 1},
		{"3 桁の番号の本文の桁まで下げる", "100. 前\n1. 対象\n", "100. 前\n     1. 対象\n", "indent", 1},
		{"ゆるい段落とコードも下げる", "- 前\n\n  続き\n\n- 対象\n\n  続き\n\n      コード\n", "- 前\n\n  続き\n\n  - 対象\n\n    続き\n\n        コード\n", "indent", 1},
		{"続くコードと子も上げる", "- 親\n\n  続き\n\n  - 対象\n\n    続き\n\n        コード\n\n    - 子\n", "- 親\n\n  続き\n\n- 対象\n\n  続き\n\n      コード\n\n  - 子\n", "outdent", 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, h := testService(t, testEngine(t), c.before, map[string]string{"part.md": "取り込んだ段落\n"})
			v, e := listRequest(t, s, h, listTargetAt(t, s, h, c.index), c.direction)
			if e != nil || disk(t, h) != c.want {
				t.Fatalf("結果=%q、期待=%q、誤り=%v、描画=%s", disk(t, h), c.want, e, h.src.HTML)
			}
			if c.name != "front matter の後ろ" && c.name != "取り込みの後ろ" {
				checkKeep(t, responseChange(t, v).Keep, []lineKeep{{1, 1, len(headingLines(c.before))}})
			}
			if _, e := applyJSON(t, s, h, `{"undo":true}`); e != nil || disk(t, h) != c.before {
				t.Fatal(e, disk(t, h))
			}
			if _, e := applyJSON(t, s, h, `{"redo":true}`); e != nil || disk(t, h) != c.want {
				t.Fatal(e, disk(t, h))
			}
		})
	}
}

// ゆるいリストでは、所属が変わると項目の段落の包みが付け外しされる。包みのほかは比べ、
// 項目の最初の段落（右クリックの対象）からも、その項目の段の情報を返す。
func TestListIndentLoose(t *testing.T) {
	for _, c := range []struct {
		name, before, want, direction string
		index                         int
	}{
		{"ゆるいリストを下げる", "- 前\n\n- 対象\n", "- 前\n\n  - 対象\n", "indent", 1},
		{"ゆるいリストの途中を下げる", "* 前\n\n* 対象\n\n* 後\n", "* 前\n\n  * 対象\n\n* 後\n", "indent", 1},
		{"ゆるいリストを上げる", "* 親\n\n  * 先\n\n  * 対象\n", "* 親\n\n  * 先\n\n* 対象\n", "outdent", 2},
		{"ゆるいタスクを下げる", "- [ ] 前\n\n- [x] 対象\n", "- [ ] 前\n\n  - [x] 対象\n", "indent", 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, h := testService(t, testEngine(t), c.before, nil)
			if _, e := listRequest(t, s, h, listTargetAt(t, s, h, c.index), c.direction); e != nil || disk(t, h) != c.want {
				t.Fatalf("結果=%q、期待=%q、誤り=%v、描画=%s", disk(t, h), c.want, e, h.src.HTML)
			}
			if _, e := applyJSON(t, s, h, `{"undo":true}`); e != nil || disk(t, h) != c.before {
				t.Fatal(e, disk(t, h))
			}
		})
	}
	// 段落の包みのほかの変化（親の続きの段落が対象の項目に移る）は、ゆるいリストでも断る。
	text := "* 親\n\n  * 対象\n\n  続きの段落\n"
	s, h := testService(t, testEngine(t), text, nil)
	if _, e := listRequest(t, s, h, listTargetAt(t, s, h, 1), "outdent"); e == nil || e.Message != listIndentCompareReason || disk(t, h) != text {
		t.Fatalf("巻き込み=%v、本文=%q", e, disk(t, h))
	}
	// 項目の最初の段落からは項目の情報を返し、2 つ目の段落とリストの外の段落からは返さない。
	s, h = testService(t, testEngine(t), "- 前\n\n- 対象\n\n  二つ目\n\n外\n", nil)
	m, e := s.blocks(context.Background(), h.src)
	if e != nil {
		t.Fatal(e)
	}
	item := listTargetAt(t, s, h, 1)
	got := map[string]*listIndentInfo{}
	for _, b := range m.blocks {
		if b.kind != "paragraph" {
			continue
		}
		v, e := s.Text(context.Background(), nil, h.src.Render, b.tag, b.public)
		if e != nil {
			t.Fatal(e)
		}
		text := v.(map[string]any)["text"].(string)
		got[text], _ = v.(map[string]any)["listIndent"].(*listIndentInfo)
	}
	if info := got["対象"]; info == nil || info.Target != item || info.Indent != "" || !strings.Contains(info.Outdent, "いちばん外側") {
		t.Fatalf("最初の段落の情報=%+v、項目=%+v", info, item)
	}
	if got["二つ目"] != nil || got["外"] != nil || got["前"] == nil {
		t.Fatalf("段落ごとの情報=%+v", got)
	}
}

func TestListIndentRefused(t *testing.T) {
	for _, c := range []struct {
		name, text, direction, reason string
		index                         int
	}{
		{"先頭", "- 前\n- 後\n", "indent", "先頭", 0},
		{"最上位", "- 前\n- 後\n", "outdent", "いちばん外側", 1},
		{"後ろに兄弟", "- 親\n  - 対象\n  - 後\n", "outdent", "後ろの項目から上げて", 1},
		{"番号が段落になる", "1. 前\n2. 対象\n", "indent", listIndentCompareReason, 1},
		{"種類が変わる", "- 親\n  1. 対象\n", "outdent", listIndentCompareReason, 1},
		{"親の続きが巻き込まれる", "- 親\n  - 対象\n\n  続きの段落\n", "outdent", listIndentCompareReason, 1},
		{"脚注の定義を含む", "- 前\n- 対象[^a]\n\n  [^a]: 定義\n\n  続き\n", "indent", "脚注の定義", 1},
		{"タブ", "- 親\n\t- 対象\n", "outdent", "", 1},
		{"空白の無い引用", ">- 前\n>- 対象\n", "indent", "引用の印の後ろに空白が無い", 1},
		{"相手だけ空白の無い引用", ">- 前\n> - 対象\n", "indent", "引用の印の後ろに空白が無い", 1},
		{"続きの行だけ空白の無い引用", "> - 前\n> - 対象\n>   続き\n>続きの行\n", "indent", "引用の印の後ろに空白が無い", 1},
		{"取り込みをまたぐ", "- 前\n\n::include{file=part.md}\n\n- 対象\n", "indent", "先頭", 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, h := testService(t, testEngine(t), c.text, map[string]string{"part.md": "別の文\n"})
			_, e := listRequest(t, s, h, listTargetAt(t, s, h, c.index), c.direction)
			if e == nil || !strings.Contains(e.Message, c.reason) || disk(t, h) != c.text || len(s.history.documents) != 0 {
				t.Fatalf("拒否=%v、本文=%q", e, disk(t, h))
			}
		})
	}
	for _, text := range []string{"本文[^a]\n\n[^a]: 定義\n", "<ul><li data-sourcepos=\"1:1-1:4\">生</li></ul>\n", "::include{file=part.md}\n", "---\ntitle: 題\n---\n\n- 項目\n"} {
		s, h := testService(t, testEngine(t), text, map[string]string{"part.md": "- 前\n- 対象\n"})
		root, _ := htmltree.Fragment(context.Background(), h.src.HTML)
		nodes, _ := htmltree.Elements(context.Background(), root, func(n *html.Node) bool { return n.Data == "li" })
		if len(nodes) == 0 {
			t.Fatal(h.src.HTML)
		}
		r := Range(htmltree.Get(nodes[len(nodes)-1], "data-sourcepos"))
		_, e := listRequest(t, s, h, blockMoveTarget{Tag: "li", Range: r}, "outdent")
		if e == nil || disk(t, h) != text {
			t.Fatal(e, disk(t, h))
		}
	}
	s, h := testService(t, testEngine(t), "---\ntitle: 題\n---\n\n- 前\n- 対象\n", nil)
	if _, e := listRequest(t, s, h, blockMoveTarget{Tag: "li", Range: "1:1-3:3"}, "indent"); e == nil {
		t.Fatal("front matter の偽の範囲を受けた")
	}
}

func TestListIndentNewlinesBufferAndInformation(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n"} {
		for _, bom := range []string{"", "\ufeff"} {
			for _, tail := range []string{"", "\n", "\n\n"} {
				before := bom + strings.ReplaceAll("- 前\n- 対象"+tail, "\n", ending)
				want := bom + strings.ReplaceAll("- 前\n  - 対象"+tail, "\n", ending)
				s, h := testService(t, testEngine(t), before, nil)
				target := listTargetAt(t, s, h, 1)
				v, e := s.Text(context.Background(), nil, h.src.Render, "li", target.Range)
				if e != nil {
					t.Fatal(e)
				}
				info := v.(map[string]any)["listIndent"].(*listIndentInfo)
				if info.Indent != "" || !strings.Contains(info.Outdent, "いちばん外側") || info.Target != target {
					t.Fatal(info)
				}
				if _, e = listRequest(t, s, h, target, "indent"); e != nil || disk(t, h) != want {
					t.Fatal(e, disk(t, h), want)
				}
				if _, e = applyJSON(t, s, h, `{"undo":true}`); e != nil || disk(t, h) != before {
					t.Fatal(e, disk(t, h))
				}
			}
		}
	}
	for _, ending := range []string{"\n", "\r\n"} {
		text := strings.ReplaceAll("- 前\n- 対象\n  - 子\n", "\n", ending)
		s, h, sent := bufferService(t, testEngine(t), text)
		v, e := listRequest(t, s, h, listTargetAt(t, s, h, 1), "indent")
		if e != nil || len(*sent) != 1 {
			t.Fatal(e, sent)
		}
		cr := strings.TrimSuffix(ending, "\n")
		want := BufferEdit{Path: h.src.DocPath, Buf: h.src.Buf, Rev: h.src.Rev, Start: 1, End: 3, Lines: []string{"  - 対象" + cr, "    - 子" + cr}}
		if !reflect.DeepEqual((*sent)[0], want) {
			t.Fatal(*sent, want)
		}
		checkKeep(t, responseChange(t, v).Keep, []lineKeep{{1, 1, 3}})
	}
}

func TestListIndentInvalidAndTrial(t *testing.T) {
	s, h := testService(t, testEngine(t), "- 前\n- 対象\n\n後\n", nil)
	target := listTargetAt(t, s, h, 1)
	for _, op := range []string{
		`{"type":"listIndent","target":{"tag":"li","range":"2:1-2:8"},"direction":"up"}`,
		`{"type":"listIndent","target":{"tag":"p","range":"2:1-2:8"},"direction":"indent"}`,
		`{"type":"listIndent","target":{"tag":"li","range":2},"direction":"indent"}`,
		`{"type":"listIndent","target":{"tag":"li","range":"2:1-2:8","extra":1},"direction":"indent"}`,
		`{"type":"listIndent","target":{"tag":"li","range":"2:1-2:8"},"direction":"indent","extra":true}`,
		`{"type":"listIndent","target":[],"direction":true}`,
	} {
		var value map[string]any
		_ = json.Unmarshal([]byte(op), &value)
		_, e := s.Apply(context.Background(), nil, map[string]any{"render": h.src.Render, "op": value})
		if e == nil || e.Status != 400 {
			t.Fatal(e)
		}
	}
	for _, failure := range []string{"本文", "属性", "描画"} {
		h.trial = func(text string) (pipeline.Result, error) {
			if failure == "描画" {
				return pipeline.Result{}, errors.New("描画失敗")
			}
			r, e := h.renderer.Render(context.Background(), text, pipeline.Context{})
			if failure == "本文" {
				r.HTML = strings.ReplaceAll(r.HTML, "後", "変化")
			} else {
				r.HTML = strings.ReplaceAll(r.HTML, "<li ", "<li title=\"変化\" ")
			}
			return r, e
		}
		if _, e := listRequest(t, s, h, target, "indent"); e == nil || e.Code != "render" || disk(t, h) != "- 前\n- 対象\n\n後\n" {
			t.Fatal(e, disk(t, h))
		}
	}
	// 空白だけの文字節点も、インライン要素の間では本文なので保つ。
	before, _ := htmltree.Fragment(context.Background(), `<ul><li><em>一</em> <em>二</em></li></ul>`)
	after, _ := htmltree.Fragment(context.Background(), `<ul><li><em>一</em><em>二</em></li></ul>`)
	if listIndentSignature(before) == listIndentSignature(after) {
		t.Fatal("本文の空白を見落とした")
	}
	// 属性の順番は署名に影響しない。
	a, _ := htmltree.Fragment(context.Background(), `<ul dir="auto" class="task-list"><li class="task-list-item" id="x">本文</li></ul>`)
	b, _ := htmltree.Fragment(context.Background(), `<ul class="task-list" dir="auto"><li id="x" class="task-list-item">本文</li></ul>`)
	if listIndentSignature(a) != listIndentSignature(b) {
		t.Fatal("属性の順番で署名が変わった")
	}
}
