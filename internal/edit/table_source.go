package edit

import (
	"context"
	"sort"
	"strings"

	"github.com/ryo-aoki-pc/md-tools/internal/htmltree"
	"github.com/ryo-aoki-pc/md-tools/internal/session"
	"github.com/ryo-aoki-pc/md-tools/mdtable/tableedit"
	"golang.org/x/net/html"
)

// 表全体の原文は、表の対応づけが連続した行を確かめた最上位のパイプの表だけで扱う。
func tableSourceRange(m *mapping, index int) (Range, *Error) {
	if index < 0 || index >= len(m.response.Tables) {
		return "", fault(422, "range", "指定した表がありません。")
	}
	t := m.response.Tables[index]
	if !t.Editable {
		return "", fault(422, "excluded", t.Reason)
	}
	if t.Kind != "pipe" {
		return "", fault(422, "excluded", "表全体の原文を編集できるのは、パイプの表だけです。")
	}
	if t.Anchor == "" {
		return "", fault(422, "boundary", "表全体の原文の範囲を確かめられません（"+t.AnchorReason+"）。")
	}
	return t.Anchor, nil
}

func (s *Service) tableText(ctx context.Context, src session.Source, r Range) (any, *Error) {
	if ctx.Err() != nil {
		return nil, fault(422, "cancelled", "原文の読み取りを取り消しました。")
	}
	m, e := s.tables(ctx, src)
	if e != nil {
		return nil, e
	}
	for index, info := range m.response.Tables {
		if info.Anchor != r {
			continue
		}
		anchor, e := tableSourceRange(m, index)
		if e != nil {
			return nil, e
		}
		start, end, _, e := offsets(src, anchor)
		if e != nil {
			return nil, e
		}
		return map[string]any{"text": removeCR(src.Text[start:end]), "render": src.Render, "range": anchor, "kind": "table", "table": index}, nil
	}
	return nil, blockFault("target")
}

func tableSourceFault() *Error {
	return fault(422, "tableSource", "表全体の原文は、前後の段落を含まないパイプの表 1 個にしてください。")
}

func (s *Service) setTableSource(ctx context.Context, src session.Source, m *mapping, index int, value string) (string, int, int, *Error) {
	r, e := tableSourceRange(m, index)
	if e != nil {
		return "", 0, 0, e
	}
	start, end, line, e := offsets(src, r)
	if e != nil {
		return "", 0, 0, e
	}
	// ブロックの追加と同じく、入力欄の前後の空行は表の外として取り除く。
	core := strings.Split(normalizeNewlines(value, "\n"), "\n")
	for len(core) > 0 && strings.TrimSpace(core[0]) == "" {
		core = core[1:]
	}
	for len(core) > 0 && strings.TrimSpace(core[len(core)-1]) == "" {
		core = core[:len(core)-1]
	}
	if len(core) == 0 {
		return "", 0, 0, tableSourceFault()
	}
	replacement := normalizeNewlines(strings.Join(core, "\n"), newlineAt(src.Text, line))
	if len(src.Text)-end+start+len(replacement) > maxDocumentBytes {
		return "", 0, 0, fault(422, "large", "編集後の文書が 16 MiB を超えています。")
	}
	// 取り除いた後に残る行は全部が表の行でなければならず、表の外になる行を黙って切り落とさない。
	last := strings.Count(replacement, "\n")
	parsed := tableedit.Parse(replacement).Tables()
	if len(parsed) != 1 || parsed[0].Kind != "pipe" || parsed[0].Parent != 0 || parsed[0].Excluded != "" || parsed[0].StartLine != 0 || parsed[0].EndLine != last {
		return "", 0, 0, tableSourceFault()
	}
	text := src.Text[:start] + replacement + src.Text[end:]
	var changed *tableedit.Table
	for _, t := range tableedit.Parse(text).Tables() {
		if t.StartLine == line-1 {
			if changed != nil {
				return "", 0, 0, tableSourceFault()
			}
			changed = t
		}
	}
	if changed == nil || changed.Kind != "pipe" || changed.Parent != 0 || changed.Excluded != "" || changed.EndLine != line-1+last {
		return "", 0, 0, tableSourceFault()
	}
	if text != src.Text {
		if e := s.verifyTableSource(ctx, src, index, changed, text); e != nil {
			return "", 0, 0, e
		}
	}
	return text, start, end, nil
}

// 行・列と中身は変えてよいが、ほかの最上位の節点は位置以外の属性も含めて同じにする。
func (s *Service) verifyTableSource(ctx context.Context, src session.Source, index int, changed *tableedit.Table, text string) *Error {
	trial, err := s.opts.Host.Trial(ctx, src, text)
	if err != nil {
		return fault(422, "render", "編集後の文書を描画できません。")
	}
	before, err := htmltree.Fragment(ctx, src.HTML)
	if err != nil {
		return fault(422, "render", "編集前の文書を読み取れません。")
	}
	after, err := htmltree.Fragment(ctx, trial.HTML)
	if err != nil {
		return fault(422, "render", "編集後の文書を読み取れません。")
	}
	a, err := tableNodes(ctx, before)
	if err != nil {
		return fault(422, "cancelled", "表の原文の確認を取り消しました。")
	}
	b, err := tableNodes(ctx, after)
	if err != nil || len(a) != len(b) || index >= len(b) || a[index].Parent != before || b[index].Parent != after || !sameGrid(b[index], changed) {
		return tableSourceFault()
	}
	// 対象の表だけを同じ印に替え、空白だけの最上位の文字節点を除いて全体を比べる。
	a[index].Parent.InsertBefore(htmltree.Text("表の原文の編集対象"), a[index])
	a[index].Parent.RemoveChild(a[index])
	b[index].Parent.InsertBefore(htmltree.Text("表の原文の編集対象"), b[index])
	b[index].Parent.RemoveChild(b[index])
	old, ok := tableSourceSignature(ctx, before)
	next, nextOK := tableSourceSignature(ctx, after)
	if !ok || !nextOK || old != next {
		return fault(422, "verify", "表全体の原文を変えると、表の外の描画も変わります。")
	}
	return nil
}

func tableSourceSignature(ctx context.Context, root *html.Node) (string, bool) {
	err := htmltree.Walk(ctx, root, func(n *html.Node) error {
		attrs := n.Attr[:0]
		for _, a := range n.Attr {
			if a.Namespace != "" || a.Key != "data-sourcepos" && a.Key != "data-checkbox-sourcepos" {
				attrs = append(attrs, a)
			}
		}
		n.Attr = attrs
		sort.Slice(n.Attr, func(i, j int) bool {
			return n.Attr[i].Namespace+":"+n.Attr[i].Key < n.Attr[j].Namespace+":"+n.Attr[j].Key
		})
		return nil
	})
	if err != nil {
		return "", false
	}
	var out strings.Builder
	for n := root.FirstChild; n != nil; n = n.NextSibling {
		if n.Type == html.TextNode && strings.TrimSpace(n.Data) == "" {
			continue
		}
		if html.Render(&out, n) != nil {
			return "", false
		}
	}
	return out.String(), true
}
