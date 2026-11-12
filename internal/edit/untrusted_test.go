package edit

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/ryo-aoki-pc/md-tools/internal/pipeline"
)

func TestUntrustedParagraphs(t *testing.T) {
	e := testEngine(t)
	for _, buffer := range []bool{false, true} {
		for _, forged := range []string{"1:1-3:6", "1:1-1:5"} {
			text := "first\n\nsecond\n\n<p data-sourcepos=\"" + forged + "\">forged</p>\n\nsafe\n"
			var s *Service
			var h *testHost
			var sent *[]BufferEdit
			if buffer {
				s, h, sent = bufferService(t, e, text)
			} else {
				s, h = testService(t, e, text, nil)
			}
			originalHTML := h.src.HTML
			if _, err := s.Text(context.Background(), nil, h.src.Render, "p", Range(forged)); err == nil || (err.Code != "untrusted" && err.Code != "ambiguous") {
				t.Fatalf("バッファ=%v、位置=%s: %v", buffer, forged, err)
			}
			if _, err := applyJSON(t, s, h, `{"range":"`+forged+`","text":"changed"}`); err == nil || err.Code != "untrusted" {
				t.Fatal(err)
			}
			if h.src.HTML != originalHTML || h.src.Text != text || h.unpositioned != 1 || h.trials != 0 {
				t.Fatal("拒否で原文・公開 HTML・描画回数が変わった", h.unpositioned, h.trials)
			}
			if buffer && len(*sent) != 0 || !buffer && disk(t, h) != text {
				t.Fatal("信用しない位置で書いた")
			}
			value, err := s.Text(context.Background(), nil, h.src.Render, "p", "7:1-7:4")
			if err != nil || value.(map[string]any)["text"] != "safe" {
				t.Fatal(value, err)
			}
			if _, err := applyJSON(t, s, h, `{"range":"7:1-7:4","text":"changed"}`); err != nil {
				t.Fatal(err)
			}
			if buffer {
				if len(*sent) != 1 || (*sent)[0].Start != 6 || (*sent)[0].End != 7 || strings.Join((*sent)[0].Lines, "") != "changed" {
					t.Fatal(*sent)
				}
			} else if disk(t, h) != strings.TrimSuffix(text, "safe\n")+"changed\n" {
				t.Fatal(disk(t, h))
			}
		}
	}
}

func TestUntrustedCheckboxes(t *testing.T) {
	e := testEngine(t)
	for _, buffer := range []bool{false, true} {
		for _, forged := range []string{"1:8-1:8", "3:4-3:4"} {
			text := "plain [ ]\n\n- [ ] task\n\n<ul><li><input type=\"checkbox\" class=\"task-list-item-checkbox\" data-checkbox-sourcepos=\"" + forged + "\"></li></ul>\n\n- [ ] safe\n"
			var s *Service
			var h *testHost
			var sent *[]BufferEdit
			if buffer {
				s, h, sent = bufferService(t, e, text)
			} else {
				s, h = testService(t, e, text, nil)
			}
			code := "untrusted"
			if forged == "3:4-3:4" {
				code = "ambiguous"
			}
			if _, err := applyJSON(t, s, h, `{"checkbox":"`+forged+`","checked":true}`); err == nil || err.Code != code {
				t.Fatal(err)
			}
			if buffer && len(*sent) != 0 || !buffer && disk(t, h) != text {
				t.Fatal("信用しないチェックボックスを書いた")
			}
			if _, err := applyJSON(t, s, h, `{"checkbox":"7:4-7:4","checked":true}`); err != nil {
				t.Fatal(err)
			}
			if buffer {
				if len(*sent) != 1 || (*sent)[0].Start != 6 || strings.Join((*sent)[0].Lines, "") != "- [x] safe" {
					t.Fatal(*sent)
				}
			} else if disk(t, h) != strings.Replace(text, "- [ ] safe", "- [x] safe", 1) {
				t.Fatal(disk(t, h))
			}
			if h.unpositioned != 1 {
				t.Fatal("チェックボックスの確定で位置なし描画を繰り返した", h.unpositioned)
			}
		}
	}
}

func TestUnpositionedOncePerRender(t *testing.T) {
	e := testEngine(t)
	for _, text := range []string{"safe\n", "sourcepos\n"} {
		for _, tablesFirst := range []bool{false, true} {
			s, h := testService(t, e, text, nil)
			for i := 0; i < 2; i++ {
				if tablesFirst {
					_ = getTables(t, s, h)
				}
				r := Range("1:1-1:4")
				if h.src.HasSourcepos {
					r = "1:1-1:9"
				}
				if _, err := s.Text(context.Background(), nil, h.src.Render, "p", r); err != nil {
					t.Fatal(err)
				}
				if _, err := s.blocks(context.Background(), h.src); err != nil {
					t.Fatal(err)
				}
				_ = getTables(t, s, h)
				want := 0
				if h.src.HasSourcepos {
					want = i + 1
				}
				if h.unpositioned != want || h.trials != 0 {
					t.Fatal(h.unpositioned, h.trials, want)
				}
				h.src.Render++
			}
		}
	}
}

func TestUnpositionedFailureDeniesAllEdits(t *testing.T) {
	e := testEngine(t)
	text := "sourcepos\n\n- [ ] task\n\n" + pipeTable + "\n" + htmlTable
	for _, buffer := range []bool{false, true} {
		var s *Service
		var h *testHost
		var sent *[]BufferEdit
		if buffer {
			s, h, sent = bufferService(t, e, text)
		} else {
			s, h = testService(t, e, text, nil)
		}
		h.unposition = func() (pipeline.Result, error) { return pipeline.Result{}, errors.New("内部の情報") }
		if _, err := s.Text(context.Background(), nil, h.src.Render, "p", "1:1-1:9"); err == nil || err.Code != "untrusted" || err.Message != "原文の位置を確かめられません。" {
			t.Fatal(err)
		}
		for _, body := range []string{
			`{"range":"1:1-1:9","text":"changed"}`,
			`{"checkbox":"3:4-3:4","checked":true}`,
			`{"table":0,"op":{"type":"set","cells":[{"row":1,"col":0,"text":"changed"}]}}`,
			`{"table":1,"op":{"type":"set","cells":[{"row":0,"col":0,"text":"changed"}]}}`,
		} {
			if _, err := applyJSON(t, s, h, body); err == nil || err.Message != "原文の位置を確かめられません。" {
				t.Fatal(body, err)
			}
		}
		for _, info := range getTables(t, s, h).Tables {
			if info.Editable || info.Reason != "原文の位置を確かめられません。" {
				t.Fatal(info)
			}
		}
		if h.unpositioned != 1 || h.trials != 0 || buffer && len(*sent) != 0 || !buffer && disk(t, h) != text {
			t.Fatal("失敗時に位置の検証を繰り返したか原文を書いた", h.unpositioned, h.trials)
		}
	}
}

func TestUntrustedTableCannotUsePipePosition(t *testing.T) {
	e := testEngine(t)
	// tfoot が先の表は印による対応づけの対象外。偽の位置を根拠にパイプ表へ結び付けない。
	forged := `<table data-sourcepos="1:1-3:16"><tfoot><tr><td>F</td><td>G</td></tr></tfoot><tbody><tr><td>C</td><td>D</td></tr></tbody></table>`
	s, h := testService(t, e, pipeTable+"\n"+forged, nil)
	m, err := s.tables(context.Background(), h.src)
	if err != nil || len(m.models) != 2 || m.models[1] != nil || m.response.Tables[1].Editable || h.unpositioned != 1 {
		t.Fatal(m, err)
	}
}

func TestUntrustedPositionInInclude(t *testing.T) {
	e := testEngine(t)
	s, h := testService(t, e, "safe\n\n::include{file=part.md}\n", map[string]string{"part.md": `<p DATA-SOURCEPOS="1:1-1:4">forged</p>`})
	if !h.src.HasSourcepos {
		t.Fatal("取り込みの位置属性の印が無い")
	}
	if _, err := applyJSON(t, s, h, `{"range":"1:1-1:4","text":"changed"}`); err == nil || err.Code != "untrusted" {
		t.Fatal(err)
	}
	if h.unpositioned != 1 || disk(t, h) != h.src.Text {
		t.Fatal("取り込みの属性で原文を書いた")
	}
}

func TestUnpositionedInputMustMatch(t *testing.T) {
	e := testEngine(t)
	// 取り込んだファイルが描画の後に変わると、位置なしの描画は別の入力を描く。その結果では見分けない。
	s, h := testService(t, e, "safe\n\n::include{file=part.md}\n", map[string]string{"part.md": `<p data-sourcepos="1:1-1:2">forged</p>`})
	if err := os.WriteFile(filepath.Join(h.src.Root, "part.md"), []byte("変わった"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, r := range []Range{"1:1-1:2", "1:1-1:4"} {
		if _, err := s.Text(context.Background(), nil, h.src.Render, "p", r); err == nil || err.Code != "untrusted" || err.Message != "原文の位置を確かめられません。" {
			t.Fatal(r, err)
		}
		if _, err := applyJSON(t, s, h, `{"range":"`+string(r)+`","text":"changed"}`); err == nil || err.Message != "原文の位置を確かめられません。" {
			t.Fatal(r, err)
		}
	}
	if h.unpositioned != 1 || disk(t, h) != h.src.Text {
		t.Fatal("入力の違う位置なし描画で原文を書いた", h.unpositioned)
	}
}

func TestUntrustedPositionFromJSONTable(t *testing.T) {
	e := testEngine(t)
	// JSON の表のセルは、JSON のエスケープを解いた後の Markdown を描く。原文に sourcepos の文字が
	// 無くても、位置属性を持つ生の HTML を作れる。
	name := "data-checkbox-" + string(rune(92)) + "u0073ourcepos"
	text := "plain [ ]\n\n- [ ] task\n\n```json:table\n" +
		`{"markdown": true, "items": [{"a": "<input type=\"checkbox\" class=\"task-list-item-checkbox\" ` + name + `=\"1:8-1:8\">"}]}` +
		"\n```\n"
	if strings.Contains(strings.ToLower(text), "sourcepos") {
		t.Fatal("試験の入力に位置属性の文字がある")
	}
	for _, buffer := range []bool{false, true} {
		var s *Service
		var h *testHost
		var sent *[]BufferEdit
		if buffer {
			s, h, sent = bufferService(t, e, text)
		} else {
			s, h = testService(t, e, text, nil)
		}
		if !h.src.HasSourcepos || !strings.Contains(h.src.HTML, `data-checkbox-sourcepos="1:8-1:8"`) {
			t.Fatalf("JSON の表から作った位置属性の印が無い: %v\n%s", h.src.HasSourcepos, h.src.HTML)
		}
		if _, err := applyJSON(t, s, h, `{"checkbox":"1:8-1:8","checked":true}`); err == nil || err.Code != "target" {
			t.Fatal("表の中のチェックボックスの位置を受けた", err)
		}
		if buffer && len(*sent) != 0 || !buffer && disk(t, h) != text {
			t.Fatal("JSON の表の位置属性で書いた")
		}
		if _, err := applyJSON(t, s, h, `{"checkbox":"3:4-3:4","checked":true}`); err != nil {
			t.Fatal(err)
		}
		if buffer {
			if len(*sent) != 1 || strings.Join((*sent)[0].Lines, "") != "- [x] task" {
				t.Fatal(*sent)
			}
		} else if disk(t, h) != strings.Replace(text, "- [ ] task", "- [x] task", 1) {
			t.Fatal(disk(t, h))
		}
	}
}

func TestUntrustedDefinitionForTerm(t *testing.T) {
	e := testEngine(t)
	// 用語の終端は次の dd の位置から決める。dd の位置が利用者の属性と区別できなければ、用語も断る。
	s, h := testService(t, e, "Apple\n: red\n", nil)
	h.src.HasSourcepos = true
	h.unposition = func() (pipeline.Result, error) {
		return pipeline.Result{HTML: `<dl><dd data-sourcepos="2:1-2:5">red</dd></dl>`, HasSourcepos: true}, nil
	}
	for _, c := range []struct{ tag, r string }{{"dt", "1:1-1:1"}, {"dd", "2:1-2:5"}} {
		if _, err := s.Text(context.Background(), nil, h.src.Render, c.tag, Range(c.r)); err == nil || err.Code != "untrusted" {
			t.Fatal(c, err)
		}
	}
	if _, err := applyJSON(t, s, h, `{"range":"1:1-1:5","text":"Pear"}`); err == nil || disk(t, h) != h.src.Text {
		t.Fatal("信用しない dd から決めた用語の範囲で書いた", err)
	}
}

func TestUntrustedRangeSharedWithRealBlock(t *testing.T) {
	e := testEngine(t)
	// 偽の段落が、本物の項目と同じ範囲を名乗る。偽の段落からは開けず、本物の項目は今までどおり直せる
	// (確定の範囲は、本物の項目の範囲そのもの)。
	text := "- item\n\n<p data-sourcepos=\"1:1-1:6\">forged</p>\n"
	s, h := testService(t, e, text, nil)
	if _, err := s.Text(context.Background(), nil, h.src.Render, "p", "1:1-1:6"); err == nil || err.Code != "untrusted" {
		t.Fatalf("偽の段落=%v\n%s", err, h.src.HTML)
	}
	// 後ろに生の HTML が続くので、項目の公開の範囲は終了列 0 になる。HTML から読む。
	public := regexp.MustCompile(`<li data-sourcepos="([^"]+)"`).FindStringSubmatch(h.src.HTML)
	if public == nil {
		t.Fatal(h.src.HTML)
	}
	value, err := s.Text(context.Background(), nil, h.src.Render, "li", Range(public[1]))
	if err != nil || value.(map[string]any)["text"] != "- item" || value.(map[string]any)["range"] != Range("1:1-1:6") {
		t.Fatal(public[1], value, err)
	}
	if _, err := applyJSON(t, s, h, `{"range":"1:1-1:6","text":"- changed"}`); err != nil {
		t.Fatal(err)
	}
	if got, want := disk(t, h), strings.Replace(text, "- item", "- changed", 1); got != want {
		t.Fatalf("原文=%q、期待=%q", got, want)
	}
}
