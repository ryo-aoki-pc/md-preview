package htmltree

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
)

// Object は、JSON の項目順を保つ。外へ出す属性の並びを map に任せない。
type Pair struct {
	Key   string
	Value any
}
type Object []Pair

// Number は JSON.parse と同じ倍精度の値と、その文字列表現を使う。
type Number float64

func (n Number) String() string {
	v := float64(n)
	if math.IsInf(v, 1) {
		return "Infinity"
	}
	if math.IsInf(v, -1) {
		return "-Infinity"
	}
	if v == 0 {
		return "0"
	}
	format := byte('g')
	if math.Abs(v) >= 1e-6 && math.Abs(v) < 1e21 {
		format = 'f'
	}
	s := strconv.FormatFloat(v, format, -1, 64)
	s = strings.ReplaceAll(s, "e-0", "e-")
	s = strings.ReplaceAll(s, "e+0", "e+")
	return s
}
func (n Number) MarshalJSON() ([]byte, error) {
	if math.IsInf(float64(n), 0) || math.IsNaN(float64(n)) {
		return []byte("null"), nil
	}
	return []byte(n.String()), nil
}

func (o Object) Get(k string) any {
	for _, p := range o {
		if p.Key == k {
			return p.Value
		}
	}
	return nil
}
func (o Object) Has(k string) bool {
	for _, p := range o {
		if p.Key == k {
			return true
		}
	}
	return false
}
func (o Object) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, p := range o {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(JSON(p.Key))
		b.WriteByte(':')
		b.WriteString(JSON(p.Value))
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}
func JSON(v any) string {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	if e.Encode(v) != nil {
		return "null"
	}
	encoded := strings.TrimSuffix(b.String(), "\n")
	// encoding/json が常にエスケープする区切り文字を JSON.stringify と合わせる。
	var out strings.Builder
	for i := 0; i < len(encoded); i++ {
		if encoded[i] == '\\' && i+1 < len(encoded) {
			if strings.HasPrefix(encoded[i:], `\u2028`) {
				out.WriteRune('\u2028')
				i += 5
				continue
			}
			if strings.HasPrefix(encoded[i:], `\u2029`) {
				out.WriteRune('\u2029')
				i += 5
				continue
			}
			out.WriteString(encoded[i : i+2])
			i++
			continue
		}
		out.WriteByte(encoded[i])
	}
	return out.String()
}
func ParseJSON(s string) (any, error) {
	return ParseJSONContext(context.Background(), s)
}
func ParseJSONContext(ctx context.Context, s string) (any, error) {
	d := json.NewDecoder(strings.NewReader(s))
	d.UseNumber()
	v, err := jsonValue(ctx, d)
	if err != nil {
		return nil, err
	}
	if _, err = d.Token(); err != io.EOF {
		return nil, fmt.Errorf("JSON の末尾が不正です。")
	}
	return v, nil
}
func jsonValue(ctx context.Context, d *json.Decoder) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	t, err := d.Token()
	if err != nil {
		return nil, err
	}
	if number, ok := t.(json.Number); ok {
		v, err := strconv.ParseFloat(string(number), 64)
		if err != nil && !math.IsInf(v, 0) {
			return nil, err
		}
		return Number(v), nil
	}
	switch t {
	case json.Delim('{'):
		o := Object{}
		indices := map[string]int{}
		for d.More() {
			k, err := d.Token()
			if err != nil {
				return nil, err
			}
			v, err := jsonValue(ctx, d)
			if err != nil {
				return nil, err
			}
			key := k.(string)
			if i, found := indices[key]; found {
				o[i].Value = v
			} else {
				indices[key] = len(o)
				o = append(o, Pair{key, v})
			}
		}
		_, err = d.Token()
		sort.SliceStable(o, func(i, j int) bool {
			a, aok := indexKey(o[i].Key)
			b, bok := indexKey(o[j].Key)
			return aok && (!bok || a < b)
		})
		return o, err
	case json.Delim('['):
		a := []any{}
		for d.More() {
			v, err := jsonValue(ctx, d)
			if err != nil {
				return nil, err
			}
			a = append(a, v)
		}
		_, err = d.Token()
		return a, err
	}
	return t, nil
}
func indexKey(k string) (uint64, bool) {
	n, err := strconv.ParseUint(k, 10, 32)
	return n, err == nil && n < 4294967295 && strconv.FormatUint(n, 10) == k
}
func RubyTruthy(v any) bool    { return v != nil && v != false }
func RubyInspect(v any) string { return RubyInspectContext(context.Background(), v) }
func RubyInspectContext(ctx context.Context, v any) string {
	if ctx.Err() != nil {
		return ""
	}
	switch x := v.(type) {
	case nil:
		return "nil"
	case string:
		return JSON(x)
	case Object:
		a := []string{}
		for _, p := range x {
			a = append(a, JSON(p.Key)+"=>"+RubyInspectContext(ctx, p.Value))
		}
		return "{" + strings.Join(a, ", ") + "}"
	case []any:
		a := []string{}
		for _, p := range x {
			a = append(a, RubyInspectContext(ctx, p))
		}
		return "[" + strings.Join(a, ", ") + "]"
	default:
		return fmt.Sprint(v)
	}
}
func RubyString(v any) string { return RubyStringContext(context.Background(), v) }
func RubyStringContext(ctx context.Context, v any) string {
	if v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return x
	case Object, []any:
		return RubyInspectContext(ctx, x)
	default:
		return fmt.Sprint(v)
	}
}

// JSString は、フィールドのキーを JavaScript のプロパティ名へ変える。
func JSString(v any) string {
	return JSStringContext(context.Background(), v)
}
func JSStringContext(ctx context.Context, v any) string {
	if ctx.Err() != nil {
		return ""
	}
	switch x := v.(type) {
	case nil:
		return "null"
	case string:
		return x
	case Object:
		return "[object Object]"
	case []any:
		items := make([]string, len(x))
		for i, v := range x {
			if v != nil {
				items[i] = JSStringContext(ctx, v)
			}
		}
		return strings.Join(items, ",")
	default:
		return fmt.Sprint(v)
	}
}
