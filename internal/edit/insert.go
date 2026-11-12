package edit

import (
	"context"
	"slices"
	"strings"

	"github.com/ryo-aoki-pc/md-tools/internal/htmltree"
	"github.com/ryo-aoki-pc/md-tools/internal/session"
	"golang.org/x/net/html"
)

// InsertAnchor は、追加先になる、外側の対象を持たないブロック。
type InsertAnchor struct {
	Tag    string `json:"tag"`
	Range  Range  `json:"range"`
	Actual Range  `json:"actual"`
}

func (m *blockMapping) anchor(b *block) *InsertAnchor {
	for b.outer >= 0 {
		b = &m.blocks[b.outer]
	}
	// 脚注の定義は、画面では文書の最後に並び、原文の位置と対応しないので、追加先にしない。
	if b.kind == "footnote" {
		return nil
	}
	if _, e := m.target(b.tag, b.public); e != nil {
		return nil
	}
	return &InsertAnchor{b.tag, b.public, b.actual}
}

func (s *Service) insertBlock(ctx context.Context, src session.Source, r request) (string, *Error) {
	m, e := s.blocks(ctx, src)
	if e != nil {
		return "", e
	}
	point, e := s.insertionPoint(ctx, src, m, r)
	if e != nil {
		return "", e
	}
	text, delta, e := insertionText(src.Text, point, r.text)
	if e != nil {
		return "", e
	}
	if e := s.verifyInsertion(ctx, src, m, point, text, delta, r.insertDocument == "end"); e != nil {
		return "", e
	}
	return text, nil
}

// 追加点は原文のバイト位置と、展開後の要素をずらす行を分けて持つ。
type insertionPoint struct {
	pos, line, cut int
	prefix         int
	anchor         *block
	empty          bool
}

func (s *Service) insertionPoint(ctx context.Context, src session.Source, m *blockMapping, r request) (insertionPoint, *Error) {
	old := src.Text
	starts := lineStarts(old)
	if r.insertDocument != "" {
		return documentInsertionPoint(src, r.insertDocument), nil
	}
	var anchor *block
	for i := range m.blocks {
		b := &m.blocks[i]
		if b.outer < 0 && b.reason == nil && b.actual == r.rangeValue && m.anchor(b) != nil {
			anchor = b
			break
		}
	}
	if anchor == nil {
		tables, e := s.tables(ctx, src)
		if e != nil {
			return insertionPoint{}, e
		}
		for _, table := range tables.response.Tables {
			if table.Anchor == "" || table.Anchor != r.rangeValue {
				continue
			}
			c, _ := coordinates(table.Anchor)
			line, _ := src.Lines.Original(c[0])
			point := insertionPoint{line: line, cut: c[0], pos: starts[line-1]}
			if r.insertSide == "after" {
				point.line, _ = src.Lines.Original(c[2])
				point.cut, point.pos = c[2]+1, len(old)
				if point.line < len(starts) {
					point.pos = starts[point.line]
				}
			}
			return point, nil
		}
		anchor, e = s.htmlWritable(ctx, src, m, r.rangeValue)
		if e != nil {
			return insertionPoint{}, e
		}
	}
	if strings.Trim(old[starts[anchor.line-1]:anchor.start], " \t") != "" {
		return insertionPoint{}, blockFault("boundary")
	}
	c, _ := coordinates(anchor.actual)
	public, _ := positionCoordinates(anchor.public)
	point := insertionPoint{line: anchor.line, cut: public[0], pos: starts[anchor.line-1], anchor: anchor}
	if r.insertSide == "after" {
		point.line, _ = src.Lines.Original(c[2])
		// 補正した水平線や注記の閉じまでを含めて、次の行へ追加する。
		point.cut, point.pos = c[2]+1, len(old)
		if point.line < len(starts) {
			point.pos = starts[point.line]
		}
	}
	return point, nil
}

func documentInsertionPoint(src session.Source, side string) insertionPoint {
	old := src.Text
	starts := lineStarts(old)
	point := insertionPoint{line: 1, cut: 1, empty: strings.TrimSpace(strings.TrimPrefix(old, "\ufeff")) == ""}
	if side == "end" {
		point.pos, point.line = len(old), len(starts)
		point.cut = src.Lines.Expanded(point.line)
		if !strings.HasSuffix(old, "\n") {
			// 最後の行が include なら、その展開全体の後ろになる。
			point.cut = src.Lines.Expanded(point.line + 1)
		}
	} else {
		point.pos = len(old) - len(strings.TrimPrefix(old, "\ufeff"))
		point.prefix = point.pos
		// front matter の判定は描画と同じ行対応を使い、閉じた次の行へ置く。
		for n := 1; n <= src.Lines.Expanded(len(starts)); n++ {
			orig, reason := src.Lines.Original(n)
			if reason != "frontmatter" {
				break
			}
			point.line, point.cut, point.pos = orig, n+1, len(old)
			point.prefix = 0
			if orig < len(starts) {
				point.pos = starts[orig]
			}
		}
	}
	if point.empty {
		point.cut = 1
	}
	return point
}

func insertionText(old string, point insertionPoint, value string) (string, int, *Error) {
	core := strings.Split(normalizeNewlines(value, "\n"), "\n")
	for len(core) > 0 && strings.TrimSpace(core[0]) == "" {
		core = core[1:]
	}
	for len(core) > 0 && strings.TrimSpace(core[len(core)-1]) == "" {
		core = core[:len(core)-1]
	}
	if len(core) == 0 {
		return "", 0, fault(422, "invalid", "追加する文を入力してください。")
	}
	if point.empty {
		text := strings.Join(core, "\n") + "\n"
		if strings.HasPrefix(old, "\ufeff") {
			text = "\ufeff" + text
		}
		if len(text) > maxDocumentBytes {
			return "", 0, fault(422, "large", "編集後の文書が 16 MiB を超えています。")
		}
		return text, strings.Count(text, "\n"), nil
	}
	ending := newlineAt(old, point.line)
	left, right := old[:point.pos], old[point.pos:]
	added := strings.Join(core, ending)
	if strings.TrimPrefix(left, "\ufeff") != "" {
		if !strings.HasSuffix(left, "\n") {
			added = ending + ending + added
		} else {
			last := strings.TrimSuffix(strings.TrimSuffix(left, "\n"), "\r")
			if n := strings.LastIndexByte(last, '\n'); n >= 0 {
				last = last[n+1:]
			}
			if strings.TrimSpace(last) != "" {
				added = ending + added
			}
		}
	}
	if right != "" {
		added += ending
		first := strings.SplitN(right, "\n", 2)[0]
		if strings.TrimSpace(first) != "" {
			added += ending
		}
	} else if strings.HasSuffix(old, "\n") {
		added += ending
	}
	text := left + added + right
	if len(text) > maxDocumentBytes {
		return "", 0, fault(422, "large", "編集後の文書が 16 MiB を超えています。")
	}
	return text, strings.Count(added, "\n"), nil
}

func (s *Service) verifyInsertion(ctx context.Context, src session.Source, m *blockMapping, point insertionPoint, text string, delta int, documentEnd bool) *Error {
	trial, err := s.opts.Host.Trial(ctx, src, text)
	if err != nil {
		return fault(422, "render", "編集後の文書を描画できません。")
	}
	// 試しの描画にも同じ補正を掛けて、補正後の範囲どうしを比べる。
	next := src
	next.Text, next.HTML, next.Lines = text, trial.HTML, trial.Source
	next.HasSourcepos, next.SourceposInput = trial.HasSourcepos, trial.SourceposInput
	after, e := s.buildBlocks(ctx, next)
	if e != nil {
		return e
	}
	if !insertionPreserves(ctx, m, after, src.HTML, trial.HTML, point.cut, delta, point.prefix) ||
		!insertionHTMLPreserves(ctx, m, after, src.HTML, trial.HTML, point.cut, delta, point.anchor) {
		return fault(422, "insert", "追加した文が前後のブロックに取り込まれます。")
	}
	if documentEnd {
		// 既存の要素が変わらなくても、閉じない構文に追加全体が消える場合がある。
		marker := htmlMarker(src.Text)
		marked, _, e := insertionText(src.Text, point, marker)
		if e != nil {
			return e
		}
		probe, err := s.opts.Host.Trial(ctx, src, marked)
		if err != nil {
			return fault(422, "render", "編集後の文書を描画できません。")
		}
		root, err := htmltree.Fragment(ctx, probe.HTML)
		if err != nil {
			return fault(422, "insert", "追加した文が前後のブロックに取り込まれます。")
		}
		found := false
		for n := root.FirstChild; n != nil; n = n.NextSibling {
			if n.Type == html.ElementNode && n.Data == "p" && n.FirstChild != nil && n.FirstChild == n.LastChild &&
				n.FirstChild.Type == html.TextNode && n.FirstChild.Data == marker {
				found = true
			}
		}
		if !found {
			return fault(422, "insert", "追加した文が前後のブロックに取り込まれます。")
		}
	}
	return nil
}

// 追加の行に収まる新しい要素だけを除き、既存の要素名と位置を木の順に比べる。
// 一覧で範囲を決められた対象は、実際の範囲で比べる。上流の公開の終端（終了列 0、入れ物の中の水平線）は、
// 後ろに続く内容で変わるので、公開の範囲のままでは、リストの後ろへの追加などを誤って断る。
// 利用者が書いた位置属性は行が増えても値が変わらないので、ずらさずに照合する。
func insertionPreserves(ctx context.Context, m, after *blockMapping, oldHTML, newHTML string, cut, delta, prefix int) bool {
	type positioned struct {
		positionAttribute
		parents string
	}
	positions := func(markup string, before bool) ([]positioned, error) {
		corrected := after.correctedRanges()
		if before {
			corrected = m.correctedRanges()
		}
		root, err := htmltree.Fragment(ctx, markup)
		if err != nil {
			return nil, err
		}
		out := []positioned{}
		err = htmltree.Walk(ctx, root, func(n *html.Node) error {
			if n.Type != html.ElementNode || !htmltree.Has(n, "data-sourcepos") {
				return nil
			}
			p := positionAttribute{n.Data, "data-sourcepos", htmltree.Get(n, "data-sourcepos")}
			if !m.untrusted[p] {
				if actual, ok := corrected[p]; ok {
					p.value = string(actual)
				}
				c, e := positionCoordinates(Range(p.value))
				if e == nil {
					if before {
						// バッファの文字に BOM がある場合、移る元の先頭行からだけ BOM の列を除く。
						if c[0] == 1 {
							c[1] -= prefix
						}
						if c[2] == 1 {
							c[3] -= prefix
						}
						if c[0] >= cut {
							c[0] += delta
							c[2] += delta
						} else if c[2] >= cut {
							// 追加点をまたぐ既存の要素は、外側のブロックへ追加できていない。
							return blockFault("boundary")
						}
						p.value = string(rangeOf(c))
					} else if c[0] >= cut && c[0] < cut+delta && c[2] < cut+delta {
						return nil
					}
				}
			}
			// 閉じていない details のように、位置は同じまま既存の対象を包む追加も断る。
			parents := ""
			for parent := n.Parent; parent != nil; parent = parent.Parent {
				if parent.Type == html.ElementNode {
					parents += "/" + parent.Data
				}
			}
			out = append(out, positioned{p, parents})
			return nil
		})
		return out, err
	}
	old, err := positions(oldHTML, true)
	if err != nil {
		return false
	}
	next, err := positions(newHTML, false)
	return err == nil && slices.Equal(old, next)
}

// 位置のない節点も、追加点の外では最上位の並びと形（要素名・入れ子・文字）を保つ。
// 追加点のある並びへ節点が増える場合は、既存の節点が同じ形のまま残れば受ける。
// 要素名だけで比べると、文書の最後の連なりの前に閉じない要素を足して、既存の節点を中へ取り込む形を見逃す。
// 追加先が生の HTML の中にあるときは、追加先を中に持つ節点だけ、中身が増えるので要素名で比べる。
// 同じ並びにいるほかの節点は、形で比べる（追加した文が、隣の節点を取り込む形を断る）。
func insertionHTMLPreserves(ctx context.Context, m, after *blockMapping, oldHTML, newHTML string, cut, delta int, anchor *block) bool {
	bucket := 0
	type shape struct {
		tag, signature string
		holds          bool
	}
	// 追加先（位置を持つ対象）を中に持つ節点かどうか。連なりそのものが追加先のときは、どの節点も持たない。
	holds := func(n *html.Node) bool {
		found := false
		if anchor != nil && anchor.tag != "html" {
			_ = htmltree.Walk(ctx, n, func(c *html.Node) error {
				if c.Type == html.ElementNode && c.Data == anchor.tag && htmltree.Get(c, "data-sourcepos") == string(anchor.public) {
					found = true
				}
				return nil
			})
		}
		return found
	}
	collect := func(markup string, mapping *blockMapping, before bool) ([][]shape, error) {
		root, err := htmltree.Fragment(ctx, markup)
		if err != nil {
			return nil, err
		}
		groups := [][]shape{{}}
		for n := root.FirstChild; n != nil; n = n.NextSibling {
			if footnoteSection(n) {
				continue
			}
			if b := mapping.htmlBoundary(n); b != nil {
				r, e := mapping.boundaryRange(b)
				if e != nil {
					r = b.Range
				}
				c, e := positionCoordinates(r)
				if e != nil {
					return nil, e
				}
				if !before && c[0] >= cut && c[0] < cut+delta && c[2] < cut+delta {
					continue
				}
				groups = append(groups, []shape{})
				if before && (c[2] < cut || c[2] == cut && c[3] == 0) {
					bucket = len(groups) - 1
				}
			} else if n.Type == html.ElementNode {
				groups[len(groups)-1] = append(groups[len(groups)-1], shape{n.Data, mapping.htmlSignature(n), before && holds(n)})
			} else if n.Type == html.TextNode && strings.TrimSpace(n.Data) != "" {
				groups[len(groups)-1] = append(groups[len(groups)-1], shape{"text", mapping.htmlSignature(n), false})
			}
		}
		return groups, nil
	}
	old, e := collect(oldHTML, m, true)
	if e != nil {
		return false
	}
	next, e := collect(newHTML, after, false)
	if e != nil || len(old) != len(next) {
		return false
	}
	for i, group := range old {
		if i != bucket {
			if !slices.Equal(group, next[i]) {
				return false
			}
			continue
		}
		index := 0
		for _, candidate := range next[i] {
			if index < len(group) && candidate.tag == group[index].tag && (group[index].holds || candidate.signature == group[index].signature) {
				index++
			}
		}
		if index != len(group) {
			return false
		}
	}
	return true
}
