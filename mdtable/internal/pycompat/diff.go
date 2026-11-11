package pycompat

import (
	"sort"
	"strconv"
	"strings"
)

// Opcode は Python の get_opcodes が返す変更区間を表す。
type Opcode struct {
	Tag            string
	I1, I2, J1, J2 int
}

type match struct{ i, j, size int }

// Opcodes は autojunk=False の SequenceMatcher と同じ区間を返す。
func Opcodes(a, b []string) []Opcode { return opcodes(a, b, false) }

func opcodes(a, b []string, autojunk bool) []Opcode {
	b2j := make(map[string][]int)
	for j, s := range b {
		b2j[s] = append(b2j[s], j)
	}
	if autojunk && len(b) >= 200 {
		for s, js := range b2j {
			if len(js) > len(b)/100+1 {
				delete(b2j, s)
			}
		}
	}
	longest := func(alo, ahi, blo, bhi int) match {
		best := match{alo, blo, 0}
		prev := make(map[int]int)
		for i := alo; i < ahi; i++ {
			next := make(map[int]int)
			for _, j := range b2j[a[i]] {
				if j < blo {
					continue
				}
				if j >= bhi {
					break
				}
				k := prev[j-1] + 1
				next[j] = k
				if k > best.size {
					best = match{i - k + 1, j - k + 1, k}
				}
			}
			prev = next
		}
		for best.i > alo && best.j > blo && a[best.i-1] == b[best.j-1] {
			best.i--
			best.j--
			best.size++
		}
		for best.i+best.size < ahi && best.j+best.size < bhi && a[best.i+best.size] == b[best.j+best.size] {
			best.size++
		}
		return best
	}
	queue := [][4]int{{0, len(a), 0, len(b)}}
	blocks := []match{}
	for len(queue) > 0 {
		x := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		m := longest(x[0], x[1], x[2], x[3])
		if m.size == 0 {
			continue
		}
		blocks = append(blocks, m)
		if x[0] < m.i && x[2] < m.j {
			queue = append(queue, [4]int{x[0], m.i, x[2], m.j})
		}
		if m.i+m.size < x[1] && m.j+m.size < x[3] {
			queue = append(queue, [4]int{m.i + m.size, x[1], m.j + m.size, x[3]})
		}
	}
	sort.Slice(blocks, func(i, j int) bool {
		if blocks[i].i != blocks[j].i {
			return blocks[i].i < blocks[j].i
		}
		return blocks[i].j < blocks[j].j
	})
	merged := []match{}
	for _, m := range blocks {
		if len(merged) > 0 {
			p := &merged[len(merged)-1]
			if p.i+p.size == m.i && p.j+p.size == m.j {
				p.size += m.size
				continue
			}
		}
		merged = append(merged, m)
	}
	merged = append(merged, match{len(a), len(b), 0})
	result := []Opcode{}
	i, j := 0, 0
	for _, m := range merged {
		tag := ""
		if i < m.i && j < m.j {
			tag = "replace"
		} else if i < m.i {
			tag = "delete"
		} else if j < m.j {
			tag = "insert"
		}
		if tag != "" {
			result = append(result, Opcode{tag, i, m.i, j, m.j})
		}
		if m.size > 0 {
			result = append(result, Opcode{"equal", m.i, m.i + m.size, m.j, m.j + m.size})
		}
		i, j = m.i+m.size, m.j+m.size
	}
	return result
}

// UnifiedDiff は Python と同じ既定の autojunk と文脈 3 行で差分を書く。
func UnifiedDiff(a, b []string, fromfile, tofile string) string {
	codes := opcodes(a, b, true)
	if len(codes) == 0 {
		return ""
	}
	if c := &codes[0]; c.Tag == "equal" {
		c.I1 = max(c.I1, c.I2-3)
		c.J1 = max(c.J1, c.J2-3)
	}
	if c := &codes[len(codes)-1]; c.Tag == "equal" {
		c.I2 = min(c.I2, c.I1+3)
		c.J2 = min(c.J2, c.J1+3)
	}
	groups := [][]Opcode{}
	group := []Opcode{}
	for _, c := range codes {
		if c.Tag == "equal" && c.I2-c.I1 > 6 {
			group = append(group, Opcode{c.Tag, c.I1, c.I1 + 3, c.J1, c.J1 + 3})
			groups = append(groups, group)
			group = []Opcode{}
			c.I1, c.J1 = c.I2-3, c.J2-3
		}
		group = append(group, c)
	}
	if len(group) > 0 && !(len(group) == 1 && group[0].Tag == "equal") {
		groups = append(groups, group)
	}
	if len(groups) == 0 {
		return ""
	}
	var out strings.Builder
	out.WriteString("--- " + fromfile + "\n+++ " + tofile + "\n")
	for _, g := range groups {
		first, last := g[0], g[len(g)-1]
		out.WriteString("@@ -" + unifiedRange(first.I1, last.I2) + " +" + unifiedRange(first.J1, last.J2) + " @@\n")
		for _, c := range g {
			if c.Tag == "equal" {
				for _, s := range a[c.I1:c.I2] {
					out.WriteString(" " + s)
				}
				continue
			}
			if c.Tag == "replace" || c.Tag == "delete" {
				for _, s := range a[c.I1:c.I2] {
					out.WriteString("-" + s)
				}
			}
			if c.Tag == "replace" || c.Tag == "insert" {
				for _, s := range b[c.J1:c.J2] {
					out.WriteString("+" + s)
				}
			}
		}
	}
	return out.String()
}

func unifiedRange(start, stop int) string {
	n := stop - start
	beginning := start + 1
	if n == 1 {
		return strconv.Itoa(beginning)
	}
	if n == 0 {
		beginning--
	}
	return strconv.Itoa(beginning) + "," + strconv.Itoa(n)
}
