package edit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"

	"github.com/ryo-aoki-pc/md-tools/internal/session"
)

type ReviewTarget struct {
	Kind            string
	Tag             string
	Range           Range
	Prev, Next      *HTMLBoundary
	Table, Row, Col int
}

type ReviewDocument struct {
	Root, RelPath string
	Render        uint64
	Hash          string
}

type ReviewItem struct {
	Kind               string
	StartLine, EndLine int
	Excerpt            string
	Table, Row, Col    int
}

// Review は描画の一覧から対象と原文を引き、ファイルを変えずに依頼文の資料を返す。
func (s *Service) Review(ctx context.Context, doc *string, render uint64, targets []ReviewTarget) (ReviewDocument, []ReviewItem, *Error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var document ReviewDocument
	if render == 0 || len(targets) == 0 {
		return document, nil, invalid()
	}
	src, e := s.source(doc, render)
	if e != nil {
		return document, nil, e
	}
	if src.Path == "" {
		return document, nil, fault(422, "unnamed", "名前の無い文書は送れません。ファイルを開いてから使ってください。")
	}
	if _, _, e = s.read(src); e != nil {
		return document, nil, e
	}
	items := make([]ReviewItem, 0, len(targets))
	for _, target := range targets {
		if ctx.Err() != nil {
			return document, nil, fault(422, "cancelled", "原文の読み取りを取り消しました。")
		}
		var item ReviewItem
		switch target.Kind {
		case "block", "html":
			m, err := s.blocks(ctx, src)
			if err != nil {
				return document, nil, err
			}
			var b *block
			if target.Kind == "block" {
				if !tagPattern.MatchString(target.Tag) {
					return document, nil, invalid()
				}
				if _, err = ParseSourceRange(target.Tag, string(target.Range)); err == nil {
					b, err = m.target(target.Tag, target.Range)
				}
			} else {
				if err = validateHTMLBoundaries(target.Prev, target.Next); err == nil {
					b, err = s.htmlTarget(ctx, src, m, target.Prev, target.Next)
				}
			}
			if err != nil {
				return document, nil, err
			}
			item, e = reviewBlock(src, b)
			if e != nil {
				return document, nil, e
			}
		case "cell":
			m, err := s.tables(ctx, src)
			if err != nil {
				return document, nil, err
			}
			if target.Table < 0 || target.Table >= len(m.response.Tables) {
				return document, nil, fault(422, "excluded", "指定した表がありません。")
			}
			t := m.response.Tables[target.Table]
			if !t.Editable {
				return document, nil, fault(422, "excluded", t.Reason)
			}
			if target.Row < 0 || target.Row >= t.Rows || target.Col < 0 || target.Col >= t.Cols {
				return document, nil, fault(422, "range", "指定したセルが範囲外です。")
			}
			var cell *cellInfo
			for i := range t.Cells {
				c := &t.Cells[i]
				if c.Row == target.Row && c.Col == target.Col {
					cell = c
					break
				}
				if target.Row >= c.Row && target.Row < c.Row+c.Rowspan && target.Col >= c.Col && target.Col < c.Col+c.Colspan {
					return document, nil, fault(422, "merged", "結合で覆われた位置です。結合セルの左上を選んでください。")
				}
			}
			if cell == nil {
				return document, nil, fault(422, "range", "指定したセルがありません。")
			}
			model := m.models[target.Table]
			// 表の解析器は、include 展開前の原文を 0 始まりの行で数える。
			item = ReviewItem{Kind: "表のセル", StartLine: model.StartLine + 1, EndLine: model.EndLine + 1, Excerpt: removeCR(cell.Source), Table: target.Table + 1, Row: target.Row + 1, Col: target.Col + 1}
		default:
			return document, nil, invalid()
		}
		items = append(items, item)
	}
	// 試しの描画中に外で変わった場合も、古い原文から依頼文を作らない。
	if _, _, e = s.read(src); e != nil {
		return document, nil, e
	}
	if _, e = s.source(doc, render); e != nil {
		return document, nil, e
	}
	sum := sha256.Sum256([]byte(src.Text))
	document = ReviewDocument{Root: src.Root, RelPath: filepath.ToSlash(src.File), Render: src.Render, Hash: hex.EncodeToString(sum[:])}
	return document, items, nil
}

func reviewBlock(src session.Source, b *block) (ReviewItem, *Error) {
	c, e := coordinates(b.actual)
	if e != nil {
		return ReviewItem{}, e
	}
	first, reason := src.Lines.Original(c[0])
	if reason != "" {
		return ReviewItem{}, fault(422, reason, reasonText(reason))
	}
	last, reason := src.Lines.Original(c[2])
	if reason != "" {
		return ReviewItem{}, fault(422, reason, reasonText(reason))
	}
	if c[3] == 0 {
		last--
	}
	kind := map[string]string{
		"paragraph": "段落", "heading": "見出し", "item": "リストの項目", "list": "リスト",
		"deflist": "説明リスト", "term": "用語", "description": "説明", "quote": "引用",
		"code": "コード", "rule": "水平線", "math": "数式", "mermaid": "図", "plantuml": "図",
		"alert": "注記", "footnote": "脚注の定義", "html": "HTML のブロック",
	}[b.kind]
	return ReviewItem{Kind: kind, StartLine: first, EndLine: last, Excerpt: removeCR(src.Text[b.start:b.end])}, nil
}
