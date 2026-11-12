package blocks

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
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

type comrakEnvironment struct {
	dir, node, reason string
	required          bool
}

func findComrak() comrakEnvironment {
	result := comrakEnvironment{
		dir:      os.Getenv("MDTABLE_COMRAK_DIR"),
		required: os.Getenv("MDTABLE_COMRAK_REQUIRED") != "",
	}
	if result.dir == "" {
		result.dir = "../../testdata/comrak"
	}
	var e error
	result.dir, e = filepath.Abs(result.dir)
	if e != nil {
		result.reason = fmt.Sprintf("comrak の場所を絶対パスに直せない: %v（mdtable/testdata/comrak で npm ci --ignore-scripts を実行し、MDTABLE_COMRAK_DIR を確認する）", e)
		return result
	}
	packageJSON := filepath.Join(result.dir, "node_modules", "comrak", "package.json")
	if info, e := os.Stat(packageJSON); e != nil || !info.Mode().IsRegular() {
		result.reason = fmt.Sprintf("comrak の package.json が見つからない: %s（mdtable/testdata/comrak で npm ci --ignore-scripts を実行する）", packageJSON)
		return result
	}
	result.node, e = exec.LookPath("node")
	if e != nil {
		result.reason = "PATH に node が見つからない（Node.js を導入し、mdtable/testdata/comrak で npm ci --ignore-scripts を実行する）"
	}
	return result
}

func TestComrakEnvironment(t *testing.T) {
	for _, tc := range []struct {
		name, dir, required, reason string
		packageJSON, node           bool
		wantSkip, wantFatal         bool
	}{
		{name: "既定の場所", packageJSON: true, node: true},
		{name: "絶対パスで指定した場所", dir: "絶対", packageJSON: true, node: true},
		{name: "相対パスで指定した場所", dir: "相対", packageJSON: true, node: true},
		{name: "準備済みなら必須でも動く", required: "1", packageJSON: true, node: true},
		{name: "comrak が無ければ省略", node: true, reason: "package.json が見つからない", wantSkip: true},
		{name: "必須で comrak が無ければ失敗", required: "有効", node: true, reason: "package.json が見つからない", wantFatal: true},
		{name: "node が無ければ省略", packageJSON: true, reason: "PATH に node が見つからない", wantSkip: true},
		{name: "必須で node が無ければ失敗", required: "1", packageJSON: true, reason: "PATH に node が見つからない", wantFatal: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			work := filepath.Join(root, "internal", "blocks")
			if e := os.MkdirAll(work, 0o755); e != nil {
				t.Fatal(e)
			}
			t.Chdir(work)
			dir := ""
			wantDir := filepath.Join(root, "testdata", "comrak")
			if tc.dir != "" {
				wantDir = filepath.Join(root, "指定した場所")
				dir = wantDir
				if tc.dir == "相対" {
					dir = filepath.Join("..", "..", "指定した場所")
				}
			}
			t.Setenv("MDTABLE_COMRAK_DIR", dir)
			t.Setenv("MDTABLE_COMRAK_REQUIRED", tc.required)
			if tc.packageJSON {
				packageDir := filepath.Join(wantDir, "node_modules", "comrak")
				if e := os.MkdirAll(packageDir, 0o755); e != nil {
					t.Fatal(e)
				}
				if e := os.WriteFile(filepath.Join(packageDir, "package.json"), []byte("{}\n"), 0o644); e != nil {
					t.Fatal(e)
				}
			}
			bin := filepath.Join(root, "bin")
			if e := os.MkdirAll(bin, 0o755); e != nil {
				t.Fatal(e)
			}
			if tc.node {
				name := "node"
				if runtime.GOOS == "windows" {
					name += ".exe"
				}
				if e := os.WriteFile(filepath.Join(bin, name), nil, 0o755); e != nil {
					t.Fatal(e)
				}
			}
			t.Setenv("PATH", bin)
			t.Setenv("PATHEXT", ".EXE")
			got := findComrak()
			if got.dir != wantDir || !filepath.IsAbs(got.dir) {
				t.Fatalf("comrak の場所: 結果 %q、期待 %q", got.dir, wantDir)
			}
			if tc.reason == "" {
				if got.reason != "" || got.node == "" {
					t.Fatalf("準備済みの comrak が使えない: %+v", got)
				}
			} else if !strings.Contains(got.reason, tc.reason) || !strings.Contains(got.reason, "mdtable/testdata/comrak で npm ci --ignore-scripts") || strings.ContainsAny(got.reason, "\r\n") {
				t.Fatalf("comrak を動かせない理由と準備の案内が違う: %q", got.reason)
			}
			if skip, fatal := got.reason != "" && !got.required, got.reason != "" && got.required; skip != tc.wantSkip || fatal != tc.wantFatal {
				t.Fatalf("準備が無いときの判断: 省略 %t、失敗 %t（期待: 省略 %t、失敗 %t）", skip, fatal, tc.wantSkip, tc.wantFatal)
			}
		})
	}
}

func checkComrak(t *testing.T, texts, names []string) {
	t.Helper()
	comrak := findComrak()
	if comrak.reason != "" {
		if comrak.required {
			t.Fatalf("%s", comrak.reason)
		}
		t.Skip(comrak.reason)
	}
	input, e := json.Marshal(texts)
	if e != nil {
		t.Fatal(e)
	}
	command := exec.Command(comrak.node, "../../testdata/comrak_ast.mjs")
	command.Env = append(os.Environ(), "MDTABLE_COMRAK_DIR="+comrak.dir)
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
