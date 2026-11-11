"""保存した単体テストの入力に対応する Go の呼び出し口を作る（Python 版は不要）。"""
import gzip
import json
import pathlib
import re
import subprocess

ROOT = pathlib.Path(__file__).resolve().parents[1]
COMMON = r'''
type pythonCase struct {
 Test, Op string
 Input, Want json.RawMessage
 Error []any
}

func pythonDecode[T any](t *testing.T, data []byte) T {
 t.Helper()
 var value T
 if err := json.Unmarshal(data, &value); err != nil { t.Fatal(err) }
 return value
}

func pythonCanonical(v any) any {
 switch x := v.(type) {
 case nil: return []any{}
 case []any:
  for i := range x { x[i] = pythonCanonical(x[i]) }; return x
 case map[string]any:
  for k := range x { x[k] = pythonCanonical(x[k]) }; return x
 }
 return v
}

func pythonCheck(t *testing.T, got any, want json.RawMessage) {
 t.Helper()
 data, err := json.Marshal(got); if err != nil { t.Fatal(err) }
 a := pythonCanonical(pythonDecode[any](t, data))
 b := pythonCanonical(pythonDecode[any](t, want))
 if !reflect.DeepEqual(a,b) { t.Fatal(pythonDifference(a,b,"結果")) }
}

func pythonDifference(a,b any,path string) string {
 if reflect.DeepEqual(a,b) {return ""}
 if x,ok:=a.(map[string]any);ok {if y,ok:=b.(map[string]any);ok {keys:=[]string{};for k:=range x {keys=append(keys,k)};for k:=range y {if _,ok:=x[k];!ok {keys=append(keys,k)}};for _,k:=range keys {if d:=pythonDifference(x[k],y[k],path+"."+k);d!="" {return d}}}}
 if x,ok:=a.([]any);ok {if y,ok:=b.([]any);ok && len(x)==len(y) {for i:=range x {if d:=pythonDifference(x[i],y[i],fmt.Sprintf("%s[%d]",path,i));d!="" {return d}}}}
 return fmt.Sprintf("%s が Python の期待値と違う: 結果 %.1000v、期待 %.1000v",path,a,b)
}

func pythonRun(t *testing.T, name string) {
 t.Helper()
 t.Setenv("MDTABLE_FONT","BIZ UDゴシック")
 f, err := os.Open("../../testdata/unit/PACKAGE.json.gz"); if err != nil { t.Fatal(err) }; defer f.Close()
 gz, err := gzip.NewReader(f); if err != nil { t.Fatal(err) }; defer gz.Close()
 var cases []pythonCase
 if err := json.NewDecoder(gz).Decode(&cases); err != nil { t.Fatal(err) }
 count := 0
 for _, c := range cases {
  if c.Test != name { continue }; count++
  t.Run(fmt.Sprintf("%s_%d", c.Op, count), func(t *testing.T) {
   got, err := pythonEvaluate(t,c)
   if len(c.Error)>0 {
    if err==nil { t.Fatalf("エラーを返さなかった: %s", c.Error[0]) }
    row,col := 0,0
    v := reflect.ValueOf(err)
    if v.Kind()==reflect.Pointer { v=v.Elem(); if v.Kind()==reflect.Struct {
     if r:=v.FieldByName("Row"); r.IsValid() { row=int(r.Int()) }
     if r:=v.FieldByName("Col"); r.IsValid() { col=int(r.Int()) }
    } }
    data,_:=json.Marshal(c.Error); pythonCheck(t,[]any{err.Error(),row,col},data)
   } else {
    if err!=nil { t.Fatal(err) }; pythonCheck(t,got,c.Want)
   }
  })
 }
 if count==0 { t.Fatal("元のテストの入力がない") }
}
'''
MODEL = r'''
type pythonModel struct { Kind string; Rows, Cols int; Cells [][]any; Holes [][]int }
func (m pythonModel) model() *model.TableModel {
 cells:=[]*model.Cell{}
 for _,v:=range m.Cells {
  align,_:=v[5].(string)
  cells=append(cells,&model.Cell{Row:int(v[0].(float64)),Col:int(v[1].(float64)),Rowspan:int(v[2].(float64)),Colspan:int(v[3].(float64)),Header:v[4].(bool),Align:align,Text:v[6].(string)})
 }
 holes:=map[model.Position]bool{}; for _,p:=range m.Holes {holes[model.Position{Row:p[0],Col:p[1]}]=true}
 return model.New(m.Kind,m.Rows,m.Cols,cells,holes)
}
func pythonModelValue(m *model.TableModel) any {
 if m==nil {return nil}; cells:=[]any{}; for _,c:=range m.Cells {cells=append(cells,c.Key())}
 holes:=[][2]int{}; for p:=range m.Holes {holes=append(holes,[2]int{p.Row,p.Col})}
 sort.Slice(holes,func(i,j int)bool {return holes[i][0]<holes[j][0] || holes[i][0]==holes[j][0] && holes[i][1]<holes[j][1]})
 return map[string]any{"kind":m.Kind,"rows":m.Rows,"cols":m.Cols,"cells":cells,"holes":holes}
}
'''
REGIONS = r'''
func pythonRegionsValue(regions []*table.Region) any {
 result:=[]any{}
 for _,r:=range regions {
  var m *model.TableModel
  if r.Pipe!=nil || r.HTML!=nil {m=r.Model()}
  links:=[]bool{}; if r.HTML!=nil {for _,c:=range r.HTML.Cells {links=append(links,c.LinkDefinitions)}}
  result=append(result,map[string]any{"number":r.Number,"kind":r.Kind,"start":r.Start,"end":r.End,
   "prefixes":r.Prefixes,"bodies":r.Bodies,"cont_prefix":r.ContPrefix,"head":r.Head,"tail":r.Tail,
   "location":r.Location,"heading":r.Heading,"heading_line":r.HeadingLine,"warnings":r.Warnings,
   "excluded":r.Excluded,"model":pythonModelValue(m),"link_definitions":links})
 }
 return result
}
'''
DISPATCH = {
"blocks": ([], r'''
func pythonEvaluate(t *testing.T,c pythonCase)(any,error) {
 switch c.Op {
 case "lines": a,b:=SplitLines(pythonDecode[string](t,c.Input));return []any{a,b},nil
 case "unescape":return UnescapePipes(pythonDecode[string](t,c.Input)),nil
 case "parse":
  a:=pythonDecode[[]any](t,c.Input); d:=ParseText(a[0].(string),a[1].(bool),a[2].(bool)); result:=[]any{}
  for _,n:=range d.Root.Walk() { chain:=[]string{};for p:=n.Parent;p!=nil;p=p.Parent {if p.Kind!=DocumentKind {chain=append(chain,p.Kind)}};result=append(result,[]any{n.Kind,n.StartLine,n.EndLine,chain}) };return result,nil
 };t.Fatalf("未知の操作: %s",c.Op);return nil,nil
}

'''),
"cellsyntax": ([],r'''
func pythonEvaluate(t *testing.T,c pythonCase)(any,error) {
 s:=pythonDecode[string](t,c.Input)
 switch c.Op {
 case "spacing":return NormalizeListSpacing(s),nil
 case "pipe":return ToPipeCell(s)
 case "chunk":return ChunkToExcel(s),nil
 case "html":v,e:=ToHTMLCell(s);var payload any=v.Text;if v.Kind=="chunk" {payload=v.Lines};return []any{v.Kind,payload,v.Warnings},e
 };t.Fatalf("未知の操作: %s",c.Op);return nil,nil
}
'''),
"cellfromhtml": (["github.com/ryo-aoki-pc/md-table-excel/internal/htmltok"],r'''
func pythonEvaluate(t *testing.T,c pythonCase)(any,error) {
 s:=pythonDecode[string](t,c.Input);tree:=htmltok.BuildTree(htmltok.Tokenize(s,0,len(s)));return Convert(tree.Children,s),nil
}
'''),
"model": (["sort"],r'''
func pythonEvaluate(t *testing.T,c pythonCase)(any,error) {
 if c.Op=="normalize" {return NormalizeText(pythonDecode[string](t,c.Input)),nil}
 m:=pythonDecode[pythonModel](t,c.Input).model(); before:=pythonModelValue(m); sheet:=SheetForm(m)
 result:=pythonModelValue(sheet);if len(sheet.Cells)>0 {sheet.Cells[0].Text="コピーの確認"};if !reflect.DeepEqual(pythonModelValue(m),before) {t.Fatal("元のセルや穴の位置が変わった")};return result,nil
}
'''),
"changes": (["sort","time","github.com/ryo-aoki-pc/md-table-excel/internal/model"],r'''
func pythonEvaluate(t *testing.T,c pythonCase)(any,error) {
 a:=pythonDecode[[]pythonModel](t,c.Input);started:=time.Now();v:=CompareModels(a[0].model(),a[1].model());cells:=[]any{}
 if c.Test=="test_changes.ChangesTest.test_large_table_with_merges_is_fast" && time.Since(started)>5*time.Second {t.Fatal("結合のある大きな表の突き合わせが 5 秒を超えた")}
 for _,x:=range v.Cells {cells=append(cells,[]any{x.Row,x.Col,x.OldText,x.NewText,x.Kind})}
 return map[string]any{"summary":v.Summary(),"details":v.Details(20),"full":v.Details(28),"cells":cells},nil
}
'''),
"table": (["sort","github.com/ryo-aoki-pc/md-table-excel/internal/model"],r'''
func pythonEvaluate(t *testing.T,c pythonCase)(any,error) {
 switch c.Op {
 case "regions","find":return pythonRegionsValue(FindTables(NewMarkdown("<test>",pythonDecode[string](t,c.Input),false))),nil
 case "cell":return PipeCellToExcel(pythonDecode[string](t,c.Input)),nil
 case "select":
  a:=pythonDecode[[]json.RawMessage](t,c.Input);found:=FindTables(NewMarkdown("<test>",pythonDecode[string](t,a[0]),false));selected,e:=SelectTables(found,pythonDecode[[]string](t,a[1]));result:=[]int{};for _,r:=range selected {result=append(result,r.Number)};return result,e
 };t.Fatalf("未知の操作: %s",c.Op);return nil,nil
}
'''),
}

BOOK_IMPORTS = ["archive/zip", "bytes", "encoding/hex", "encoding/xml", "io", "path/filepath", "sort", "strconv", "strings", "sync", "github.com/xuri/excelize/v2", "github.com/ryo-aoki-pc/md-table-excel/internal/version"]
BOOK_HELPERS = (ROOT / "testdata" / "book_helpers.txt").read_text(encoding="utf-8")
DISPATCH["workbook"] = (BOOK_IMPORTS + ["github.com/ryo-aoki-pc/md-table-excel/internal/model"], r'''
func pythonEvaluate(t *testing.T,c pythonCase)(any,error) {
 switch c.Op {
 case "check":s:=CheckCellText(pythonDecode[string](t,c.Input));if s=="" {return nil,nil};return s,nil
 case "escape":return EscapeForExcel(pythonDecode[string](t,c.Input)),nil
 case "font_entry":a:=pythonDecode[[]string](t,c.Input);return FontEntryMatches(a[0],a[1]),nil
 case "sheet_name":a:=pythonDecode[[]json.RawMessage](t,c.Input);used:=map[string]bool{};for _,s:=range pythonDecode[[]string](t,a[2]) {used[s]=true};name:=SheetNameFor(pythonDecode[int](t,a[0]),pythonDecode[string](t,a[1]),used);if !used[strings.ToLower(name)] {t.Fatal("使ったシート名を記録していない")};return name,nil
 case "read":
  
  input:=pythonDecode[struct{Book string;Options struct{Force,Strict bool;Tables []int}}](t,c.Input)
  directory:=t.TempDir();path:=filepath.Join(directory,"book.xlsx");pythonRestoreBook(t,input.Book,path,directory)
  data,e:=Read(path,input.Options.Force,input.Options.Strict,input.Options.Tables)
  if e!=nil {return nil,fmt.Errorf("%s",strings.ReplaceAll(e.Error(),directory,"<DIR>"))}
  tables:=[]any{}
  for _,x:=range data.Tables {
   var kind,start,end,hash,fingerprint,title,missing any
   if x.Kind!="" {kind=x.Kind;start=x.Start;end=x.End};if x.SourceHash!="" {hash=x.SourceHash};if x.Fingerprint!="" {fingerprint=x.Fingerprint};if x.SheetTitle!="" {title=x.SheetTitle};if x.Missing!="" {missing=x.Missing}
   tables=append(tables,map[string]any{"number":x.Number,"kind":kind,"start":start,"end":end,"source_hash":hash,"fingerprint":fingerprint,"sheet_title":title,"occurrence":x.Occurrence,"same":x.Same,"missing":missing,"errors":x.Errors,"warnings":x.Warnings,"model":pythonModelValue(x.Model)})
  }
  hashes:=[]string{};for x:=range data.SourceHashes {hashes=append(hashes,x)};sort.Strings(hashes)
  return pythonNormalize(map[string]any{"markdown_path":data.MarkdownPath,"markdown_relpath":data.MarkdownRelpath,"tables":tables,"warnings":data.Warnings,"source_hashes":hashes},directory),nil

 }
 t.Fatalf("未知の操作: %s",c.Op);return nil,nil
}
''')
DISPATCH["sync"] = (BOOK_IMPORTS + ["github.com/ryo-aoki-pc/md-table-excel/internal/model", "github.com/ryo-aoki-pc/md-table-excel/internal/table"], r'''
func pythonReport(r *Report) any {
 var output any;if r.Output!="" {output=r.Output};return map[string]any{"messages":r.Messages,"warnings":r.Warnings,"errors":r.Errors,"diff":r.Diff,"changed":r.Changed,"output":output}
}
func pythonEvaluate(t *testing.T,c pythonCase)(any,error) {
 if c.Op=="verify" {
  a:=pythonDecode[[]json.RawMessage](t,c.Input);md:=table.NewMarkdown("<test>",pythonDecode[string](t,a[0]),false);regions:=table.FindTables(md)
  inputs:=pythonDecode[[]struct{Number int;Lines []string;Model *pythonModel;Target string}](t,a[2]);replacements:=[]*table.Replacement{}
  for _,v:=range inputs {var m *model.TableModel;if v.Model!=nil {m=v.Model.model()};replacements=append(replacements,&table.Replacement{Region:regions[v.Number-1],Lines:v.Lines,Model:m,TargetKind:v.Target})}
  return Verify(md,pythonDecode[string](t,a[1]),replacements),nil
 }
 if c.Op=="render" {
  a:=pythonDecode[[]json.RawMessage](t,c.Input);md:=table.NewMarkdown("<test>",pythonDecode[string](t,a[0]),false);r:=table.FindTables(md)[pythonDecode[int](t,a[1])-1];m:=pythonDecode[pythonModel](t,a[2]).model();warnings:=[]string{}
  lines,reason,e:=render(r,m,&warnings,pythonDecode[bool](t,a[3]));var converted any;if reason!="" {converted=reason};return []any{lines,converted,warnings},e
 }
 if c.Op=="rewrite" {
  a:=pythonDecode[[]json.RawMessage](t,c.Input);source:=pythonDecode[string](t,a[0]);number:=pythonDecode[int](t,a[1]);m:=pythonDecode[pythonModel](t,a[2]).model()
  md:=table.NewMarkdown("<test>",source,false);r:=table.FindTables(md)[number-1];m=normalizedSheetModel(m,r);warnings:=[]string{}
  lines,converted,e:=render(r,m,&warnings,false);if e!=nil {return nil,e};replacement:=&table.Replacement{Region:r,Lines:lines};after:=table.ApplyReplacements(md,[]*table.Replacement{replacement});var reason any;if converted!="" {reason=converted}
  return []any{after,reason,Verify(md,after,[]*table.Replacement{replacement})},nil
 }
 directory:=t.TempDir()
 var report *Report;var e error;var path string
 if c.Op=="import" {
  input:=pythonDecode[struct{Book,Md,Before string;Exists bool;Options struct{DryRun bool `json:"dry_run"`;Force bool;KeepKind bool `json:"keep_kind"`;StrictText bool `json:"strict_text"`;Tables []int}}](t,c.Input)
  path=filepath.Join(directory,input.Md);if input.Exists {pythonWriteHex(t,path,input.Before)};book:=filepath.Join(directory,"book.xlsx");pythonRestoreBook(t,input.Book,book,directory)
  report,e=ImportWorkbook(book,path,input.Options.DryRun,input.Options.Force,input.Options.KeepKind,input.Options.StrictText,input.Options.Tables)
 } else {
  input:=pythonDecode[struct{Path,Before,Font string;Exists bool;Args []json.RawMessage;Options map[string]json.RawMessage}](t,c.Input)
  if input.Font!="" {t.Setenv("MDTABLE_FONT",input.Font)}
  path=filepath.Join(directory,input.Path);if input.Exists {pythonWriteHex(t,path,input.Before)}
  opt:=func(name string)bool {if v,ok:=input.Options[name];ok {return pythonDecode[bool](t,v)};return false}
  if c.Op=="convert" {
   selectors:=pythonDecode[[]string](t,input.Args[0]);target:=pythonDecode[string](t,input.Args[1]);dry:=opt("dry_run");if len(input.Args)>2 {dry=pythonDecode[bool](t,input.Args[2])};report,e=Convert(path,selectors,target,dry)
  } else if c.Op=="export" {
   output:="";if len(input.Args)>0 {output=pythonDecode[string](t,input.Args[0]);output=strings.ReplaceAll(output,"<DIR>",directory)};var selectors []string;if v,ok:=input.Options["selectors"];ok {selectors=pythonDecode[[]string](t,v)};report,e=Export(path,output,selectors,opt("force"))
  } else {t.Fatalf("未知の操作: %s",c.Op)}
 }
 if e!=nil {return nil,fmt.Errorf("%s",strings.ReplaceAll(e.Error(),directory,"<DIR>"))}
 result:=map[string]any{"report":pythonReport(report),"after":pythonReadHex(t,path)}
 if c.Op=="export" && report.Output!="" {result["properties"]=pythonInspectBook(t,report.Output,directory)}
 return pythonNormalize(result,directory),nil
}
''')

DISPATCH["cli"] = (BOOK_IMPORTS + ["context"], r'''
// cli.main の出力先だけを一時ファイルに向け、実際の解析とコマンド処理を実行する。
func pythonCommand(t *testing.T,argv []string)(int,string,string) {
 return pythonInvoke(t,context.Background(),argv,func(string)error {return fmt.Errorf("このテストでは Excel を起動しない")})
}
func pythonInvoke(t *testing.T,ctx context.Context,argv []string,open func(string)error)(int,string,string) {
 t.Helper();oldOut,oldErr:=stdout,stderr
 out,e:=os.CreateTemp(t.TempDir(),"stdout");if e!=nil {t.Fatal(e)};defer out.Close()
 errout,e:=os.CreateTemp(t.TempDir(),"stderr");if e!=nil {t.Fatal(e)};defer errout.Close()
 stdout,stderr=textStream{out},textStream{errout};defer func(){stdout,stderr=oldOut,oldErr}()
 code:=mainContext(ctx,argv,open)
 a,e:=os.ReadFile(out.Name());if e!=nil {t.Fatal(e)};b,e:=os.ReadFile(errout.Name());if e!=nil {t.Fatal(e)}
 if runtimeGOOSWindows() {a=bytes.ReplaceAll(a,[]byte("\r\n"),[]byte("\n"));b=bytes.ReplaceAll(b,[]byte("\r\n"),[]byte("\n"))}
 return code,string(a),string(b)
}
func runtimeGOOSWindows() bool {return os.PathSeparator=='\\'}
func pythonEvaluate(t *testing.T,c pythonCase)(any,error) {
 input:=pythonDecode[struct{Args []string;Files map[string]struct{Book,Text string}}](t,c.Input)
 directory:=t.TempDir();for name,f:=range input.Files {path:=filepath.Join(directory,name);if f.Book!="" {pythonRestoreBook(t,f.Book,path,directory)} else {pythonWriteHex(t,path,f.Text)}}
 for i,s:=range input.Args {input.Args[i]=strings.ReplaceAll(s,"<DIR>",directory)}
 code,out,errout:=pythonCommand(t,input.Args)
 expected:=pythonDecode[struct{Code int}](t,c.Want)
 after:=map[string]string{};entries,e:=os.ReadDir(directory);if e!=nil {t.Fatal(e)};for _,p:=range entries {if strings.HasSuffix(p.Name(),".md") {after[p.Name()]=pythonReadHex(t,filepath.Join(directory,p.Name()))}}
 if expected.Code==2 {
  if code!=2 || out!="" || errout=="" || strings.Contains(errout,"Traceback") {t.Fatalf("引数の誤りの扱い: %d %q %q",code,out,errout)}
  // argparse の使い方の文だけを除き、終了コード・標準出力・文書は同じ条件で比べる。
  wanted:=pythonDecode[map[string]any](t,c.Want);errout=wanted["err"].(string)
 }
 return pythonNormalize(map[string]any{"code":code,"out":out,"err":errout,"after":after},directory),nil
}
''')

def write_package(package, imports, body, extra=""):
    path = ROOT / "testdata" / "unit" / (package + ".json.gz")
    records = json.loads(gzip.decompress(path.read_bytes()))
    names = sorted({r["test"] for r in records})
    methods = []
    for name in names:
        go_name = "TestPython" + "".join(x.title() for x in re.split("[^a-zA-Z0-9]+", name))
        methods.append('// Python: ' + name + '\nfunc ' + go_name + '(t *testing.T) { pythonRun(t, "' + name + '") }\n')
    code = '// Python の単体テストと同じ入力を保存したデータで検査する。\npackage ' + package + '\n\nimport (\n'
    code += ''.join('"' + x + '"\n' for x in ["compress/gzip", "encoding/json", "os", "fmt", "reflect", "testing"] + imports) + ')\n'
    common = COMMON.replace("PACKAGE", package)
    if package in ("cli", "sync", "workbook"):
        common = common.replace("b := pythonCanonical(pythonDecode[any](t, want))", "b := pythonCanonical(pythonVersionExpected(pythonDecode[any](t, want)))")
    code += common
    if package in ("table", "model", "changes", "sync", "workbook"):
        code += MODEL.replace("model.", "") if package == "model" else MODEL
    if package == "table":
        code += REGIONS.replace("table.", "")
    if package in ("workbook", "sync", "cli"):
        code += BOOK_HELPERS
    code += body + extra + '\n' + '\n'.join(methods)
    with open(ROOT / "internal" / package / "python_port_test.go", "w", encoding="utf-8", newline="\n") as handle:
        handle.write(code)


if __name__ == "__main__":
    for package, (imports, body) in DISPATCH.items():
        write_package(package, imports, body)
    subprocess.run(["gofmt", "-w"] + [str(ROOT / "internal" / p / "python_port_test.go") for p in DISPATCH], check=True)
    coverage = json.loads((ROOT / "testdata" / "coverage.json").read_text(encoding="utf-8"))
    if "remaining" in coverage:
        coverage["native_or_python_specific"] = coverage.pop("remaining")
    with open(ROOT / "testdata" / "coverage.json", "w", encoding="utf-8", newline="\n") as handle:
        json.dump(coverage, handle, ensure_ascii=False, indent=2)
        handle.write("\n")
    files = {}
    for path in sorted((ROOT / "internal").rglob("*_test.go")):
        for name in re.findall(r"// Python: (test_\S+)", path.read_text(encoding="utf-8")):
            files.setdefault(name, []).append(path.relative_to(ROOT).as_posix())
    skipped = {
        "test_cli.CliTest.test_read_line_from_terminal_uses_input": "Python の input() の呼び分けを確かめるもの。Go は端末でもファイル記述子から読み、組み込みの input() を使わない。",
        "test_cli.CliTest.test_read_line_without_stdin": "Python の sys.stdin=None のときの EOFError を確かめるもの。Go のプロセスにはこの Python の状態がない。",
    }
    all_names = set(coverage["recorded"]) | set(coverage["native_or_python_specific"])
    if all_names != set(files) | set(skipped):
        raise RuntimeError("対応が記録されていないテスト: " + repr(sorted(all_names - set(files) - set(skipped))))
    with open(ROOT / "testdata" / "ported_tests.json", "w", encoding="utf-8", newline="\n") as handle:
        json.dump(dict(total=len(all_names), ported=len(files), skipped=skipped, files=files), handle, ensure_ascii=False, indent=2)
        handle.write("\n")
