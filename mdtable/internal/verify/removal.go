package verify

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/ryo-aoki-pc/md-tools/mdtable/internal/blocks"
	"github.com/ryo-aoki-pc/md-tools/mdtable/internal/table"
)

// Removal は表の削除後に、残るブロックの入れ物と、ほかの表の原文・解釈を確かめる。
// start・end は空行の整理も含む置換範囲（end は含まない）。
func Removal(md *table.MarkdownFile, text string, removed *table.Region, start, end, count int) Outcome {
	next := table.NewMarkdown(md.Path, text, md.BOM)
	regions := table.FindTables(next)
	out := Outcome{Markdown: next, Regions: regions}
	shift := func(line int) int {
		if line >= end {
			return line + count - (end - start)
		}
		return line
	}
	// 入れ物も比較するので、隣接する引用やリストが一つになる削除も断る。
	var signature func(*blocks.Node, bool) string
	signature = func(n *blocks.Node, old bool) string {
		if old && removed.Start <= n.StartLine && n.EndLine <= removed.End {
			return ""
		}
		children := ""
		for _, child := range n.Children {
			children += signature(child, old)
		}
		if n.Kind == blocks.DocumentKind && children == "" {
			return ""
		}
		if len(n.Children) > 0 && children == "" {
			return ""
		}
		position := ""
		if len(n.Children) == 0 || n.Kind == blocks.Table {
			a, b := n.StartLine, n.EndLine
			if old {
				a, b = shift(a), shift(b)
			}
			position = fmt.Sprintf("%d:%d", a, b)
		}
		i := n.Info
		return fmt.Sprintf("(%s:%s:%s:%s:%s:%d:%d:%t:%t:%t:%s)", n.Kind, position, i.Type, i.BulletChar, i.Delimiter, i.Start, i.Level, i.Tight, i.Fenced, i.Setext, children)
	}
	if signature(md.Doc().Root, true) != signature(next.Doc().Root, false) {
		out.Problems = append(out.Problems, "表を削除すると、前後の Markdown の解釈が変わるため、項目全体や引用全体を編集して消してください")
	}
	oldRegions := table.FindTables(md)
	wanted := make([]*table.Region, 0, len(oldRegions))
	for _, r := range oldRegions {
		if r.Start >= removed.Start && r.End <= removed.End {
			continue
		}
		wanted = append(wanted, r)
	}
	if len(wanted) != len(regions) {
		out.Problems = append(out.Problems, "表を削除すると、ほかの表の数が変わる")
		return out
	}
	for i, old := range wanted {
		r := regions[i]
		if r.Start != shift(old.Start) || r.End != shift(old.End) || r.Kind != old.Kind || r.Location != old.Location || r.Excluded != old.Excluded || r.SourceText() != old.SourceText() || old.Excluded == "" && !reflect.DeepEqual(r.Model(), old.Model()) {
			out.Problems = append(out.Problems, "表を削除すると、ほかの表の原文や解釈が変わる")
			break
		}
	}
	// 位置だけの比較で、前後の段落がつながった変化を見逃さない。
	if strings.Join(LeafSignature(md.Doc(), []LineRange{{removed.Start, removed.End}}, shift), "\x00") != strings.Join(LeafSignature(next.Doc(), nil, func(i int) int { return i }), "\x00") && len(out.Problems) == 0 {
		out.Problems = append(out.Problems, "表を削除すると、前後の Markdown の解釈が変わる")
	}
	return out
}
