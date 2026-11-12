package edit

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestInsertTables(t *testing.T) {
	e := testEngine(t)
	for _, table := range []string{pipeTable, htmlTable, "<TABLE class=\"元の属性\">\n<tr><td>値</td></tr>\n</TABLE>\n"} {
		for _, side := range []string{"before", "after"} {
			for _, mode := range []Mode{File, Buffer} {
				for _, ending := range []string{"\n", "\r\n"} {
					t.Run(fmt.Sprintf("%s/%s/%v/%q", table, side, mode, ending), func(t *testing.T) {
						old := strings.ReplaceAll("前\n\n"+table+"\n後\n", "\n", ending)
						s, h, updated := footnoteService(t, e, old, mode)
						info := getTables(t, s, h).Tables[0]
						if info.Anchor == "" || info.AnchorReason != "" {
							t.Fatalf("追加先=%+v", info)
						}
						a, b, _, err := offsets(h.src, info.Anchor)
						if err != nil || h.src.Text[a:b] != strings.ReplaceAll(strings.TrimSuffix(table, "\n"), "\n", ending) {
							t.Fatal("追加先が表の原文の行と違う", info.Anchor, err)
						}
						if _, err := s.Apply(context.Background(), nil, map[string]any{"render": h.src.Render,
							"insert": map[string]any{"anchor": string(info.Anchor), "side": side}, "text": "追加"}); err != nil {
							t.Fatalf("追加=%v\n%s", err, h.src.HTML)
						}
						want := "前\n\n追加\n\n" + table + "\n後\n"
						if side == "after" {
							want = "前\n\n" + table + "\n追加\n\n後\n"
						}
						want = strings.ReplaceAll(want, "\n", ending)
						if got := updated(); got != want {
							t.Fatalf("原文=%q、期待=%q", got, want)
						}
					})
				}
			}
		}
	}
}

func TestTableInsertionAnchors(t *testing.T) {
	e := testEngine(t)
	for _, c := range []struct {
		name, text, reason string
		allowed            bool
	}{
		{"パイプ", pipeTable, "", true},
		{"HTML", htmlTable, "", true},
		{"HTML の位置属性", strings.Replace(htmlTable, "<table>", `<table data-sourcepos="1:1-1:1">`, 1), "", true},
		{"引用", "> " + strings.ReplaceAll(strings.TrimSuffix(pipeTable, "\n"), "\n", "\n> ") + "\n", "最上位", false},
		{"リスト", "- 項目\n\n  " + strings.ReplaceAll(strings.TrimSuffix(pipeTable, "\n"), "\n", "\n  ") + "\n", "最上位", false},
		{"details", "<details>\n<summary>題</summary>\n\n" + pipeTable + "\n</details>\n", "最上位", false},
		{"注記", ">>> [!note]\n" + pipeTable + ">>>\n", "最上位", false},
		{"入れ子", "<table><tr><td><table><tr><td>中</td></tr></table></td></tr></table>\n", "入れ子", false},
		{"JSON", "```json:table\n{\"markdown\":true,\"fields\":[{\"key\":\"a\",\"label\":\"列\"}],\"items\":[{\"a\":\"値\"}]}\n```\n", "JSON", false},
		{"取り込み", "::include{file=part.md}\n", "取り込んだ", false},
		{"HTML の前の文字", "文字" + htmlTable, "表以外", false},
		{"HTML の後ろの文字", strings.TrimSuffix(htmlTable, "\n") + "文字\n", "表以外", false},
		{"同じ行の表", strings.TrimSuffix(htmlTable, "\n") + htmlTable, "表以外", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, h := testService(t, e, c.text, map[string]string{"part.md": pipeTable})
			info := getTables(t, s, h)
			if len(info.Tables) == 0 {
				t.Fatalf("表がない: %s", h.src.HTML)
			}
			for _, table := range info.Tables {
				if (table.Anchor != "") != c.allowed || c.allowed && table.AnchorReason != "" ||
					!c.allowed && (table.AnchorReason == "" || !strings.Contains(table.AnchorReason, c.reason) && table.AnchorReason != table.Reason) {
					t.Fatalf("追加先=%+v", table)
				}
			}
		})
	}
}

func TestInsertTablePreservesNeighbors(t *testing.T) {
	e := testEngine(t)
	for _, mode := range []Mode{File, Buffer} {
		for _, c := range []struct{ table, side, added string }{
			{pipeTable, "before", "```"},
			{htmlTable, "before", "<details>"},
			{htmlTable, "after", "<details>"},
		} {
			t.Run(fmt.Sprint(mode, c), func(t *testing.T) {
				old := "前\n\n" + c.table + "\n後\n"
				s, h := testService(t, e, old, nil)
				var sent *[]BufferEdit
				if mode == Buffer {
					s, h, sent = bufferService(t, e, old)
				}
				anchor := getTables(t, s, h).Tables[0].Anchor
				_, err := s.Apply(context.Background(), nil, map[string]any{"render": h.src.Render,
					"insert": map[string]any{"anchor": string(anchor), "side": c.side}, "text": c.added})
				if err == nil || err.Code != "insert" {
					t.Fatalf("拒否=%v\n%s", err, h.src.HTML)
				}
				if mode == File && disk(t, h) != old || sent != nil && len(*sent) != 0 {
					t.Fatal("断った追加を書いた")
				}
			})
		}
	}
}

func TestInsertTableEdgesAndExpandedLines(t *testing.T) {
	e := testEngine(t)
	for _, table := range []string{pipeTable, htmlTable} {
		for _, side := range []string{"before", "after"} {
			for _, prefix := range []string{"", "---\ntitle: x\n---\n\n", "::include{file=part.md}\n\n"} {
				t.Run(fmt.Sprint(table, side, prefix), func(t *testing.T) {
					old := prefix + strings.TrimSuffix(table, "\n")
					s, h := testService(t, e, old, map[string]string{"part.md": "取り込み\n\nもう一段\n"})
					info := getTables(t, s, h).Tables[0]
					_, err := s.Apply(context.Background(), nil, map[string]any{"render": h.src.Render,
						"insert": map[string]any{"anchor": string(info.Anchor), "side": side}, "text": "追加"})
					if err != nil {
						t.Fatal(err)
					}
					want := prefix + "追加\n\n" + strings.TrimSuffix(table, "\n")
					if side == "after" {
						want = old + "\n\n追加"
					}
					if disk(t, h) != want {
						t.Fatalf("原文=%q、期待=%q", disk(t, h), want)
					}
				})
			}
		}
	}
}

func TestInsertTableRejectsAbsorbedRows(t *testing.T) {
	e := testEngine(t)
	s, h := testService(t, e, pipeTable+"\n後\n", nil)
	m, err := s.blocks(context.Background(), h.src)
	if err != nil {
		t.Fatal(err)
	}
	anchor := getTables(t, s, h).Tables[0].Anchor
	point, err := s.insertionPoint(context.Background(), h.src, m, request{rangeValue: anchor, insertSide: "after"})
	if err != nil {
		t.Fatal(err)
	}
	// 通常の追加は空行で区切る。空行が欠けて本文が表の行になる場合を、試しの描画が断ることも確かめる。
	text := h.src.Text[:point.pos] + "| a |\n" + h.src.Text[point.pos:]
	if err := s.verifyInsertion(context.Background(), h.src, m, point, text, 1, false); err == nil || err.Code != "insert" {
		t.Fatal("表の行に取り込む追加を断らなかった", err)
	}
}
