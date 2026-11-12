// Package workbook は Excel ブックの読み書きとセルの変換を行う。
package workbook

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"unicode/utf16"

	"github.com/ryo-aoki-pc/md-table-excel/internal/model"
	"github.com/ryo-aoki-pc/md-table-excel/internal/pycompat"
	"github.com/ryo-aoki-pc/md-table-excel/internal/table"
)

const MetaSheet = "_mdtable"
const IndexSheet = "目次"
const NamePrefix = "mdtable_"

type Error struct{ Message string }

func (e *Error) Error() string                 { return e.Message }
func failure(format string, args ...any) error { return &Error{fmt.Sprintf(format, args...)} }

type ExportTable struct {
	Region           *table.Region
	Model            *model.TableModel
	Occurrence, Same int
	SheetName        string
}

type SheetTable struct {
	Number, Start, End, Occurrence            int
	Kind, SourceHash, Fingerprint, SheetTitle string
	Same                                      *int
	Model                                     *model.TableModel
	Missing                                   string
	Errors, Warnings                          []string
}

type Data struct {
	MarkdownPath, MarkdownRelpath string
	Tables                        []*SheetTable
	SourceHashes                  map[string]bool
	Warnings                      []string
}

var fontCache = struct {
	sync.Mutex
	values map[string]bool
}{values: map[string]bool{}}

// FontName は環境変数とインストール済みのフォントから選ぶ。
func FontName() string {
	if name := os.Getenv("MDTABLE_FONT"); name != "" {
		return name
	}
	fontCache.Lock()
	defer fontCache.Unlock()
	const preferred = "HackGen Console NF"
	found, ok := fontCache.values[preferred]
	if !ok {
		found = fontInstalled(preferred)
		fontCache.values[preferred] = found
	}
	if found {
		return preferred
	}
	return "BIZ UDゴシック"
}

var fontKind = regexp.MustCompile(`\s*\([^()]*\)\s*$`)

func FontEntryMatches(entry, family string) bool {
	family = pycompat.Lower(family)
	for _, name := range strings.Split(fontKind.ReplaceAllString(entry, ""), "&") {
		name = pycompat.Lower(pycompat.Strip(name))
		if name == family || strings.HasPrefix(name, family+" ") {
			return true
		}
	}
	return false
}

func CheckCellText(text string) string {
	for _, ch := range text {
		if ch < 32 && ch != '\t' && ch != '\n' && ch != '\r' {
			return "制御文字が入っている"
		}
	}
	if len(utf16.Encode([]rune(text))) > 32767 {
		return "Excel の 1 セルの上限（32,767 文字）を超える"
	}
	return ""
}

var escapeLike = regexp.MustCompile(`^x[0-9A-Fa-f]{4}_`)

// EscapeForExcel は終端の下線が次の開始にもなる場合を含めて逃がす。
func EscapeForExcel(text string) string {
	var out strings.Builder
	for i, ch := range text {
		if ch == '_' && escapeLike.MatchString(text[i+1:]) {
			out.WriteString("_x005F_")
		} else {
			out.WriteRune(ch)
		}
	}
	return out.String()
}

var forbiddenSheet = regexp.MustCompile(`[\\/?*\[\]:]`)

func SheetNameFor(number int, heading string, used map[string]bool) string {
	title := forbiddenSheet.ReplaceAllString(pycompat.Strip(heading), "")
	title = strings.Trim(strings.Join(pycompat.Split(title), " "), " '")
	name := fmt.Sprintf("表%d", number)
	if title != "" {
		name += " " + title
	}
	trim := func(s string, limit int) string {
		runes := []rune(s)
		for len(utf16.Encode(runes)) > limit {
			runes = runes[:len(runes)-1]
		}
		return string(runes)
	}
	name = strings.TrimRight(trim(name, 31), " '")
	candidate := name
	for counter := 2; used[pycompat.Lower(candidate)] || pycompat.Lower(candidate) == "history"; counter++ {
		suffix := fmt.Sprintf("(%d)", counter)
		candidate = trim(name, 31-len(suffix)) + suffix
	}
	used[pycompat.Lower(candidate)] = true
	return candidate
}

func quoteSheet(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

var quickReference = []string{
	"セルの書き方（GLFM の Markdown）",
	"・セル内の改行（Alt+Enter）は表示上の改行。空行は段落の区切り",
	"・箇条書き「- 」、番号付き「1. 」、入れ子は親の本文の位置まで空白で字下げ。リストは空行で終わる",
	"・説明リスト: 「用語」の次の行に「: 説明」。組と組の間は空行",
	"・コードブロック: ``` で囲む（言語名も書ける）",
	"・「|」はそのまま書く。行頭の「-」「1.」「>」「:」などを文字として書くときは「\\-」のようにエスケープする",
	"・「x」の次の行に「2. y」と書くと 1 つの段落になる（番号付きリストは 1 から始めるか、前に空行を入れる）",
	"・Markdown の表: 1 行目が見出し。列の配置は列の中で多いものになる",
	"・HTML の表: 太字かつ塗りつぶしのセルが見出し（<th>）。セル結合は rowspan / colspan",
	"・セル結合やコードブロックがある Markdown の表は、書き戻すと HTML の表になる",
	"・行を挿入すると上の行の書式が写る。見出しの直下に挿入したら [挿入オプション] で「下と同じ書式を適用」を選ぶ",
	"・行や列を消すときは、行・列ごと削除する（中身だけを消すと空の行・列が残る）",
}
