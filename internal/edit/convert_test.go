package edit

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ryo-aoki-pc/md-tools/internal/pipeline"
	"github.com/ryo-aoki-pc/md-tools/internal/session"
)

func TestTableConvertDeleteRequestShapes(t *testing.T) {
	e := testEngine(t)
	s, h := testService(t, e, pipeTable, nil)
	for _, op := range []string{
		`{"type":"convert"}`, `{"type":"convert","to":true}`, `{"type":"convert","to":null}`,
		`{"type":"convert","to":"HTML"}`, `{"type":"convert","to":"html","row":0}`,
		`{"type":"deleteTable","to":"pipe"}`, `{"type":"deleteTable","count":1}`,
	} {
		if _, err := applyJSON(t, s, h, `{"table":0,"op":`+op+`}`); err == nil || err.Code != "invalid" {
			t.Fatalf("不正な op を受けた: %s %v", op, err)
		}
	}
	if disk(t, h) != pipeTable || len(s.history.documents) != 0 {
		t.Fatal("不正な要求で文書や履歴を変えた")
	}
}

func TestTableConvertDeleteWritesAndUndo(t *testing.T) {
	e := testEngine(t)
	for _, to := range []string{"html", "pipe", "deleteTable"} {
		for _, ending := range []string{"\n", "\r\n"} {
			t.Run(to+strings.ReplaceAll(ending, "\n", "LF"), func(t *testing.T) {
				table := pipeTable
				if to == "pipe" {
					table = "<table><tr><th>見出し</th><th>二列</th></tr><tr><td>本文</td><td>値</td></tr></table>\n"
				}
				original := strings.ReplaceAll("前\n\n"+table+"\n後\n\n"+pipeTable, "\n", ending)
				s, h := testService(t, e, original, nil)
				info := getTables(t, s, h).Tables[0]
				if to != "deleteTable" && (info.ConvertLocked[to] != "" || info.ConvertLocked[string(info.Kind)] == "") || to == "deleteTable" && info.DeleteLocked != "" {
					t.Fatalf("操作できる表を断った: %+v", info)
				}
				op := `{"type":"convert","to":"` + to + `"}`
				if to == "deleteTable" {
					op = `{"type":"deleteTable"}`
				}
				trials := h.trials
				if _, err := applyJSON(t, s, h, `{"table":0,"op":`+op+`}`); err != nil {
					t.Fatal(err)
				}
				changed := disk(t, h)
				if changed == original || !strings.HasSuffix(changed, strings.ReplaceAll("後\n\n"+pipeTable, "\n", ending)) {
					t.Fatalf("変更か範囲外のバイトが違う: %q", changed)
				}
				if to != "deleteTable" && h.trials != trials+1 {
					t.Fatalf("変換後の試しの描画が一回でない: %d", h.trials-trials)
				}
				if to == "deleteTable" && changed != strings.ReplaceAll("前\n\n後\n\n"+pipeTable, "\n", ending) {
					t.Fatalf("削除と空行の整理: %q", changed)
				}
				if _, err := applyJSON(t, s, h, `{"undo":true}`); err != nil || disk(t, h) != original {
					t.Fatal("一手で元へ戻せない", err)
				}
				if _, err := applyJSON(t, s, h, `{"undo":true}`); err == nil {
					t.Fatal("一個の op が複数の履歴になった")
				}
				if _, err := applyJSON(t, s, h, `{"redo":true}`); err != nil || disk(t, h) != changed {
					t.Fatal("やり直せない", err)
				}
			})
		}
	}
}

func TestTableConversionTrialRejectsChanges(t *testing.T) {
	e := testEngine(t)
	for _, to := range []string{"html", "pipe"} {
		for _, change := range []string{"表の数", "列数", "行数", "左上", "タスク", "描画失敗"} {
			t.Run(to+change, func(t *testing.T) {
				original := pipeTable
				if to == "pipe" {
					original = "<table><tr><th>見出し</th><th>二列</th></tr><tr><td>本文</td><td>値</td></tr></table>\n"
				}
				s, h := testService(t, e, original, nil)
				getTables(t, s, h)
				h.trial = func(text string) (pipeline.Result, error) {
					if change == "描画失敗" {
						return pipeline.Result{}, errors.New("試験の描画の失敗")
					}
					r, err := h.renderer.Render(context.Background(), text, session.FileContext(h.src.Root, h.src.File, "", false))
					switch change {
					case "表の数":
						r.HTML += "<table><tr><td>追加</td></tr></table>"
					case "列数":
						r.HTML = strings.Replace(r.HTML, "</tr>", "<td>追加</td></tr>", 1)
					case "行数":
						r.HTML = strings.Replace(r.HTML, "</table>", "<tr><td>追加</td><td>追加</td></tr></table>", 1)
					case "左上":
						r.HTML = strings.Replace(r.HTML, "見出し", "変更した文字", 1)
					case "タスク":
						r.HTML = strings.Replace(r.HTML, "</td>", `<input type="checkbox"></td>`, 1)
					}
					return r, err
				}
				if _, err := applyJSON(t, s, h, `{"table":0,"op":{"type":"convert","to":"`+to+`"}}`); err == nil || err.Status != 422 {
					t.Fatalf("描画の変化を受けた: %v", err)
				}
				if disk(t, h) != original || len(s.history.documents) != 0 {
					t.Fatal("断った変換でファイルや履歴を変えた")
				}
			})
		}
	}
}

func TestTableConvertDeleteBufferIsOneEdit(t *testing.T) {
	e := testEngine(t)
	for _, op := range []string{`{"type":"convert","to":"html"}`, `{"type":"convert","to":"pipe"}`, `{"type":"deleteTable"}`} {
		for _, ending := range []string{"\n", "\r\n"} {
			table := pipeTable
			if strings.Contains(op, `"to":"pipe"`) {
				table = "<table><tr><th>見出し</th><th>二列</th></tr><tr><td>本文</td><td>値</td></tr></table>\n"
			}
			original := strings.ReplaceAll("前\n\n"+table+"\n後\n", "\n", ending)
			s, h, sent := bufferService(t, e, original)
			if _, err := applyJSON(t, s, h, `{"table":0,"op":`+op+`}`); err != nil || len(*sent) != 1 {
				t.Fatal("一個の置換として送れない", err, *sent)
			}
			edit := (*sent)[0]
			lines := bufferLines(original)
			text := strings.Join(append(append(append([]string{}, lines[:edit.Start]...), edit.Lines...), lines[edit.End:]...), "\n") + "\n"
			if strings.Contains(op, "deleteTable") {
				if text != strings.ReplaceAll("前\n\n後\n", "\n", ending) {
					t.Fatalf("削除の置換: %+v", edit)
				}
			} else if strings.Contains(text, "<table>") != strings.Contains(op, `"to":"html"`) || !strings.HasPrefix(text, strings.ReplaceAll("前\n\n", "\n", ending)) || !strings.HasSuffix(text, strings.ReplaceAll("\n後\n", "\n", ending)) {
				t.Fatalf("変換の置換: %+v", edit)
			}
		}
	}
}

func TestTableConversionNoopAndLockedReasons(t *testing.T) {
	e := testEngine(t)
	s, h := testService(t, e, pipeTable, nil)
	getTables(t, s, h)
	trials := h.trials
	v, err := applyJSON(t, s, h, `{"table":0,"op":{"type":"convert","to":"pipe"}}`)
	if err != nil || v.(map[string]any)["unchanged"] != true || h.trials != trials || len(s.history.documents) != 0 {
		t.Fatal("同じ種類への変換で本文・描画・履歴を変えた", v, err)
	}
	for _, c := range []struct{ text, op, locked string }{
		{"| 見出し |\n| --- |\n| [ ] |\n", `{"type":"convert","to":"html"}`, "convert"},
		{"<table><tr><th>見出し</th></tr><tr><td>[X]</td></tr></table>\n", `{"type":"convert","to":"pipe"}`, "convert"},
		{"- | 見出し |\n  | --- |\n  | 本文 |\n", `{"type":"deleteTable"}`, "delete"},
		// 前後の解釈が変わる削除は、表情報では断らず、削除のときに断る。
		{"- 項目\n\n  " + strings.ReplaceAll(strings.TrimSuffix(pipeTable, "\n"), "\n", "\n  ") + "\n", `{"type":"deleteTable"}`, ""},
	} {
		s, h := testService(t, e, c.text, nil)
		info := getTables(t, s, h).Tables[0]
		if c.locked == "delete" && info.DeleteLocked == "" || c.locked == "convert" && info.ConvertLocked[map[bool]string{true: "pipe", false: "html"}[info.Kind == "html"]] == "" {
			t.Fatalf("無効の理由が無い: %+v", info)
		}
		if _, err := applyJSON(t, s, h, `{"table":0,"op":`+c.op+`}`); err == nil || disk(t, h) != c.text {
			t.Fatal("断る形の表を編集した", err)
		}
	}
}
