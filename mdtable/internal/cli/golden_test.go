package cli

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	tablesync "github.com/ryo-aoki-pc/md-table-excel/internal/sync"
)

type goldenResult struct {
	Code            int
	Out, Err, After string
}
type goldenCase struct {
	Args []string
	Want goldenResult
}
type goldenImport struct {
	Kind, Book, Before string
	Args               []string
	Want               goldenResult
}
type goldenData struct {
	Documents []struct {
		Source string
		Cases  []goldenCase
	}
	Imports []goldenImport
	Exports []struct {
		Source string
		Want   json.RawMessage
	}
}

// 正解文字列の共有は保存量を減らすためだけで、実行前に元の文字列に戻す。
// 読み込んだデータは書き換えず、並行するテストで共有する。
var savedGoldens = sync.OnceValues(func() (goldenData, error) {
	var result goldenData
	f, e := os.Open(filepath.Join(pythonTestdataDir, "goldens.json.gz"))
	if e != nil {
		return result, e
	}
	defer f.Close()
	gz, e := gzip.NewReader(f)
	if e != nil {
		return result, e
	}
	defer gz.Close()
	var saved struct {
		Strings []string
		Value   any
	}
	if e = json.NewDecoder(gz).Decode(&saved); e != nil {
		return result, e
	}
	var expand func(any) any
	expand = func(v any) any {
		switch x := v.(type) {
		case map[string]any:
			if index, ok := x["$text"]; ok {
				return saved.Strings[int(index.(float64))]
			}
			for k, item := range x {
				x[k] = expand(item)
			}
		case []any:
			for i, item := range x {
				x[i] = expand(item)
			}
		}
		return v
	}
	b, e := json.Marshal(pythonNormalize(expand(saved.Value), "<保存先>"))
	if e != nil {
		return result, e
	}
	e = json.Unmarshal(b, &result)
	return result, e
})

func readGoldens(t *testing.T) goldenData {
	t.Helper()
	data, e := savedGoldens()
	if e != nil {
		t.Fatal(e)
	}
	return data
}

type goldenRequest struct {
	Kind, Source string
	Offset       int
	Cases        []goldenCase
	Imports      []goldenImport
	Want         json.RawMessage
}

// CLI の標準出力・作業フォルダー・環境変数は子プロセスごとに分離する。
// プロセスをコマンドごとに起動せず、文書やブックごとにまとめて実行する。
var goldenSlots = make(chan struct{}, min(8, runtime.GOMAXPROCS(0)))

func runGoldenWorker(t *testing.T, requests ...goldenRequest) {
	t.Helper()
	goldenSlots <- struct{}{}
	defer func() { <-goldenSlots }()
	input, e := json.Marshal(requests)
	if e != nil {
		t.Fatal(e)
	}
	// 子の時間の制限は、親のテストの残り時間にする（-race では子が遅くなるので、決め打ちにしない）
	timeout := "-test.timeout=0"
	if deadline, ok := t.Deadline(); ok {
		timeout = "-test.timeout=" + time.Until(deadline).Round(time.Second).String()
	}
	command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestPythonGoldenWorker$", timeout)
	command.Dir = filepath.Join(pythonTestdataDir, "..", "internal", "cli")
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		switch strings.ToUpper(key) {
		case "MDTABLE_GOLDEN_WORKER", "MDTABLE_FONT", "MDTABLE_DEBUG", "NO_COLOR":
			continue
		}
		command.Env = append(command.Env, entry)
	}
	command.Env = append(command.Env, "MDTABLE_GOLDEN_WORKER=1", "MDTABLE_FONT=BIZ UDゴシック", "NO_COLOR=1")
	command.Stdin = bytes.NewReader(input)
	output, e := command.CombinedOutput()
	if e != nil {
		t.Fatalf("分離したゴールデンの検査が失敗した: %v\n%s", e, output)
	}
}

// 子プロセスの中では直列に動かし、出力の一時ファイルをコマンド間で再利用する。
func goldenCommands(t *testing.T, directory string) func([]string) (int, string, string) {
	t.Helper()
	out, e := os.CreateTemp(directory, "stdout-")
	if e != nil {
		t.Fatal(e)
	}
	errs, e := os.CreateTemp(directory, "stderr-")
	if e != nil {
		out.Close()
		t.Fatal(e)
	}
	previousOut, previousErr := stdout, stderr
	stdout, stderr = textStream{out}, textStream{errs}
	t.Cleanup(func() { stdout, stderr = previousOut, previousErr; out.Close(); errs.Close() })
	return func(argv []string) (int, string, string) {
		for _, f := range []*os.File{out, errs} {
			if e := f.Truncate(0); e != nil {
				t.Fatal(e)
			}
			if _, e := f.Seek(0, io.SeekStart); e != nil {
				t.Fatal(e)
			}
		}
		code := mainContext(context.Background(), argv, func(string) error { return fmt.Errorf("この検査では Excel を起動しない") })
		a, e := os.ReadFile(out.Name())
		if e != nil {
			t.Fatal(e)
		}
		b, e := os.ReadFile(errs.Name())
		if e != nil {
			t.Fatal(e)
		}
		if runtime.GOOS == "windows" {
			a = bytes.ReplaceAll(a, []byte("\r\n"), []byte("\n"))
			b = bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n"))
		}
		return code, string(a), string(b)
	}
}

func TestPythonGoldenWorker(t *testing.T) {
	if os.Getenv("MDTABLE_GOLDEN_WORKER") != "1" {
		t.Skip("ゴールデン専用の子プロセスから実行する")
	}
	var requests []goldenRequest
	if e := json.NewDecoder(os.Stdin).Decode(&requests); e != nil {
		t.Fatal(e)
	}
	for _, request := range requests {
		t.Run(request.Kind+"_"+request.Source, func(t *testing.T) {
			checkGoldenRequest(t, request)
		})
	}
}

func checkGoldenRequest(t *testing.T, request goldenRequest) {
	directory := t.TempDir()
	// このプロセスには並行テストがないので、CLI に渡す相対パスも検査できる。
	t.Chdir(directory)
	command := goldenCommands(t, directory)
	check := func(t *testing.T, args []string, want goldenResult) {
		t.Helper()
		code, out, errs := command(args)
		got := pythonNormalize(goldenResult{code, out, errs, pythonReadHex(t, filepath.Join(directory, "document.md"))}, directory)
		expected, e := json.Marshal(want)
		if e != nil {
			t.Fatal(e)
		}
		pythonCheck(t, got, expected)
	}
	switch request.Kind {
	case "document":
		before, e := os.ReadFile(filepath.Join(pythonTestdataDir, filepath.FromSlash(request.Source)))
		if e != nil {
			t.Fatal(e)
		}
		for index, c := range request.Cases {
			t.Run(fmt.Sprintf("%04d_%s", request.Offset+index, strings.Join(c.Args, "_")), func(t *testing.T) {
				if e := os.WriteFile("document.md", before, 0600); e != nil {
					t.Fatal(e)
				}
				check(t, c.Args, c.Want)
			})
		}
	case "import":
		pythonRestoreBook(t, request.Imports[0].Book, "book.xlsx", directory)
		for index, c := range request.Imports {
			t.Run(fmt.Sprintf("%02d_%s", index, strings.Join(c.Args[4:], "_")), func(t *testing.T) {
				pythonWriteHex(t, "document.md", c.Before)
				check(t, c.Args, c.Want)
			})
		}
	case "export":
		before, e := os.ReadFile(filepath.Join(pythonTestdataDir, filepath.FromSlash(request.Source)))
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile("document.md", before, 0600); e != nil {
			t.Fatal(e)
		}
		report, e := tablesync.Export("document.md", "book.xlsx", nil, false)
		if e != nil {
			t.Fatal(e)
		}
		if len(report.Errors) != 0 {
			t.Fatal(report.Errors)
		}
		pythonCheck(t, pythonInspectBook(t, "book.xlsx", directory), request.Want)
	default:
		t.Fatalf("未知のゴールデン: %s", request.Kind)
	}
}

// 大きな文書も分割した後、複数の組を一つのプロセスにまとめて起動回数を抑える。
func runGoldenBatches(t *testing.T, requests []goldenRequest) {
	t.Helper()
	batches := make([][]goldenRequest, min(cap(goldenSlots), len(requests)))
	for index, request := range requests {
		batches[index%len(batches)] = append(batches[index%len(batches)], request)
	}
	for index, batch := range batches {
		t.Run(fmt.Sprintf("組%d", index+1), func(t *testing.T) {
			t.Parallel()
			runGoldenWorker(t, batch...)
		})
	}
}

// scripts/compare-go.py の cases_for と同じ全組を Python の結果と比べる。
func TestPythonDocumentGoldens(t *testing.T) {
	t.Parallel()
	data := readGoldens(t)
	if len(data.Documents) != 153 {
		t.Fatalf("文書の数が違う: %d", len(data.Documents))
	}
	requests := []goldenRequest{}
	for _, doc := range data.Documents {
		for start := 0; start < len(doc.Cases); start += 40 {
			end := min(start+40, len(doc.Cases))
			requests = append(requests, goldenRequest{Kind: "document", Source: doc.Source, Offset: start, Cases: doc.Cases[start:end]})
		}
	}
	runGoldenBatches(t, requests)
}

// scripts/compare-go-workbook.py の編集済みブックと、全保護オプションを保存する。
func TestPythonImportGoldens(t *testing.T) {
	t.Parallel()
	groups := map[string][]goldenImport{}
	for _, c := range readGoldens(t).Imports {
		groups[c.Kind] = append(groups[c.Kind], c)
	}
	requests := []goldenRequest{}
	for kind, cases := range groups {
		requests = append(requests, goldenRequest{Kind: "import", Source: kind, Imports: cases})
	}
	runGoldenBatches(t, requests)
}

// export は ZIP のバイト列ではなく、openpyxl で読んだ Python ブックの性質と比べる。
func TestPythonExportProperties(t *testing.T) {
	t.Parallel()
	requests := []goldenRequest{}
	for _, c := range readGoldens(t).Exports {
		requests = append(requests, goldenRequest{Kind: "export", Source: c.Source, Want: c.Want})
	}
	runGoldenBatches(t, requests)
}

// cli.main の --version も、保存時の版ではなく実行する版を使って検査する。
func TestPythonVersionGolden(t *testing.T) {
	t.Parallel()
	source := "documents/fuzz/fuzz-0000.md"
	after := pythonReadHex(t, filepath.Join(pythonTestdataDir, filepath.FromSlash(source)))
	runGoldenWorker(t, goldenRequest{Kind: "document", Source: source, Cases: []goldenCase{{
		Args: []string{"--version"},
		Want: goldenResult{Code: 0, Out: "mdtable <VERSION>\n", After: after},
	}}})
}
