// Package table は循環参照を持つ document・pipe_table・html_table の処理をまとめる。
package table

import (
	"context"
	"errors"
	"fmt"
	"github.com/ryo-aoki-pc/md-table-excel/internal/blocks"
	"github.com/ryo-aoki-pc/md-table-excel/internal/model"
	"github.com/ryo-aoki-pc/md-table-excel/internal/pycompat"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

type MarkdownFile struct {
	Path           string
	BOM            bool
	Lines, Endings []string
	doc            *blocks.Document
}

// document.MarkdownFile.__init__ に対応する文書の生成。
func NewMarkdown(path, text string, bom bool) *MarkdownFile {
	lines, endings := blocks.SplitLines(text)
	return &MarkdownFile{Path: path, BOM: bom, Lines: lines, Endings: endings}
}

// document.MarkdownFile.load に対応する UTF-8 の読み込み。
func Load(path string) (*MarkdownFile, error) {
	data, e := os.ReadFile(path)
	if e != nil {
		return nil, fmt.Errorf("Markdown を読めません: %s (%s)", path, StrError(e))
	}
	bom := strings.HasPrefix(string(data), "\xef\xbb\xbf")
	if bom {
		data = data[3:]
	}
	if !utf8.Valid(data) {
		pos := 0
		for pos < len(data) {
			r, n := utf8.DecodeRune(data[pos:])
			if r == utf8.RuneError && n == 1 {
				break
			}
			pos += n
		}
		return nil, fmt.Errorf("Markdown が UTF-8 ではありません: %s（%d バイト目）", path, pos+1)
	}
	return NewMarkdown(path, string(data), bom), nil
}

// cli.main の OSError の文に対応する理由。
func StrError(e error) string {
	if errors.Is(e, os.ErrNotExist) {
		return "No such file or directory"
	}
	if errors.Is(e, os.ErrPermission) {
		return "Permission denied"
	}
	if p, ok := e.(*os.PathError); ok {
		return p.Err.Error()
	}
	return e.Error()
}

// document.MarkdownFile.text に対応する元の改行の連結。
func (m *MarkdownFile) Text() string {
	var b strings.Builder
	for i, l := range m.Lines {
		b.WriteString(l)
		b.WriteString(m.Endings[i])
	}
	return b.String()
}

// document.MarkdownFile.doc に対応する遅延解析。
func (m *MarkdownFile) Doc() *blocks.Document {
	if m.doc == nil {
		m.doc = blocks.Parse(m.Lines, true, false)
	}
	return m.doc
}

// document.MarkdownFile.default_ending に対応する改行の選択。
func (m *MarkdownFile) DefaultEnding() string {
	for _, e := range m.Endings {
		if e != "" {
			return e
		}
	}
	return "\n"
}

type Region struct {
	Number                                    int
	Kind                                      string
	Start, End                                int
	Prefixes, Bodies                          []string
	ContPrefix, Head, Tail, Location, Heading string
	HeadingLine                               *int
	Excluded                                  string
	Warnings                                  []string
	Node, Container                           *blocks.Node
	Pipe                                      *PipeTable
	HTML                                      *HTMLTable
	Pending                                   *pending
	TightContainer, NextNonblank              bool
}

// document.TableRegion.first_prefix に対応する先頭接頭辞。
func (r *Region) FirstPrefix() string {
	if len(r.Prefixes) > 0 {
		return r.Prefixes[0]
	}
	return ""
}

// document.TableRegion.blank_prefix に対応する空行の接頭辞。
func (r *Region) BlankPrefix() string { return strings.TrimRight(r.ContPrefix, " \t") }

// document.TableRegion.source_text と source_hash に対応する中身。
func (r *Region) SourceText() string { return strings.Join(r.Bodies, "\n") }

// document.TableRegion.source_hash に対応する指紋。
func (r *Region) SourceHash() string { return model.SHA256Text(r.SourceText()) }

// document.TableRegion.line_count に対応する行数。
func (r *Region) LineCount() int { return r.End - r.Start + 1 }

// document.find_tables などの region.parsed.model に対応する共通の参照。
func (r *Region) Model() *model.TableModel {
	if r.Kind == "pipe" {
		return r.Pipe.Model
	}
	return r.HTML.Model
}

// document.describe_location に対応する場所の説明。
func DescribeLocation(c *blocks.Node, prefix string) string {
	var chain []*blocks.Node
	for n := c; n != nil && n.Kind != blocks.DocumentKind; n = n.Parent {
		chain = append(chain, n)
	}
	names := []string{}
	kinds := map[string]string{blocks.BlockQuote: "引用", blocks.Alert: "アラート", blocks.MultilineBlockQuote: "引用（>>>）", blocks.DescriptionDetails: "説明リスト", blocks.FootnoteDefinition: "脚注"}
	for i := len(chain) - 1; i >= 0; i-- {
		n := chain[i]
		if n.Kind == blocks.Item {
			if n.Info.Type == "ordered" {
				names = append(names, "番号付きリスト")
			} else {
				names = append(names, "箇条書き")
			}
		} else if name := kinds[n.Kind]; name != "" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return ""
	}
	return fmt.Sprintf("%sの中（字下げ %d）", strings.Join(names, " > "), pycompat.Len(pycompat.ExpandTabs(prefix, 4)))
}

// document._block_container に対応する所属コンテナ。
func blockContainer(n *blocks.Node) *blocks.Node {
	for n = n.Parent; n != nil && !blocks.BlockContainers[n.Kind]; n = n.Parent {
	}
	return n
}

// document._container_is_tight に対応するリストの判定。
func containerTight(c *blocks.Node) bool {
	if c == nil || c.Parent == nil {
		return false
	}
	if c.Kind == blocks.Item || c.Kind == blocks.DescriptionDetails {
		return c.Parent.Info.Tight
	}
	return false
}

type heading struct {
	start, end int
	text       string
}

var trailingHeading = regexp.MustCompile(`(?:^|[ \t]+)#+[ \t]*$`)

// document._headings に対応する見出しの一覧。
func headings(m *MarkdownFile) []heading {
	var out []heading
	for _, n := range m.Doc().Nodes(blocks.Heading) {
		text := ""
		if n.Info.Setext {
			var parts []string
			for _, l := range n.Lines {
				parts = append(parts, pycompat.Strip(m.Lines[l.Number][l.Offset:]))
			}
			text = strings.Join(parts, " ")
		} else {
			line, offset := n.StartLine, 0
			if len(n.Lines) > 0 {
				line, offset = n.Lines[0].Number, n.Lines[0].Offset
			}
			text = pycompat.Strip(m.Lines[line][offset:])
			text = pycompat.Strip(trailingHeading.ReplaceAllString(text, ""))
		}
		out = append(out, heading{n.StartLine, n.EndLine, text})
	}
	return out
}

// document._pipe_region に対応するパイプ表の範囲。
func pipeRegion(m *MarkdownFile, n *blocks.Node) *Region {
	header := n.Info.Header
	r := &Region{Kind: "pipe", Start: header.Number, End: n.EndLine, Node: n, Container: blockContainer(n)}
	offsets := map[int]int{header.Number: header.Offset, n.Info.DelimiterLine.Number: n.Info.DelimiterLine.Offset}
	for _, l := range n.Info.Rows {
		offsets[l.Number] = l.Offset
	}
	for i := r.Start; i <= r.End; i++ {
		s := m.Lines[i]
		o := offsets[i]
		r.Prefixes = append(r.Prefixes, s[:o])
		r.Bodies = append(r.Bodies, s[o:])
	}
	r.ContPrefix = r.Prefixes[0]
	if len(r.Prefixes) > 1 {
		r.ContPrefix = r.Prefixes[1]
	}
	if header.Lazy || m.Doc().LineInfos[header.Number].Lazy {
		r.Excluded = "見出し行が引用やリストの続きの行になっていない（遅延行）"
	}
	tabbed := false
	unique := map[string]bool{}
	for _, p := range r.Prefixes[1:] {
		if strings.Contains(p, "\t") {
			tabbed = true
		}
		unique[p] = true
	}
	if tabbed && len(unique) > 1 {
		r.Excluded = "タブを含む字下げが行ごとに違う"
	}
	return r
}

// document.find_tables に対応する検出と番号。
func FindTables(m *MarkdownFile) []*Region {
	var regions []*Region
	for _, n := range m.Doc().Nodes(blocks.Table) {
		regions = append(regions, pipeRegion(m, n))
	}
	regions = append(regions, findHTMLRegions(m)...)
	sort.SliceStable(regions, func(i, j int) bool { return regions[i].Start < regions[j].Start })
	hs := headings(m)
	infos := m.Doc().LineInfos
	for i, r := range regions {
		r.Number = i + 1
		r.Location = DescribeLocation(r.Container, r.ContPrefix)
		r.TightContainer = containerTight(r.Container)
		following := r.End + 1
		if following < len(m.Lines) && r.Container != nil {
			if offset, ok := infos[following].OffsetAfter(r.Container); ok {
				r.NextNonblank = pycompat.Strip(m.Lines[following][offset:]) != ""
			}
		}
		for _, h := range hs {
			if h.end < r.Start {
				line := h.start
				r.HeadingLine = &line
				r.Heading = h.text
			}
		}
		if r.Excluded == "" {
			if r.Kind == "pipe" {
				analyzePipe(m, r)
			} else {
				analyzeHTML(m, r)
			}
		}
	}
	return regions
}

var selectLine = regexp.MustCompile(`^[Ll][` + pycompat.ReDigit + `]+$`)
var selectNumber = regexp.MustCompile(`^[` + pycompat.ReDigit + `]+$`)

// document.select_tables の int の表示に対応し、int の範囲を超える数字も十進で表す。
func selectorDecimal(s string) string {
	var b strings.Builder
	for _, ch := range s {
		n, _ := pycompat.Atoi(string(ch))
		b.WriteByte(byte('0' + n))
	}
	result := strings.TrimLeft(b.String(), "0")
	if result == "" {
		return "0"
	}
	return result
}

// document.select_tables に対応する番号・行・見出しの選択。
func SelectTables(regions []*Region, selectors []string) ([]*Region, error) {
	if len(selectors) == 0 {
		return append([]*Region{}, regions...), nil
	}
	var chosen []*Region
	seen := map[*Region]bool{}
	for _, selector := range selectors {
		text := pycompat.Strip(selector)
		var found []*Region
		missing := ""
		if selectLine.MatchString(text) {
			line, ok := pycompat.Atoi(text[1:])
			if ok {
				line--
				for _, r := range regions {
					if r.Start <= line && line <= r.End {
						found = append(found, r)
					}
				}
			}
			missing = fmt.Sprintf("%s 行目を含む表がありません", selectorDecimal(text[1:]))
		} else if selectNumber.MatchString(text) {
			number, ok := pycompat.Atoi(text)
			if ok {
				for _, r := range regions {
					if r.Number == number {
						found = append(found, r)
					}
				}
			}
			missing = fmt.Sprintf("表 %s はありません（表は %d 個）", selectorDecimal(text), len(regions))
		}
		if len(found) == 0 {
			for _, r := range regions {
				if r.HeadingLine != nil && pycompat.Strip(r.Heading) == text {
					found = append(found, r)
				}
			}
			if len(found) == 0 {
				if missing == "" {
					missing = fmt.Sprintf("直前の見出しが「%s」の表がありません（--table は番号・L行番号・直前の見出しの文字）", text)
				}
				return nil, errors.New(missing)
			}
			unique := map[int]bool{}
			for _, r := range found {
				unique[*r.HeadingLine] = true
			}
			if len(unique) > 1 {
				var lines []int
				for l := range unique {
					lines = append(lines, l)
				}
				sort.Ints(lines)
				var parts []string
				for _, l := range lines {
					parts = append(parts, fmt.Sprintf("%d 行目", l+1))
				}
				return nil, fmt.Errorf("見出し「%s」が複数あります（%s）。番号か L行番号 で選んでください", text, strings.Join(parts, "・"))
			}
		}
		for _, r := range found {
			if !seen[r] {
				seen[r] = true
				chosen = append(chosen, r)
			}
		}
	}
	sort.SliceStable(chosen, func(i, j int) bool { return chosen[i].Number < chosen[j].Number })
	return chosen, nil
}

type Replacement struct {
	Region     *Region
	Lines      []string
	Model      *model.TableModel
	TargetKind string
	Expect     *Expectation
}

// document.apply_replacements に対応する元の改行を保った置換。
func ApplyReplacements(m *MarkdownFile, replacements []*Replacement) string {
	lines := append([]string{}, m.Lines...)
	endings := append([]string{}, m.Endings...)
	ordered := append([]*Replacement{}, replacements...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Region.Start > ordered[j].Region.Start })
	for _, replacement := range ordered {
		r := replacement.Region
		start, end := r.Start, r.End
		first := endings[start]
		if first == "" {
			first = m.DefaultEnding()
		}
		last := endings[end]
		newLines := replacement.Lines
		newEndings := make([]string, len(newLines))
		for i := range newEndings {
			newEndings[i] = first
		}
		kept := map[int]bool{}
		for _, op := range pycompat.Opcodes(lines[start:end+1], newLines) {
			if op.Tag == "equal" {
				for k := 0; k < op.I2-op.I1; k++ {
					ending := endings[start+op.I1+k]
					if ending == "" {
						ending = first
					}
					newEndings[op.J1+k] = ending
					kept[op.J1+k] = true
				}
			}
		}
		if len(newEndings) > 0 && (last == "" || !kept[len(newLines)-1]) {
			newEndings[len(newEndings)-1] = last
		}
		lines = append(append(append([]string{}, lines[:start]...), newLines...), lines[end+1:]...)
		endings = append(append(append([]string{}, endings[:start]...), newEndings...), endings[end+1:]...)
	}
	var b strings.Builder
	for i, l := range lines {
		b.WriteString(l)
		b.WriteString(endings[i])
	}
	return b.String()
}

// document.write_text_atomic に対応する同じフォルダー内での原子的な書き込み。
func WriteTextAtomic(path, text string, bom bool) error {
	return WriteTextAtomicContext(context.Background(), path, text, bom)
}

// WriteTextAtomicContext は取り消された書き込みの一時ファイルを閉じて削除する。
func WriteTextAtomicContext(ctx context.Context, path, text string, bom bool) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	target, e := filepath.Abs(path)
	if e != nil {
		return e
	}
	if real, e := filepath.EvalSymlinks(target); e == nil {
		target = real
	}
	data := []byte(text)
	if bom {
		data = append([]byte{0xef, 0xbb, 0xbf}, data...)
	}
	f, e := os.CreateTemp(filepath.Dir(target), ".mdtable-*.tmp")
	if e != nil {
		return e
	}
	temp := f.Name()
	defer os.Remove(temp)
	defer f.Close()
	_, e = f.Write(data)
	ce := f.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	if st, e := os.Stat(target); e == nil {
		_ = os.Chmod(temp, st.Mode().Perm())
	}
	return ReplaceWithRetryContext(ctx, temp, target, 5)
}

// document.replace_with_retry に対応する置換の再試行。
func ReplaceWithRetry(source, target string, retries int) error {
	return ReplaceWithRetryContext(context.Background(), source, target, retries)
}

// ReplaceWithRetryContext は置換の直前と再試行の待ち時間に取り消しを受け付ける。
func ReplaceWithRetryContext(ctx context.Context, source, target string, retries int) error {
	for i := 0; i < retries; i++ {
		if e := ctx.Err(); e != nil {
			return e
		}
		e := os.Rename(source, target)
		if e == nil {
			return nil
		}
		if canceled := ctx.Err(); canceled != nil {
			return canceled
		}
		if !IsPermissionError(e) || i == retries-1 {
			return e
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(i+1) * 200 * time.Millisecond):
		}
	}
	return nil
}
