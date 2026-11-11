package pycompat

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func fixture(t *testing.T, name string, target any) {
	t.Helper()
	f, err := os.Open("testdata/" + name + ".json.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	z, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	if err = json.NewDecoder(z).Decode(target); err != nil {
		t.Fatal(err)
	}
}

func TestOpcodesPython(t *testing.T) {
	var cases []struct {
		A, B     []string
		Expected []Opcode
	}
	fixture(t, "opcodes", &cases)
	for i, c := range cases {
		if got := Opcodes(c.A, c.B); !reflect.DeepEqual(got, c.Expected) {
			t.Fatalf("組 %d: 得た値=%v 正解=%v", i, got, c.Expected)
		}
	}
	t.Logf("Python の正解 %d 組と一致", len(cases))
}

func TestUnifiedDiffPython(t *testing.T) {
	var cases []struct {
		A, B                       []string
		Fromfile, Tofile, Expected string
	}
	fixture(t, "diffs", &cases)
	for i, c := range cases {
		if got := UnifiedDiff(c.A, c.B, c.Fromfile, c.Tofile); got != c.Expected {
			t.Fatalf("組 %d: 得た値=%q 正解=%q", i, got, c.Expected)
		}
	}
	t.Logf("Python の正解 %d 組と一致", len(cases))
}

func TestStringsPython(t *testing.T) {
	var cases []struct {
		S, Lower, Strip, Lstrip, Rstrip, Expanded string
		Split, Lines, Keep                        []string
		Alpha                                     bool
		Length, Tabsize                           int
	}
	fixture(t, "strings", &cases)
	for i, c := range cases {
		got := []any{Lower(c.S), Strip(c.S), LStrip(c.S), RStrip(c.S), Split(c.S), SplitLines(c.S, false), SplitLines(c.S, true), IsAlpha(c.S), Len(c.S), ExpandTabs(c.S, c.Tabsize)}
		want := []any{c.Lower, c.Strip, c.Lstrip, c.Rstrip, c.Split, c.Lines, c.Keep, c.Alpha, c.Length, c.Expanded}
		if !reflect.DeepEqual(got, want) {
			for j := range got {
				if !reflect.DeepEqual(got[j], want[j]) {
					t.Errorf("組 %d の項目 %d (%q): 得た値=%#v 正解=%#v", i, j, c.S, got[j], want[j])
				}
			}
			t.FailNow()
		}
	}
	t.Logf("Python の正解 %d 組と一致", len(cases))
}

func TestAllUnicodePython(t *testing.T) {
	var metadata struct {
		Python, Unicode, SHA256 string
		Characters              int
		SigmaContextSHA256      string `json:"sigma_context_sha256"`
	}
	data, err := os.ReadFile("testdata/unicode.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.Python != PythonVersion || metadata.Unicode != UnicodeVersion {
		t.Fatal("表と正解データの Python または Unicode の版が違います")
	}
	digest := sha256.New()
	contextDigest := sha256.New()
	var buf [8]byte
	writeText := func(value string) {
		binary.LittleEndian.PutUint32(buf[:4], uint32(len(value)))
		digest.Write(buf[:4])
		io.WriteString(digest, value)
	}
	digit := regexp.MustCompile("^[" + ReDigit + "]$")
	space := regexp.MustCompile("^[" + ReSpace + "]$")
	word := regexp.MustCompile("^[" + ReWord + "]$")
	negativeWord := regexp.MustCompile("^[^" + ReWord + "]$")
	count := 0
	for r := rune(0); r <= 0x10ffff; r++ {
		if r >= 0xd800 && r <= 0xdfff {
			continue
		}
		count++
		s := string(r)
		p := properties(r)
		flags := p.flags & (casedFlag | ignorableFlag)
		if IsSpace(r) {
			flags |= spaceFlag
		}
		if IsAlpha(s) {
			flags |= alphaFlag
		}
		if IsReDigit(r) {
			flags |= digitFlag
		}
		if IsReWord(r) {
			flags |= wordFlag
		}
		if digit.MatchString(s) != IsReDigit(r) || space.MatchString(s) != IsReSpace(r) || word.MatchString(s) != IsReWord(r) || negativeWord.MatchString(s) == IsReWord(r) || IsSpace(r) != IsReSpace(r) || Len(s) != 1 {
			t.Fatalf("文字 U+%04X の判定が一致しません", r)
		}
		decimal := -1
		if IsReDigit(r) {
			var ok bool
			decimal, ok = Atoi(s)
			if !ok || decimal < 0 || decimal > 9 {
				t.Fatalf("文字 U+%04X の数字変換が不正です", r)
			}
		}
		binary.LittleEndian.PutUint32(buf[:4], uint32(r))
		buf[4] = flags
		binary.LittleEndian.PutUint16(buf[5:7], uint16(Combining(r)))
		buf[7] = byte(int8(decimal))
		digest.Write(buf[:])
		io.WriteString(digest, Category(r)+EastAsianWidth(r))
		io.WriteString(digest, Lower(s))
		digest.Write([]byte{0})
		for _, value := range []string{Strip(s), LStrip(s), RStrip(s), ExpandTabs(s+"\tX\r"+s+"\t\n", 4)} {
			writeText(value)
		}
		for _, values := range [][]string{Split(s), SplitLines(s, false), SplitLines(s, true)} {
			binary.LittleEndian.PutUint32(buf[:4], uint32(len(values)))
			digest.Write(buf[:4])
			for _, value := range values {
				writeText(value)
			}
		}
		for _, text := range []string{"AΣ" + s + "A", "A" + s + "Σ", s + "Σ"} {
			io.WriteString(contextDigest, Lower(text))
			contextDigest.Write([]byte{0})
		}
	}
	if got := hex.EncodeToString(digest.Sum(nil)); got != metadata.SHA256 {
		t.Fatalf("全文字の SHA256 が不一致: %s 正解=%s", got, metadata.SHA256)
	}
	if got := hex.EncodeToString(contextDigest.Sum(nil)); got != metadata.SigmaContextSHA256 {
		t.Fatalf("全文字のシグマ文脈の SHA256 が不一致: %s 正解=%s", got, metadata.SigmaContextSHA256)
	}
	if count != metadata.Characters {
		t.Fatalf("文字数が不一致: %d", count)
	}
	t.Logf("Python %s / Unicode %s の %d 文字、文脈 %d 組と一致", PythonVersion, UnicodeVersion, count, count*3)
}

func TestHTMLUnescapePython(t *testing.T) {
	var cases []struct{ S, Expected string }
	fixture(t, "html", &cases)
	for i, c := range cases {
		if got := HTMLUnescape(c.S); got != c.Expected {
			t.Fatalf("組 %d (%q): 得た値=%q 正解=%q", i, c.S, got, c.Expected)
		}
	}
	t.Logf("Python の正解 %d 組と一致", len(cases))
}

func TestHTMLDecimalLimit(t *testing.T) {
	if pythonIntMaxStrDigits == 0 {
		t.Skip("生成元 Python の桁数制限は無効です")
	}
	defer func() {
		if recover() == nil {
			t.Error("Python が拒否する桁数の数値参照を受け付けました")
		}
	}()
	HTMLUnescape("&#" + strings.Repeat("0", pythonIntMaxStrDigits+1) + ";")
}

func TestFloatReprPython(t *testing.T) {
	var cases []struct{ Bits, Expected, JSON string }
	fixture(t, "floats", &cases)
	for i, c := range cases {
		bits, err := strconv.ParseUint(c.Bits, 16, 64)
		if err != nil {
			t.Fatal(err)
		}
		f := math.Float64frombits(bits)
		if got := FloatRepr(f); got != c.Expected {
			t.Fatalf("組 %d (%s): 得た値=%s 正解=%s", i, c.Bits, got, c.Expected)
		}
		if got := JSONDumps(f, false); got != c.JSON {
			t.Fatalf("組 %d の JSON: 得た値=%s 正解=%s", i, got, c.JSON)
		}
	}
	t.Logf("Python の正解 %d 組と一致", len(cases))
}

type valueNode struct {
	Kind  string
	Value json.RawMessage
}

func decodeNode(t *testing.T, n valueNode) any {
	t.Helper()
	decode := func(target any) {
		t.Helper()
		if err := json.Unmarshal(n.Value, target); err != nil {
			t.Fatal(err)
		}
	}
	switch n.Kind {
	case "null":
		return nil
	case "bool":
		var v bool
		decode(&v)
		return v
	case "int":
		var v string
		decode(&v)
		i, err := strconv.Atoi(v)
		if err != nil {
			t.Fatal(err)
		}
		return i
	case "float":
		var v string
		decode(&v)
		bits, err := strconv.ParseUint(v, 16, 64)
		if err != nil {
			t.Fatal(err)
		}
		return math.Float64frombits(bits)
	case "string":
		var v string
		decode(&v)
		return v
	case "array":
		var ns []valueNode
		decode(&ns)
		v := make([]any, 0, len(ns))
		for _, child := range ns {
			v = append(v, decodeNode(t, child))
		}
		return v
	case "object":
		var pairs [][]json.RawMessage
		decode(&pairs)
		m := NewOrderedMap()
		for _, pair := range pairs {
			var key string
			var child valueNode
			if err := json.Unmarshal(pair[0], &key); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(pair[1], &child); err != nil {
				t.Fatal(err)
			}
			m.Set(key, decodeNode(t, child))
		}
		return m
	default:
		t.Fatalf("未知の正解データの型 %q", n.Kind)
		return nil
	}
}

func TestJSONDumpsPython(t *testing.T) {
	var cases []struct {
		Value            valueNode
		Unsorted, Sorted string
	}
	fixture(t, "json", &cases)
	for i, c := range cases {
		v := decodeNode(t, c.Value)
		if got := JSONDumps(v, false); got != c.Unsorted {
			t.Fatalf("組 %d: 得た値=%q 正解=%q", i, got, c.Unsorted)
		}
		if got := JSONDumps(v, true); got != c.Sorted {
			t.Fatalf("組 %d の整列: 得た値=%q 正解=%q", i, got, c.Sorted)
		}
		if m, ok := v.(*OrderedMap); ok {
			if got := JSONDumps(m.values, true); got != c.Sorted {
				t.Fatalf("組 %d の map: 得た値=%q 正解=%q", i, got, c.Sorted)
			}
		}
	}
	t.Logf("Python の正解 %d 組を整列の有無で比較して一致", len(cases))
}

func TestOrderedMapUpdates(t *testing.T) {
	var m OrderedMap
	m.Set("z", 1)
	m.Set("a", 2)
	m.Set("z", 3)
	if got := JSONDumps(&m, false); got != `{"z": 3, "a": 2}` {
		t.Fatal(got)
	}
	if got := JSONDumps(&m, true); got != `{"a": 2, "z": 3}` {
		t.Fatal(got)
	}
	if got := JSONDumps(&m, false); got != `{"z": 3, "a": 2}` {
		t.Fatal(got)
	}
	if got := JSONDumps([]any(nil), false); got != "[]" {
		t.Fatal(got)
	}
	if got := JSONDumps(map[string]any(nil), true); got != "{}" {
		t.Fatal(got)
	}
}

func TestUnsortedMapRejected(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("順番の決まらない map を受け付けました")
		}
	}()
	JSONDumps(map[string]any{}, false)
}

func TestAtoiBoundaries(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	for _, s := range []string{strconv.Itoa(maxInt), "０００１２３", "١٢٣", "𝟙𝟚𝟛"} {
		n, ok := Atoi(s)
		if !ok {
			t.Fatal(s)
		}
		if s != strconv.Itoa(maxInt) && n != 123 {
			t.Fatal(s, n)
		}
	}
	for _, s := range []string{"", "-1", "+1", "1 2", "²", "a", strconv.FormatUint(uint64(maxInt)+1, 10), strings.Repeat("9", 1000)} {
		if n, ok := Atoi(s); ok {
			t.Fatalf("範囲外の値 %q を %d として受け付けました", s, n)
		}
	}
	if pythonIntMaxStrDigits > 0 {
		if _, ok := Atoi(strings.Repeat("０", pythonIntMaxStrDigits+1)); ok {
			t.Error("Python の整数文字列の桁数制限を超えた全角のゼロを受け付けました")
		}
	}
}

func ExampleUnifiedDiff() {
	fmt.Print(UnifiedDiff([]string{"旧\n"}, []string{"新\n"}, "元", "先"))
	// Output:
	// --- 元
	// +++ 先
	// @@ -1 +1 @@
	// -旧
	// +新
}
