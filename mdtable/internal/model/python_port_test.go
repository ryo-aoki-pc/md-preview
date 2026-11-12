// Python の単体テストと同じ入力を保存したデータで検査する。
package model

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sort"
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
	f, err := os.Open("../../testdata/unit/model.json.gz")
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

func (m pythonModel) model() *TableModel {
	cells := []*Cell{}
	for _, v := range m.Cells {
		align, _ := v[5].(string)
		cells = append(cells, &Cell{Row: int(v[0].(float64)), Col: int(v[1].(float64)), Rowspan: int(v[2].(float64)), Colspan: int(v[3].(float64)), Header: v[4].(bool), Align: align, Text: v[6].(string)})
	}
	holes := map[Position]bool{}
	for _, p := range m.Holes {
		holes[Position{Row: p[0], Col: p[1]}] = true
	}
	return New(m.Kind, m.Rows, m.Cols, cells, holes)
}
func pythonModelValue(m *TableModel) any {
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
	if c.Op == "normalize" {
		return NormalizeText(pythonDecode[string](t, c.Input)), nil
	}
	m := pythonDecode[pythonModel](t, c.Input).model()
	before := pythonModelValue(m)
	sheet := SheetForm(m)
	result := pythonModelValue(sheet)
	if len(sheet.Cells) > 0 {
		sheet.Cells[0].Text = "コピーの確認"
	}
	if !reflect.DeepEqual(pythonModelValue(m), before) {
		t.Fatal("元のセルや穴の位置が変わった")
	}
	return result, nil
}

// Python: test_workbook.NormalizeTest.test_normalize
func TestPythonTestWorkbookNormalizetestTestNormalize(t *testing.T) {
	pythonRun(t, "test_workbook.NormalizeTest.test_normalize")
}

// Python: test_workbook.NormalizeTest.test_sheet_form_copies_cells_and_fills_holes
func TestPythonTestWorkbookNormalizetestTestSheetFormCopiesCellsAndFillsHoles(t *testing.T) {
	pythonRun(t, "test_workbook.NormalizeTest.test_sheet_form_copies_cells_and_fills_holes")
}
