package pycompat

import (
	"math"
	"sort"
	"strconv"
	"strings"
)

// OrderedMap はキーを最初に入れた順番を保つ。値の更新では順番を変えない。
type OrderedMap struct {
	keys   []string
	values map[string]any
}

// NewOrderedMap は空の辞書を作る。
func NewOrderedMap() *OrderedMap { return &OrderedMap{values: make(map[string]any)} }

// Set は値を追加または更新する。ゼロ値の OrderedMap にも使える。
func (m *OrderedMap) Set(key string, value any) {
	if m.values == nil {
		m.values = make(map[string]any)
	}
	if _, ok := m.values[key]; !ok {
		m.keys = append(m.keys, key)
	}
	m.values[key] = value
}

// FloatRepr は最短の十進表記を Python の指数表記の境界と綴りに合わせる。
func FloatRepr(f float64) string {
	if math.IsNaN(f) {
		return "nan"
	}
	if math.IsInf(f, 1) {
		return "inf"
	}
	if math.IsInf(f, -1) {
		return "-inf"
	}
	s := strconv.FormatFloat(f, 'e', -1, 64)
	pos := strings.LastIndexByte(s, 'e')
	exponent, _ := strconv.Atoi(s[pos+1:])
	if exponent >= -4 && exponent < 16 {
		s = strconv.FormatFloat(f, 'f', -1, 64)
		if !strings.Contains(s, ".") {
			s += ".0"
		}
	}
	return s
}

// JSONDumps は ensure_ascii=False、既定の区切りで JSON を書く。
func JSONDumps(v any, sortKeys bool) string {
	var out strings.Builder
	var write func(any)
	write = func(v any) {
		switch x := v.(type) {
		case nil:
			out.WriteString("null")
		case bool:
			if x {
				out.WriteString("true")
			} else {
				out.WriteString("false")
			}
		case int:
			out.WriteString(strconv.Itoa(x))
		case float64:
			if math.IsNaN(x) {
				out.WriteString("NaN")
			} else if math.IsInf(x, 1) {
				out.WriteString("Infinity")
			} else if math.IsInf(x, -1) {
				out.WriteString("-Infinity")
			} else {
				out.WriteString(FloatRepr(x))
			}
		case string:
			writeJSONString(&out, x)
		case []any:
			out.WriteByte('[')
			for i, value := range x {
				if i > 0 {
					out.WriteString(", ")
				}
				write(value)
			}
			out.WriteByte(']')
		case map[string]any:
			if !sortKeys {
				panic("map[string]any には sortKeys=true が必要です")
			}
			keys := make([]string, 0, len(x))
			for key := range x {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			out.WriteByte('{')
			for i, key := range keys {
				if i > 0 {
					out.WriteString(", ")
				}
				writeJSONString(&out, key)
				out.WriteString(": ")
				write(x[key])
			}
			out.WriteByte('}')
		case *OrderedMap:
			if x == nil {
				out.WriteString("null")
				return
			}
			keys := x.keys
			if sortKeys {
				keys = append([]string(nil), keys...)
				sort.Strings(keys)
			}
			out.WriteByte('{')
			for i, key := range keys {
				if i > 0 {
					out.WriteString(", ")
				}
				writeJSONString(&out, key)
				out.WriteString(": ")
				write(x.values[key])
			}
			out.WriteByte('}')
		default:
			panic("JSONDumps で扱えない値の型です")
		}
	}
	write(v)
	return out.String()
}

func writeJSONString(out *strings.Builder, s string) {
	const hex = "0123456789abcdef"
	out.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"', '\\':
			out.WriteByte('\\')
			out.WriteRune(r)
		case '\b':
			out.WriteString(`\b`)
		case '\f':
			out.WriteString(`\f`)
		case '\n':
			out.WriteString(`\n`)
		case '\r':
			out.WriteString(`\r`)
		case '\t':
			out.WriteString(`\t`)
		default:
			if r < 0x20 {
				out.WriteString(`\u00`)
				out.WriteByte(hex[r>>4])
				out.WriteByte(hex[r&15])
			} else {
				out.WriteRune(r)
			}
		}
	}
	out.WriteByte('"')
}
