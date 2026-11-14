package edit

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/ryo-aoki-pc/md-tools/internal/htmltree"
	"github.com/ryo-aoki-pc/md-tools/internal/session"
	"github.com/ryo-aoki-pc/md-tools/mdtable/tableedit"
	"golang.org/x/net/html"
)

type documentState struct {
	Editable bool   `json:"editable"`
	Reason   string `json:"reason"`
}
type cellInfo struct {
	Row         int    `json:"row"`
	Col         int    `json:"col"`
	Rowspan     int    `json:"rowspan"`
	Colspan     int    `json:"colspan"`
	Header      bool   `json:"header"`
	Align       string `json:"align"`
	Text        string `json:"text"`
	Raw         bool   `json:"raw"`
	Source      string `json:"source"`
	Tag         string `json:"tag"`
	CopyLocked  string `json:"copyLocked"`
	MergeLocked string `json:"mergeLocked"`
}
type tableInfo struct {
	Index         int                 `json:"index"`
	Start         int                 `json:"start"`
	Number        int                 `json:"number"`
	Kind          tableedit.Kind      `json:"kind"`
	Editable      bool                `json:"editable"`
	Reason        string              `json:"reason"`
	Anchor        Range               `json:"anchor"`
	AnchorReason  string              `json:"anchorReason"`
	Rows          int                 `json:"rows"`
	Cols          int                 `json:"cols"`
	RowsLocked    string              `json:"rowsLocked"`
	ColsLocked    string              `json:"colsLocked"`
	CellsLocked   string              `json:"cellsLocked"`
	SortLocked    []string            `json:"sortLocked"`
	Sections      []tableedit.Section `json:"sections"`
	Warnings      []string            `json:"warnings"`
	Cells         []cellInfo          `json:"cells"`
	Hash          string              `json:"hash"`
	RowTags       []string            `json:"rowTags"`
	Delimiters    []string            `json:"delimiters"`
	CopyLocked    string              `json:"copyLocked"`
	ConvertLocked map[string]string   `json:"convertLocked"`
	DeleteLocked  string              `json:"deleteLocked"`
}
type tablesResponse struct {
	BlockMoves []*blockMoveInfo `json:"blockMoves"`
	// 文書の先頭へ追加したときに、下書きの位置をずらす最初の行（front matter の後ろ）。
	InsertStart int `json:"insertStart"`
	// 空白と空行だけの文書。追加した文で全体を置き換える（ページは「最初のブロックを書く」にする）。
	Empty    bool          `json:"empty"`
	Render   uint64        `json:"render"`
	Document documentState `json:"document"`
	Undo     undoState     `json:"undo"`
	Tables   []tableInfo   `json:"tables"`
	Headings []headingInfo `json:"headings"`
	Origin   string        `json:"origin"`
	Version  string        `json:"version"`
}
type mapping struct {
	response tablesResponse
	document *tableedit.Document
	models   []*tableedit.Table
}

func tableNodes(ctx context.Context, root *html.Node) ([]*html.Node, error) {
	return htmltree.Elements(ctx, root, func(n *html.Node) bool { return n.Data == "table" })
}
func (s *Service) tables(ctx context.Context, src session.Source) (*mapping, *Error) {
	key := pathKey(src.Path)
	if m := s.cache[key]; m != nil && m.response.Render == src.Render {
		return m, nil
	}
	positions, e := s.blocks(ctx, src)
	if e != nil {
		// 位置の検査はブロックの一覧と共有する。誤りの文は表の言葉にする。
		if e.Code == "cancelled" {
			return nil, fault(422, "cancelled", "表の対応の確認を取り消しました。")
		}
		return nil, fault(422, "render", "描画した表を読み取れません。")
	}
	root, err := htmltree.Fragment(ctx, src.HTML)
	if err != nil {
		return nil, fault(422, "render", "描画した表を読み取れません。")
	}
	nodes, err := tableNodes(ctx, root)
	if err != nil {
		return nil, fault(422, "render", "描画した表を読み取れません。")
	}
	m := &mapping{response: tablesResponse{Render: src.Render, Document: documentState{true, ""}, Tables: make([]tableInfo, len(nodes))}, document: tableedit.Parse(src.Text), models: make([]*tableedit.Table, len(nodes))}
	models := m.document.Tables()
	pipes := map[int]*tableedit.Table{}
	htmlModels := []*tableedit.Table{}
	for _, t := range models {
		if t.Kind == "pipe" {
			pipes[t.StartLine] = t
		}
		if t.Kind == "html" && t.Excluded == "" && t.Tag != nil {
			htmlModels = append(htmlModels, t)
		}
	}
	for i, n := range nodes {
		if positions.positionReason(n, "data-sourcepos") != nil {
			continue
		}
		pos, e := coordinates(Range(htmltree.Get(n, "data-sourcepos")))
		if e != nil {
			continue
		}
		line, reason := src.Lines.Original(pos[0])
		if reason == "" {
			m.models[i] = pipes[line-1]
		}
	}
	if len(htmlModels) > 0 && positions.positionErr == nil {
		var nonce [16]byte
		if _, err = rand.Read(nonce[:]); err != nil {
			return nil, fault(500, "internal", "表の対応を確かめる印を作れません。")
		}
		prefix := "gpe-" + hex.EncodeToString(nonce[:]) + "-"
		// 要求ごとの印が元からある場合は、別の乱数で再試行せず安全に断る。
		if !strings.Contains(src.Text, prefix) && !strings.Contains(src.HTML, prefix) {
			sort.Slice(htmlModels, func(i, j int) bool { return htmlModels[i].Tag.NameEnd < htmlModels[j].Tag.NameEnd })
			var marked strings.Builder
			marked.Grow(len(src.Text) + len(htmlModels)*(len(prefix)+32))
			last := 0
			valid := true
			byNumber := map[int]*tableedit.Table{}
			// 差し込み前の位置を使って一度で組み立て、末尾から差し込む場合と同じ本文にする。
			for _, t := range htmlModels {
				p := t.Tag.NameEnd
				if p < last || p > len(src.Text) {
					valid = false
					break
				}
				marked.WriteString(src.Text[last:p])
				fmt.Fprintf(&marked, ` data-sourcepos="%s%d"`, prefix, t.Number)
				last = p
				byNumber[t.Number] = t
			}
			if valid {
				marked.WriteString(src.Text[last:])
				trial, e := s.opts.Host.Trial(ctx, src, marked.String())
				if e == nil {
					trialRoot, e := htmltree.Fragment(ctx, trial.HTML)
					if e == nil && sameTree(ctx, root, trialRoot, prefix, byNumber) {
						trialTables, e := tableNodes(ctx, trialRoot)
						if e == nil && len(nodes) == len(trialTables) {
							for i, n := range trialTables {
								if number, ok := marker(htmltree.Get(n, "data-sourcepos"), prefix, byNumber); ok {
									m.models[i] = byNumber[number]
								}
							}
						}
					}
				}
			}
		}
	}
	counts := map[int]int{}
	for _, t := range m.models {
		if t != nil {
			counts[t.Number]++
		}
	}
	for i, n := range nodes {
		if ctx.Err() != nil {
			return nil, fault(422, "cancelled", "表の対応の確認を取り消しました。")
		}
		info := tableInfo{Index: i, Reason: "原文の表と対応づけられない", Warnings: []string{}, Cells: []cellInfo{}, RowTags: []string{}, Delimiters: []string{}, Sections: []tableedit.Section{}}
		if positions.positionReason(n, "data-sourcepos") == nil {
			if pos, err := coordinates(Range(htmltree.Get(n, "data-sourcepos"))); err == nil {
				if _, reason := src.Lines.Original(pos[0]); reason != "shift" {
					info.Start = pos[0]
				}
			}
		}
		t := m.models[i]
		if t != nil {
			info.Number, info.Kind, info.Rows, info.Cols = t.Number, t.Kind, t.Rows, t.Cols
			info.RowsLocked, info.ColsLocked = t.RowsLocked, t.ColsLocked
			info.CellsLocked = t.CellsLocked
			info.SortLocked = t.SortLocked
			info.Sections = append(info.Sections, t.Sections...)
			info.ConvertLocked = map[string]string{"html": t.ConvertLocked("html"), "pipe": t.ConvertLocked("pipe")}
			info.DeleteLocked = t.DeleteLocked()
			info.Warnings = append(info.Warnings, t.Warnings...)
			switch {
			case counts[t.Number] > 1:
				info.Reason = "複数の表が同じ原文を指している"
			case htmltree.Ancestor(n, func(p *html.Node) bool { return p.Type == html.ElementNode && p.Data == "table" }) || t.Parent != 0:
				info.Reason = "入れ子の表"
			case t.Excluded != "":
				info.Reason = t.Excluded
			case !sameGrid(n, t):
				info.Reason = "描画した表と原文のセルの形が違う"
			default:
				info.Editable, info.Reason = true, ""
				info.Hash, info.CopyLocked = t.SourceHash, t.CopyLocked
				info.RowTags = append(info.RowTags, t.RowTags...)
				info.Delimiters = append(info.Delimiters, t.Delimiters...)
				for _, c := range t.Cells {
					info.Cells = append(info.Cells, cellInfo{Row: c.Row, Col: c.Col, Rowspan: c.Rowspan, Colspan: c.Colspan, Header: c.Header, Align: c.Align, Text: c.Text, Raw: c.Complex, Source: c.Source, Tag: c.Tag, CopyLocked: c.CopyLocked, MergeLocked: c.MergeLocked})
				}
			}
		} else if htmltree.Has(n, "data-table-fields") {
			info.Reason = "JSON から作った表"
		} else if pos, e := coordinates(Range(htmltree.Get(n, "data-sourcepos"))); e == nil {
			if _, reason := src.Lines.Original(pos[0]); reason == "include" {
				info.Reason = "取り込んだファイルの表"
			}
		}
		if positions.positionErr != nil {
			info.Editable, info.Reason = false, positions.positionErr.Message
		}
		// 位置属性の無い表（生の HTML の表）は、原文と対応づけたモデルの開始行を公開の行にする。
		if info.Start == 0 && info.Editable && t != nil && t.StartLine >= 0 {
			if _, reason := src.Lines.Original(1); reason != "shift" {
				line := src.Lines.Expanded(t.StartLine + 1)
				if orig, reason := src.Lines.Original(line); reason == "" && orig == t.StartLine+1 {
					info.Start = line
				}
			}
		}
		m.response.Tables[i] = info
		m.response.Tables[i].Anchor, m.response.Tables[i].AnchorReason = tableAnchor(ctx, src, root, n, t, info)
	}
	if _, reason := src.Lines.Original(1); reason == "shift" {
		m.response.Document = documentState{false, reasonText(reason)}
	}
	// 文書ごとに最新の描画だけを保持する。開かれる文書が増えても上限を設ける。
	if len(s.cache) >= historyDocuments {
		for k := range s.cache {
			delete(s.cache, k)
			delete(s.blockCache, k)
			break
		}
	}
	s.cache[key] = m
	return m, nil
}
func marker(value, prefix string, models map[int]*tableedit.Table) (int, bool) {
	if !strings.HasPrefix(value, prefix) {
		return 0, false
	}
	n, e := strconv.Atoi(strings.TrimPrefix(value, prefix))
	return n, e == nil && models[n] != nil && value == prefix+strconv.Itoa(n)
}

// セルの形まで一致した表だけを使う。対応が曖昧な表では、原文の行も追加先として信用しない。
func tableAnchor(ctx context.Context, src session.Source, root, n *html.Node, t *tableedit.Table, info tableInfo) (Range, string) {
	if !info.Editable || t == nil {
		return "", info.Reason
	}
	children, err := tableNodes(ctx, n)
	if err != nil || len(children) != 1 || t.Parent != 0 {
		return "", "入れ子の表の前後には追加できません"
	}
	if n.Parent != root {
		return "", "最上位の表の前後だけに追加できます"
	}
	starts := lineStarts(src.Text)
	if t.StartLine < 0 || t.EndLine < t.StartLine || t.EndLine >= len(starts) {
		return "", "表の原文の行を確かめられません"
	}
	end := len(src.Text)
	if t.EndLine+1 < len(starts) {
		end = starts[t.EndLine+1] - 1
	}
	if end > starts[t.EndLine] && src.Text[end-1] == '\r' {
		end--
	}
	r := rangeOf([4]int{src.Lines.Expanded(t.StartLine + 1), 1, src.Lines.Expanded(t.EndLine + 1), end - starts[t.EndLine]})
	a, b, _, e := offsetsAt(src, r, starts)
	if e != nil || a != starts[t.StartLine] || b != end {
		return "", "表の原文の連続した行を確かめられません"
	}
	if t.Kind == "html" {
		raw := strings.TrimSpace(src.Text[a:b])
		lower := strings.ToLower(raw)
		if !strings.HasPrefix(lower, "<table") || !strings.HasSuffix(lower, "</table>") ||
			t.Tag == nil || t.Tag.Start < a || t.Tag.Start > b || strings.TrimSpace(src.Text[a:t.Tag.Start]) != "" || !singleHTMLTable(raw) {
			return "", "表の最初と最後の行に表以外の文字があります"
		}
	}
	return r, ""
}

// 同じ行に別の表や節点が並んでいるときに、まとめて追加先にしない。
func singleHTMLTable(raw string) bool {
	z := html.NewTokenizer(strings.NewReader(raw))
	depth, count := 0, 0
	for {
		kind := z.Next()
		if kind == html.ErrorToken {
			return depth == 0 && count == 1
		}
		token := z.Token()
		if kind == html.StartTagToken && token.Data == "table" {
			if depth == 0 {
				count++
			}
			depth++
		} else if kind == html.EndTagToken && token.Data == "table" {
			depth--
			if depth < 0 {
				return false
			}
		} else if depth == 0 && (kind != html.TextToken || strings.TrimSpace(token.Data) != "") {
			return false
		}
	}
}
