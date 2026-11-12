// Package blocks は Markdown のブロックと行ごとの接頭辞を解析する。
package blocks

import (
	"regexp"
	"strings"

	"github.com/ryo-aoki-pc/md-table-excel/internal/pycompat"
)

const (
	DocumentKind        = "document"
	FrontMatter         = "front_matter"
	BlockQuote          = "block_quote"
	Alert               = "alert"
	MultilineBlockQuote = "multiline_block_quote"
	List                = "list"
	Item                = "item"
	DescriptionList     = "description_list"
	DescriptionItem     = "description_item"
	DescriptionTerm     = "description_term"
	DescriptionDetails  = "description_details"
	FootnoteDefinition  = "footnote_definition"
	Paragraph           = "paragraph"
	Heading             = "heading"
	ThematicBreak       = "thematic_break"
	CodeBlock           = "code_block"
	HTMLBlock           = "html_block"
	Table               = "table"
	TableRow            = "table_row"
)

var BlockContainers = map[string]bool{DocumentKind: true, BlockQuote: true, Alert: true, MultilineBlockQuote: true, FootnoteDefinition: true, DescriptionTerm: true, DescriptionDetails: true, Item: true}
var acceptsLines = map[string]bool{Paragraph: true, Heading: true, CodeBlock: true}

const asciiSpace = " \t\n\r\v\f"
const blockTags = "address|article|aside|base|basefont|blockquote|body|caption|center|col|colgroup|dd|details|dialog|dir|div|dl|dt|fieldset|figcaption|figure|footer|form|frame|frameset|h1|h2|h3|h4|h5|h6|head|header|hr|html|iframe|legend|li|link|main|menu|menuitem|nav|noframes|ol|optgroup|option|p|param|search|section|title|summary|table|tbody|td|tfoot|th|thead|tr|track|ul"

var htmlStarts = []*regexp.Regexp{regexp.MustCompile(`(?i)^<(?:script|pre|textarea|style)(?:[ \t\x0b\x0c\r\n]|>)`), regexp.MustCompile(`^<!--`), regexp.MustCompile(`^<\?`), regexp.MustCompile(`^<![A-Za-z]`), regexp.MustCompile(`^<!\[CDATA\[`), regexp.MustCompile(`(?i)^</?(?:` + blockTags + `)(?:[ \t\x0b\x0c\r\n]|/?>)`)}

const attribute = `(?:[ \t\x0b\x0c\r\n]+[a-zA-Z_:][a-zA-Z0-9:._-]*(?:[ \t\x0b\x0c\r\n]*=[ \t\x0b\x0c\r\n]*(?:[^ \t\r\n\x0b\x0c"'=<>` + "`" + `]+|'[^']*'|"[^"]*"))?)`

var htmlStart7 = regexp.MustCompile(`^<(?:[A-Za-z][A-Za-z0-9-]*` + attribute + `*[ \t\x0b\x0c\r\n]*/?>|/[A-Za-z][A-Za-z0-9-]*[ \t\x0b\x0c\r\n]*>)[\t\n\x0c ]*[\r\n]`)
var htmlEnds = map[int]*regexp.Regexp{1: regexp.MustCompile(`(?i)^[^\n]*</(?:script|pre|textarea|style)>`), 2: regexp.MustCompile(`^[^\n]*-->`), 3: regexp.MustCompile(`^[^\n]*\?>`), 4: regexp.MustCompile(`^[^\n]*>`), 5: regexp.MustCompile(`^[^\n]*\]\]>`)}
var atx = regexp.MustCompile(`^#{1,6}(?:[ \t]+|[\r\n])`)
var setext = regexp.MustCompile(`^(=+|-+)[ \t]*[\r\n]`)
var footnote = regexp.MustCompile(`^\[\^[^\] \r\n\t]+\]:[ \t]*`)
var description = regexp.MustCompile(`^[:~][ \t]+`)
var alert = regexp.MustCompile(`(?i)^>+ \[!(?:note|tip|important|warning|caution)\]`)
var tableStart = regexp.MustCompile(`^\|?[ \t\x0b\x0c]*:?-+:?[ \t\x0b\x0c]*(?:\|[ \t\x0b\x0c]*:?-+:?[ \t\x0b\x0c]*)*\|?[ \t\x0b\x0c]*(?:\r\n|\r|\n|$)`)
var frontOpen = regexp.MustCompile(`^(---|\+\+\+|;;;)[A-Za-z0-9_-]*[ \t]*$`)

type Line struct {
	Number, Offset int
	Lazy           bool
}
type Pair struct{ Number, Offset int }
type Info struct {
	Type, BulletChar, Delimiter, FenceChar                                                                        string
	Start, Padding, MarkerOffset, MarkerLength, FenceLength, FenceOffset, Level, Underline, BlockType, NumColumns int
	Tight, Multiline, Fenced, Closed, Setext, OnlyLinkDefinitions                                                 bool
	CloseLine                                                                                                     *int
	Alignments                                                                                                    []string
	Header                                                                                                        Line
	DelimiterLine                                                                                                 Pair
	Rows                                                                                                          []Pair
}
type Node struct {
	Kind                            string
	Parent                          *Node
	Children                        []*Node
	Open                            bool
	StartLine, StartOffset, EndLine int
	Lines                           []Line
	Info                            Info
	LastLineBlank, TableVisited     bool
}

// blocks.Node.__init__ に対応する節の生成。
func NewNode(kind string, line, offset int, info Info) *Node {
	return &Node{Kind: kind, Open: true, StartLine: line, StartOffset: offset, EndLine: line, Info: info}
}

// blocks.Node.append に対応する子の追加。
func (n *Node) Append(c *Node) { c.Parent = n; n.Children = append(n.Children, c) }

// blocks.Node.detach に対応する切り離し。
func (n *Node) Detach() {
	if n.Parent != nil {
		p := n.Parent
		for i, c := range p.Children {
			if c == n {
				p.Children = append(p.Children[:i], p.Children[i+1:]...)
				break
			}
		}
		n.Parent = nil
	}
}

// blocks.Node.insert_before と insert_after に対応する挿入。
func (n *Node) Insert(c *Node, after bool) {
	p := n.Parent
	c.Parent = p
	for i, x := range p.Children {
		if x == n {
			if after {
				i++
			}
			p.Children = append(p.Children, nil)
			copy(p.Children[i+1:], p.Children[i:])
			p.Children[i] = c
			return
		}
	}
}

// blocks.Node.walk に対応する深さ優先の走査。
func (n *Node) Walk() []*Node {
	out := []*Node{n}
	for _, c := range n.Children {
		out = append(out, c.Walk()...)
	}
	return out
}

// blocks.Node.children[-1] に対応する末尾の子の参照。
func (n *Node) last() *Node {
	if len(n.Children) == 0 {
		return nil
	}
	return n.Children[len(n.Children)-1]
}

type Offset struct {
	Node   *Node
	Offset int
}
type LineInfo struct {
	Matched    *Node
	AllMatched bool
	PrefixEnd  int
	Lazy       bool
	Offsets    []Offset
}

// blocks.LineInfo.offset_after に対応する接頭辞の位置。
func (i *LineInfo) OffsetAfter(n *Node) (int, bool) {
	if n.Kind == DocumentKind {
		return 0, true
	}
	for _, o := range i.Offsets {
		if o.Node == n {
			return o.Offset, true
		}
	}
	return 0, false
}

type TableCell struct {
	Start, End   int
	Raw, Content string
}
type TableRowParse struct {
	ParagraphOffset int
	Cells           []TableCell
}

// blocks._strip_ascii_space に対応する ASCII 空白の除去。
func StripASCIISpace(s string) string { return strings.Trim(s, asciiSpace) }

// blocks._table_cell_length に対応する最長一致。
func tableCellLength(s string, i int) int {
	j := i
	for j < len(s) {
		if s[j] == '\r' || s[j] == '\n' {
			break
		}
		if s[j] == '|' && !(j > i && s[j-1] == '\\') {
			break
		}
		j++
	}
	return j - i
}

// blocks._table_cell_end_length に対応する区切り。
func tableCellEndLength(s string, i int) int {
	if i >= len(s) || s[i] != '|' {
		return 0
	}
	j := i + 1
	for j < len(s) && strings.ContainsRune(" \t\v\f", rune(s[j])) {
		j++
	}
	return j - i
}

// blocks._table_row_end_length に対応する行末。
func tableRowEndLength(s string, i int) int {
	j := i
	for j < len(s) && strings.ContainsRune(" \t\v\f", rune(s[j])) {
		j++
	}
	if strings.HasPrefix(s[j:], "\r\n") {
		return j + 2 - i
	}
	if j < len(s) && (s[j] == '\r' || s[j] == '\n') {
		return j + 1 - i
	}
	return 0
}

// blocks.unescape_pipes に対応するパイプのエスケープ解除。
func UnescapePipes(s string) string {
	var out strings.Builder
	for i := 0; i < len(s); {
		if s[i] == '\\' {
			if i+1 < len(s) && s[i+1] == '|' {
				out.WriteByte('|')
			} else {
				out.WriteString(s[i:min(i+2, len(s))])
			}
			i += 2
		} else {
			out.WriteByte(s[i])
			i++
		}
	}
	return out.String()
}

// blocks.parse_table_row に対応する表の行の解析。
func ParseTableRow(s string) *TableRowParse {
	n := len(s)
	var cells []TableCell
	offset := tableCellEndLength(s, 0)
	paragraphOffset := 0
	for offset < n {
		cl := tableCellLength(s, offset)
		pl := tableCellEndLength(s, offset+cl)
		if cl > 0 || pl > 0 {
			raw := s[offset : offset+cl]
			cells = append(cells, TableCell{offset, offset + cl, raw, StripASCIISpace(UnescapePipes(raw))})
		}
		offset += cl + pl
		if pl == 0 {
			re := tableRowEndLength(s, offset)
			offset += re
			if re == 0 || offset == n {
				break
			}
			paragraphOffset = offset
			cells = nil
			offset += tableCellEndLength(s, offset)
		}
	}
	if offset != n || len(cells) == 0 {
		return nil
	}
	return &TableRowParse{paragraphOffset, cells}
}

// blocks.is_delimiter_row に対応する判定。
func IsDelimiterRow(s string) bool { return tableStart.MatchString(s) }

// blocks.parse_list_marker に対応するリスト記号の解析。
func ParseListMarker(s string, pos int, interrupt bool) (int, Info, bool) {
	n := len(s)
	if pos >= n {
		return 0, Info{}, false
	}
	ch := s[pos]
	start := pos
	blankTail := func(i int) bool {
		for i < n && (s[i] == ' ' || s[i] == '\t') {
			i++
		}
		return i == n || s[i] == '\r' || s[i] == '\n'
	}
	if strings.ContainsRune("*-+", rune(ch)) {
		pos++
		if pos < n && !strings.ContainsRune(asciiSpace, rune(s[pos])) {
			return 0, Info{}, false
		}
		if interrupt && blankTail(pos) {
			return 0, Info{}, false
		}
		return pos - start, Info{Type: "bullet", BulletChar: string(ch), Start: 1, Tight: true}, true
	}
	if ch >= '0' && ch <= '9' {
		number, digits := 0, 0
		for {
			number = number*10 + int(s[pos]-'0')
			pos++
			digits++
			if pos == n {
				return 0, Info{}, false
			}
			if !(digits < 9 && s[pos] >= '0' && s[pos] <= '9') {
				break
			}
		}
		if interrupt && number != 1 {
			return 0, Info{}, false
		}
		ch = s[pos]
		if ch != '.' && ch != ')' {
			return 0, Info{}, false
		}
		pos++
		if pos == n || !strings.ContainsRune(asciiSpace, rune(s[pos])) {
			return 0, Info{}, false
		}
		if interrupt && blankTail(pos) {
			return 0, Info{}, false
		}
		return pos - start, Info{Type: "ordered", Delimiter: string(ch), Start: number, Tight: true}, true
	}
	return 0, Info{}, false
}

// blocks._lists_match に対応する比較。
func listsMatch(a, b Info) bool {
	return a.Type == b.Type && a.Delimiter == b.Delimiter && a.BulletChar == b.BulletChar
}

// blocks._can_contain に対応する包含判定。
func canContain(p, c string) bool {
	if c == DocumentKind {
		return false
	}
	if c == FrontMatter {
		return p == DocumentKind
	}
	if BlockContainers[p] {
		return c != Item
	}
	switch p {
	case List:
		return c == Item
	case DescriptionList:
		return c == DescriptionItem
	case DescriptionItem:
		return c == DescriptionTerm || c == DescriptionDetails
	case Table:
		return c == TableRow
	}
	return false
}

const linkLabel = `\[(?:[^\\\[\]]|\\.){0,999}\]`

var linkReference = regexp.MustCompile(`^[ ]{0,3}` + linkLabel + `:[ \t]*(?:\n[ \t]*)?(?:<[^<>\n]*>|[^` + pycompat.ReSpace + `<][^` + pycompat.ReSpace + `]*)(?:(?:[ \t]+|[ \t]*\n[ \t]*)(?:"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'|\((?:[^()\\]|\\.)*\)))?[ \t]*(?:\n|$)`)
var linkLabelRE = regexp.MustCompile(`^[ ]{0,3}(` + linkLabel + `)`)

// blocks.strip_link_reference_definitions に対応する定義の除去。
func StripLinkReferenceDefinitions(s string) string {
	pos := 0
	for pos < len(s) {
		m := linkReference.FindStringIndex(s[pos:])
		if m == nil || m[1] == 0 {
			break
		}
		if pycompat.Strip(s[pos:pos+m[1]]) == "" {
			break
		}
		label := linkLabelRE.FindStringSubmatch(s[pos:])[1]
		if pycompat.Strip(label[1:len(label)-1]) == "" {
			break
		}
		pos += m[1]
	}
	return s[pos:]
}

type parser struct {
	keepDefinitions                                            bool
	sourceLines                                                []string
	root, current                                              *Node
	lineNumber                                                 int
	lineInfos                                                  []*LineInfo
	offset, column, firstNonspace, firstNonspaceColumn, indent int
	blank, partiallyConsumedTab                                bool
	thematicBreakKillPos                                       int
	frontMatter                                                bool
	info                                                       *LineInfo
}

// blocks._Parser.find_first_nonspace に対応する空白の走査。
func (p *parser) findFirstNonspace(s string) {
	toTab := 4 - p.column%4
	if p.firstNonspace <= p.offset {
		p.firstNonspace = p.offset
		p.firstNonspaceColumn = p.column
		for {
			ch := charAt(s, p.firstNonspace)
			if ch == ' ' {
				p.firstNonspace++
				p.firstNonspaceColumn++
				toTab--
				if toTab == 0 {
					toTab = 4
				}
			} else if ch == '\t' {
				p.firstNonspace++
				p.firstNonspaceColumn += toTab
				toTab = 4
			} else {
				break
			}
		}
	}
	p.indent = p.firstNonspaceColumn - p.column
	ch := charAt(s, p.firstNonspace)
	p.blank = ch == 0 || ch == '\r' || ch == '\n'
}

// blocks._Parser.advance_offset に対応する位置の移動。
func (p *parser) advance(s string, count int, columns bool) {
	for count > 0 {
		ch := s[p.offset]
		if ch == '\t' {
			toTab := 4 - p.column%4
			if columns {
				p.partiallyConsumedTab = toTab > count
				advance := min(count, toTab)
				p.column += advance
				if !p.partiallyConsumedTab {
					p.offset++
				}
				count -= advance
			} else {
				p.partiallyConsumedTab = false
				p.column += toTab
				p.offset++
				count--
			}
		} else {
			p.partiallyConsumedTab = false
			p.offset++
			p.column++
			count--
		}
	}
}

// blocks._Parser._char_at に対応する境界付き参照。
func charAt(s string, i int) byte {
	if i < 0 || i >= len(s) {
		return 0
	}
	return s[i]
}

// blocks._Parser.add_child に対応する節の追加。
func (p *parser) addChild(parent *Node, kind string, start int, info Info) *Node {
	for !canContain(parent.Kind, kind) {
		parent = p.finalize(parent)
	}
	n := NewNode(kind, p.lineNumber, start, info)
	parent.Append(n)
	return n
}

// blocks._Parser.add_line に対応する行の追加。
func (p *parser) addLine(n *Node, s string, lazy bool) {
	if p.partiallyConsumedTab {
		p.offset++
	}
	n.Lines = append(n.Lines, Line{p.lineNumber, p.offset, lazy})
	n.EndLine = p.lineNumber
}

// blocks._Parser.finalize に対応する節の確定。
func (p *parser) finalize(n *Node) *Node {
	n.Open = false
	parent := n.Parent
	if n.Kind == Paragraph {
		if StripASCIISpace(StripLinkReferenceDefinitions(p.nodeContent(n))) == "" {
			n.Info.OnlyLinkDefinitions = true
			if !p.keepDefinitions {
				n.Detach()
			}
		}
	} else if n.Kind == List {
		n.Info.Tight = p.listTight(n)
	}
	return parent
}

// blocks._Parser.node_content に対応する中身の連結。
func (p *parser) nodeContent(n *Node) string {
	var b strings.Builder
	for _, l := range n.Lines {
		b.WriteString(p.sourceLines[l.Number][l.Offset:])
		b.WriteByte('\n')
	}
	return b.String()
}

// blocks._Parser._ends_with_blank_line に対応する末尾の空行判定。
func endsWithBlank(n *Node) bool {
	for n != nil {
		if n.LastLineBlank {
			return true
		}
		if n.Kind == List || n.Kind == Item {
			n = n.last()
		} else {
			n = nil
		}
	}
	return false
}

// blocks._Parser._list_tight に対応する空行の判定。
func (p *parser) listTight(n *Node) bool {
	for i, item := range n.Children {
		hasNext := i+1 < len(n.Children)
		if item.LastLineBlank && hasNext {
			return false
		}
		for j, sub := range item.Children {
			if (hasNext || j+1 < len(item.Children)) && endsWithBlank(sub) {
				return false
			}
		}
	}
	return true
}

// blocks._Parser.parse に対応する文書全体の解析。
func (p *parser) parse() *Node {
	lines := p.sourceLines
	start := 0
	if p.frontMatter && len(lines) > 0 {
		m := frontOpen.FindStringSubmatch(lines[0])
		if m != nil {
			for i := 1; i < len(lines); i++ {
				if strings.TrimRight(lines[i], " \t") == m[1] {
					n := NewNode(FrontMatter, 0, 0, Info{})
					n.EndLine = i
					n.Open = false
					p.root.Append(n)
					start = i + 1
					break
				}
			}
		}
	}
	for i := 0; i < start; i++ {
		p.lineInfos = append(p.lineInfos, &LineInfo{Matched: p.root, AllMatched: true})
	}
	p.lineNumber = start - 1
	for i := start; i < len(lines); i++ {
		p.processLine(lines[i] + "\n")
	}
	for p.current != p.root {
		p.current = p.finalize(p.current)
	}
	p.finalize(p.root)
	computeEndLines(p.root, p.sourceLines)
	return p.root
}

// blocks._Parser.process_line に対応する 1 行の処理。
func (p *parser) processLine(s string) {
	p.offset = 0
	p.column = 0
	p.firstNonspace = 0
	p.firstNonspaceColumn = 0
	p.indent = 0
	p.thematicBreakKillPos = 0
	p.blank = false
	p.partiallyConsumedTab = false
	p.lineNumber++
	info := &LineInfo{}
	p.lineInfos = append(p.lineInfos, info)
	p.info = info
	last, all, ok := p.checkOpenBlocks(s)
	if !ok {
		info.PrefixEnd = p.offset
		return
	}
	info.Matched = last
	info.AllMatched = all
	info.PrefixEnd = p.offset
	current := p.current
	container := p.openNewBlocks(last, s, all)
	if current == p.current {
		p.addText(container, last, s, info)
	}
}

// blocks._Parser.check_open_blocks に対応する継続判定。
func (p *parser) checkOpenBlocks(s string) (*Node, bool, bool) {
	c := p.root
	all := false
	for {
		last := c.last()
		if last == nil || !last.Open {
			all = true
			break
		}
		c = last
		p.findFirstNonspace(s)
		matched := true
		switch c.Kind {
		case BlockQuote:
			matched = p.blockQuotePrefix(s)
		case Alert:
			if !c.Info.Multiline {
				matched = p.blockQuotePrefix(s)
			} else if !p.multilinePrefix(s, c) {
				return nil, false, false
			}
		case Item, DescriptionItem:
			matched = p.itemPrefix(s, c)
		case CodeBlock:
			state := p.codePrefix(s, c)
			if state < 0 {
				return nil, false, false
			}
			matched = state > 0
		case HTMLBlock:
			matched = !(c.Info.BlockType >= 6 && p.blank)
		case Paragraph:
			matched = !p.blank
		case Table:
			matched = ParseTableRow(s[p.firstNonspace:]) != nil
		case Heading, TableRow:
			matched = false
		case FootnoteDefinition:
			matched = p.footnotePrefix(s)
		case MultilineBlockQuote:
			if !p.multilinePrefix(s, c) {
				return nil, false, false
			}
		}
		if !matched {
			break
		}
		p.info.Offsets = append(p.info.Offsets, Offset{c, p.offset})
	}
	if !all {
		c = c.Parent
	}
	return c, all, true
}

// blocks._Parser.parse_block_quote_prefix に対応する引用接頭辞。
func (p *parser) blockQuotePrefix(s string) bool {
	if p.indent <= 3 && charAt(s, p.firstNonspace) == '>' {
		p.advance(s, p.indent+1, true)
		if ch := charAt(s, p.offset); ch == ' ' || ch == '\t' {
			p.advance(s, 1, true)
		}
		return true
	}
	return false
}

// blocks._Parser.parse_item_prefix に対応するリストの接頭辞。
func (p *parser) itemPrefix(s string, c *Node) bool {
	need := c.Info.MarkerOffset + c.Info.Padding
	if p.indent >= need {
		p.advance(s, need, true)
		return true
	}
	if p.blank && len(c.Children) > 0 {
		p.advance(s, p.firstNonspace-p.offset, false)
		return true
	}
	return false
}

// blocks._OPEN_CODE_FENCE と _CLOSE_CODE_FENCE の先読みを後続の検査に移したもの。
func fenceRun(s string, pos int, close bool) int {
	ch := charAt(s, pos)
	if ch != '`' && ch != '~' {
		return 0
	}
	j := pos
	for charAt(s, j) == ch {
		j++
	}
	if j-pos < 3 {
		return 0
	}
	tail := s[j:]
	if close {
		tail = strings.TrimLeft(tail, " \t")
		if len(tail) == 0 || (tail[0] != '\r' && tail[0] != '\n') {
			return 0
		}
	} else {
		if ch == '`' {
			if k := strings.IndexAny(tail, "`\r\n"); k < 0 || tail[k] == '`' {
				return 0
			}
		} else if strings.IndexAny(tail, "\r\n") < 0 {
			return 0
		}
	}
	return j - pos
}

// blocks._MULTILINE_FENCE の先読みを後続の検査に移したもの。
func multilineRun(s string, pos int) int {
	j := pos
	for charAt(s, j) == '>' {
		j++
	}
	if j-pos < 3 {
		return 0
	}
	tail := strings.TrimLeft(s[j:], " \t")
	if len(tail) == 0 || (tail[0] != '\r' && tail[0] != '\n') {
		return 0
	}
	return j - pos
}

// blocks._Parser.parse_code_block_prefix に対応するコードの継続。
func (p *parser) codePrefix(s string, c *Node) int {
	info := &c.Info
	if !info.Fenced {
		if p.indent >= 4 {
			p.advance(s, 4, true)
			return 1
		}
		if p.blank {
			p.advance(s, p.firstNonspace-p.offset, false)
			return 1
		}
		return 0
	}
	matched := 0
	if p.indent <= 3 && string(charAt(s, p.firstNonspace)) == info.FenceChar {
		matched = fenceRun(s, p.firstNonspace, true)
	}
	if matched >= info.FenceLength {
		info.Closed = true
		line := p.lineNumber
		info.CloseLine = &line
		c.EndLine = p.lineNumber
		p.advance(s, matched, false)
		p.current = p.finalize(c)
		return -1
	}
	for rem := info.FenceOffset; rem > 0; rem-- {
		ch := charAt(s, p.offset)
		if ch != ' ' && ch != '\t' {
			break
		}
		p.advance(s, 1, true)
	}
	return 1
}

// blocks._Parser.parse_footnote_definition_prefix に対応する脚注の継続。
func (p *parser) footnotePrefix(s string) bool {
	if p.indent >= 4 {
		p.advance(s, 4, true)
		return true
	}
	return s == "\n" || s == "\r\n"
}

// blocks._Parser.parse_multiline_block_quote_prefix に対応する複数行引用の継続。
func (p *parser) multilinePrefix(s string, c *Node) bool {
	matched := 0
	if p.indent <= 3 && charAt(s, p.firstNonspace) == '>' {
		matched = multilineRun(s, p.firstNonspace)
	}
	if matched >= c.Info.FenceLength {
		p.advance(s, matched, false)
		child := c.last()
		if child != nil && child.Open {
			for child.last() != nil && child.last().Open && child.Kind != List {
				child = child.last()
			}
			p.finalize(child)
		}
		c.EndLine = p.lineNumber
		p.current = p.finalize(c)
		return false
	}
	for rem := c.Info.FenceOffset; rem > 0; rem-- {
		ch := charAt(s, p.offset)
		if ch != ' ' && ch != '\t' {
			break
		}
		p.advance(s, 1, true)
	}
	return true
}

// blocks._Parser.open_new_blocks に対応する新しい節の開始。
func (p *parser) openNewBlocks(c *Node, s string, all bool) *Node {
	lazy := p.current.Kind == Paragraph
	depth := 0
	for c.Kind != CodeBlock && c.Kind != HTMLBlock {
		depth++
		p.findFirstNonspace(s)
		indented := p.indent >= 4
		var n *Node
		if !indented {
			handlers := []func(*Node, string) *Node{p.handleAlert, p.handleMultiline, p.handleBlockQuote, p.handleATX, p.handleFence, p.handleHTML, p.handleSetext, func(c *Node, s string) *Node { return p.handleThematic(c, s, all) }, func(c *Node, s string) *Node { return p.handleFootnote(c, s, depth) }, p.handleDescription}
			for _, h := range handlers {
				n = h(c, s)
				if n != nil {
					break
				}
			}
		}
		if n == nil {
			n = p.handleList(c, s, indented, depth)
		}
		if n == nil && indented && !lazy && !p.blank {
			p.advance(s, 4, true)
			n = p.addChild(c, CodeBlock, p.offset, Info{})
		}
		if n == nil {
			n = p.handleTable(c, s, indented)
		}
		if n == nil {
			break
		}
		c = n
		p.info.Offsets = append(p.info.Offsets, Offset{c, p.offset})
		if acceptsLines[c.Kind] {
			break
		}
		lazy = false
	}
	return c
}

// blocks._Parser.handle_alert に対応するアラート開始。
func (p *parser) handleAlert(c *Node, s string) *Node {
	tail := s[p.firstNonspace:]
	if !alert.MatchString(tail) {
		return nil
	}
	length := 0
	for _, ch := range tail {
		if ch == ']' {
			break
		}
		if ch == '>' {
			length++
		}
	}
	if length == 2 {
		return nil
	}
	start := p.firstNonspace
	p.advance(s, len(s)-1-p.offset, false)
	return p.addChild(c, Alert, start, Info{Multiline: length >= 3, FenceLength: length, FenceOffset: p.indent})
}

// blocks._Parser.handle_multiline_blockquote に対応する複数行引用開始。
func (p *parser) handleMultiline(c *Node, s string) *Node {
	matched := multilineRun(s, p.firstNonspace)
	if matched == 0 {
		return nil
	}
	n := p.addChild(c, MultilineBlockQuote, p.firstNonspace, Info{FenceLength: matched, FenceOffset: p.indent})
	p.advance(s, p.firstNonspace+matched-p.offset, false)
	return n
}

// blocks._Parser.handle_blockquote に対応する引用開始。
func (p *parser) handleBlockQuote(c *Node, s string) *Node {
	if charAt(s, p.firstNonspace) != '>' {
		return nil
	}
	start := p.firstNonspace
	p.advance(s, start+1-p.offset, false)
	if ch := charAt(s, p.offset); ch == ' ' || ch == '\t' {
		p.advance(s, 1, true)
	}
	return p.addChild(c, BlockQuote, start, Info{})
}

// blocks._Parser.handle_atx_heading に対応する見出し開始。
func (p *parser) handleATX(c *Node, s string) *Node {
	m := atx.FindString(s[p.firstNonspace:])
	if m == "" {
		return nil
	}
	start := p.firstNonspace
	level := 0
	for charAt(s, start+level) == '#' {
		level++
	}
	matched := len(m)
	if ch := m[len(m)-1]; ch == '\r' || ch == '\n' {
		matched--
	}
	p.advance(s, start+matched-p.offset, false)
	return p.addChild(c, Heading, start, Info{Level: level})
}

// blocks._Parser.handle_code_fence に対応するコードフェンス開始。
func (p *parser) handleFence(c *Node, s string) *Node {
	length := fenceRun(s, p.firstNonspace, false)
	if length == 0 {
		return nil
	}
	start := p.firstNonspace
	n := p.addChild(c, CodeBlock, start, Info{Fenced: true, FenceChar: string(s[start]), FenceLength: length, FenceOffset: p.indent})
	p.advance(s, start+length-p.offset, false)
	return n
}

// blocks._Parser.handle_html_block に対応する HTML 開始。
func (p *parser) handleHTML(c *Node, s string) *Node {
	if charAt(s, p.firstNonspace) != '<' {
		return nil
	}
	kind := 0
	for i, r := range htmlStarts {
		if r.MatchString(s[p.firstNonspace:]) {
			kind = i + 1
			break
		}
	}
	if kind == 0 && c.Kind != Paragraph && htmlStart7.MatchString(s[p.firstNonspace:]) {
		kind = 7
	}
	if kind == 0 {
		return nil
	}
	return p.addChild(c, HTMLBlock, p.firstNonspace, Info{BlockType: kind})
}

// blocks._Parser.handle_setext_heading に対応する下線見出し。
func (p *parser) handleSetext(c *Node, s string) *Node {
	if c.Kind != Paragraph {
		return nil
	}
	m := setext.FindStringSubmatch(s[p.firstNonspace:])
	if m == nil {
		return nil
	}
	if StripASCIISpace(StripLinkReferenceDefinitions(p.nodeContent(c))) != "" {
		level := 2
		if m[1][0] == '=' {
			level = 1
		}
		c.Kind = Heading
		c.Info = Info{Level: level, Setext: true, Underline: p.lineNumber}
		c.EndLine = p.lineNumber
		p.advance(s, len(s)-1-p.offset, false)
	}
	return c
}

// blocks._Parser.handle_thematic_break に対応する区切り線。
func (p *parser) handleThematic(c *Node, s string, all bool) *Node {
	if c.Kind == Paragraph && !all {
		return nil
	}
	if p.thematicBreakKillPos > p.firstNonspace {
		return nil
	}
	pos := p.firstNonspace
	ch := charAt(s, pos)
	if ch != '*' && ch != '_' && ch != '-' {
		p.thematicBreakKillPos = pos
		return nil
	}
	count := 1
	var next byte
	for {
		pos++
		next = charAt(s, pos)
		if next == 0 {
			break
		}
		if next == ch {
			count++
		} else if next != ' ' && next != '\t' {
			break
		}
	}
	if !(count >= 3 && (next == 0 || next == '\r' || next == '\n')) {
		p.thematicBreakKillPos = pos
		return nil
	}
	n := p.addChild(c, ThematicBreak, p.firstNonspace, Info{})
	p.advance(s, len(s)-1-p.offset, false)
	return n
}

// blocks._Parser.handle_footnote に対応する脚注開始。
func (p *parser) handleFootnote(c *Node, s string, depth int) *Node {
	if depth >= 100 {
		return nil
	}
	m := footnote.FindString(s[p.firstNonspace:])
	if m == "" {
		return nil
	}
	start := p.firstNonspace
	p.advance(s, start+len(m)-p.offset, false)
	return p.addChild(c, FootnoteDefinition, start, Info{})
}

// blocks._Parser.handle_description_list に対応する説明リスト開始。
func (p *parser) handleDescription(c *Node, s string) *Node {
	m := description.FindString(s[p.firstNonspace:])
	if m == "" {
		return nil
	}
	details := p.descriptionDetails(c, len(m))
	if details == nil {
		return nil
	}
	p.advance(s, p.firstNonspace+len(m)-p.offset, false)
	if ch := charAt(s, p.offset); ch == ' ' || ch == '\t' {
		p.advance(s, 1, true)
	}
	return details
}

// blocks._Parser._parse_description_details に対応する説明リストの構造。
func (p *parser) descriptionDetails(c *Node, matched int) *Node {
	tight := false
	last := c.last()
	if last == nil {
		if c.Kind != Paragraph || c.Parent == nil {
			return nil
		}
		tight = true
		c = c.Parent
		last = c.last()
	}
	if last.Kind == Paragraph {
		paragraph := last
		paragraph.Detach()
		paragraph.Open = false
		previous := c.last()
		var dl *Node
		if previous != nil && previous.Kind == DescriptionList {
			dl = previous
			for n := dl; n != nil; n = n.Parent {
				n.Open = true
			}
		} else {
			dl = NewNode(DescriptionList, paragraph.StartLine, paragraph.StartOffset, Info{})
			c.Append(dl)
		}
		item := NewNode(DescriptionItem, paragraph.StartLine, paragraph.StartOffset, Info{MarkerOffset: p.indent, Padding: matched, Tight: tight})
		dl.Append(item)
		term := NewNode(DescriptionTerm, paragraph.StartLine, paragraph.StartOffset, Info{})
		item.Append(term)
		term.Append(paragraph)
		details := NewNode(DescriptionDetails, p.lineNumber, p.firstNonspace, Info{})
		item.Append(details)
		return details
	}
	if last.Kind == DescriptionItem {
		item := NewNode(DescriptionItem, p.lineNumber, p.firstNonspace, Info{MarkerOffset: p.indent, Padding: matched, Tight: last.Info.Tight})
		last.Parent.Append(item)
		details := NewNode(DescriptionDetails, p.lineNumber, p.firstNonspace, Info{})
		item.Append(details)
		return details
	}
	return nil
}

// blocks._Parser.handle_list に対応するリスト開始。
func (p *parser) handleList(c *Node, s string, indented bool, depth int) *Node {
	if !((!indented || c.Kind == List) && p.indent < 4 && depth < 100) {
		return nil
	}
	matched, data, ok := ParseListMarker(s, p.firstNonspace, c.Kind == Paragraph)
	if !ok {
		return nil
	}
	start := p.firstNonspace
	p.advance(s, start+matched-p.offset, false)
	savedTab, savedOffset, savedColumn := p.partiallyConsumedTab, p.offset, p.column
	for p.column-savedColumn <= 5 {
		ch := charAt(s, p.offset)
		if ch != ' ' && ch != '\t' {
			break
		}
		p.advance(s, 1, true)
	}
	spaces := p.column - savedColumn
	ch := charAt(s, p.offset)
	if !(spaces >= 1 && spaces < 5) || ch == '\r' || ch == '\n' || ch == 0 {
		data.Padding = matched + 1
		p.partiallyConsumedTab = savedTab
		p.offset = savedOffset
		p.column = savedColumn
		if spaces > 0 {
			p.advance(s, 1, true)
		}
	} else {
		data.Padding = matched + spaces
	}
	data.MarkerOffset = p.indent
	data.MarkerLength = matched
	if c.Kind != List || !listsMatch(c.Info, data) {
		c = p.addChild(c, List, start, data)
	}
	return p.addChild(c, Item, start, data)
}

// blocks._Parser.handle_table と _try_opening_table_header、_try_opening_table_row に対応する表開始。
func (p *parser) handleTable(c *Node, s string, indented bool) *Node {
	if indented {
		return nil
	}
	if c.Kind == Paragraph {
		if c.TableVisited {
			return c
		}
		rest := s[p.firstNonspace:]
		if !tableStart.MatchString(rest) {
			return c
		}
		delim := ParseTableRow(rest)
		if delim == nil {
			c.TableVisited = true
			return c
		}
		header := ParseTableRow(p.nodeContent(c))
		if header == nil || len(header.Cells) != len(delim.Cells) {
			c.TableVisited = true
			return c
		}
		hl := c.Lines[len(c.Lines)-1]
		if header.ParagraphOffset > 0 {
			preface := NewNode(Paragraph, c.StartLine, c.StartOffset, Info{})
			preface.Lines = append([]Line{}, c.Lines[:len(c.Lines)-1]...)
			preface.EndLine = preface.Lines[len(preface.Lines)-1].Number
			preface.Open = false
			c.Insert(preface, false)
		}
		var aligns []string
		for _, cell := range delim.Cells {
			left, right := strings.HasPrefix(cell.Content, ":"), strings.HasSuffix(cell.Content, ":")
			a := ""
			if left && right {
				a = "center"
			} else if left {
				a = "left"
			} else if right {
				a = "right"
			}
			aligns = append(aligns, a)
		}
		table := NewNode(Table, hl.Number, hl.Offset, Info{Alignments: aligns, NumColumns: len(header.Cells), Header: hl, DelimiterLine: Pair{p.lineNumber, p.firstNonspace}})
		table.EndLine = p.lineNumber
		p.advance(s, len(s)-1-p.offset, false)
		c.Insert(table, true)
		c.Detach()
		return table
	}
	if c.Kind == Table {
		if p.blank || ParseTableRow(s[p.firstNonspace:]) == nil {
			return nil
		}
		row := p.addChild(c, TableRow, p.firstNonspace, Info{})
		c.Info.Rows = append(c.Info.Rows, Pair{p.lineNumber, p.firstNonspace})
		c.EndLine = p.lineNumber
		p.advance(s, len(s)-1-p.offset, false)
		return row
	}
	return nil
}

// blocks._Parser.add_text_to_container に対応する本文追加。
func (p *parser) addText(c, last *Node, s string, info *LineInfo) {
	p.findFirstNonspace(s)
	if p.blank && c.last() != nil {
		c.last().LastLineBlank = true
	}
	kind := c.Kind
	lastBlank := true
	switch kind {
	case BlockQuote, Heading, ThematicBreak, MultilineBlockQuote, Alert:
		lastBlank = false
	case CodeBlock:
		lastBlank = !c.Info.Fenced
	case Item:
		lastBlank = len(c.Children) > 0 || c.StartLine != p.lineNumber
	}
	c.LastLineBlank = p.blank && lastBlank
	for n := c.Parent; n != nil; n = n.Parent {
		n.LastLineBlank = false
	}
	if p.current != last && c == last && !p.blank && p.current.Kind == Paragraph {
		info.Lazy = true
		p.addLine(p.current, s, true)
		return
	}
	for p.current != last {
		p.current = p.finalize(p.current)
	}
	if kind == CodeBlock {
		p.addLine(c, s, false)
	} else if kind == HTMLBlock {
		p.addLine(c, s, false)
		if r := htmlEnds[c.Info.BlockType]; r != nil && r.MatchString(s[p.firstNonspace:]) {
			c = p.finalize(c)
		}
	} else if p.blank {
	} else if acceptsLines[kind] {
		p.advance(s, p.firstNonspace-p.offset, false)
		p.addLine(c, s, false)
	} else {
		c = p.addChild(c, Paragraph, p.firstNonspace, Info{})
		p.advance(s, p.firstNonspace-p.offset, false)
		p.addLine(c, s, false)
	}
	p.current = c
}

// blocks._compute_end_lines に対応する終端行の計算。
func computeEndLines(n *Node, lines []string) int {
	kind := n.Kind
	if kind == Paragraph || kind == Heading || kind == CodeBlock || kind == HTMLBlock {
		end := n.StartLine
		for _, l := range n.Lines {
			if kind == HTMLBlock && strings.Trim(lines[l.Number][l.Offset:], " \t") == "" {
				continue
			}
			end = max(end, l.Number)
		}
		if kind == CodeBlock && n.Info.Fenced {
			if n.Info.CloseLine != nil {
				end = max(end, *n.Info.CloseLine)
			} else if len(n.Lines) > 0 {
				end = max(end, n.Lines[len(n.Lines)-1].Number)
			}
		}
		if kind == Heading && n.Info.Setext {
			end = max(end, n.Info.Underline)
		}
		n.EndLine = end
		return end
	}
	end := max(n.StartLine, n.EndLine)
	for _, c := range n.Children {
		end = max(end, computeEndLines(c, lines))
	}
	if kind == Table {
		end = max(end, n.Info.DelimiterLine.Number)
	}
	n.EndLine = end
	return end
}

type Document struct {
	Root      *Node
	Lines     []string
	LineInfos []*LineInfo
}

// blocks.Document.nodes に対応する節の列挙。
func (d *Document) Nodes(kind string) []*Node {
	var out []*Node
	for _, n := range d.Root.Walk() {
		if kind == "" || n.Kind == kind {
			out = append(out, n)
		}
	}
	return out
}

// blocks.Document.text_of に対応する葉の本文。
func (d *Document) TextOf(n *Node) string {
	var b strings.Builder
	for _, l := range n.Lines {
		b.WriteString(d.Lines[l.Number][l.Offset:])
		b.WriteByte('\n')
	}
	return b.String()
}

// blocks.parse に対応するブロック解析。
func Parse(lines []string, frontMatter, keepDefinitions bool) *Document {
	root := NewNode(DocumentKind, 0, 0, Info{})
	p := &parser{sourceLines: lines, frontMatter: frontMatter, keepDefinitions: keepDefinitions, root: root, current: root, lineNumber: -1}
	p.parse()
	return &Document{root, lines, p.lineInfos}
}

// blocks.parse_text に対応する文字列の解析。
func ParseText(s string, frontMatter, keepDefinitions bool) *Document {
	lines, _ := SplitLines(s)
	return Parse(lines, frontMatter, keepDefinitions)
}

var lineSplit = regexp.MustCompile(`\r\n|\r|\n`)

// blocks.split_lines に対応する改行を保つ分割。
func SplitLines(s string) ([]string, []string) {
	var lines, endings []string
	pos := 0
	for _, m := range lineSplit.FindAllStringIndex(s, -1) {
		lines = append(lines, s[pos:m[0]])
		endings = append(endings, s[m[0]:m[1]])
		pos = m[1]
	}
	if pos < len(s) {
		lines = append(lines, s[pos:])
		endings = append(endings, "")
	}
	return lines, endings
}
