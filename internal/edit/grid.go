package edit

import (
	"strconv"
	"strings"

	"github.com/ryo-aoki-pc/md-tools/internal/htmltree"
	"github.com/ryo-aoki-pc/md-tools/mdtable/tableedit"
	"golang.org/x/net/html"
)

func span(n *html.Node, name string, max int, zero bool) int {
	value := strings.TrimSpace(htmltree.Get(n, name))
	if value == "" {
		return 1
	}
	// HTML の非負整数は先頭の + と数字の後ろの文字を許す。
	value = strings.TrimPrefix(value, "+")
	end := 0
	for end < len(value) && value[end] >= '0' && value[end] <= '9' {
		end++
	}
	if end == 0 {
		return 1
	}
	v, err := strconv.ParseUint(value[:end], 10, 64)
	if err != nil || v > uint64(max) {
		return max
	}
	if v == 0 && !zero {
		return 1
	}
	return int(v)
}
func sameGrid(n *html.Node, t *tableedit.Table) bool {
	groups := [][]*html.Node{}
	direct := []*html.Node{}
	flush := func() {
		if len(direct) > 0 {
			groups = append(groups, direct)
			direct = nil
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode {
			continue
		}
		if c.Data == "tr" {
			direct = append(direct, c)
			continue
		}
		flush()
		if c.Data == "thead" || c.Data == "tbody" || c.Data == "tfoot" {
			rows := []*html.Node{}
			for r := c.FirstChild; r != nil; r = r.NextSibling {
				if r.Type == html.ElementNode && r.Data == "tr" {
					rows = append(rows, r)
				}
			}
			groups = append(groups, rows)
		}
	}
	flush()
	rows := 0
	for _, g := range groups {
		rows += len(g)
	}
	if rows != t.Rows {
		return false
	}
	row, index, width := 0, 0, 0
	for _, g := range groups {
		occupied := map[int]int{}
		for local, r := range g {
			col := 0
			for c := r.FirstChild; c != nil; c = c.NextSibling {
				if c.Type != html.ElementNode || c.Data != "th" && c.Data != "td" {
					continue
				}
				for occupied[col] > local {
					col++
				}
				cs, rs := span(c, "colspan", 1000, false), span(c, "rowspan", 65534, true)
				if rs == 0 || rs > len(g)-local {
					rs = len(g) - local
				}
				if index >= len(t.Cells) || col+cs > t.Cols {
					return false
				}
				model := t.Cells[index]
				if model.Row != row || model.Col != col || model.Rowspan != rs || model.Colspan != cs || model.Header != (c.Data == "th") {
					return false
				}
				for x := col; x < col+cs; x++ {
					if occupied[x] > local {
						return false
					}
					occupied[x] = local + rs
				}
				col += cs
				if col > width {
					width = col
				}
				index++
			}
			row++
		}
	}
	return index == len(t.Cells) && width == t.Cols
}
