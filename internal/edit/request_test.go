package edit

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/ryo-aoki-pc/md-tools/internal/pipeline"
	"github.com/ryo-aoki-pc/md-tools/mdtable/tableedit"
)

func TestInvalidRequestsDoNotWrite(t *testing.T) {
	e := testEngine(t)
	s, h := testService(t, e, pipeTable, nil)
	for _, body := range []string{
		`{}`, `{"render":"1","table":0,"op":{}}`, `{"render":1,"range":"1:1-1:1","text":true}`, `{"render":1,"checkbox":"1:1-1:1","checked":"true"}`, `{"undo":false}`, `{"undo":true,"redo":true}`,
		`{"render":1,"table":0,"op":{"type":"set","cells":[{"row":true,"col":0,"text":"変更"}]}}`,
		`{"render":1,"table":0,"op":{"type":"paste","row":0,"col":0,"values":[[true]]}}`,
		`{"render":1,"table":0,"op":{"type":"insertRows","row":0,"side":"over","count":1}}`,
		`{"render":1,"table":0,"op":{"type":"deleteColumns","col":0,"count":0}}`,
		`{"render":1,"table":0,"op":{"type":"setSource","row":0,"col":0,"source":null}}`,
		`{"render":1,"range":"1:1-1:1","text":"変更","path":"outside.md"}`,
		// 同じ行で終了列が 0 の範囲は、開始より前になる。
		`{"render":1,"range":"1:1-1:0","text":"変更"}`,
		`{"render":1,"checkbox":"1:3-1:0","checked":true}`,
		// 画面の操作では届かない個数は断る。
		`{"render":1,"table":0,"op":{"type":"insertRows","row":1,"side":"after","count":1001}}`,
		`{"render":1,"table":0,"op":{"type":"deleteColumns","col":0,"count":9007199254740991}}`,
		`{"render":1,"table":0,"op":{"type":"set","cells":[` + strings.TrimSuffix(strings.Repeat(`{"row":1,"col":0,"text":"x"},`, 10001), ",") + `]}}`,
		`{"render":1,"table":0,"op":{"type":"paste","row":0,"col":0,"values":[` + strings.TrimSuffix(strings.Repeat(`["a","b","c","d","e"],`, 2001), ",") + `]}}`,
	} {
		if _, err := applyJSON(t, s, h, body); err == nil || err.Status != 400 {
			t.Errorf("%s: %v", body, err)
		}
	}
	if disk(t, h) != pipeTable {
		t.Fatal("不正な要求を書き込んだ")
	}
}

func TestInvalidFragmentRequestsDoNotWrite(t *testing.T) {
	e := testEngine(t)
	s, h := testService(t, e, pipeTable, nil)
	f, err := tableedit.ValuesFragment([][]string{{"値"}})
	if err != nil {
		t.Fatal(err)
	}
	valid := jsonObject(t, f)
	mutations := []func(map[string]any){
		func(m map[string]any) { m["unknown"] = true },
		func(m map[string]any) { m["rows"] = "1" },
		func(m map[string]any) { m["rows"] = 0 },
		func(m map[string]any) { m["rows"] = 1001 },
		func(m map[string]any) { m["cols"] = 1001 },
		func(m map[string]any) { m["rows"], m["cols"] = 73, 137 },
		func(m map[string]any) { m["row_tags"] = []any{"<tr>", "<tr>"} },
		func(m map[string]any) { m["delimiters"] = []any{true} },
		func(m map[string]any) { m["cells"] = true },
		func(m map[string]any) { m["cells"].([]any)[0].(map[string]any)["unknown"] = true },
		func(m map[string]any) { m["cells"].([]any)[0].(map[string]any)["row"] = false },
		func(m map[string]any) { m["cells"].([]any)[0].(map[string]any)["source"] = 3 },
	}
	for i, mutate := range mutations {
		m := jsonObject(t, valid)
		mutate(m)
		_, err := s.Apply(context.Background(), nil, jsonObject(t, map[string]any{"render": 1, "table": 0, "op": map[string]any{"type": "pasteFragment", "row": 1, "col": 0, "fragment": m}}))
		if err == nil || err.Status != 400 {
			t.Errorf("写し%d: %v", i, err)
		}
	}
	from := `{"table":0,"row":1,"count":1,"render":1,"hash":"` + strings.Repeat("a", 64) + `","origin":"` + strings.Repeat("b", 32) + `"}`
	for _, op := range []string{
		`{"type":"duplicateRows","row":0,"count":0}`,
		`{"type":"duplicateColumns","col":0,"count":1001}`,
		`{"type":"insertRowsFrom","row":0,"side":"over","values":[["x"]]}`,
		`{"type":"insertRowsFrom","row":0,"side":"after","values":[["x"]],"fragment":{}}`,
		`{"type":"insertColumnsFrom","col":0,"side":"before","fragment":false}`,
		`{"type":"pasteFragment","row":0,"col":0,"fragment":"{}"}`,
		`{"type":"moveRows","row":1,"side":"after","from":` + strings.Replace(from, `"hash":"`+strings.Repeat("a", 64)+`",`, "", 1) + `}`,
		`{"type":"moveRows","row":1,"side":"after","from":` + strings.Replace(from, strings.Repeat("a", 64), strings.Repeat("a", 63), 1) + `}`,
		`{"type":"moveRows","row":1,"side":"after","from":` + strings.Replace(from, strings.Repeat("b", 32), strings.Repeat("z", 32), 1) + `}`,
		`{"type":"moveRows","row":1,"side":"after","from":` + strings.Replace(from, `"count":1`, `"count":1001`, 1) + `}`,
		`{"type":"moveRows","row":1,"side":"after","from":` + strings.Replace(from, `"table":0`, `"table":0,"unknown":true`, 1) + `}`,
		`{"type":"moveCells","row":0,"col":0,"from":{"table":0,"row":0,"col":0,"rows":101,"cols":100,"render":1,"hash":"` + strings.Repeat("a", 64) + `","origin":"` + strings.Repeat("b", 32) + `"}}`,
		`{"type":"insertRowsFrom","row":0,"side":"after","values":[` + strings.TrimSuffix(strings.Repeat(`["x"],`, 1001), ",") + `]}`,
	} {
		if _, err := applyJSON(t, s, h, fmt.Sprintf(`{"table":0,"op":%s}`, op)); err == nil || err.Status != 400 {
			t.Errorf("%s: %v", op, err)
		}
	}
	if disk(t, h) != pipeTable || len(s.history.documents) != 0 {
		t.Fatal("不正な要求を書き込んだ")
	}
}

// 生の HTML の表に、パイプの表の行を指す data-sourcepos を書いても、編集が別の表へ行かない。
func TestForgedSourceposDoesNotCrossTables(t *testing.T) {
	e := testEngine(t)
	forged := strings.Replace(htmlTable, "<table>", `<table data-sourcepos="1:1-3:16">`, 1)
	s, h := testService(t, e, pipeTable+"\n"+forged, nil)
	r := getTables(t, s, h)
	if len(r.Tables) != 2 || r.Tables[0].Editable || r.Tables[0].Kind != "" || !r.Tables[1].Editable || r.Tables[1].Kind != "html" {
		t.Fatalf("偽の位置属性で対応が変わった: %+v", r.Tables)
	}
	if _, err := applyJSON(t, s, h, `{"table":1,"op":{"type":"set","cells":[{"row":0,"col":1,"text":"変更"}]}}`); err != nil {
		t.Fatal(err)
	}
	if got := disk(t, h); !strings.HasPrefix(got, pipeTable) || !strings.Contains(got, "<td>変更</td>") {
		t.Fatalf("HTML の表への編集が別の場所に書かれた: %s", got)
	}
	// 対象外の HTML の表（tfoot が tbody の前）には印が付かない。偽の位置属性がパイプの表を指すと、
	// 本物も同じ位置属性なので、どちらも編集させない。
	excluded := `<table data-sourcepos="1:1-3:16"><tfoot><tr><td>F</td><td>G</td></tr></tfoot><tbody><tr><td>C</td><td>D</td></tr></tbody></table>` + "\n"
	s, h = testService(t, e, pipeTable+"\n"+excluded, nil)
	r = getTables(t, s, h)
	if len(r.Tables) != 2 || r.Tables[0].Editable || r.Tables[1].Editable {
		t.Fatalf("同じ原文を指す表が編集できる: %+v", r.Tables)
	}
	if _, err := applyJSON(t, s, h, `{"table":1,"op":{"type":"set","cells":[{"row":0,"col":0,"text":"変更"}]}}`); err == nil || err.Status != 422 {
		t.Fatal(err)
	}
	if disk(t, h) != pipeTable+"\n"+excluded {
		t.Fatal("編集できない表へ書き込んだ")
	}
}
func TestRechecksBeforeWriting(t *testing.T) {
	e := testEngine(t)
	for _, kind := range []string{"changed", "stale"} {
		t.Run(kind, func(t *testing.T) {
			s, h := testService(t, e, "本文", nil)
			h.trial = func(string) (pipeline.Result, error) {
				if kind == "changed" {
					if err := os.WriteFile(h.src.Path, []byte("外部"), 0644); err != nil {
						t.Fatal(err)
					}
				} else {
					h.src.Stale = true
				}
				return pipeline.Result{}, nil
			}
			if _, err := applyJSON(t, s, h, `{"range":"1:1-1:6","text":"変更"}`); err == nil || err.Code != kind {
				t.Fatal(err)
			}
			if strings.Contains(disk(t, h), "変更") {
				t.Fatal("再検査前に書き込んだ")
			}
		})
	}
}
func TestMissingBufferAndReload(t *testing.T) {
	if _, e := New(Options{}).Tables(context.Background(), nil, 1); e == nil || e.Status != 404 {
		t.Fatal(e)
	}
	e := testEngine(t)
	s, h := testService(t, e, "本文", nil)
	s.opts.Mode = Buffer
	if getTables(t, s, h).Document.Editable {
		t.Fatal("未対応のバッファを許した")
	}
	s.opts.Mode = File
	h.src.Current = false
	if _, err := applyJSON(t, s, h, `{"range":"1:1-1:6","text":"変更"}`); err != nil {
		t.Fatal(err)
	}
	if h.reloads != 1 {
		t.Fatal("ファイル文書を読み直さない")
	}
}
func TestTableErrorStatuses(t *testing.T) {
	for _, c := range []struct {
		code   string
		status int
	}{{"stale", 409}, {"invalid", 422}, {"merged", 422}, {"range", 422}, {"unsupported", 422}, {"excluded", 422}, {"verify", 422}, {"internal", 500}, {"write", 500}} {
		e := tableError(&tableedit.Error{Code: c.code, Message: "日本語の誤り"})
		if e.Status != c.status || e.Code != c.code || e.Message != "日本語の誤り" {
			t.Fatal(e)
		}
	}
}
func TestMultipleShapeEditsAndCancellation(t *testing.T) {
	e := testEngine(t)
	s, h := testService(t, e, htmlTable, nil)
	if _, err := applyJSON(t, s, h, `{"table":0,"op":{"type":"insertRows","row":1,"side":"after","count":2}}`); err != nil {
		t.Fatal(err)
	}
	if getTables(t, s, h).Tables[0].Rows != 4 {
		t.Fatal("複数行を追加しない")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	before := disk(t, h)
	if _, err := s.Apply(ctx, nil, map[string]any{"render": h.src.Render, "table": 0, "op": map[string]any{"type": "insertRows", "row": 1, "side": "after", "count": 2}}); err == nil {
		t.Fatal("取り消された編集を許した")
	}
	if disk(t, h) != before {
		t.Fatal("取り消した本文を書いた")
	}
}
