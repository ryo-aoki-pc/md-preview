// Python の単体テストと同じ入力を保存したデータで検査する。
package cellfromhtml

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"github.com/ryo-aoki-pc/md-table-excel/internal/htmltok"
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
	f, err := os.Open("../../testdata/unit/cellfromhtml.json.gz")
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
	tree := htmltok.BuildTree(htmltok.Tokenize(s, 0, len(s)))
	return Convert(tree.Children, s), nil
}

// Python: test_cells.HtmlToExcelTest.test_code
func TestPythonTestCellsHtmltoexceltestTestCode(t *testing.T) {
	pythonRun(t, "test_cells.HtmlToExcelTest.test_code")
}

// Python: test_cells.HtmlToExcelTest.test_description_lists
func TestPythonTestCellsHtmltoexceltestTestDescriptionLists(t *testing.T) {
	pythonRun(t, "test_cells.HtmlToExcelTest.test_description_lists")
}

// Python: test_cells.HtmlToExcelTest.test_emphasis
func TestPythonTestCellsHtmltoexceltestTestEmphasis(t *testing.T) {
	pythonRun(t, "test_cells.HtmlToExcelTest.test_emphasis")
}

// Python: test_cells.HtmlToExcelTest.test_escapes
func TestPythonTestCellsHtmltoexceltestTestEscapes(t *testing.T) {
	pythonRun(t, "test_cells.HtmlToExcelTest.test_escapes")
}

// Python: test_cells.HtmlToExcelTest.test_links
func TestPythonTestCellsHtmltoexceltestTestLinks(t *testing.T) {
	pythonRun(t, "test_cells.HtmlToExcelTest.test_links")
}

// Python: test_cells.HtmlToExcelTest.test_lists
func TestPythonTestCellsHtmltoexceltestTestLists(t *testing.T) {
	pythonRun(t, "test_cells.HtmlToExcelTest.test_lists")
}

// Python: test_cells.HtmlToExcelTest.test_other_blocks
func TestPythonTestCellsHtmltoexceltestTestOtherBlocks(t *testing.T) {
	pythonRun(t, "test_cells.HtmlToExcelTest.test_other_blocks")
}

// Python: test_cells.HtmlToExcelTest.test_pre
func TestPythonTestCellsHtmltoexceltestTestPre(t *testing.T) {
	pythonRun(t, "test_cells.HtmlToExcelTest.test_pre")
}

// Python: test_cells.HtmlToExcelTest.test_raw_link_after_backslash
func TestPythonTestCellsHtmltoexceltestTestRawLinkAfterBackslash(t *testing.T) {
	pythonRun(t, "test_cells.HtmlToExcelTest.test_raw_link_after_backslash")
}

// Python: test_cells.HtmlToExcelTest.test_raw_links_and_images
func TestPythonTestCellsHtmltoexceltestTestRawLinksAndImages(t *testing.T) {
	pythonRun(t, "test_cells.HtmlToExcelTest.test_raw_links_and_images")
}

// Python: test_cells.HtmlToExcelTest.test_raw_links_decode_entities
func TestPythonTestCellsHtmltoexceltestTestRawLinksDecodeEntities(t *testing.T) {
	pythonRun(t, "test_cells.HtmlToExcelTest.test_raw_links_decode_entities")
}

// Python: test_cells.HtmlToExcelTest.test_raw_links_do_not_cross_elements
func TestPythonTestCellsHtmltoexceltestTestRawLinksDoNotCrossElements(t *testing.T) {
	pythonRun(t, "test_cells.HtmlToExcelTest.test_raw_links_do_not_cross_elements")
}

// Python: test_cells.HtmlToExcelTest.test_raw_links_do_not_cross_newlines
func TestPythonTestCellsHtmltoexceltestTestRawLinksDoNotCrossNewlines(t *testing.T) {
	pythonRun(t, "test_cells.HtmlToExcelTest.test_raw_links_do_not_cross_newlines")
}

// Python: test_cells.HtmlToExcelTest.test_raw_links_escape_surrounding_text
func TestPythonTestCellsHtmltoexceltestTestRawLinksEscapeSurroundingText(t *testing.T) {
	pythonRun(t, "test_cells.HtmlToExcelTest.test_raw_links_escape_surrounding_text")
}

// Python: test_cells.HtmlToExcelTest.test_raw_links_in_excluded_elements
func TestPythonTestCellsHtmltoexceltestTestRawLinksInExcludedElements(t *testing.T) {
	pythonRun(t, "test_cells.HtmlToExcelTest.test_raw_links_in_excluded_elements")
}

// Python: test_cells.HtmlToExcelTest.test_raw_links_in_flow_elements
func TestPythonTestCellsHtmltoexceltestTestRawLinksInFlowElements(t *testing.T) {
	pythonRun(t, "test_cells.HtmlToExcelTest.test_raw_links_in_flow_elements")
}

// Python: test_cells.HtmlToExcelTest.test_raw_links_keep_inner_markdown_and_destination_spaces
func TestPythonTestCellsHtmltoexceltestTestRawLinksKeepInnerMarkdownAndDestinationSpaces(t *testing.T) {
	pythonRun(t, "test_cells.HtmlToExcelTest.test_raw_links_keep_inner_markdown_and_destination_spaces")
}

// Python: test_cells.HtmlToExcelTest.test_raw_links_keep_whitespace_rules
func TestPythonTestCellsHtmltoexceltestTestRawLinksKeepWhitespaceRules(t *testing.T) {
	pythonRun(t, "test_cells.HtmlToExcelTest.test_raw_links_keep_whitespace_rules")
}

// Python: test_cells.HtmlToExcelTest.test_raw_reference_links_are_escaped
func TestPythonTestCellsHtmltoexceltestTestRawReferenceLinksAreEscaped(t *testing.T) {
	pythonRun(t, "test_cells.HtmlToExcelTest.test_raw_reference_links_are_escaped")
}

// Python: test_cells.HtmlToExcelTest.test_tilde_and_dollar
func TestPythonTestCellsHtmltoexceltestTestTildeAndDollar(t *testing.T) {
	pythonRun(t, "test_cells.HtmlToExcelTest.test_tilde_and_dollar")
}

// Python: test_cells.HtmlToExcelTest.test_whitespace_and_entities
func TestPythonTestCellsHtmltoexceltestTestWhitespaceAndEntities(t *testing.T) {
	pythonRun(t, "test_cells.HtmlToExcelTest.test_whitespace_and_entities")
}

// Python: test_cells.ListSpacingTest.test_html_elements_use_same_spacing
func TestPythonTestCellsListspacingtestTestHtmlElementsUseSameSpacing(t *testing.T) {
	pythonRun(t, "test_cells.ListSpacingTest.test_html_elements_use_same_spacing")
}
