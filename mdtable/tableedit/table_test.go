package tableedit_test

import (
	"strings"
	"testing"

	"github.com/ryo-aoki-pc/md-tools/mdtable/tableedit"
)

func TestConvertTablesInContainers(t *testing.T) {
	pipe := "| 見出し | 次 |\n| :--- | ---: |\n| 本文 | 値 |\n"
	html := "<table>\n<tr><th align=\"left\">見出し</th><th align=\"right\">次</th></tr>\n<tr><td align=\"left\">本文</td><td align=\"right\">値</td></tr>\n</table>\n"
	for _, ending := range []string{"\n", "\r\n"} {
		for _, container := range []struct{ name, before, prefix, after string }{
			{"本文", "前\n\n", "", "\n後\n\n| 別 |\n| --- |\n| 表 |\n"},
			{"引用", "前\n\n", "> ", "\n後\n"},
			{"リスト", "前\n\n- 項目\n\n", "  ", "\n- 次\n\n後\n"},
		} {
			for _, source := range []string{pipe, html} {
				kind := "html"
				if source == html {
					kind = "pipe"
				}
				t.Run(container.name+kind+strings.ReplaceAll(ending, "\n", "LF"), func(t *testing.T) {
					body := container.prefix + strings.ReplaceAll(strings.TrimSuffix(source, "\n"), "\n", "\n"+container.prefix) + "\n"
					before, after := strings.ReplaceAll(container.before, "\n", ending), strings.ReplaceAll(container.after, "\n", ending)
					original := before + strings.ReplaceAll(body, "\n", ending) + after
					d := tableedit.Parse(original)
					if reason := d.Table(1).ConvertLocked(kind); reason != "" {
						t.Fatal(reason)
					}
					r, err := d.ConvertTable(target(d.Table(1)), kind)
					next := mustChange(t, r, err)
					if next.Table(1).Kind != tableedit.Kind(kind) || !strings.HasPrefix(r.Text, before) || !strings.HasSuffix(r.Text, after) {
						t.Fatalf("範囲外の原文か種類が変わった: %q", r.Text)
					}
					if ending == "\r\n" && strings.Contains(strings.ReplaceAll(r.Text, "\r\n", ""), "\n") {
						t.Fatal("CRLF の中に LF が混ざった")
					}
					unchanged, err := next.ConvertTable(target(next.Table(1)), kind)
					if err != nil || unchanged.Changed || unchanged.Text != r.Text || next.Table(1).ConvertLocked(kind) == "" {
						t.Fatal("同じ種類への変換", err)
					}
				})
			}
		}
	}
}

func TestConversionRefusals(t *testing.T) {
	for _, mark := range []string{"[ ]", "[x]", "[X]"} {
		for _, source := range []string{
			"| 見出し |\n| --- |\n|  " + mark + "  |\n",
			"<table><tr><th>見出し</th></tr><tr><td>  " + mark + "  </td></tr></table>\n",
		} {
			d := tableedit.Parse(source)
			to := "html"
			if d.Table(1).Kind == "html" {
				to = "pipe"
			}
			r, err := d.ConvertTable(target(d.Table(1)), to)
			if r != nil || err == nil || d.Table(1).ConvertLocked(to) == "" || d.Text() != source {
				t.Fatalf("タスクの印だけのセルを変換した: %q %v", source, err)
			}
		}
	}
	for _, c := range []struct{ source, to string }{
		{"| 見出し |\n| --- |\n| <br>本文 |\n", "html"},
		{"<table><tr><th colspan=\"2\">見出し</th></tr><tr><td>本文</td><td>値</td></tr></table>\n", "pipe"},
		{"| 見出し |\n| --- |\n| <div>hello<br>world</div> |\n", "html"},
		{"<table>\n<tr><th>見出し</th></tr>\n<tr><td>\n\n[a]: /url\n\n</td></tr>\n</table>\n", "pipe"},
	} {
		d := tableedit.Parse(c.source)
		if r, err := d.ConvertTable(target(d.Table(1)), c.to); r != nil || err == nil || d.Table(1).ConvertLocked(c.to) == "" {
			t.Fatalf("変換で変わるセルを受けた: %q %v", c.source, err)
		}
	}
	d := tableedit.Parse("| a |\n| --- |\n")
	_, err := d.ConvertTable(target(d.Table(1)), "unknown")
	code(t, err, "invalid")
	_, err = d.ConvertTable(tableedit.Target{Number: 1, SourceHash: "違う"}, "html")
	code(t, err, "stale")
}

func TestDeleteTableAndOutsideBytes(t *testing.T) {
	for _, table := range []string{"| 見出し |\n| --- |\n| 本文 |\n", "<table><tr><th>見出し</th></tr><tr><td>本文</td></tr></table>\n"} {
		for _, ending := range []string{"\n", "\r\n"} {
			for _, c := range []struct{ before, prefix, after, want string }{
				{"前\n\n\n", "", "\n\n後\n\n| 別 |\n| --- |\n| 表 |\n", "前\n\n後\n\n| 別 |\n| --- |\n| 表 |\n"},
				{"\n\n", "", "\n後\n", "後\n"},
				{"前\n\n", "", "\n\n", "前\n"},
				{"\n", "", "\n", ""},
				{"前\n\n", "> ", "\n後\n", "前\n\n後\n"},
				{"- 項目\n\n", "  ", "\n- 次\n\n  続き\n", "- 項目\n\n- 次\n\n  続き\n"},
			} {
				body := c.prefix + strings.ReplaceAll(strings.TrimSuffix(table, "\n"), "\n", "\n"+c.prefix) + "\n"
				original := strings.ReplaceAll(c.before+body+c.after, "\n", ending)
				d := tableedit.Parse(original)
				r, err := d.DeleteTable(target(d.Table(1)))
				if err != nil {
					t.Fatalf("削除できない: %q: %v", original, err)
				}
				if !r.Changed || r.Text != strings.ReplaceAll(c.want, "\n", ending) || d.Table(1).DeleteLocked() != "" {
					t.Fatalf("削除結果: %q、期待: %q、理由: %s", r.Text, c.want, d.Table(1).DeleteLocked())
				}
			}
		}
	}
}

func TestDeleteTableRefusesChangedContainers(t *testing.T) {
	// locked は、文書を読み直さずに分かる理由（同じ行の印や文字）として、表情報で先に断るもの。
	// それ以外は、削除するときに前後の解釈を確かめて断る。
	for _, c := range []struct {
		text   string
		locked bool
	}{
		{"- | 見出し |\n  | --- |\n  | 本文 |\n", true},
		{"- <table><tr><td>本文</td></tr></table>\n", true},
		{"- 項目\n\n  | 見出し |\n  | --- |\n  | 本文 |\n", false},
		{"> <table><tr><td>本文</td></tr></table>\n> <div>外</div>\n", false},
		{"> 前\n>\n> | 見出し |\n> | --- |\n> | 本文 |\n>\n> 後\n", false},
		{"<table><tr><td>本文</td></tr></table><div>外</div>\n", true},
	} {
		d := tableedit.Parse(c.text)
		if r, err := d.DeleteTable(target(d.Table(1))); r != nil || err == nil || d.Text() != c.text {
			t.Fatalf("解釈が変わる削除を受けた: %q %v", c.text, err)
		}
		if locked := d.Table(1).DeleteLocked() != ""; locked != c.locked {
			t.Fatalf("表情報の削除の理由: %q、期待: %v、理由: %q", c.text, c.locked, d.Table(1).DeleteLocked())
		}
	}
}

func TestDeleteTableWithoutFinalNewline(t *testing.T) {
	for _, text := range []string{"| 見出し |\n| --- |", "<table><tr><td>本文</td></tr></table>"} {
		for _, c := range []struct{ source, want string }{
			{text, ""},
			{"前\n\n" + text, "前\n"},
			{text + "\n\n後", "後"},
			{"前\n\n" + text + "\n\n後", "前\n\n後"},
		} {
			d := tableedit.Parse(c.source)
			r, err := d.DeleteTable(target(d.Table(1)))
			if err != nil || r.Text != c.want {
				t.Fatalf("末尾の改行が無い削除: %q: %+v %v", c.source, r, err)
			}
		}
	}
}

func TestDeleteTableAllowsQuotePrefixSpacing(t *testing.T) {
	text := "前\n\n>  | 見出し |\n> | --- |\n> | 本文 |\n\n後\n"
	d := tableedit.Parse(text)
	r, err := d.DeleteTable(target(d.Table(1)))
	if err != nil || r.Text != "前\n\n後\n" {
		t.Fatal("引用の接頭辞の空白だけで削除を断った", r, err)
	}
}
