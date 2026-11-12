package edit

import (
	"context"
	"strings"
	"testing"
)

func TestCellFormatRequestShapes(t *testing.T) {
	for _, kind := range []string{"setAlign", "setHeader"} {
		valid := map[string]any{"type": kind, "row": 0, "col": 1, "rows": 2, "cols": 3}
		key, values := "align", []any{"", "left", "center", "right"}
		invalid := []any{nil, true, 0, "justify", "RIGHT", "center ", []any{}}
		if kind == "setHeader" {
			key, values = "header", []any{true, false}
			invalid = []any{nil, "true", 0, 1, []any{}}
		}
		for _, value := range values {
			valid[key] = value
			if _, err := parseOperation(valid); err != nil {
				t.Fatal("正しい op を断った", valid, err)
			}
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
		for _, name := range []string{"align", "header", "count", "side", "to", "source", "from", "fragment"} {
			if name == key {
				continue
			}
			m := copyOp()
			m[name] = 1
			if _, err := parseOperation(m); err == nil {
				t.Fatal("余分な項目を受けた", m)
			}
		}
		for _, value := range invalid {
			m := copyOp()
			m[key] = value
			if _, err := parseOperation(m); err == nil {
				t.Fatal("不正な値を受けた", m)
			}
		}
		for _, name := range []string{"row", "col", "rows", "cols"} {
			for _, value := range []any{nil, true, "1", -1, 1.5, float64(9007199254740992)} {
				m := copyOp()
				m[name] = value
				if _, err := parseOperation(m); err == nil {
					t.Fatal("不正な範囲を受けた", m)
				}
			}
			if name == "rows" || name == "cols" {
				for _, value := range []int{0, maxOperationCount + 1} {
					m := copyOp()
					m[name] = value
					if _, err := parseOperation(m); err == nil {
						t.Fatal("大きさの上限・下限を受けた", m)
					}
				}
			}
		}
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

var formatOperations = []struct{ name, source, op, want string }{
	{"列の揃え", "| A | B |\n| :----- | ---: |\n| 値 | 外 |\n", `{"type":"setAlign","row":1,"col":0,"rows":1,"cols":1,"align":"center"}`, "| A | B |\n| :-----: | ---: |\n| 値 | 外 |\n"},
	{"align", "<table>\n<tr><td align='left'>値</td><td>外</td></tr>\n</table>\n", `{"type":"setAlign","row":0,"col":0,"rows":1,"cols":1,"align":"right"}`, "<table>\n<tr><td align='right'>値</td><td>外</td></tr>\n</table>\n"},
	{"style", "<table>\n<tr><td style='text-align:left;'>値</td><td>外</td></tr>\n</table>\n", `{"type":"setAlign","row":0,"col":0,"rows":1,"cols":1,"align":"center"}`, "<table>\n<tr><td style='text-align: center;'>値</td><td>外</td></tr>\n</table>\n"},
	{"既定", "<table>\n<tr><td align='left' style='text-align: right'>値</td><td>外</td></tr>\n</table>\n", `{"type":"setAlign","row":0,"col":0,"rows":1,"cols":1,"align":""}`, "<table>\n<tr><td>値</td><td>外</td></tr>\n</table>\n"},
	{"見出しセル", "<table>\n<tr><td colspan='2' title='保持'>値</td><td>外</td></tr>\n</table>\n", `{"type":"setHeader","row":0,"col":0,"rows":1,"cols":2,"header":true}`, "<table>\n<tr><th colspan='2' title='保持'>値</th><td>外</td></tr>\n</table>\n"},
	{"通常のセル", "<table>\n<tr><th title='保持'>値</th><td>外</td></tr>\n</table>\n", `{"type":"setHeader","row":0,"col":0,"rows":1,"cols":1,"header":false}`, "<table>\n<tr><td title='保持'>値</td><td>外</td></tr>\n</table>\n"},
}

func TestCellFormatWritesUndoAndBuffer(t *testing.T) {
	e := testEngine(t)
	for _, c := range formatOperations {
		for _, ending := range []string{"\n", "\r\n"} {
			t.Run(c.name+strings.ReplaceAll(ending, "\n", "LF"), func(t *testing.T) {
				original := strings.ReplaceAll("前\n\n"+c.source+"\n後\n\n"+pipeTable, "\n", ending)
				want := strings.ReplaceAll("前\n\n"+c.want+"\n後\n\n"+pipeTable, "\n", ending)
				s, h := testService(t, e, original, nil)
				if _, err := applyJSON(t, s, h, `{"table":0,"op":`+c.op+`}`); err != nil || disk(t, h) != want {
					t.Fatal("書き換えのバイトが違う", err, disk(t, h))
				}
				if !getTables(t, s, h).Tables[0].Editable {
					t.Fatal("編集後の格子が描画と一致しない")
				}
				if _, err := applyJSON(t, s, h, `{"undo":true}`); err != nil || disk(t, h) != original {
					t.Fatal("一手で戻せない", err)
				}
				if _, err := applyJSON(t, s, h, `{"undo":true}`); err == nil {
					t.Fatal("一個の操作が複数の履歴になった")
				}
				if _, err := applyJSON(t, s, h, `{"redo":true}`); err != nil || disk(t, h) != want {
					t.Fatal("やり直せない", err)
				}
				s, h, sent := bufferService(t, e, original)
				if _, err := applyJSON(t, s, h, `{"table":0,"op":`+c.op+`}`); err != nil || len(*sent) != 1 {
					t.Fatal("一個の置換として送れない", err, *sent)
				}
				edit := (*sent)[0]
				lines := bufferLines(original)
				text := strings.Join(append(append(append([]string{}, lines[:edit.Start]...), edit.Lines...), lines[edit.End:]...), "\n") + "\n"
				if text != want || edit.End-edit.Start != 1 || len(edit.Lines) != 1 {
					t.Fatal("一行の置換が違う", edit, text)
				}
			})
		}
	}
}

func TestCellFormatUnchangedHasNoHistory(t *testing.T) {
	e := testEngine(t)
	for _, op := range []string{`{"type":"setAlign","row":1,"col":0,"rows":1,"cols":1,"align":"left"}`, `{"type":"setHeader","row":1,"col":0,"rows":1,"cols":1,"header":false}`} {
		s, h := testService(t, e, pipeTable, nil)
		if strings.Contains(op, "setHeader") {
			s, h = testService(t, e, "<table><tr><th>A</th></tr><tr><td>B</td></tr></table>\n", nil)
		}
		v, err := applyJSON(t, s, h, `{"table":0,"op":`+op+`}`)
		if err != nil || v.(map[string]any)["unchanged"] != true || len(s.history.documents) != 0 {
			t.Fatal("変更のない操作が履歴を作った", v, err)
		}
		if _, err := s.Apply(context.Background(), nil, map[string]any{"undo": true}); err == nil {
			t.Fatal("変更のない操作を取り消せた")
		}
	}
}
