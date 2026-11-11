// Python の単体テストと同じ入力を保存したデータで検査する。
package table

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"github.com/ryo-aoki-pc/md-table-excel/internal/model"
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
	f, err := os.Open("../../testdata/unit/table.json.gz")
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

func pythonRegionsValue(regions []*Region) any {
	result := []any{}
	for _, r := range regions {
		var m *model.TableModel
		if r.Pipe != nil || r.HTML != nil {
			m = r.Model()
		}
		links := []bool{}
		if r.HTML != nil {
			for _, c := range r.HTML.Cells {
				links = append(links, c.LinkDefinitions)
			}
		}
		result = append(result, map[string]any{"number": r.Number, "kind": r.Kind, "start": r.Start, "end": r.End,
			"prefixes": r.Prefixes, "bodies": r.Bodies, "cont_prefix": r.ContPrefix, "head": r.Head, "tail": r.Tail,
			"location": r.Location, "heading": r.Heading, "heading_line": r.HeadingLine, "warnings": r.Warnings,
			"excluded": r.Excluded, "model": pythonModelValue(m), "link_definitions": links})
	}
	return result
}

func pythonEvaluate(t *testing.T, c pythonCase) (any, error) {
	switch c.Op {
	case "regions", "find":
		return pythonRegionsValue(FindTables(NewMarkdown("<test>", pythonDecode[string](t, c.Input), false))), nil
	case "cell":
		return PipeCellToExcel(pythonDecode[string](t, c.Input)), nil
	case "select":
		a := pythonDecode[[]json.RawMessage](t, c.Input)
		found := FindTables(NewMarkdown("<test>", pythonDecode[string](t, a[0]), false))
		selected, e := SelectTables(found, pythonDecode[[]string](t, a[1]))
		result := []int{}
		for _, r := range selected {
			result = append(result, r.Number)
		}
		return result, e
	}
	t.Fatalf("未知の操作: %s", c.Op)
	return nil, nil
}

// Python: test_blocks.IndentedTableTest.test_blockquote_list
func TestPythonTestBlocksIndentedtabletestTestBlockquoteList(t *testing.T) {
	pythonRun(t, "test_blocks.IndentedTableTest.test_blockquote_list")
}

// Python: test_blocks.IndentedTableTest.test_description_in_list
func TestPythonTestBlocksIndentedtabletestTestDescriptionInList(t *testing.T) {
	pythonRun(t, "test_blocks.IndentedTableTest.test_description_in_list")
}

// Python: test_blocks.IndentedTableTest.test_description_list
func TestPythonTestBlocksIndentedtabletestTestDescriptionList(t *testing.T) {
	pythonRun(t, "test_blocks.IndentedTableTest.test_description_list")
}

// Python: test_blocks.IndentedTableTest.test_extra_indentation
func TestPythonTestBlocksIndentedtabletestTestExtraIndentation(t *testing.T) {
	pythonRun(t, "test_blocks.IndentedTableTest.test_extra_indentation")
}

// Python: test_blocks.IndentedTableTest.test_footnote
func TestPythonTestBlocksIndentedtabletestTestFootnote(t *testing.T) {
	pythonRun(t, "test_blocks.IndentedTableTest.test_footnote")
}

// Python: test_blocks.IndentedTableTest.test_html_table_in_list
func TestPythonTestBlocksIndentedtabletestTestHtmlTableInList(t *testing.T) {
	pythonRun(t, "test_blocks.IndentedTableTest.test_html_table_in_list")
}

// Python: test_blocks.IndentedTableTest.test_nested_list
func TestPythonTestBlocksIndentedtabletestTestNestedList(t *testing.T) {
	pythonRun(t, "test_blocks.IndentedTableTest.test_nested_list")
}

// Python: test_blocks.IndentedTableTest.test_ordered_list
func TestPythonTestBlocksIndentedtabletestTestOrderedList(t *testing.T) {
	pythonRun(t, "test_blocks.IndentedTableTest.test_ordered_list")
}

// Python: test_blocks.IndentedTableTest.test_table_on_marker_line
func TestPythonTestBlocksIndentedtabletestTestTableOnMarkerLine(t *testing.T) {
	pythonRun(t, "test_blocks.IndentedTableTest.test_table_on_marker_line")
}

// Python: test_blocks.IndentedTableTest.test_tabs
func TestPythonTestBlocksIndentedtabletestTestTabs(t *testing.T) {
	pythonRun(t, "test_blocks.IndentedTableTest.test_tabs")
}

// Python: test_blocks.TableDetectionTest.test_block_starts_end_table
func TestPythonTestBlocksTabledetectiontestTestBlockStartsEndTable(t *testing.T) {
	pythonRun(t, "test_blocks.TableDetectionTest.test_block_starts_end_table")
}

// Python: test_blocks.TableDetectionTest.test_blockquote_and_lazy_lines
func TestPythonTestBlocksTabledetectiontestTestBlockquoteAndLazyLines(t *testing.T) {
	pythonRun(t, "test_blocks.TableDetectionTest.test_blockquote_and_lazy_lines")
}

// Python: test_blocks.TableDetectionTest.test_cell_count_mismatch
func TestPythonTestBlocksTabledetectiontestTestCellCountMismatch(t *testing.T) {
	pythonRun(t, "test_blocks.TableDetectionTest.test_cell_count_mismatch")
}

// Python: test_blocks.TableDetectionTest.test_fences_close_with_container
func TestPythonTestBlocksTabledetectiontestTestFencesCloseWithContainer(t *testing.T) {
	pythonRun(t, "test_blocks.TableDetectionTest.test_fences_close_with_container")
}

// Python: test_blocks.TableDetectionTest.test_front_matter_and_multiline_quote
func TestPythonTestBlocksTabledetectiontestTestFrontMatterAndMultilineQuote(t *testing.T) {
	pythonRun(t, "test_blocks.TableDetectionTest.test_front_matter_and_multiline_quote")
}

// Python: test_blocks.TableDetectionTest.test_hidden_cells
func TestPythonTestBlocksTabledetectiontestTestHiddenCells(t *testing.T) {
	pythonRun(t, "test_blocks.TableDetectionTest.test_hidden_cells")
}

// Python: test_blocks.TableDetectionTest.test_html_blocks_hide_tables
func TestPythonTestBlocksTabledetectiontestTestHtmlBlocksHideTables(t *testing.T) {
	pythonRun(t, "test_blocks.TableDetectionTest.test_html_blocks_hide_tables")
}

// Python: test_blocks.TableDetectionTest.test_lines_that_stay_rows
func TestPythonTestBlocksTabledetectiontestTestLinesThatStayRows(t *testing.T) {
	pythonRun(t, "test_blocks.TableDetectionTest.test_lines_that_stay_rows")
}

// Python: test_blocks.TableDetectionTest.test_list_indentation
func TestPythonTestBlocksTabledetectiontestTestListIndentation(t *testing.T) {
	pythonRun(t, "test_blocks.TableDetectionTest.test_list_indentation")
}

// Python: test_blocks.TableDetectionTest.test_pipe_escapes
func TestPythonTestBlocksTabledetectiontestTestPipeEscapes(t *testing.T) {
	pythonRun(t, "test_blocks.TableDetectionTest.test_pipe_escapes")
}

// Python: test_blocks.TableDetectionTest.test_rows_without_pipes_continue
func TestPythonTestBlocksTabledetectiontestTestRowsWithoutPipesContinue(t *testing.T) {
	pythonRun(t, "test_blocks.TableDetectionTest.test_rows_without_pipes_continue")
}

// Python: test_blocks.TableDetectionTest.test_setext_wins
func TestPythonTestBlocksTabledetectiontestTestSetextWins(t *testing.T) {
	pythonRun(t, "test_blocks.TableDetectionTest.test_setext_wins")
}

// Python: test_blocks.TableDetectionTest.test_table_interrupts_paragraph
func TestPythonTestBlocksTabledetectiontestTestTableInterruptsParagraph(t *testing.T) {
	pythonRun(t, "test_blocks.TableDetectionTest.test_table_interrupts_paragraph")
}

// Python: test_cells.ExcelToPipeTest.test_round_trip_converges
func TestPythonTestCellsExceltopipetestTestRoundTripConverges(t *testing.T) {
	pythonRun(t, "test_cells.ExcelToPipeTest.test_round_trip_converges")
}

// Python: test_cells.HtmlToExcelTest.test_raw_links_do_not_change_markdown_chunks_or_pipe_cells
func TestPythonTestCellsHtmltoexceltestTestRawLinksDoNotChangeMarkdownChunksOrPipeCells(t *testing.T) {
	pythonRun(t, "test_cells.HtmlToExcelTest.test_raw_links_do_not_change_markdown_chunks_or_pipe_cells")
}

// Python: test_cells.HtmlToExcelTest.test_raw_links_in_flow_elements
func TestPythonTestCellsHtmltoexceltestTestRawLinksInFlowElements(t *testing.T) {
	pythonRun(t, "test_cells.HtmlToExcelTest.test_raw_links_in_flow_elements")
}

// Python: test_cells.ListSpacingTest.test_literal_task_markers_round_trip
func TestPythonTestCellsListspacingtestTestLiteralTaskMarkersRoundTrip(t *testing.T) {
	pythonRun(t, "test_cells.ListSpacingTest.test_literal_task_markers_round_trip")
}

// Python: test_cells.ListSpacingTest.test_non_task_brackets_stay_literal
func TestPythonTestCellsListspacingtestTestNonTaskBracketsStayLiteral(t *testing.T) {
	pythonRun(t, "test_cells.ListSpacingTest.test_non_task_brackets_stay_literal")
}

// Python: test_cells.PipeToExcelTest.test_breaks
func TestPythonTestCellsPipetoexceltestTestBreaks(t *testing.T) {
	pythonRun(t, "test_cells.PipeToExcelTest.test_breaks")
}

// Python: test_cells.PipeToExcelTest.test_line_start_escapes
func TestPythonTestCellsPipetoexceltestTestLineStartEscapes(t *testing.T) {
	pythonRun(t, "test_cells.PipeToExcelTest.test_line_start_escapes")
}

// Python: test_cells.PipeToExcelTest.test_lists
func TestPythonTestCellsPipetoexceltestTestLists(t *testing.T) {
	pythonRun(t, "test_cells.PipeToExcelTest.test_lists")
}

// Python: test_cli.CliTest.test_convert_raw_html_links_to_pipe_and_back
func TestPythonTestCliClitestTestConvertRawHtmlLinksToPipeAndBack(t *testing.T) {
	pythonRun(t, "test_cli.CliTest.test_convert_raw_html_links_to_pipe_and_back")
}

// Python: test_convert.ConversionVerifyTest.test_conversion_requires_target_kind
func TestPythonTestConvertConversionverifytestTestConversionRequiresTargetKind(t *testing.T) {
	pythonRun(t, "test_convert.ConversionVerifyTest.test_conversion_requires_target_kind")
}

// Python: test_convert.ConversionVerifyTest.test_model_differences_block_conversion
func TestPythonTestConvertConversionverifytestTestModelDifferencesBlockConversion(t *testing.T) {
	pythonRun(t, "test_convert.ConversionVerifyTest.test_model_differences_block_conversion")
}

// Python: test_convert.ConvertTest.test_container_prefixes_and_tight_warning
func TestPythonTestConvertConverttestTestContainerPrefixesAndTightWarning(t *testing.T) {
	pythonRun(t, "test_convert.ConvertTest.test_container_prefixes_and_tight_warning")
}

// Python: test_convert.ConvertTest.test_excluded_table_is_a_problem_even_with_same_kind
func TestPythonTestConvertConverttestTestExcludedTableIsAProblemEvenWithSameKind(t *testing.T) {
	pythonRun(t, "test_convert.ConvertTest.test_excluded_table_is_a_problem_even_with_same_kind")
}

// Python: test_convert.ConvertTest.test_html_cells_breaks_lists_pipes_and_alignment
func TestPythonTestConvertConverttestTestHtmlCellsBreaksListsPipesAndAlignment(t *testing.T) {
	pythonRun(t, "test_convert.ConvertTest.test_html_cells_breaks_lists_pipes_and_alignment")
}

// Python: test_convert.ConvertTest.test_html_paragraph_lists_convert_both_ways
func TestPythonTestConvertConverttestTestHtmlParagraphListsConvertBothWays(t *testing.T) {
	pythonRun(t, "test_convert.ConvertTest.test_html_paragraph_lists_convert_both_ways")
}

// Python: test_convert.ConvertTest.test_multiple_selectors_and_unselected_problem
func TestPythonTestConvertConverttestTestMultipleSelectorsAndUnselectedProblem(t *testing.T) {
	pythonRun(t, "test_convert.ConvertTest.test_multiple_selectors_and_unselected_problem")
}

// Python: test_convert.ConvertTest.test_pipe_html_pipe_preserves_models_and_other_lines
func TestPythonTestConvertConverttestTestPipeHtmlPipePreservesModelsAndOtherLines(t *testing.T) {
	pythonRun(t, "test_convert.ConvertTest.test_pipe_html_pipe_preserves_models_and_other_lines")
}

// Python: test_convert.ConvertTest.test_refuses_when_blank_line_changes_following_block
func TestPythonTestConvertConverttestTestRefusesWhenBlankLineChangesFollowingBlock(t *testing.T) {
	pythonRun(t, "test_convert.ConvertTest.test_refuses_when_blank_line_changes_following_block")
}

// Python: test_convert.ConvertTest.test_table_in_description_list
func TestPythonTestConvertConverttestTestTableInDescriptionList(t *testing.T) {
	pythonRun(t, "test_convert.ConvertTest.test_table_in_description_list")
}

// Python: test_tables.ComplexCellTest.test_cell_write_errors_include_position_in_both_render_paths
func TestPythonTestTablesComplexcelltestTestCellWriteErrorsIncludePositionInBothRenderPaths(t *testing.T) {
	pythonRun(t, "test_tables.ComplexCellTest.test_cell_write_errors_include_position_in_both_render_paths")
}

// Python: test_tables.ComplexCellTest.test_link_definitions_are_recorded_only_in_markdown
func TestPythonTestTablesComplexcelltestTestLinkDefinitionsAreRecordedOnlyInMarkdown(t *testing.T) {
	pythonRun(t, "test_tables.ComplexCellTest.test_link_definitions_are_recorded_only_in_markdown")
}

// Python: test_tables.HtmlStructureTest.test_blockquote
func TestPythonTestTablesHtmlstructuretestTestBlockquote(t *testing.T) {
	pythonRun(t, "test_tables.HtmlStructureTest.test_blockquote")
}

// Python: test_tables.HtmlStructureTest.test_end_tag_in_paragraph
func TestPythonTestTablesHtmlstructuretestTestEndTagInParagraph(t *testing.T) {
	pythonRun(t, "test_tables.HtmlStructureTest.test_end_tag_in_paragraph")
}

// Python: test_tables.HtmlStructureTest.test_fence_hides_tags
func TestPythonTestTablesHtmlstructuretestTestFenceHidesTags(t *testing.T) {
	pythonRun(t, "test_tables.HtmlStructureTest.test_fence_hides_tags")
}

// Python: test_tables.HtmlStructureTest.test_implicit_end_tags
func TestPythonTestTablesHtmlstructuretestTestImplicitEndTags(t *testing.T) {
	pythonRun(t, "test_tables.HtmlStructureTest.test_implicit_end_tags")
}

// Python: test_tables.HtmlStructureTest.test_indented_chunk_is_excluded
func TestPythonTestTablesHtmlstructuretestTestIndentedChunkIsExcluded(t *testing.T) {
	pythonRun(t, "test_tables.HtmlStructureTest.test_indented_chunk_is_excluded")
}

// Python: test_tables.HtmlStructureTest.test_markdown_chunk
func TestPythonTestTablesHtmlstructuretestTestMarkdownChunk(t *testing.T) {
	pythonRun(t, "test_tables.HtmlStructureTest.test_markdown_chunk")
}

// Python: test_tables.HtmlStructureTest.test_pre_with_blank_line
func TestPythonTestTablesHtmlstructuretestTestPreWithBlankLine(t *testing.T) {
	pythonRun(t, "test_tables.HtmlStructureTest.test_pre_with_blank_line")
}

// Python: test_tables.HtmlStructureTest.test_spans
func TestPythonTestTablesHtmlstructuretestTestSpans(t *testing.T) {
	pythonRun(t, "test_tables.HtmlStructureTest.test_spans")
}

// Python: test_tables.HtmlStructureTest.test_text_around_table
func TestPythonTestTablesHtmlstructuretestTestTextAroundTable(t *testing.T) {
	pythonRun(t, "test_tables.HtmlStructureTest.test_text_around_table")
}

// Python: test_tables.HtmlStructureTest.test_text_between_rows
func TestPythonTestTablesHtmlstructuretestTestTextBetweenRows(t *testing.T) {
	pythonRun(t, "test_tables.HtmlStructureTest.test_text_between_rows")
}

// Python: test_tables.HtmlStructureTest.test_tricky_tokens
func TestPythonTestTablesHtmlstructuretestTestTrickyTokens(t *testing.T) {
	pythonRun(t, "test_tables.HtmlStructureTest.test_tricky_tokens")
}

// Python: test_tables.TableSelectionTest.test_duplicate_heading_is_ambiguous
func TestPythonTestTablesTableselectiontestTestDuplicateHeadingIsAmbiguous(t *testing.T) {
	pythonRun(t, "test_tables.TableSelectionTest.test_duplicate_heading_is_ambiguous")
}

// Python: test_tables.TableSelectionTest.test_duplicate_heading_without_table_is_not_ambiguous
func TestPythonTestTablesTableselectiontestTestDuplicateHeadingWithoutTableIsNotAmbiguous(t *testing.T) {
	pythonRun(t, "test_tables.TableSelectionTest.test_duplicate_heading_without_table_is_not_ambiguous")
}

// Python: test_tables.TableSelectionTest.test_heading_line_without_heading_and_setext_heading
func TestPythonTestTablesTableselectiontestTestHeadingLineWithoutHeadingAndSetextHeading(t *testing.T) {
	pythonRun(t, "test_tables.TableSelectionTest.test_heading_line_without_heading_and_setext_heading")
}

// Python: test_tables.TableSelectionTest.test_heading_selects_excluded_table_too
func TestPythonTestTablesTableselectiontestTestHeadingSelectsExcludedTableToo(t *testing.T) {
	pythonRun(t, "test_tables.TableSelectionTest.test_heading_selects_excluded_table_too")
}

// Python: test_tables.TableSelectionTest.test_heading_selects_one_or_several_tables
func TestPythonTestTablesTableselectiontestTestHeadingSelectsOneOrSeveralTables(t *testing.T) {
	pythonRun(t, "test_tables.TableSelectionTest.test_heading_selects_one_or_several_tables")
}

// Python: test_tables.TableSelectionTest.test_missing_heading_requires_exact_match
func TestPythonTestTablesTableselectiontestTestMissingHeadingRequiresExactMatch(t *testing.T) {
	pythonRun(t, "test_tables.TableSelectionTest.test_missing_heading_requires_exact_match")
}

// Python: test_tables.TableSelectionTest.test_numeric_and_line_like_headings
func TestPythonTestTablesTableselectiontestTestNumericAndLineLikeHeadings(t *testing.T) {
	pythonRun(t, "test_tables.TableSelectionTest.test_numeric_and_line_like_headings")
}

// Python: test_workbook.ExportImportTest.test_html_alignment_with_added_row
func TestPythonTestWorkbookExportimporttestTestHtmlAlignmentWithAddedRow(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_html_alignment_with_added_row")
}

// Python: test_workbook.ExportImportTest.test_html_header_with_added_column
func TestPythonTestWorkbookExportimporttestTestHtmlHeaderWithAddedColumn(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_html_header_with_added_column")
}

// Python: test_workbook.ExportImportTest.test_many_tables_with_body
func TestPythonTestWorkbookExportimporttestTestManyTablesWithBody(t *testing.T) {
	pythonRun(t, "test_workbook.ExportImportTest.test_many_tables_with_body")
}
