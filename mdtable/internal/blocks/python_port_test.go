// Python の単体テストと同じ入力を保存したデータで検査する。
package blocks

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"
)

type pythonCase struct {
	Test, Op    string
	Input, Want json.RawMessage
	Error       []any
}

func pythonDecode[T any](t *testing.T, data []byte) T {
	t.Helper()
	var value T
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func pythonCanonical(v any) any {
	switch x := v.(type) {
	case nil:
		return []any{}
	case []any:
		for i := range x {
			x[i] = pythonCanonical(x[i])
		}
		return x
	case map[string]any:
		for k := range x {
			x[k] = pythonCanonical(x[k])
		}
		return x
	}
	return v
}

func pythonCheck(t *testing.T, got any, want json.RawMessage) {
	t.Helper()
	data, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	a := pythonCanonical(pythonDecode[any](t, data))
	b := pythonCanonical(pythonDecode[any](t, want))
	if !reflect.DeepEqual(a, b) {
		t.Fatal(pythonDifference(a, b, "結果"))
	}
}

func pythonDifference(a, b any, path string) string {
	if reflect.DeepEqual(a, b) {
		return ""
	}
	if x, ok := a.(map[string]any); ok {
		if y, ok := b.(map[string]any); ok {
			keys := []string{}
			for k := range x {
				keys = append(keys, k)
			}
			for k := range y {
				if _, ok := x[k]; !ok {
					keys = append(keys, k)
				}
			}
			for _, k := range keys {
				if d := pythonDifference(x[k], y[k], path+"."+k); d != "" {
					return d
				}
			}
		}
	}
	if x, ok := a.([]any); ok {
		if y, ok := b.([]any); ok && len(x) == len(y) {
			for i := range x {
				if d := pythonDifference(x[i], y[i], fmt.Sprintf("%s[%d]", path, i)); d != "" {
					return d
				}
			}
		}
	}
	return fmt.Sprintf("%s が Python の期待値と違う: 結果 %.1000v、期待 %.1000v", path, a, b)
}

func pythonRun(t *testing.T, name string) {
	t.Helper()
	t.Setenv("MDTABLE_FONT", "BIZ UDゴシック")
	f, err := os.Open("../../testdata/unit/blocks.json.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	var cases []pythonCase
	if err := json.NewDecoder(gz).Decode(&cases); err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, c := range cases {
		if c.Test != name {
			continue
		}
		count++
		t.Run(fmt.Sprintf("%s_%d", c.Op, count), func(t *testing.T) {
			got, err := pythonEvaluate(t, c)
			if len(c.Error) > 0 {
				if err == nil {
					t.Fatalf("エラーを返さなかった: %s", c.Error[0])
				}
				row, col := 0, 0
				v := reflect.ValueOf(err)
				if v.Kind() == reflect.Pointer {
					v = v.Elem()
					if v.Kind() == reflect.Struct {
						if r := v.FieldByName("Row"); r.IsValid() {
							row = int(r.Int())
						}
						if r := v.FieldByName("Col"); r.IsValid() {
							col = int(r.Int())
						}
					}
				}
				data, _ := json.Marshal(c.Error)
				pythonCheck(t, []any{err.Error(), row, col}, data)
			} else {
				if err != nil {
					t.Fatal(err)
				}
				pythonCheck(t, got, c.Want)
			}
		})
	}
	if count == 0 {
		t.Fatal("元のテストの入力がない")
	}
}

func pythonEvaluate(t *testing.T, c pythonCase) (any, error) {
	switch c.Op {
	case "lines":
		a, b := SplitLines(pythonDecode[string](t, c.Input))
		return []any{a, b}, nil
	case "unescape":
		return UnescapePipes(pythonDecode[string](t, c.Input)), nil
	case "parse":
		a := pythonDecode[[]any](t, c.Input)
		d := ParseText(a[0].(string), a[1].(bool), a[2].(bool))
		result := []any{}
		for _, n := range d.Root.Walk() {
			chain := []string{}
			for p := n.Parent; p != nil; p = p.Parent {
				if p.Kind != DocumentKind {
					chain = append(chain, p.Kind)
				}
			}
			result = append(result, []any{n.Kind, n.StartLine, n.EndLine, chain})
		}
		return result, nil
	}
	t.Fatalf("未知の操作: %s", c.Op)
	return nil, nil
}

// Python: test_blocks.BlockParserTest.test_description_list_structure
func TestPythonTestBlocksBlockparsertestTestDescriptionListStructure(t *testing.T) {
	pythonRun(t, "test_blocks.BlockParserTest.test_description_list_structure")
}

// Python: test_blocks.BlockParserTest.test_line_endings
func TestPythonTestBlocksBlockparsertestTestLineEndings(t *testing.T) {
	pythonRun(t, "test_blocks.BlockParserTest.test_line_endings")
}

// Python: test_blocks.BlockParserTest.test_unescape_pipes
func TestPythonTestBlocksBlockparsertestTestUnescapePipes(t *testing.T) {
	pythonRun(t, "test_blocks.BlockParserTest.test_unescape_pipes")
}

// Python: test_cells.ExcelToPipeTest.test_round_trip_converges
func TestPythonTestCellsExceltopipetestTestRoundTripConverges(t *testing.T) {
	pythonRun(t, "test_cells.ExcelToPipeTest.test_round_trip_converges")
}

// Python: test_tables.PipeToHtmlTest.test_merge_converts_and_keeps_following_list
func TestPythonTestTablesPipetohtmltestTestMergeConvertsAndKeepsFollowingList(t *testing.T) {
	pythonRun(t, "test_tables.PipeToHtmlTest.test_merge_converts_and_keeps_following_list")
}
