package server

import (
	"bytes"
	"encoding/json"
	"io"
	"strconv"
)

// DecodeJSON は JSON.parse と同じく、大きな指数の数値を無限大として読んでから型を検査する。
// Go の通常の Unmarshal では、そのような値を含む行全体が構文の誤りになる。
func DecodeJSON(body []byte) (any, error) {
	d := json.NewDecoder(bytes.NewReader(body))
	d.UseNumber()
	var value any
	if e := d.Decode(&value); e != nil {
		return nil, e
	}
	var extra any
	if e := d.Decode(&extra); e != io.EOF {
		if e == nil {
			return nil, io.ErrUnexpectedEOF
		}
		return nil, e
	}
	var convert func(any) any
	convert = func(v any) any {
		switch x := v.(type) {
		case json.Number:
			n, _ := strconv.ParseFloat(string(x), 64)
			return n
		case map[string]any:
			for k, child := range x {
				x[k] = convert(child)
			}
		case []any:
			for i, child := range x {
				x[i] = convert(child)
			}
		}
		return v
	}
	return convert(value), nil
}

// Stringify は HTTP/SSE と stdio の JSON を、JSON.stringify と同じエスケープで返す。
// HTML のエスケープで SSE のキューのバイト数を増やさない。ページ内の設定には使わない。
func Stringify(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	raw := buffer.Bytes()
	raw = raw[:len(raw)-1]
	out := make([]byte, 0, len(raw))
	for i := 0; i < len(raw); {
		if raw[i] == '\\' && i+1 < len(raw) {
			if raw[i+1] == '\\' {
				out = append(out, raw[i:i+2]...)
				i += 2
				continue
			}
			if bytes.HasPrefix(raw[i:], []byte(`\u2028`)) {
				out = append(out, []byte("\u2028")...)
				i += 6
				continue
			}
			if bytes.HasPrefix(raw[i:], []byte(`\u2029`)) {
				out = append(out, []byte("\u2029")...)
				i += 6
				continue
			}
			out = append(out, raw[i:i+2]...)
			i += 2
			continue
		}
		out = append(out, raw[i])
		i++
	}
	return out, nil
}
