package edit

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ryo-aoki-pc/md-tools/engine"
	"github.com/ryo-aoki-pc/md-tools/internal/htmltree"
	"github.com/ryo-aoki-pc/md-tools/internal/session"
	"golang.org/x/net/html"
)

func checkboxRanges(t *testing.T, h *testHost) []Range {
	t.Helper()
	root, err := htmltree.Fragment(context.Background(), h.src.HTML)
	if err != nil {
		t.Fatal(err)
	}
	nodes, err := htmltree.Elements(context.Background(), root, func(n *html.Node) bool {
		return n.Data == "input" && htmltree.HasClass(n, "task-list-item-checkbox") && htmltree.Has(n, "data-checkbox-sourcepos")
	})
	if err != nil {
		t.Fatal(err)
	}
	ranges := []Range{}
	for _, n := range nodes {
		ranges = append(ranges, Range(htmltree.Get(n, "data-checkbox-sourcepos")))
	}
	return ranges
}

func checkboxService(t *testing.T, e *engine.Engine, text string, mode Mode, files map[string]string) (*Service, *testHost, *[]BufferEdit) {
	t.Helper()
	if mode == File {
		s, h := testService(t, e, text, files)
		return s, h, nil
	}
	s, h, sent := bufferService(t, e, text)
	for name, contents := range files {
		if err := os.WriteFile(filepath.Join(h.src.Root, name), []byte(contents), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if len(files) > 0 {
		result, err := h.renderer.Render(context.Background(), text, session.FileContext(h.src.Root, h.src.File, "", false))
		if err != nil {
			t.Fatal(err)
		}
		h.src.HTML, h.src.Lines = result.HTML, result.Source
		h.src.HasSourcepos, h.src.SourceposInput = result.HasSourcepos, result.SourceposInput
	}
	return s, h, sent
}

func TestTableAndFootnoteCheckboxWrites(t *testing.T) {
	e := testEngine(t)
	for _, c := range []struct{ name, text string }{
		{"HTMLの本文セル", "<table><tbody><tr><td>\n\n- [ ] 作業\n\n</td></tr></tbody></table>\n"},
		{"HTMLの見出しセル", "<table><thead><tr><th>\n\n- [ ] 作業\n\n</th></tr></thead><tbody><tr><td>値</td></tr></tbody></table>\n"},
		{"パイプの表", "| 列 |\n| --- |\n| [ ] |\n"},
		{"脚注", "本文[^a]\n\n[^a]: 定義\n\n    - [ ] 作業\n"},
		{"脚注の表", "本文[^a]\n\n[^a]: 定義\n\n    | 列 |\n    | --- |\n    | [ ] |\n"},
		{"先行する表とセル内の二番目", pipeTable + "\n<table><tr><td>\n\n- [ ] 前\n- [ ] 作業\n\n</td></tr></table>\n"},
	} {
		for _, mode := range []Mode{File, Buffer} {
			for _, ending := range []string{"\n", "\r\n"} {
				t.Run(fmt.Sprintf("%s/%v/%q", c.name, mode, ending), func(t *testing.T) {
					text := strings.ReplaceAll(c.text, "\n", ending)
					s, h, sent := checkboxService(t, e, text, mode, nil)
					ranges := checkboxRanges(t, h)
					if len(ranges) == 0 {
						t.Fatalf("チェックボックスの位置が無い: %s", h.src.HTML)
					}
					r := ranges[len(ranges)-1]
					// 表の一覧を先に取った場合も、確定で同じ対応を使い回す。
					getTables(t, s, h)
					trials := h.trials
					if _, err := s.Apply(context.Background(), nil, map[string]any{"render": h.src.Render, "checkbox": string(r), "checked": true}); err != nil {
						t.Fatalf("切り替え=%v\n%s", err, h.src.HTML)
					}
					start, end, line, err := offsets(h.src, r)
					if err != nil || end-start != 1 {
						t.Fatal(start, end, line, err)
					}
					needle := "[ ] 作業"
					if !strings.Contains(text, needle) {
						needle = "[ ]"
					}
					want := strings.Replace(text, needle, strings.Replace(needle, "[ ]", "[x]", 1), 1)
					got := ""
					if mode == File {
						got = disk(t, h)
					} else {
						if len(*sent) != 1 {
							t.Fatalf("置き換え=%+v", *sent)
						}
						r := (*sent)[0]
						if r.Start != line-1 || r.End != line || len(r.Lines) != 1 {
							t.Fatalf("1行だけの置き換えでない: %+v", r)
						}
						lines := bufferLines(text)
						lines[r.Start] = r.Lines[0]
						got = strings.Join(lines, "\n") + "\n"
					}
					if got != want {
						t.Fatalf("原文=%q、期待=%q", got, want)
					}
					if h.trials != trials+1 {
						t.Fatal("確定で表の対応を作り直した", trials, h.trials)
					}
					if h.unpositioned != 0 {
						t.Fatal("位置属性を持たない入力で試しの描画を行った", h.unpositioned)
					}
				})
			}
		}
	}
}

func TestExcludedTableCheckboxes(t *testing.T) {
	e := testEngine(t)
	const raw = "<table><tr><td>\n\n- [ ] 作業\n\n</td></tr></table>\n"
	for _, c := range []struct {
		name, text, code, reason string
		files                    map[string]string
	}{
		{"入れ子の内外", "<table><tr><td>\n\n- [ ] 外側\n\n" + raw + "\n</td></tr></table>\n", "target", "", nil},
		{"JSONの位置", "plain [ ]\n\n```json:table\n" + `{"markdown":true,"items":[{"a":"<input type=\"checkbox\" class=\"task-list-item-checkbox\" data-checkbox-sourcepos=\"1:8-1:8\">"}]}` + "\n```\n", "target", "", nil},
		{"利用者の位置", "plain [ ]\n\n<table><tr><td><input type=\"checkbox\" class=\"task-list-item-checkbox\" data-checkbox-sourcepos=\"1:8-1:8\"></td></tr></table>\n", "target", "", nil},
		{"脚注の利用者の位置", "plain [ ]\n\n<section data-footnotes><ol><li><input type=\"checkbox\" class=\"task-list-item-checkbox\" data-checkbox-sourcepos=\"1:8-1:8\"></li></ol></section>\n", "target", "", nil},
		{"順序の異なる表", "<table><tfoot><tr><td>\n\n- [ ] 作業\n\n</td></tr></tfoot><tbody><tr><td>本文</td></tr></tbody></table>\n", "excluded", "原文の表と対応づけられない", nil},
		{"取り込んだパイプの表", "::include{file=part.md}\n", "excluded", "取り込んだファイルの表", map[string]string{"part.md": "| 列 |\n| --- |\n| [ ] |\n"}},
		{"取り込んだHTMLの表", "::include{file=part.md}\n", "excluded", "原文の表と対応づけられない", map[string]string{"part.md": raw}},
	} {
		for _, mode := range []Mode{File, Buffer} {
			for _, ending := range []string{"\n", "\r\n"} {
				t.Run(fmt.Sprintf("%s/%v/%q", c.name, mode, ending), func(t *testing.T) {
					text := strings.ReplaceAll(c.text, "\n", ending)
					files := map[string]string{}
					for name, contents := range c.files {
						files[name] = strings.ReplaceAll(contents, "\n", ending)
					}
					s, h, sent := checkboxService(t, e, text, mode, files)
					ranges := checkboxRanges(t, h)
					if len(ranges) == 0 || c.name == "入れ子の内外" && len(ranges) != 2 {
						t.Fatalf("位置=%q\n%s", ranges, h.src.HTML)
					}
					for _, r := range ranges {
						_, err := s.Apply(context.Background(), nil, map[string]any{"render": h.src.Render, "checkbox": string(r), "checked": true})
						if err == nil || err.Status != 422 || err.Code != c.code || !strings.Contains(err.Message, c.reason) {
							t.Fatalf("%sの拒否=%+v、期待=%s/%s", r, err, c.code, c.reason)
						}
						if c.code == "excluded" && !strings.HasPrefix(err.Message, "この表の中のタスクは切り替えられません（") {
							t.Fatal(err.Message)
						}
					}
					if mode == File && disk(t, h) != text || mode == Buffer && len(*sent) != 0 || h.src.Text != text {
						t.Fatal("対象外のチェックボックスで書き込んだ")
					}
					for name, contents := range files {
						got, err := os.ReadFile(filepath.Join(h.src.Root, name))
						if err != nil || string(got) != contents {
							t.Fatal("取り込んだファイルを変えた", err)
						}
					}
				})
			}
		}
	}
}

func TestAmbiguousCheckboxes(t *testing.T) {
	e := testEngine(t)
	for _, text := range []string{"- [ ] 作業\n", "<table><tr><td>\n\n- [ ] 作業\n\n</td></tr></table>\n", "本文[^a]\n\n[^a]: 定義\n\n    - [ ] 作業\n"} {
		for _, mode := range []Mode{File, Buffer} {
			s, h, sent := checkboxService(t, e, text, mode, nil)
			r := checkboxRanges(t, h)[0]
			h.src.HTML += h.src.HTML
			if _, err := s.Apply(context.Background(), nil, map[string]any{"render": h.src.Render, "checkbox": string(r), "checked": true}); err == nil || err.Code != "ambiguous" {
				t.Fatal(err)
			}
			if mode == File && disk(t, h) != h.src.Text || mode == Buffer && len(*sent) != 0 {
				t.Fatal("位置の重複したチェックボックスで書き込んだ")
			}
		}
	}
}

func TestTableCheckboxMissingReason(t *testing.T) {
	e := testEngine(t)
	for _, mode := range []Mode{File, Buffer} {
		for _, missing := range []bool{false, true} {
			s, h, sent := checkboxService(t, e, "| 列 |\n| --- |\n| [ ] |\n", mode, nil)
			r := checkboxRanges(t, h)[0]
			m, err := s.tables(context.Background(), h.src)
			if err != nil {
				t.Fatal(err)
			}
			m.response.Tables[0].Editable, m.response.Tables[0].Reason = false, ""
			if missing {
				m.response.Tables = nil
			}
			_, err = s.Apply(context.Background(), nil, map[string]any{"render": h.src.Render, "checkbox": string(r), "checked": true})
			if err == nil || err.Code != "excluded" || err.Message != "この表の中のタスクは切り替えられません（表の対応を確かめられない）。" {
				t.Fatal(err)
			}
			if mode == File && disk(t, h) != h.src.Text || mode == Buffer && len(*sent) != 0 {
				t.Fatal("表の対応を失っているのに書き込んだ")
			}
		}
	}
}
