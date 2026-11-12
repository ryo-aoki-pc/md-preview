package blocks

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

var oracleTypes = map[string]string{
	"Paragraph": "paragraph", "Heading": "heading", "CodeBlock": "code_block", "HtmlBlock": "html_block", "ThematicBreak": "thematic_break", "Table": "table",
	"BlockQuote": "block_quote", "Alert": "alert", "MultilineBlockQuote": "multiline_block_quote", "List": "list", "Item": "item", "TaskItem": "item",
	"DescriptionList": "description_list", "DescriptionItem": "description_item", "DescriptionTerm": "description_term", "DescriptionDetails": "description_details", "FootnoteDefinition": "footnote_definition",
}
var oracleLeaves = map[string]bool{"paragraph": true, "heading": true, "html_block": true, "thematic_break": true, "table": true}

type oracleNode struct {
	Type       string
	Parent     *int
	Start, End int
}

func oracleFiles(t *testing.T) []string {
	t.Helper()
	root := os.Getenv("MDTABLE_CORPUS")
	if root == "" {
		t.Skip("MDTABLE_CORPUS が要る")
	}
	files := []string{}
	e := filepath.WalkDir(root, func(path string, entry os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", ".claude", "node_modules":
				return filepath.SkipDir
			}
		} else if strings.HasSuffix(entry.Name(), ".md") {
			files = append(files, path)
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	sort.Strings(files)
	return files
}

func oracleSignature(text string) map[string]int {
	result := map[string]int{}
	add := func(v any) { b, _ := json.Marshal(v); result[string(b)]++ }
	for _, n := range ParseText(text, false, false).Root.Walk() {
		if !oracleLeaves[n.Kind] {
			continue
		}
		chain := []string{}
		for p := n.Parent; p != nil && p.Kind != DocumentKind; p = p.Parent {
			chain = append(chain, p.Kind)
		}
		add([]any{n.Kind, n.StartLine, n.EndLine, chain})
		if n.Kind == Table {
			add([]any{"row", n.Info.Header.Number})
			for _, r := range n.Info.Rows {
				add([]any{"row", r.Number})
			}
		}
	}
	return result
}

func checkComrak(t *testing.T, texts, names []string) {
	t.Helper()
	if os.Getenv("MDTABLE_COMRAK_DIR") == "" {
		t.Skip("MDTABLE_COMRAK_DIR と node が要る")
	}
	node, e := exec.LookPath("node")
	if e != nil {
		t.Skip("MDTABLE_COMRAK_DIR と node が要る")
	}
	input, e := json.Marshal(texts)
	if e != nil {
		t.Fatal(e)
	}
	command := exec.Command(node, "../../testdata/comrak_ast.mjs")
	command.Stdin = bytes.NewReader(input)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, e := command.Output()
	if e != nil {
		t.Fatalf("comrak を実行できない: %v %s", e, stderr.String())
	}
	var all [][]oracleNode
	if e = json.Unmarshal(output, &all); e != nil {
		t.Fatal(e)
	}
	if len(all) != len(texts) {
		t.Fatalf("comrak の文書数: %d", len(all))
	}
	for i, nodes := range all {
		t.Run(names[i], func(t *testing.T) {
			expected := map[string]int{}
			add := func(v any) { b, _ := json.Marshal(v); expected[string(b)]++ }
			for _, n := range nodes {
				if n.Type == "TableRow" {
					add([]any{"row", n.Start - 1})
				}
				kind := oracleTypes[n.Type]
				if !oracleLeaves[kind] {
					continue
				}
				chain := []string{}
				for parent := n.Parent; parent != nil; parent = nodes[*parent].Parent {
					if k := oracleTypes[nodes[*parent].Type]; k != "" {
						chain = append(chain, k)
					}
				}
				end := n.End
				if kind == "thematic_break" {
					end = n.Start
				}
				add([]any{kind, n.Start - 1, end - 1, chain})
			}
			if got := oracleSignature(texts[i]); !reflect.DeepEqual(got, expected) {
				t.Fatalf("comrak と構造が違う\n結果: %v\n期待: %v", got, expected)
			}
		})
	}
}

// Python: test_oracles.ComrakTest.test_samples
func TestPythonComrakSamples(t *testing.T) {
	data, e := os.ReadFile("../../testdata/oracle_samples.json")
	if e != nil {
		t.Fatal(e)
	}
	var samples []string
	if e = json.Unmarshal(data, &samples); e != nil {
		t.Fatal(e)
	}
	names := []string{}
	for i := range samples {
		names = append(names, fmt.Sprintf("見本%d", i+1))
	}
	checkComrak(t, samples, names)
}

// Python: test_oracles.ComrakTest.test_corpus
func TestPythonComrakCorpus(t *testing.T) {
	files := oracleFiles(t)
	for start := 0; start < len(files); start += 20 {
		end := min(start+20, len(files))
		texts := []string{}
		for _, path := range files[start:end] {
			data, e := os.ReadFile(path)
			if e != nil {
				t.Fatal(e)
			}
			texts = append(texts, strings.TrimPrefix(string(data), "\ufeff"))
		}
		checkComrak(t, texts, files[start:end])
	}
}
