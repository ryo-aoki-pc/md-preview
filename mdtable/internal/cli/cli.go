// Package cli は表の一覧・変換と Excel ブックの編集用コマンドを提供する。
package cli

import (
	"context"
	"errors"
	"fmt"
	"github.com/ryo-aoki-pc/md-table-excel/internal/pycompat"
	"github.com/ryo-aoki-pc/md-table-excel/internal/sync"
	"github.com/ryo-aoki-pc/md-table-excel/internal/table"
	"github.com/ryo-aoki-pc/md-table-excel/internal/version"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
)

type textStream struct{ file *os.File }

// cli._prepare_streams に対応する UTF-8 と Windows の改行。
func (s textStream) Write(p []byte) (int, error) {
	text := string(p)
	if runtime.GOOS == "windows" {
		text = strings.ReplaceAll(text, "\n", "\r\n")
	}
	_, e := io.WriteString(s.file, text)
	return len(p), e
}

var stdout = textStream{os.Stdout}
var stderr = textStream{os.Stderr}

// cli._err に対応する標準エラーへの 1 行。
func errLine(s string) { fmt.Fprintln(stderr, s) }

// cli._use_color に対応する色の可否。
func useColor(f *os.File) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	return enableTerminal(f)
}

// cli._color_diff に対応する差分の色。
func colorDiff(diff string) string {
	var out strings.Builder
	for _, line := range pycompat.SplitLines(diff, true) {
		color := ""
		switch {
		case strings.HasPrefix(line, "---") || strings.HasPrefix(line, "+++"):
			color = "\x1b[1m"
		case strings.HasPrefix(line, "@@"):
			color = "\x1b[36m"
		case strings.HasPrefix(line, "+"):
			color = "\x1b[32m"
		case strings.HasPrefix(line, "-"):
			color = "\x1b[31m"
		}
		if color != "" {
			body := strings.TrimRight(line, "\r\n")
			line = color + body + "\x1b[0m" + line[len(body):]
		}
		out.WriteString(line)
	}
	return out.String()
}

// cli._write_diff に対応する差分の出力。
func writeDiff(diff string) {
	if useColor(os.Stdout) {
		diff = colorDiff(diff)
	}
	fmt.Fprint(stdout, diff)
}

// cli._print_report に対応する結果の出力。
func printReport(r *sync.Report) {
	for _, s := range r.Messages {
		errLine(s)
	}
	for _, s := range r.Warnings {
		errLine("警告: " + s)
	}
	for _, s := range r.Errors {
		errLine("エラー: " + s)
	}
}

type args struct {
	command, markdown, target   string
	output, md                  string
	selectors                   []string
	json, dryRun                bool
	force, keepKind, strictText bool
}

// cli.build_parser に対応するコマンドの引数解析。
func parse(argv []string) (args, error) {
	a := args{}
	if len(argv) == 0 {
		return a, fmt.Errorf("コマンドを指定してください")
	}
	a.command = argv[0]
	positionals := false
	for i := 1; i < len(argv); i++ {
		s := argv[i]
		if s == "--" && !positionals {
			positionals = true
			continue
		}
		if !positionals && strings.HasPrefix(s, "-") {
			switch {
			case s == "--json" && a.command == "list":
				a.json = true
			case s == "--dry-run" && (a.command == "convert" || a.command == "import" || a.command == "edit"):
				a.dryRun = true
			case s == "--force" && (a.command == "export" || a.command == "import"):
				a.force = true
			case s == "--keep-kind" && (a.command == "import" || a.command == "edit"):
				a.keepKind = true
			case s == "--strict-text" && (a.command == "import" || a.command == "edit"):
				a.strictText = true
			case (s == "--table" && a.command != "list") || (s == "--to" && a.command == "convert") || ((s == "-o" || s == "--output") && a.command == "export") || (s == "--md" && a.command == "import"):
				if i+1 >= len(argv) {
					return a, fmt.Errorf("%s に値を指定してください", s)
				}
				i++
				if s == "--table" {
					a.selectors = append(a.selectors, argv[i])
				} else if s == "--to" {
					a.target = argv[i]
				} else if s == "--md" {
					a.md = argv[i]
				} else {
					a.output = argv[i]
				}
			case strings.HasPrefix(s, "--table=") && a.command != "list":
				a.selectors = append(a.selectors, strings.TrimPrefix(s, "--table="))
			case strings.HasPrefix(s, "--to=") && a.command == "convert":
				a.target = strings.TrimPrefix(s, "--to=")
			case strings.HasPrefix(s, "--output=") && a.command == "export":
				a.output = strings.TrimPrefix(s, "--output=")
			case strings.HasPrefix(s, "-o") && !strings.HasPrefix(s, "--") && a.command == "export":
				a.output = strings.TrimPrefix(s, "-o")
			case strings.HasPrefix(s, "--md=") && a.command == "import":
				a.md = strings.TrimPrefix(s, "--md=")
			default:
				return a, fmt.Errorf("引数が正しくありません: %s", s)
			}
		} else if a.markdown == "" {
			a.markdown = s
		} else {
			return a, fmt.Errorf("引数が多すぎます: %s", s)
		}
	}
	if a.command != "list" && a.command != "convert" && a.command != "export" && a.command != "import" && a.command != "edit" {
		return a, fmt.Errorf("コマンドが正しくありません: %s", a.command)
	}
	if a.markdown == "" {
		return a, fmt.Errorf("Markdown のファイルを指定してください")
	}
	if a.command == "convert" && (len(a.selectors) == 0 || (a.target != "html" && a.target != "pipe")) {
		return a, fmt.Errorf("convert には --table と --to html|pipe を指定してください")
	}
	if a.strictText && a.force {
		return a, fmt.Errorf("usage: mdtable [-h] [--version] コマンド ...\nmdtable: error: --strict-text と --force は同時に指定できません")
	}
	return a, nil
}

// cli.cmd_list に対応する一覧の出力。
func cmdList(ctx context.Context, a args) error {
	regions, e := sync.ListTablesContext(ctx, a.markdown)
	if canceled := ctx.Err(); canceled != nil {
		return canceled
	}
	if e != nil {
		return e
	}
	if a.json {
		tables := []any{}
		for _, r := range regions {
			m := pycompat.NewOrderedMap()
			m.Set("number", r.Number)
			m.Set("start_line", r.Start+1)
			m.Set("end_line", r.End+1)
			m.Set("kind", r.Kind)
			var rows, cols, excluded, headingLine any
			if r.Excluded == "" {
				rows, cols = r.Model().Rows, r.Model().Cols
			} else {
				excluded = r.Excluded
			}
			if r.HeadingLine != nil {
				headingLine = *r.HeadingLine + 1
			}
			m.Set("rows", rows)
			m.Set("cols", cols)
			m.Set("excluded", excluded)
			m.Set("location", r.Location)
			m.Set("heading", r.Heading)
			m.Set("heading_line", headingLine)
			tables = append(tables, m)
		}
		m := pycompat.NewOrderedMap()
		path, e := filepath.Abs(a.markdown)
		if e != nil {
			return e
		}
		m.Set("version", 1)
		m.Set("markdown", path)
		m.Set("tables", tables)
		fmt.Fprintln(stdout, pycompat.JSONDumps(m, false))
		return nil
	}
	if len(regions) == 0 {
		errLine("表がありません")
		return nil
	}
	for _, r := range regions {
		kind := "HTML"
		if r.Kind == "pipe" {
			kind = "Markdown"
		}
		size := "対象外: " + r.Excluded
		if r.Excluded == "" {
			m := r.Model()
			size = fmt.Sprintf("%d 行 × %d 列", m.Rows, m.Cols)
		}
		parts := []string{fmt.Sprintf("表 %d", r.Number), fmt.Sprintf("%d〜%d 行", r.Start+1, r.End+1), kind, size}
		if r.Location != "" {
			parts = append(parts, r.Location)
		}
		if r.Heading != "" {
			parts = append(parts, "見出し: "+r.Heading)
		}
		fmt.Fprintln(stdout, strings.Join(parts, "\t"))
	}
	return nil
}

// cli.cmd_convert に対応する変換結果の出力。
func cmdConvert(ctx context.Context, a args) (int, error) {
	r, e := sync.ConvertContext(ctx, a.markdown, a.selectors, a.target, a.dryRun)
	if canceled := ctx.Err(); canceled != nil {
		return 1, canceled
	}
	if e != nil {
		return 1, e
	}
	if a.dryRun && r.Diff != "" {
		writeDiff(r.Diff)
	}
	printReport(r)
	if len(r.Errors) > 0 {
		return 1, nil
	}
	if !r.Changed {
		errLine("Markdown は変わりません")
	} else if a.dryRun {
		errLine("（--dry-run なので書き戻していません）")
	} else {
		errLine("書き戻しました: " + r.Output)
	}
	return 0, nil
}

// Main は Ctrl+C の受け口を一つにまとめ、処理の後始末が終わってから戻る。
func Main(argv []string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return mainContext(ctx, argv, openWorkbook)
}

// cli.main に対応する終了コードと例外処理。
func mainContext(ctx context.Context, argv []string, open func(string) error) (code int) {
	defer func() {
		if r := recover(); r != nil {
			code = 1
			if os.Getenv("MDTABLE_DEBUG") == "1" {
				fmt.Fprintln(stderr, r)
				fmt.Fprint(stderr, string(debug.Stack()))
			} else {
				errLine(fmt.Sprintf("エラー: 想定外の失敗です（MDTABLE_DEBUG=1 で詳細を出せます）: %v", r))
			}
		}
	}()
	if errors.Is(ctx.Err(), context.Canceled) {
		errLine("中止しました")
		return 1
	}
	if len(argv) == 1 && argv[0] == "--version" {
		fmt.Fprintln(stdout, "mdtable "+version.Version)
		return 0
	}
	for _, s := range argv {
		if s == "-h" || s == "--help" {
			help := commandHelp[argv[0]]
			if help == "" {
				help = commandHelp[""]
			}
			fmt.Fprint(stdout, help)
			return 0
		}
	}
	a, e := parse(argv)
	if e != nil {
		errLine(e.Error())
		return 2
	}
	switch a.command {
	case "list":
		e = cmdList(ctx, a)
	case "convert":
		code, e = cmdConvert(ctx, a)
	case "export":
		var r *sync.Report
		r, e = sync.ExportContext(ctx, a.markdown, a.output, a.selectors, a.force)
		if canceled := ctx.Err(); canceled != nil {
			e = canceled
		}
		if e == nil {
			printReport(r)
			fmt.Fprintln(stdout, r.Output)
		}
	case "import":
		code, e = cmdImport(ctx, a)
	case "edit":
		code, e = cmdEdit(ctx, a, open)
	}
	if errors.Is(e, context.Canceled) || (a.command != "edit" || e != nil) && errors.Is(ctx.Err(), context.Canceled) {
		errLine("中止しました")
		return 1
	}
	if e != nil {
		message := e.Error()
		if p, ok := e.(*os.PathError); ok {
			message = p.Path + ": " + table.StrError(p)
		}
		errLine("エラー: " + message)
		return 1
	}
	return code
}
