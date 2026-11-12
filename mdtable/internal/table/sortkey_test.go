package table

import (
	"reflect"
	"sort"
	"testing"
)

func TestSortNumbers(t *testing.T) {
	for _, c := range []struct{ text, value string }{
		{"-1.5", "-1.5"}, {"  +1.50\t", "1.5"}, {"1,234", "1234"}, {"12,345,678.90", "12345678.9"},
		{"12%", ".12"}, {"-12.5%", "-.125"}, {"１２３", "123"}, {"－１２．５０", "-12.5"}, {"＋１２．５", "12.5"},
		{"0", "-0"}, {"000.000", "0"}, {".005", "0.005"}, {"12.", "12"}, {"1e2", "100"}, {"1,000e-2%", ".1"},
		{"9007199254740993", "9007199254740993"}, {"1e999999999999999999999", "10e999999999999999999998"},
		{"１，２３４", "1234"}, {"１２％", ".12"}, {"　＋１．５　", "1.5"},
	} {
		a, b := parseSortNumber(c.text), parseSortNumber(c.value)
		if a == nil || b == nil || compareSortNumber(a, b) != 0 {
			t.Fatalf("数値の読み方が違う: %q と %q", c.text, c.value)
		}
	}
	for _, text := range []string{"", " ", "1,2,3", "1,23", "1234,567", "1,,234", ",123", "1.2.3", "12 個", "NaN", "Inf", "0x10", "1_000", "1%2", "%", "1 %", "--1", ".", "1e", "１，２３", "１　２"} {
		if parseSortNumber(text) != nil {
			t.Fatalf("文字を数値として読んだ: %q", text)
		}
	}
	for _, c := range [][2]string{{"-10", "-2"}, {"-.1", "0"}, {"0", ".001"}, {".009", ".01"}, {"1.0001", "1.001"}, {"9007199254740992", "9007199254740993"}, {"1e999999999999999999998", "1e999999999999999999999"}} {
		if compareSortNumber(parseSortNumber(c[0]), parseSortNumber(c[1])) >= 0 {
			t.Fatal("数値の順序が違う", c)
		}
	}
}

func TestFoldSortText(t *testing.T) {
	if len(halfwidthKana) != 0xff9f-0xff61+1 {
		t.Fatal("半角カタカナの対応表の長さが違う")
	}
	for _, c := range [][2]string{
		{"ＡＢＣａｂｃ０１２３！～　", "abcabc0123!~ "}, {"ｶﾞｷﾞｸﾞｹﾞｺﾞ", "ガギグゲゴ"},
		{"ｻﾞｼﾞｽﾞｾﾞｿﾞﾀﾞﾁﾞﾂﾞﾃﾞﾄﾞ", "ザジズゼゾダヂヅデド"}, {"ﾊﾞﾋﾞﾌﾞﾍﾞﾎﾞ", "バビブベボ"},
		{"ﾊﾟﾋﾟﾌﾟﾍﾟﾎﾟｳﾞﾜﾞｦﾞ", "パピプペポヴヷヺ"}, {"ｶﾅ｡｢｣､･ｰ", "カナ。「」、・ー"},
		{"ｱﾞｱﾟ", "ア\u3099ア\u309a"}, {"ÄΣ", "äσ"},
	} {
		if got := foldSortText(c[0]); got != c[1] {
			t.Fatalf("幅と大小の畳み込みが違う: %q 得た %q 期待 %q", c[0], got, c[1])
		}
	}
}

func TestSortKeys(t *testing.T) {
	for _, c := range []struct {
		name              string
		values, asc, desc []string
	}{
		{"種類と空", []string{"", "文字", "12%", "-1.5", " ", "1,234", "１２３"}, []string{"-1.5", "12%", "１２３", "1,234", "文字", "", " "}, []string{"文字", "1,234", "１２３", "12%", "-1.5", "", " "}},
		{"自然な順", []string{"項目10", "項目2", "項目01", "項目1", "項目0002a", "項目2b"}, []string{"項目01", "項目1", "項目2", "項目0002a", "項目2b", "項目10"}, []string{"項目10", "項目2b", "項目0002a", "項目2", "項目1", "項目01"}},
		{"正規化と原文", []string{"ｶﾅ", "カナ", "Ａ2", "a10", "a2", "A2"}, []string{"A2", "a2", "Ａ2", "a10", "カナ", "ｶﾅ"}, []string{"ｶﾅ", "カナ", "a10", "Ａ2", "a2", "A2"}},
		{"安定", []string{"1.0", "１", "1", "100%", "同じ", "同じ"}, []string{"1.0", "１", "1", "100%", "同じ", "同じ"}, []string{"同じ", "同じ", "1.0", "１", "1", "100%"}},
	} {
		for _, desc := range []bool{false, true} {
			got := append([]string{}, c.values...)
			sort.SliceStable(got, func(i, j int) bool { return compareSortKey(newSortKey(got[i]), newSortKey(got[j]), desc) < 0 })
			want := c.asc
			if desc {
				want = c.desc
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("%s 降順=%v: 得た %q 期待 %q", c.name, desc, got, want)
			}
		}
	}
	if newSortKey("ＡＢＣｶﾅ").text != newSortKey("abcカナ").text {
		t.Fatal("大小と全角半角を区別した")
	}
	if newSortKey("**12**").kind != 1 || newSortKey("[12](/x)").kind != 1 {
		t.Fatal("Markdown の記号を取り除いた")
	}
}
