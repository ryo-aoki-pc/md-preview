// Python の単体テストと同じ入力を保存したデータで検査する。
package changes

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"github.com/ryo-aoki-pc/md-table-excel/internal/model"
	"os"
	"reflect"
	"sort"
	"testing"
	"time"
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
	f, err := os.Open("../../testdata/unit/changes.json.gz")
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

type pythonModel struct {
	Kind       string
	Rows, Cols int
	Cells      [][]any
	Holes      [][]int
}

func (m pythonModel) model() *model.TableModel {
	cells := []*model.Cell{}
	for _, v := range m.Cells {
		align, _ := v[5].(string)
		cells = append(cells, &model.Cell{Row: int(v[0].(float64)), Col: int(v[1].(float64)), Rowspan: int(v[2].(float64)), Colspan: int(v[3].(float64)), Header: v[4].(bool), Align: align, Text: v[6].(string)})
	}
	holes := map[model.Position]bool{}
	for _, p := range m.Holes {
		holes[model.Position{Row: p[0], Col: p[1]}] = true
	}
	return model.New(m.Kind, m.Rows, m.Cols, cells, holes)
}
func pythonModelValue(m *model.TableModel) any {
	if m == nil {
		return nil
	}
	cells := []any{}
	for _, c := range m.Cells {
		cells = append(cells, c.Key())
	}
	holes := [][2]int{}
	for p := range m.Holes {
		holes = append(holes, [2]int{p.Row, p.Col})
	}
	sort.Slice(holes, func(i, j int) bool {
		return holes[i][0] < holes[j][0] || holes[i][0] == holes[j][0] && holes[i][1] < holes[j][1]
	})
	return map[string]any{"kind": m.Kind, "rows": m.Rows, "cols": m.Cols, "cells": cells, "holes": holes}
}

func pythonEvaluate(t *testing.T, c pythonCase) (any, error) {
	a := pythonDecode[[]pythonModel](t, c.Input)
	started := time.Now()
	v := CompareModels(a[0].model(), a[1].model())
	cells := []any{}
	if c.Test == "test_changes.ChangesTest.test_large_table_with_merges_is_fast" && time.Since(started) > 5*time.Second {
		t.Fatal("結合のある大きな表の突き合わせが 5 秒を超えた")
	}
	for _, x := range v.Cells {
		cells = append(cells, []any{x.Row, x.Col, x.OldText, x.NewText, x.Kind})
	}
	return map[string]any{"summary": v.Summary(), "details": v.Details(20), "full": v.Details(28), "cells": cells}, nil
}

// Python: test_changes.ChangesTest.test_added_column_next_to_edited_column
func TestPythonTestChangesChangestestTestAddedColumnNextToEditedColumn(t *testing.T) {
	pythonRun(t, "test_changes.ChangesTest.test_added_column_next_to_edited_column")
}

// Python: test_changes.ChangesTest.test_alignment_and_header_only
func TestPythonTestChangesChangestestTestAlignmentAndHeaderOnly(t *testing.T) {
	pythonRun(t, "test_changes.ChangesTest.test_alignment_and_header_only")
}

// Python: test_changes.ChangesTest.test_changed_cell_uses_new_row
func TestPythonTestChangesChangestestTestChangedCellUsesNewRow(t *testing.T) {
	pythonRun(t, "test_changes.ChangesTest.test_changed_cell_uses_new_row")
}

// Python: test_changes.ChangesTest.test_columns_then_rows
func TestPythonTestChangesChangestestTestColumnsThenRows(t *testing.T) {
	pythonRun(t, "test_changes.ChangesTest.test_columns_then_rows")
}

// Python: test_changes.ChangesTest.test_delete_middle_row
func TestPythonTestChangesChangestestTestDeleteMiddleRow(t *testing.T) {
	pythonRun(t, "test_changes.ChangesTest.test_delete_middle_row")
}

// Python: test_changes.ChangesTest.test_detail_limit_counts_cells_with_several_changes_once
func TestPythonTestChangesChangestestTestDetailLimitCountsCellsWithSeveralChangesOnce(t *testing.T) {
	pythonRun(t, "test_changes.ChangesTest.test_detail_limit_counts_cells_with_several_changes_once")
}

// Python: test_changes.ChangesTest.test_fill_or_clear_hole
func TestPythonTestChangesChangestestTestFillOrClearHole(t *testing.T) {
	pythonRun(t, "test_changes.ChangesTest.test_fill_or_clear_hole")
}

// Python: test_changes.ChangesTest.test_insert_and_delete_columns
func TestPythonTestChangesChangestestTestInsertAndDeleteColumns(t *testing.T) {
	pythonRun(t, "test_changes.ChangesTest.test_insert_and_delete_columns")
}

// Python: test_changes.ChangesTest.test_insert_middle_row_does_not_change_later_cells
func TestPythonTestChangesChangestestTestInsertMiddleRowDoesNotChangeLaterCells(t *testing.T) {
	pythonRun(t, "test_changes.ChangesTest.test_insert_middle_row_does_not_change_later_cells")
}

// Python: test_changes.ChangesTest.test_large_table_with_merges_is_fast
func TestPythonTestChangesChangestestTestLargeTableWithMergesIsFast(t *testing.T) {
	pythonRun(t, "test_changes.ChangesTest.test_large_table_with_merges_is_fast")
}

// Python: test_changes.ChangesTest.test_limit_and_excel_column_address
func TestPythonTestChangesChangestestTestLimitAndExcelColumnAddress(t *testing.T) {
	pythonRun(t, "test_changes.ChangesTest.test_limit_and_excel_column_address")
}

// Python: test_changes.ChangesTest.test_long_text_append_and_delete_use_same_window
func TestPythonTestChangesChangestestTestLongTextAppendAndDeleteUseSameWindow(t *testing.T) {
	pythonRun(t, "test_changes.ChangesTest.test_long_text_append_and_delete_use_same_window")
}

// Python: test_changes.ChangesTest.test_long_text_shows_first_difference
func TestPythonTestChangesChangestestTestLongTextShowsFirstDifference(t *testing.T) {
	pythonRun(t, "test_changes.ChangesTest.test_long_text_shows_first_difference")
}

// Python: test_changes.ChangesTest.test_long_text_window_normalizes_newlines
func TestPythonTestChangesChangestestTestLongTextWindowNormalizesNewlines(t *testing.T) {
	pythonRun(t, "test_changes.ChangesTest.test_long_text_window_normalizes_newlines")
}

// Python: test_changes.ChangesTest.test_merge_and_unmerge_at_anchor
func TestPythonTestChangesChangestestTestMergeAndUnmergeAtAnchor(t *testing.T) {
	pythonRun(t, "test_changes.ChangesTest.test_merge_and_unmerge_at_anchor")
}

// Python: test_changes.ChangesTest.test_merged_cell_text_and_extent
func TestPythonTestChangesChangestestTestMergedCellTextAndExtent(t *testing.T) {
	pythonRun(t, "test_changes.ChangesTest.test_merged_cell_text_and_extent")
}

// Python: test_changes.ChangesTest.test_multiple_kinds_count_cell_once
func TestPythonTestChangesChangestestTestMultipleKindsCountCellOnce(t *testing.T) {
	pythonRun(t, "test_changes.ChangesTest.test_multiple_kinds_count_cell_once")
}

// Python: test_changes.ChangesTest.test_repeated_rows_and_empty_table
func TestPythonTestChangesChangestestTestRepeatedRowsAndEmptyTable(t *testing.T) {
	pythonRun(t, "test_changes.ChangesTest.test_repeated_rows_and_empty_table")
}

// Python: test_changes.ChangesTest.test_repeated_rows_are_not_junk
func TestPythonTestChangesChangestestTestRepeatedRowsAreNotJunk(t *testing.T) {
	pythonRun(t, "test_changes.ChangesTest.test_repeated_rows_are_not_junk")
}

// Python: test_changes.ChangesTest.test_rows_and_columns_changed_together
func TestPythonTestChangesChangestestTestRowsAndColumnsChangedTogether(t *testing.T) {
	pythonRun(t, "test_changes.ChangesTest.test_rows_and_columns_changed_together")
}

// Python: test_changes.ChangesTest.test_shortening_newlines_and_empty_text
func TestPythonTestChangesChangestestTestShorteningNewlinesAndEmptyText(t *testing.T) {
	pythonRun(t, "test_changes.ChangesTest.test_shortening_newlines_and_empty_text")
}

// Python: test_changes.ChangesTest.test_single_cell_and_no_change
func TestPythonTestChangesChangestestTestSingleCellAndNoChange(t *testing.T) {
	pythonRun(t, "test_changes.ChangesTest.test_single_cell_and_no_change")
}
