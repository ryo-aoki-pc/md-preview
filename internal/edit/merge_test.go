package edit

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/ryo-aoki-pc/md-tools/internal/pipeline"
	"github.com/ryo-aoki-pc/md-tools/internal/session"
)

func TestCellMergeRequestShapes(t *testing.T) {
	for _, kind := range []string{"mergeCells", "mergeCellsAsHTML", "unmergeCells"} {
		valid := map[string]any{"type": kind, "row": 0, "col": 1}
		if kind != "unmergeCells" {
			valid["rows"], valid["cols"] = 2, 3
		}
		if _, err := parseOperation(valid); err != nil {
			t.Fatal("正しい op を断った", kind, err)
		}
		copyOp := func() map[string]any {
			m := map[string]any{}
			for key, value := range valid {
				m[key] = value
			}
			return m
		}
		for key := range valid {
			m := copyOp()
			delete(m, key)
			if _, err := parseOperation(m); err == nil {
				t.Fatal("必須の項目の省略を受けた", m)
			}
		}
		for _, key := range []string{"count", "side", "to", "source", "from", "fragment", "rows", "cols"} {
			if _, exists := valid[key]; exists {
				continue
			}
			m := copyOp()
			m[key] = 1
			if _, err := parseOperation(m); err == nil {
				t.Fatal("余分な項目を受けた", m)
			}
		}
		for _, key := range []string{"row", "col", "rows", "cols"} {
			if _, exists := valid[key]; !exists {
				continue
			}
			for _, value := range []any{nil, true, "1", -1, 1.5, float64(9007199254740992)} {
				m := copyOp()
				m[key] = value
				if _, err := parseOperation(m); err == nil {
					t.Fatal("不正な座標や大きさを受けた", m)
				}
			}
			if key == "rows" || key == "cols" {
				for _, value := range []int{0, maxOperationCount + 1} {
					m := copyOp()
					m[key] = value
					if _, err := parseOperation(m); err == nil {
						t.Fatal("大きさの上限・下限を受けた", m)
					}
				}
			}
		}
		if kind != "unmergeCells" {
			m := copyOp()
			m["rows"], m["cols"] = 100, 101
			if _, err := parseOperation(m); err == nil {
				t.Fatal("セル数の上限を受けた", m)
			}
			m["cols"] = 100
			if _, err := parseOperation(m); err != nil {
				t.Fatal("セル数の上限ちょうどを断った", err)
			}
		}
	}
}

var mergeOperations = []struct{ name, source, op string }{
	{"結合", "<table>\n<tr><th>A</th><th>B</th></tr>\n<tr><td>左上</td><td>消す</td></tr>\n<tr><td>消す2</td><td>消す3</td></tr>\n</table>\n", `{"type":"mergeCells","row":1,"col":0,"rows":2,"cols":2}`},
	{"解除", "<table>\n<tr><th>A</th><th>B</th></tr>\n<tr><td rowspan='2' colspan='2' style='text-align:center'>左上</td></tr>\n<tr></tr>\n</table>\n", `{"type":"unmergeCells","row":1,"col":0}`},
	{"HTML にして結合", "| A | B |\n| --- | --- |\n| 左上 | 消す |\n| 消す2 | 消す3 |\n", `{"type":"mergeCellsAsHTML","row":1,"col":0,"rows":2,"cols":2}`},
}

func TestCellMergeWritesAndUndo(t *testing.T) {
	e := testEngine(t)
	for _, c := range mergeOperations {
		for _, ending := range []string{"\n", "\r\n"} {
			t.Run(c.name+strings.ReplaceAll(ending, "\n", "LF"), func(t *testing.T) {
				original := strings.ReplaceAll("前\n\n"+c.source+"\n後\n\n"+pipeTable, "\n", ending)
				s, h := testService(t, e, original, nil)
				info := getTables(t, s, h).Tables[0]
				if info.CellsLocked != "" || info.Kind == "html" && len(info.Sections) == 0 {
					t.Fatal("セルの操作と区画の情報が違う", info)
				}
				trials := h.trials
				if _, err := applyJSON(t, s, h, `{"table":0,"op":`+c.op+`}`); err != nil {
					t.Fatal(err)
				}
				changed := disk(t, h)
				if changed == original || !strings.HasPrefix(changed, strings.ReplaceAll("前\n\n", "\n", ending)) || !strings.HasSuffix(changed, strings.ReplaceAll("\n後\n\n"+pipeTable, "\n", ending)) {
					t.Fatal("編集か範囲外の原文が違う", changed)
				}
				if c.name == "HTML にして結合" && h.trials != trials+1 {
					t.Fatal("試しの描画が一回でない", h.trials-trials)
				}
				if !getTables(t, s, h).Tables[0].Editable {
					t.Fatal("編集後の格子が描画と一致しない")
				}
				if _, err := applyJSON(t, s, h, `{"undo":true}`); err != nil || disk(t, h) != original {
					t.Fatal("一手で戻せない", err)
				}
				if _, err := applyJSON(t, s, h, `{"undo":true}`); err == nil {
					t.Fatal("一回の操作が複数の履歴になった")
				}
				if _, err := applyJSON(t, s, h, `{"redo":true}`); err != nil || disk(t, h) != changed {
					t.Fatal("やり直せない", err)
				}
			})
		}
	}
}

func TestCellMergeBufferIsOneEdit(t *testing.T) {
	e := testEngine(t)
	for _, c := range mergeOperations {
		for _, ending := range []string{"\n", "\r\n"} {
			original := strings.ReplaceAll("前\n\n"+c.source+"\n後\n", "\n", ending)
			s, h, sent := bufferService(t, e, original)
			if _, err := applyJSON(t, s, h, `{"table":0,"op":`+c.op+`}`); err != nil || len(*sent) != 1 {
				t.Fatal("一個の置換として送れない", c.name, err, *sent)
			}
			edit := (*sent)[0]
			lines := bufferLines(original)
			text := strings.Join(append(append(append([]string{}, lines[:edit.Start]...), edit.Lines...), lines[edit.End:]...), "\n") + "\n"
			if text == original || !strings.HasPrefix(text, strings.ReplaceAll("前\n\n", "\n", ending)) || !strings.HasSuffix(text, strings.ReplaceAll("\n後\n", "\n", ending)) {
				t.Fatal("行の置換か範囲外のバイトが違う", edit)
			}
		}
	}
}

func TestCellMergeConversionTrialRejectsGridChanges(t *testing.T) {
	e := testEngine(t)
	c := mergeOperations[2]
	for _, change := range []string{"表の数", "結合", "空の行", "列", "描画失敗"} {
		t.Run(change, func(t *testing.T) {
			s, h := testService(t, e, c.source, nil)
			getTables(t, s, h)
			h.trial = func(text string) (pipeline.Result, error) {
				if change == "描画失敗" {
					return pipeline.Result{}, errors.New("試験の描画の失敗")
				}
				r, err := h.renderer.Render(context.Background(), text, session.FileContext(h.src.Root, h.src.File, "", false))
				switch change {
				case "表の数":
					r.HTML += "<table><tr><td>追加</td></tr></table>"
				case "結合":
					r.HTML = strings.ReplaceAll(r.HTML, `colspan="2"`, `colspan="1"`)
				case "空の行":
					r.HTML = regexp.MustCompile(`<tr>\s*</tr>`).ReplaceAllString(r.HTML, "")
				case "列":
					r.HTML = strings.Replace(r.HTML, "</tr>", "<td>追加</td></tr>", 1)
				}
				return r, err
			}
			if _, err := applyJSON(t, s, h, `{"table":0,"op":`+c.op+`}`); err == nil || err.Status != 422 {
				t.Fatal("変わった格子の描画を受けた", err)
			}
			if disk(t, h) != c.source || len(s.history.documents) != 0 {
				t.Fatal("試しの描画で断った後に書いた")
			}
		})
	}
}

func TestCellsLockedAndCoveredCellReasons(t *testing.T) {
	e := testEngine(t)
	for _, source := range []string{
		"<table><tr><td colspan='0'>A</td><td>B</td></tr></table>\n",
		"<table><tr><td>A<td>B</tr></table>\n",
	} {
		s, h := testService(t, e, source, nil)
		info := getTables(t, s, h).Tables[0]
		if !info.Editable || info.CellsLocked == "" {
			t.Fatal("セルの形を変えられない理由が無い", info)
		}
		for _, op := range []string{`{"type":"mergeCells","row":0,"col":0,"rows":1,"cols":2}`, `{"type":"unmergeCells","row":0,"col":0}`} {
			if _, err := applyJSON(t, s, h, `{"table":0,"op":`+op+`}`); err == nil || disk(t, h) != source {
				t.Fatal("セルの操作を断らなかった", err)
			}
		}
	}
	s, h := testService(t, e, "<table><tr><td>A</td><td>\n\n[ref]: /example\n\nB\n\n</td></tr></table>\n", nil)
	info := getTables(t, s, h).Tables[0]
	if len(info.Cells) != 2 || info.Cells[1].MergeLocked == "" || info.Cells[0].MergeLocked != "" {
		t.Fatal("覆われるセルを断る理由が無い", info)
	}
}
