package edit

import (
	"strings"
	"testing"
)

func TestSortRowsRequestShape(t *testing.T) {
	valid := map[string]any{"type": "sortRows", "row": 0, "col": 1, "order": "asc"}
	copyOp := func() map[string]any {
		m := map[string]any{}
		for key, value := range valid {
			m[key] = value
		}
		return m
	}
	for _, order := range []string{"asc", "desc"} {
		valid["order"] = order
		if _, err := parseOperation(valid); err != nil {
			t.Fatal("正しい並べ替えを断った", err)
		}
	}
	for name := range valid {
		m := copyOp()
		delete(m, name)
		if _, err := parseOperation(m); err == nil {
			t.Fatal("項目の省略を受けた", m)
		}
	}
	for _, name := range []string{"count", "side", "rows", "cols", "align", "header", "source", "from", "to", "fragment", "values"} {
		m := copyOp()
		m[name] = 0
		if _, err := parseOperation(m); err == nil {
			t.Fatal("余分な項目を受けた", m)
		}
	}
	for _, order := range []any{"", "ASC", "ascending", "desc ", nil, true, 1, []any{}} {
		m := copyOp()
		m["order"] = order
		if _, err := parseOperation(m); err == nil {
			t.Fatal("不正な向きを受けた", m)
		}
	}
	for _, name := range []string{"row", "col"} {
		for _, value := range []any{-1, 1.5, "1", nil, true, float64(9007199254740992)} {
			m := copyOp()
			m[name] = value
			if _, err := parseOperation(m); err == nil {
				t.Fatal("不正な位置を受けた", m)
			}
		}
	}
}

func TestSortRowsWritesUndoAndBuffer(t *testing.T) {
	e := testEngine(t)
	for _, c := range []struct {
		name, source, asc, desc string
		row                     int
	}{
		{"パイプ", "| 鍵 | 内容 |\n| :----- | ----: |\n| 10 | 十 |\n| 2 | 二 |\n| | 空 |\n", "| 鍵 | 内容 |\n| :----- | ----: |\n| 2 | 二 |\n| 10 | 十 |\n| | 空 |\n", "| 鍵 | 内容 |\n| :----- | ----: |\n| 10 | 十 |\n| 2 | 二 |\n| | 空 |\n", 1},
		{"HTML", "<table>\n<thead><tr><th>鍵</th></tr></thead>\n<tbody><tr title='十'><td>10</td></tr><tr><td>2</td></tr><tr><td></td></tr></tbody>\n<tfoot><tr><td>外</td></tr></tfoot>\n</table>\n", "<table>\n<thead><tr><th>鍵</th></tr></thead>\n<tbody><tr><td>2</td></tr><tr title='十'><td>10</td></tr><tr><td></td></tr></tbody>\n<tfoot><tr><td>外</td></tr></tfoot>\n</table>\n", "<table>\n<thead><tr><th>鍵</th></tr></thead>\n<tbody><tr title='十'><td>10</td></tr><tr><td>2</td></tr><tr><td></td></tr></tbody>\n<tfoot><tr><td>外</td></tr></tfoot>\n</table>\n", 1},
	} {
		for _, ending := range []string{"\n", "\r\n"} {
			t.Run(c.name+strings.ReplaceAll(ending, "\n", "LF"), func(t *testing.T) {
				wrap := func(body string) string {
					return strings.ReplaceAll("前\n\n"+body+"\n後\n\n"+pipeTable, "\n", ending)
				}
				original, want := wrap(c.source), wrap(c.asc)
				op := `{"table":0,"op":{"type":"sortRows","row":1,"col":0,"order":"asc"}}`
				s, h := testService(t, e, original, nil)
				if _, err := applyJSON(t, s, h, op); err != nil || disk(t, h) != want {
					t.Fatal("並べ替えのバイトが違う", err, disk(t, h))
				}
				if !getTables(t, s, h).Tables[0].Editable {
					t.Fatal("並べ替え後の格子を対応づけられない")
				}
				if _, err := applyJSON(t, s, h, `{"undo":true}`); err != nil || disk(t, h) != original {
					t.Fatal("一手で戻らない", err)
				}
				if _, err := applyJSON(t, s, h, `{"undo":true}`); err == nil {
					t.Fatal("並べ替えが複数の履歴になった")
				}
				if _, err := applyJSON(t, s, h, `{"redo":true}`); err != nil || disk(t, h) != want {
					t.Fatal("やり直せない", err)
				}
				if _, err := applyJSON(t, s, h, strings.Replace(op, `"asc"`, `"desc"`, 1)); err != nil || disk(t, h) != wrap(c.desc) {
					t.Fatal("降順で並べ替えられない", err, disk(t, h))
				}
				s, h, sent := bufferService(t, e, original)
				if _, err := applyJSON(t, s, h, op); err != nil || len(*sent) != 1 {
					t.Fatal("一個の行の置き換えとして送れない", err, *sent)
				}
				edit := (*sent)[0]
				lines := bufferLines(original)
				text := strings.Join(append(append(append([]string{}, lines[:edit.Start]...), edit.Lines...), lines[edit.End:]...), "\n") + "\n"
				if text != want {
					t.Fatal("行の置き換えが違う", edit, text)
				}
			})
		}
	}
}

func TestSortRowsReasonsAndUnchanged(t *testing.T) {
	e := testEngine(t)
	for _, c := range []struct {
		source, reason string
		row            int
	}{
		{"| 鍵 |\n| --- |\n| 1 |\n| 2 |\n", "", 1},
		{"| 鍵 |\n| --- |\n| 1 |\n", "2 行未満", 1},
		{"<table><thead><tr><th>鍵</th></tr></thead><tbody><tr><td>1</td></tr><tr><td>2</td></tr></tbody></table>\n", "見出しの区画", 0},
		{"<table><tr><td rowspan='2'>1</td><td>外</td></tr><tr><td>外</td></tr></table>\n", "行の結合", 1},
		{"<table><tr><td colspan='0'>1</td></tr><tr><td>2</td></tr></table>\n", "結合の値", 0},
	} {
		s, h := testService(t, e, c.source, nil)
		info := getTables(t, s, h).Tables[0]
		if len(info.SortLocked) != info.Rows || !strings.Contains(info.SortLocked[c.row], c.reason) {
			t.Fatal("理由が届かない", info)
		}
		m := map[string]any{"render": h.src.Render, "table": 0, "op": map[string]any{"type": "sortRows", "row": c.row, "col": 0, "order": "asc"}}
		v, err := s.Apply(t.Context(), nil, m)
		if c.reason != "" {
			if err == nil || !strings.Contains(err.Message, c.reason) {
				t.Fatal("断る理由が違う", err)
			}
		} else if err != nil || v.(map[string]any)["unchanged"] != true {
			t.Fatal("整列済みを変えた", v, err)
		}
		if disk(t, h) != c.source || len(s.history.documents) != 0 {
			t.Fatal("変更しない操作が原文か履歴を変えた")
		}
	}
}
