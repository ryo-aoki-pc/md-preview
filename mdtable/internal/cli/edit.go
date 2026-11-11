package cli

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/ryo-aoki-pc/md-table-excel/internal/pycompat"
	"github.com/ryo-aoki-pc/md-table-excel/internal/sync"
	"github.com/ryo-aoki-pc/md-table-excel/internal/table"
	"github.com/ryo-aoki-pc/md-table-excel/internal/workbook"
)

func cmdImport(ctx context.Context, a args) (int, error) {
	var tables []int
	for _, s := range a.selectors {
		text := pycompat.Strip(s)
		valid := text != ""
		for _, ch := range text {
			valid = valid && pycompat.IsReDigit(ch)
		}
		n, ok := pycompat.Atoi(text)
		if !valid || !ok {
			return 1, fmt.Errorf("import の --table には、ブックの表の番号を指定してください: %s", s)
		}
		tables = append(tables, n)
	}
	r, e := sync.ImportWorkbookContext(ctx, a.markdown, a.md, a.dryRun, a.force, a.keepKind, a.strictText, tables)
	if canceled := ctx.Err(); canceled != nil {
		return 1, canceled
	}
	if e != nil {
		return 1, e
	}
	return importReport(r, a.dryRun), nil
}

func importReport(r *sync.Report, dryRun bool) int {
	if dryRun && r.Diff != "" {
		writeDiff(r.Diff)
	}
	printReport(r)
	if len(r.Errors) > 0 {
		return 1
	}
	if !r.Changed {
		errLine("Markdown は変わりません")
	} else if dryRun {
		errLine("（--dry-run なので書き戻していません）")
	} else {
		errLine("書き戻しました: " + r.Output)
	}
	return 0
}

func fileHash(path string) ([32]byte, error) {
	f, e := os.Open(path)
	if e != nil {
		return [32]byte{}, e
	}
	defer f.Close()
	h := sha256.New()
	if _, e = io.Copy(h, f); e != nil {
		return [32]byte{}, e
	}
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result, nil
}

func ownerFiles(directory string) (bool, error) {
	entries, e := os.ReadDir(directory)
	if e != nil {
		return false, e
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "~$") || strings.HasPrefix(entry.Name(), ".~lock.") {
			return true, nil
		}
	}
	return false, nil
}

const pollSeconds = 300 * time.Millisecond
const openGrace = 3 * time.Second
const eofGrace = 30 * time.Second

type inputResult struct {
	line string
	err  error
}

// waitUntilClosed は要求した一行だけを読み、所有者ファイルの消滅も待つ。
func waitUntilClosed(ctx context.Context, directory, xlsx string) error {
	requests := make(chan struct{}, 1)
	results := make(chan inputResult, 1)
	done := make(chan struct{})
	defer close(done)
	input := os.Stdin
	go func() {
		for {
			select {
			case <-done:
				return
			case <-requests:
			}
			line, e := readLine(input)
			line = strings.TrimRight(strings.TrimSuffix(line, "\n"), "\r")
			select {
			case results <- inputResult{line, e}:
			case <-done:
				return
			}
			if e != nil {
				return
			}
		}
	}()
	requests <- struct{}{}
	started := time.Now()
	seenOwner, confirming := false, false
	var held *string
	var eofDeadline time.Time
	answered := func(answer string) (bool, error) {
		if confirming && pycompat.Lower(pycompat.Strip(answer)) == "y" {
			return true, nil
		}
		if !confirming {
			errLine("ブックがまだ開いているようです。保存して閉じると自動で進みます（このまま取り込むなら y を入力して Enter）")
			confirming = true
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(pollSeconds):
		}
		requests <- struct{}{}
		return false, nil
	}
	for {
		if e := ctx.Err(); e != nil {
			return e
		}
		owners, e := ownerFiles(directory)
		if e != nil {
			return e
		}
		if owners {
			seenOwner = true
		} else if seenOwner {
			f, e := os.Open(xlsx)
			if e == nil {
				f.Close()
				errLine("ブックが閉じられたので取り込みます")
				return nil
			}
			if !table.IsPermissionError(e) {
				return e
			}
		}
		if held != nil {
			if owners {
				answer := *held
				held = nil
				proceed, e := answered(answer)
				if e != nil || proceed {
					return e
				}
			} else if time.Since(started) >= openGrace {
				return nil
			} else {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(pollSeconds):
				}
			}
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case result := <-results:
			if result.err != nil {
				if inputInterrupted(result.err) {
					return context.Canceled
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
				}
			}
			if result.err == io.EOF {
				eofDeadline = time.Now().Add(eofGrace)
			} else if result.err != nil {
				return result.err
			} else {
				owners, e = ownerFiles(directory)
				if e != nil {
					return e
				}
				if owners {
					seenOwner = true
					proceed, e := answered(result.line)
					if e != nil || proceed {
						return e
					}
				} else if seenOwner || time.Since(started) >= openGrace {
					return nil
				} else {
					line := result.line
					held = &line
				}
			}
		case <-time.After(pollSeconds):
		}
		if !eofDeadline.IsZero() && !seenOwner && !time.Now().Before(eofDeadline) {
			return io.EOF
		}
	}
}

func readLine(input *os.File) (string, error) {
	var data []byte
	var one [1]byte
	for {
		n, e := input.Read(one[:])
		if n > 0 {
			if one[0] == '\n' {
				break
			}
			data = append(data, one[0])
		}
		if e != nil {
			if e == io.EOF && len(data) > 0 {
				break
			}
			return "", e
		}
	}
	return pycompat.DecodeUTF8(data), nil
}

func cmdEdit(ctx context.Context, a args, open func(string) error) (code int, result error) {
	if runtime.GOOS != "windows" {
		return 1, fmt.Errorf("edit は Windows だけで使えます（export と import を使ってください）")
	}
	markdown, e := filepath.Abs(a.markdown)
	if e != nil {
		return 1, e
	}
	directory, e := os.MkdirTemp("", "mdtable-")
	if e != nil {
		return 1, e
	}
	var token [4]byte
	if _, e = rand.Read(token[:]); e != nil {
		os.Remove(directory)
		return 1, e
	}
	stem := pycompat.FileStem(markdown)
	xlsx := filepath.Join(directory, fmt.Sprintf("%s.tables.%x.xlsx", stem, token))
	options := ""
	if a.keepKind {
		options += " --keep-kind"
	}
	if a.strictText {
		options += " --strict-text"
	}
	command := fmt.Sprintf(`mdtable import "%s" --md "%s"%s`, xlsx, markdown, options)
	keep, opened := false, false
	defer func() {
		if canceled := ctx.Err(); canceled != nil {
			result = canceled
		}
		if opened && (result == io.EOF || errors.Is(result, context.Canceled)) {
			keep = true
			errLine("")
			errLine("中止しました。ブックを残しました: " + xlsx)
			errLine("取り込むときは: " + command)
			result = nil
			code = 1
		} else if result != nil && opened {
			errLine("ブックを残しました: " + xlsx)
			errLine("取り込むときは: " + command)
		}
		if !keep {
			_ = os.RemoveAll(directory)
			if _, e := os.Stat(directory); e == nil {
				errLine("一時フォルダーを消せませんでした（ブックを閉じてから消してください）: " + directory)
			}
		}
	}()
	r, e := sync.ExportContext(ctx, markdown, xlsx, a.selectors, true)
	if e != nil {
		return 1, e
	}
	printReport(r)
	before, e := fileHash(xlsx)
	if e != nil {
		return 1, e
	}
	if e = ctx.Err(); e != nil {
		return 1, e
	}
	if e = open(xlsx); e != nil {
		return 1, e
	}
	opened, keep = true, true
	errLine("Excel で編集して保存し、ブックを閉じると自動で書き戻します（Enter でも進めます。中止は Ctrl+C）")
	if e = waitUntilClosed(ctx, directory, xlsx); e != nil {
		return 1, e
	}
	if _, e = os.Stat(markdown); e != nil {
		return 1, fmt.Errorf("書き戻す Markdown が見つかりません（--md で指定してください）: %s", markdown)
	}
	after, e := fileHash(xlsx)
	if e != nil {
		return 1, e
	}
	if after == before {
		errLine("ブックは保存されていないので、Markdown は変えません")
		keep = false
		return 0, nil
	}
	for attempt := 0; attempt < 2; attempt++ {
		r, e = sync.ImportWorkbookContext(ctx, xlsx, markdown, a.dryRun, false, a.keepKind, a.strictText, nil)
		var bookError *workbook.Error
		if e == nil || !errors.As(e, &bookError) || attempt == 1 {
			break
		}
		select {
		case <-ctx.Done():
			return 1, ctx.Err()
		case <-time.After(time.Second):
		}
	}
	if e != nil {
		return 1, e
	}
	if a.dryRun && r.Diff != "" {
		writeDiff(r.Diff)
	}
	printReport(r)
	if len(r.Errors) > 0 {
		errLine("ブックを残しました。直してから次で取り込めます:")
		errLine("  " + command)
		return 1, nil
	}
	if r.Changed {
		if a.dryRun {
			errLine("（--dry-run なので書き戻していません）")
			errLine("ブックを残しました。取り込むときは:")
			errLine("  " + command)
			return 0, nil
		}
		errLine("書き戻しました: " + markdown)
	} else {
		errLine("Markdown は変わりません")
	}
	keep = false
	return 0, nil
}
