package table

import (
	"cmp"
	"math/big"
	"regexp"
	"sort"
	"strings"
)

var halfwidthKana = []rune("。「」、・ヲァィゥェォャュョッーアイウエオカキクケコサシスセソタチツテトナニヌネノハヒフヘホマミムメモヤユヨラリルレロワン\u3099\u309a")
var voicedKana = map[rune]rune{
	'ウ': 'ヴ', 'カ': 'ガ', 'キ': 'ギ', 'ク': 'グ', 'ケ': 'ゲ', 'コ': 'ゴ',
	'サ': 'ザ', 'シ': 'ジ', 'ス': 'ズ', 'セ': 'ゼ', 'ソ': 'ゾ',
	'タ': 'ダ', 'チ': 'ヂ', 'ツ': 'ヅ', 'テ': 'デ', 'ト': 'ド',
	'ハ': 'バ', 'ヒ': 'ビ', 'フ': 'ブ', 'ヘ': 'ベ', 'ホ': 'ボ',
	'ワ': 'ヷ', 'ヰ': 'ヸ', 'ヱ': 'ヹ', 'ヲ': 'ヺ',
}
var semivoicedKana = map[rune]rune{'ハ': 'パ', 'ヒ': 'ピ', 'フ': 'プ', 'ヘ': 'ペ', 'ホ': 'ポ'}

// foldSortText は比較に使う英数・空白とカタカナの幅、英字の大小を畳む。
func foldSortText(text string) string {
	out := make([]rune, 0, len(text))
	for _, r := range text {
		if (r == '\uff9e' || r == '\uff9f') && len(out) > 0 {
			composed := voicedKana[out[len(out)-1]]
			if r == '\uff9f' {
				composed = semivoicedKana[out[len(out)-1]]
			}
			if composed != 0 {
				out[len(out)-1] = composed
				continue
			}
		}
		switch {
		case r >= '\uff01' && r <= '\uff5e':
			r -= 0xfee0
		case r == '\u3000':
			r = ' '
		case r >= '\uff61' && r <= '\uff9f':
			r = halfwidthKana[r-'\uff61']
		}
		out = append(out, r)
	}
	return strings.ToLower(string(out))
}

// 数値は十進の桁と小数点の位置で持ち、丸めと巨大な指数による桁の展開を避ける。
type sortNumber struct {
	negative bool
	digits   string
	position *big.Int
}

var sortNumberPattern = regexp.MustCompile(`^([+-]?)(?:(\d+|\d{1,3}(?:,\d{3})+)(?:\.(\d*))?|\.(\d+))(?:[eE]([+-]?\d+))?(%?)$`)

func parseSortNumber(text string) *sortNumber {
	text = foldSortText(strings.TrimSpace(text))
	m := sortNumberPattern.FindStringSubmatch(text)
	if m == nil {
		return nil
	}
	fraction := m[3] + m[4]
	digits := strings.TrimLeft(strings.ReplaceAll(m[2], ",", "")+fraction, "0")
	position := new(big.Int)
	if m[5] != "" {
		position.SetString(m[5], 10)
	}
	position.Add(position, big.NewInt(int64(len(digits)-len(fraction))))
	if m[6] != "" {
		position.Sub(position, big.NewInt(2))
	}
	return &sortNumber{negative: m[1] == "-" && digits != "", digits: digits, position: position}
}

func compareSortNumber(a, b *sortNumber) int {
	if a.negative != b.negative {
		if a.negative {
			return -1
		}
		return 1
	}
	result := 0
	if a.digits == "" || b.digits == "" {
		result = cmp.Compare(len(a.digits), len(b.digits))
	} else if result = a.position.Cmp(b.position); result == 0 {
		for i := 0; i < max(len(a.digits), len(b.digits)); i++ {
			x, y := byte('0'), byte('0')
			if i < len(a.digits) {
				x = a.digits[i]
			}
			if i < len(b.digits) {
				y = b.digits[i]
			}
			if result = cmp.Compare(x, y); result != 0 {
				break
			}
		}
	}
	if a.negative {
		return -result
	}
	return result
}

type sortKey struct {
	kind           int // 数値、文字、空の順。
	number         *sortNumber
	text, original string
}

func newSortKey(text string) sortKey {
	if strings.TrimSpace(text) == "" {
		return sortKey{kind: 2}
	}
	if n := parseSortNumber(text); n != nil {
		return sortKey{number: n}
	}
	return sortKey{kind: 1, text: foldSortText(text), original: text}
}

func compareNatural(a, b string) int {
	for len(a) > 0 && len(b) > 0 {
		if a[0] >= '0' && a[0] <= '9' && b[0] >= '0' && b[0] <= '9' {
			i, j := 0, 0
			for i < len(a) && a[i] >= '0' && a[i] <= '9' {
				i++
			}
			for j < len(b) && b[j] >= '0' && b[j] <= '9' {
				j++
			}
			x, y := strings.TrimLeft(a[:i], "0"), strings.TrimLeft(b[:j], "0")
			if c := cmp.Compare(len(x), len(y)); c != 0 {
				return c
			}
			if c := strings.Compare(x, y); c != 0 {
				return c
			}
			a, b = a[i:], b[j:]
		} else {
			// UTF-8 のバイトの辞書順はコードポイントの辞書順と一致する。
			if c := cmp.Compare(a[0], b[0]); c != 0 {
				return c
			}
			a, b = a[1:], b[1:]
		}
	}
	return cmp.Compare(len(a), len(b))
}

func compareSortKey(a, b sortKey, desc bool) int {
	// 空は向きによらず末尾に置く。
	if a.kind == 2 || b.kind == 2 {
		return cmp.Compare(a.kind, b.kind)
	}
	c := cmp.Compare(a.kind, b.kind)
	if c == 0 {
		if a.kind == 0 {
			c = compareSortNumber(a.number, b.number)
		} else {
			c = compareNatural(a.text, b.text)
			if c == 0 {
				c = strings.Compare(a.original, b.original)
			}
		}
	}
	if desc {
		return -c
	}
	return c
}

// SortedRows は指定した行の新しい位置に置く元の行番号を返す。
func SortedRows(r *Region, first, count, col int, desc bool) []int {
	rows := make([]int, count)
	keys := make([]sortKey, count)
	for i := range rows {
		rows[i] = first + i
		keys[i] = newSortKey("")
	}
	for _, c := range r.Model().Cells {
		if c.Col <= col && col < c.Col+c.Colspan {
			key := newSortKey(c.Text)
			for row := max(first, c.Row); row < min(first+count, c.Row+c.Rowspan); row++ {
				keys[row-first] = key
			}
		}
	}
	sort.SliceStable(rows, func(i, j int) bool { return compareSortKey(keys[rows[i]-first], keys[rows[j]-first], desc) < 0 })
	return rows
}
