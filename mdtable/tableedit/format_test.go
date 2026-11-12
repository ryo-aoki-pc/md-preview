package tableedit_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/ryo-aoki-pc/md-tools/mdtable/tableedit"
)

func TestSetPipeAlignPreservesDelimiter(t *testing.T) {
	for _, from := range []string{"", "left", "center", "right"} {
		for _, to := range []string{"", "left", "center", "right"} {
			spec := func(align string) string {
				v := "-----"
				if align == "left" || align == "center" {
					v = ":" + v
				}
				if align == "center" || align == "right" {
					v += ":"
				}
				return v
			}
			source := "| 先 | 対象 | 後 |\n|:--\t|  " + spec(from) + "\t| ----: |\n| 外 | 値 | 外 | 余り |\n"
			want := strings.Replace(source, "  "+spec(from)+"\t", "  "+spec(to)+"\t", 1)
			if from == to {
				d := tableedit.Parse(source)
				r, err := d.SetAlign(target(d.Table(1)), tableedit.Rect{Row: 1, Col: 1, Rows: 1, Cols: 1}, to)
				if err != nil || r.Changed || r.Text != source {
					t.Fatal("同じ揃えを変えた", r, err)
				}
				continue
			}
			t.Run(from+"から"+to, func(t *testing.T) {
				mergeContainers(t, source, want, func(d *tableedit.Document) (*tableedit.Result, error) {
					r, err := d.SetAlign(target(d.Table(1)), tableedit.Rect{Row: 1, Col: 1, Rows: 1, Cols: 1}, to)
					if err == nil {
						before := map[int]string{}
						for _, c := range d.Table(1).Cells {
							before[c.Col] = c.Align
						}
						for _, c := range r.Document.Table(1).Cells {
							if c.Col == 1 && c.Align != to || c.Col != 1 && c.Align != before[c.Col] {
								t.Fatal("選んだ列か、ほかの列の揃えが違う", c)
							}
						}
					}
					return r, err
				})
			})
		}
	}
	mergeContainers(t, "A | B | C\n:--- | ----: | :-----:\n値 | 値 | 値\n", "A | B | C\n--- | ---- | :-----:\n値 | 値 | 値\n", func(d *tableedit.Document) (*tableedit.Result, error) {
		return d.SetAlign(target(d.Table(1)), tableedit.Rect{Rows: 2, Cols: 2}, "")
	})
}

func TestSetHTMLAlignPreservesAttributes(t *testing.T) {
	for _, c := range []struct{ name, attrs, other, align, want string }{
		{"align の値", " class='残す' align = 'left' title=保持", "", "center", " class='残す' align = 'center' title=保持"},
		{"非引用", " align=right", "", "left", " align=left"},
		{"値なし", " align", "", "right", " align=\"right\""},
		{"align を足す", " class='残す'", "", "right", " align=\"right\" class='残す'"},
		{"align を消す", " class='残す' align='left' title=保持", "", "", " class='残す' title=保持"},
		{"style の宣言", " style='color:red; text-align: left; padding:1px'", "", "center", " style='color:red; text-align: center; padding:1px'"},
		{"空白のない宣言", " style='text-align:right;'", "", "left", " style='text-align: left;'"},
		{"大文字", " STYLE=' TEXT-ALIGN :RIGHT; color:red'", "", "left", " STYLE=' text-align: left; color:red'"},
		{"非引用の style", " style=text-align:right", "", "center", " style=\"text-align: center\""},
		{"style だけ消す", " style='text-align:right;' class='残す'", "", "", " class='残す'"},
		{"宣言を消す", " style='color:red; text-align:right; padding:1px'", "", "", " style='color:red; padding:1px'"},
		{"両方を書き換え", " align='center' style='text-align: right'", "", "left", " align='left' style='text-align: left'"},
		{"両方を消す", " align='center' style='color:red; text-align:right'", "", "", " style='color:red;'"},
		{"表に合わせ style を足す", " class='残す'", " style='text-align: left'", "right", " style=\"text-align: right\" class='残す'"},
		{"既存の style へ足す", " style='color:red'", " style='text-align: left'", "center", " style='color:red; text-align: center'"},
		{"末尾の区切りを保つ", " style='color:red;  '", " style='text-align: left'", "center", " style='color:red;   text-align: center'"},
		{"無関係な style を保つ", " style='color:red'", "", "left", " align=\"left\" style='color:red'"},
		{"align の書き方を保つ", " align='left'", " style='text-align: left'", "right", " align='right'"},
		{"style の書き方を保つ", " style='text-align: left'", " align='left'", "right", " style='text-align: right'"},
		{"改行した属性", "\n  title='保持'\n  align = \"left\"\n  class='保持'", "", "right", "\n  title='保持'\n  align = \"right\"\n  class='保持'"},
		{"改行した style", " style='color:red;\n  text-align:right;\n  padding:1px'", "", "left", " style='color:red;\n  text-align: left;\n  padding:1px'"},
		{"宣言の重複", " style='text-align:left; text-align:right;'", "", "center", " style='text-align: center; text-align: center;'"},
		{"引用符と括弧の中", " style='background:url(data:x;text-align:bogus); content: \";text-align:wrong\"; text-align:right'", "", "left", " style='background:url(data:x;text-align:bogus); content: \";text-align:wrong\"; text-align: left'"},
	} {
		t.Run(c.name, func(t *testing.T) {
			source := "<table>\n<tr><td" + c.attrs + ">値<b>保持</b></td><td" + c.other + ">外</td></tr>\n</table>\n"
			want := "<table>\n<tr><td" + c.want + ">値<b>保持</b></td><td" + c.other + ">外</td></tr>\n</table>\n"
			mergeContainers(t, source, want, func(d *tableedit.Document) (*tableedit.Result, error) {
				return d.SetAlign(target(d.Table(1)), tableedit.Rect{Rows: 1, Cols: 1}, c.align)
			})
		})
	}
}

// GitLab の消毒は style の中で最初に一致した「text-align: 値」だけを残す。
// コメント・url()・文字列の中に宣言に似た文字があり、書いた揃えが効かない形は断る。
func TestSetHTMLAlignRefusesStyleGitLabWouldMisread(t *testing.T) {
	for _, attrs := range []string{
		" style='background:url(data:x;text-align: left); text-align:right'",
		" style='content: \"text-align: left\"; text-align:right'",
		" style='/* color: red */ text-align: right'",
	} {
		source := "<table>\n<tr><td" + attrs + ">値</td><td>外</td></tr>\n</table>\n"
		d := tableedit.Parse(source)
		if r, err := d.SetAlign(target(d.Table(1)), tableedit.Rect{Rows: 1, Cols: 1}, "center"); r != nil || err == nil || !strings.Contains(err.Error(), "GitLab で効く揃え") || d.Text() != source {
			t.Fatalf("GitLab で効かない揃えを書いた: %q %v", attrs, err)
		}
	}
}

func TestSetHeaderInHeadAndFoot(t *testing.T) {
	mergeContainers(t, "<table>\n<thead>\n<tr><td colspan='2'>見出し</td></tr>\n</thead>\n<tfoot>\n<tr><th>計</th><td>1</td></tr>\n</tfoot>\n</table>\n",
		"<table>\n<thead>\n<tr><th colspan='2'>見出し</th></tr>\n</thead>\n<tfoot>\n<tr><th>計</th><td>1</td></tr>\n</tfoot>\n</table>\n",
		func(d *tableedit.Document) (*tableedit.Result, error) {
			return d.SetHeader(target(d.Table(1)), tableedit.Rect{Rows: 1, Cols: 2}, true)
		})
	mergeContainers(t, "<table>\n<thead>\n<tr><th colspan='2'>見出し</th></tr>\n</thead>\n<tfoot>\n<tr><th>計</th><td>1</td></tr>\n</tfoot>\n</table>\n",
		"<table>\n<thead>\n<tr><th colspan='2'>見出し</th></tr>\n</thead>\n<tfoot>\n<tr><td>計</td><td>1</td></tr>\n</tfoot>\n</table>\n",
		func(d *tableedit.Document) (*tableedit.Result, error) {
			return d.SetHeader(target(d.Table(1)), tableedit.Rect{Row: 1, Rows: 1, Cols: 1}, false)
		})
}

func TestSetHeaderAndMergedFormat(t *testing.T) {
	mergeContainers(t, "<TABLE>\n<TR><TD\n  title='保持'\n  align=right>値<b>本文</b></TD ><td>外</td></TR>\n</TABLE>\n", "<TABLE>\n<TR><th\n  title='保持'\n  align=right>値<b>本文</b></th ><td>外</td></TR>\n</TABLE>\n", func(d *tableedit.Document) (*tableedit.Result, error) {
		return d.SetHeader(target(d.Table(1)), tableedit.Rect{Rows: 1, Cols: 1}, true)
	})
	for _, header := range []bool{true, false} {
		from, to := "td", "th"
		if !header {
			from, to = to, from
		}
		source := "<table>\n<tr><" + from + " class='保持' colspan='2'>値<b>本文</b></" + from + "><td>外</td></tr>\n</table>\n"
		want := strings.Replace(strings.Replace(source, "<"+from+" class=", "<"+to+" class=", 1), "</"+from+">", "</"+to+">", 1)
		mergeContainers(t, source, want, func(d *tableedit.Document) (*tableedit.Result, error) {
			return d.SetHeader(target(d.Table(1)), tableedit.Rect{Rows: 1, Cols: 2}, header)
		})
	}
	mergeContainers(t, "<table>\n<tr><td colspan='2'>値</td><td>外</td></tr>\n</table>\n", "<table>\n<tr><td align=\"right\" colspan='2'>値</td><td>外</td></tr>\n</table>\n", func(d *tableedit.Document) (*tableedit.Result, error) {
		return d.SetAlign(target(d.Table(1)), tableedit.Rect{Rows: 1, Cols: 2}, "right")
	})
	mergeContainers(t, "<table>\n<tr><td>A</td><td>B</td></tr>\n<tr><td>C</td><td>D</td></tr>\n</table>\n", "<table>\n<tr><th>A</th><th>B</th></tr>\n<tr><th>C</th><th>D</th></tr>\n</table>\n", func(d *tableedit.Document) (*tableedit.Result, error) {
		return d.SetHeader(target(d.Table(1)), tableedit.Rect{Rows: 2, Cols: 2}, true)
	})
}

func TestCellFormatUnchangedAndRefusals(t *testing.T) {
	for _, source := range []string{"<table><tr><td>値</td></tr></table>\n", "<table><tr><td align='right'>値</td></tr></table>\n", "<table><tr><th style='text-align: right'>値</th></tr></table>\n"} {
		d := tableedit.Parse(source)
		cell := d.Table(1).Cells[0]
		for _, edit := range []func() (*tableedit.Result, error){
			func() (*tableedit.Result, error) {
				return d.SetAlign(target(d.Table(1)), tableedit.Rect{Rows: 1, Cols: 1}, cell.Align)
			},
			func() (*tableedit.Result, error) {
				return d.SetHeader(target(d.Table(1)), tableedit.Rect{Rows: 1, Cols: 1}, cell.Header)
			},
		} {
			r, err := edit()
			if err != nil || r.Changed || r.Text != source {
				t.Fatal("同じ書式を変えた", r, err)
			}
		}
	}
	for _, c := range []struct {
		source, code string
		rect         tableedit.Rect
	}{
		{"<table><tr><td>値<td>外</tr></table>\n", "unsupported", tableedit.Rect{Rows: 1, Cols: 1}},
		{"<table><tr><td colspan='0'>値</td></tr></table>\n", "unsupported", tableedit.Rect{Rows: 1, Cols: 1}},
		{"<table><tr><td rowspan='3'>値</td></tr></table>\n", "unsupported", tableedit.Rect{Rows: 1, Cols: 1}},
		{"<table><tr><td colspan='2'>値</td></tr></table>\n", "merged", tableedit.Rect{Rows: 1, Cols: 1}},
		{"<table><tr><td>値</td></tr></table>\n", "range", tableedit.Rect{Rows: 0, Cols: 1}},
		{"<table><tr><td>値</td></tr></table>\n", "range", tableedit.Rect{Rows: 1, Cols: 2}},
		{"<table><tr><td>値</td></tr></table>\n", "range", tableedit.Rect{Row: int(^uint(0) >> 1), Rows: 1, Cols: 1}},
	} {
		d := tableedit.Parse(c.source)
		for _, edit := range []func() (*tableedit.Result, error){
			func() (*tableedit.Result, error) { return d.SetAlign(target(d.Table(1)), c.rect, "right") },
			func() (*tableedit.Result, error) { return d.SetHeader(target(d.Table(1)), c.rect, true) },
		} {
			r, err := edit()
			var failure *tableedit.Error
			if r != nil || !errors.As(err, &failure) || failure.Code != c.code || d.Text() != c.source {
				t.Fatal("断る理由が違う", r, err)
			}
		}
	}
	d := tableedit.Parse("| A |\n| --- |\n| 値 |\n")
	for _, align := range []string{"justify", "LEFT", " center", "bogus"} {
		if _, err := d.SetAlign(target(d.Table(1)), tableedit.Rect{Rows: 1, Cols: 1}, align); err == nil || err.(*tableedit.Error).Code != "invalid" {
			t.Fatal("不正な揃えを受けた", err)
		}
	}
	if _, err := d.SetHeader(target(d.Table(1)), tableedit.Rect{Rows: 1, Cols: 1}, true); err == nil || err.(*tableedit.Error).Code != "unsupported" {
		t.Fatal("パイプの見出しセルを変えた", err)
	}
}
