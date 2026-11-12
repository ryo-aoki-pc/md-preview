// Python の単体テストと同じ入力を保存したデータで検査する。
package cellsyntax

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
	f, err := os.Open("../../testdata/unit/cellsyntax.json.gz")
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
	s := pythonDecode[string](t, c.Input)
	switch c.Op {
	case "spacing":
		return NormalizeListSpacing(s), nil
	case "pipe":
		return ToPipeCell(s)
	case "chunk":
		return ChunkToExcel(s), nil
	case "html":
		v, e := ToHTMLCell(s)
		var payload any = v.Text
		if v.Kind == "chunk" {
			payload = v.Lines
		}
		return []any{v.Kind, payload, v.Warnings}, e
	}
	t.Fatalf("未知の操作: %s", c.Op)
	return nil, nil
}

// Python: test_cells.ChunkTest.test_chunk_to_excel
func TestPythonTestCellsChunktestTestChunkToExcel(t *testing.T) {
	pythonRun(t, "test_cells.ChunkTest.test_chunk_to_excel")
}

// Python: test_cells.ChunkTest.test_convergence
func TestPythonTestCellsChunktestTestConvergence(t *testing.T) {
	pythonRun(t, "test_cells.ChunkTest.test_convergence")
}

// Python: test_cells.ChunkTest.test_forward
func TestPythonTestCellsChunktestTestForward(t *testing.T) {
	pythonRun(t, "test_cells.ChunkTest.test_forward")
}

// Python: test_cells.ChunkTest.test_lists_get_blank_lines
func TestPythonTestCellsChunktestTestListsGetBlankLines(t *testing.T) {
	pythonRun(t, "test_cells.ChunkTest.test_lists_get_blank_lines")
}

// Python: test_cells.ChunkTest.test_plain
func TestPythonTestCellsChunktestTestPlain(t *testing.T) {
	pythonRun(t, "test_cells.ChunkTest.test_plain")
}

// Python: test_cells.ChunkTest.test_refuses_table_tags
func TestPythonTestCellsChunktestTestRefusesTableTags(t *testing.T) {
	pythonRun(t, "test_cells.ChunkTest.test_refuses_table_tags")
}

// Python: test_cells.ChunkTest.test_tasks_and_definitions
func TestPythonTestCellsChunktestTestTasksAndDefinitions(t *testing.T) {
	pythonRun(t, "test_cells.ChunkTest.test_tasks_and_definitions")
}

// Python: test_cells.ChunkTest.test_unclosed_fence_is_closed
func TestPythonTestCellsChunktestTestUnclosedFenceIsClosed(t *testing.T) {
	pythonRun(t, "test_cells.ChunkTest.test_unclosed_fence_is_closed")
}

// Python: test_cells.ChunkTest.test_verbatim_blocks
func TestPythonTestCellsChunktestTestVerbatimBlocks(t *testing.T) {
	pythonRun(t, "test_cells.ChunkTest.test_verbatim_blocks")
}

// Python: test_cells.ExcelToPipeTest.test_blocks
func TestPythonTestCellsExceltopipetestTestBlocks(t *testing.T) {
	pythonRun(t, "test_cells.ExcelToPipeTest.test_blocks")
}

// Python: test_cells.ExcelToPipeTest.test_breaks
func TestPythonTestCellsExceltopipetestTestBreaks(t *testing.T) {
	pythonRun(t, "test_cells.ExcelToPipeTest.test_breaks")
}

// Python: test_cells.ExcelToPipeTest.test_not_representable
func TestPythonTestCellsExceltopipetestTestNotRepresentable(t *testing.T) {
	pythonRun(t, "test_cells.ExcelToPipeTest.test_not_representable")
}

// Python: test_cells.ExcelToPipeTest.test_pipes
func TestPythonTestCellsExceltopipetestTestPipes(t *testing.T) {
	pythonRun(t, "test_cells.ExcelToPipeTest.test_pipes")
}

// Python: test_cells.ExcelToPipeTest.test_round_trip_converges
func TestPythonTestCellsExceltopipetestTestRoundTripConverges(t *testing.T) {
	pythonRun(t, "test_cells.ExcelToPipeTest.test_round_trip_converges")
}

// Python: test_cells.ListSpacingTest.test_alternating_paragraphs_and_lists
func TestPythonTestCellsListspacingtestTestAlternatingParagraphsAndLists(t *testing.T) {
	pythonRun(t, "test_cells.ListSpacingTest.test_alternating_paragraphs_and_lists")
}

// Python: test_cells.ListSpacingTest.test_interrupting_lists
func TestPythonTestCellsListspacingtestTestInterruptingLists(t *testing.T) {
	pythonRun(t, "test_cells.ListSpacingTest.test_interrupting_lists")
}

// Python: test_cells.ListSpacingTest.test_keeps_blank_lines_inside_loose_list
func TestPythonTestCellsListspacingtestTestKeepsBlankLinesInsideLooseList(t *testing.T) {
	pythonRun(t, "test_cells.ListSpacingTest.test_keeps_blank_lines_inside_loose_list")
}

// Python: test_cells.ListSpacingTest.test_keeps_noninterrupting_and_nested_spacing
func TestPythonTestCellsListspacingtestTestKeepsNoninterruptingAndNestedSpacing(t *testing.T) {
	pythonRun(t, "test_cells.ListSpacingTest.test_keeps_noninterrupting_and_nested_spacing")
}

// Python: test_cells.ListSpacingTest.test_literal_task_markers_round_trip
func TestPythonTestCellsListspacingtestTestLiteralTaskMarkersRoundTrip(t *testing.T) {
	pythonRun(t, "test_cells.ListSpacingTest.test_literal_task_markers_round_trip")
}

// Python: test_cells.ListSpacingTest.test_refuses_pipe_table_before_plain_fast_path
func TestPythonTestCellsListspacingtestTestRefusesPipeTableBeforePlainFastPath(t *testing.T) {
	pythonRun(t, "test_cells.ListSpacingTest.test_refuses_pipe_table_before_plain_fast_path")
}

// Python: test_cells.ListSpacingTest.test_without_double_newline_does_not_parse
func TestPythonTestCellsListspacingtestTestWithoutDoubleNewlineDoesNotParse(t *testing.T) {
	pythonRun(t, "test_cells.ListSpacingTest.test_without_double_newline_does_not_parse")
}
