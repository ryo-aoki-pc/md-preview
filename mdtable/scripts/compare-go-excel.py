"""Excel の実機（COM）で保存・編集したブックを、Python 版と Go 版で取り込んで比べる（Windows と Excel が要る）。

文書ごとに、両方の版でブックを書き出し、それぞれを Excel で「保存だけ」と、いくつかの編集をして保存する。
その後、どちらのブックも両方の版で import --dry-run（と、保存だけのものは書き戻しも）して、
終了コード・標準出力・標準エラー・書き戻した文書のバイト列を比べる。フォルダーの場所は <DIR> に置き換える。

使い方:
  python scripts/compare-go-excel.py --go <Go 版の mdtable.exe> --py-root <Python 版のリポジトリ> --out <結果のフォルダー>
         [--edits none,text,...] <文書か文書のフォルダー> ...

違いがあれば <結果のフォルダー>/mismatch/ に 1 件ずつ書き、終了コード 1 で終わる。
"""

import argparse
import json
import os
import shutil
import subprocess
import sys

import openpyxl

HERE = os.path.dirname(os.path.abspath(__file__))
TIMEOUT = 300
EDITS = ["none", "text", "insert-row", "align", "number", "number-force", "date-force", "merge", "delete-col",
         "header", "rich", "rename-sheet", "add-sheet", "outside", "formula", "strict"]

# 1 つの Excel で 300 冊ほどを開け閉めすると、終了するときに Excel が異常終了し、Windows が /restore で
# 起動し直す（Python 版のブックだけでも起きた）。そのため、BATCH 冊ごとに Excel を起動し直す
BATCH = 100

PS_SCRIPT = r"""
param([Parameter(Mandatory)][string]$Tasks)
$ErrorActionPreference = 'Stop'
$items = @(Get-Content -LiteralPath $Tasks -Raw -Encoding utf8 | ConvertFrom-Json)
$excel = New-Object -ComObject Excel.Application
try {
  $excel.Visible = $false
  $excel.DisplayAlerts = $false
  foreach ($t in $items) {
    try {
      $wb = $excel.Workbooks.Open([string]$t.book)
      $ws = $wb.Worksheets.Item([string]$t.sheet)
      $r = [int]$t.row; $c = [int]$t.col; $cols = [int]$t.cols
      switch ([string]$t.edit) {
        'none' { }
        'text' { $ws.Cells.Item($r, $c).Value2 = "変えた **文字**`n2 行目" }
        'insert-row' {
          [void]$ws.Rows.Item($r).Insert()
          for ($k = 1; $k -le $cols; $k++) { $ws.Cells.Item($r, $k).Value2 = "足した $k" }
        }
        'align' { $ws.Cells.Item($r, $c).HorizontalAlignment = -4108 }
        'number' { $ws.Cells.Item($r, $c).Value2 = 123 }
        'number-force' { $ws.Cells.Item($r, $c).NumberFormatLocal = 'G/標準'; $ws.Cells.Item($r, $c).Value2 = 1.5 }
        'date-force' { $ws.Cells.Item($r, $c).NumberFormat = 'yyyy/m/d'; $ws.Cells.Item($r, $c).Value2 = 45000 }
        'merge' { if ($cols -ge 2) { [void]$ws.Range($ws.Cells.Item($r, 1), $ws.Cells.Item($r, 2)).Merge() } }
        'delete-col' { if ($cols -ge 2) { [void]$ws.Columns.Item($cols).Delete() } }
        'header' { $ws.Cells.Item($r, $c).Font.Bold = $true; $ws.Cells.Item($r, $c).Interior.Color = 14277081 }
        'rich' { $ws.Cells.Item($r, $c).Value2 = "太字と普通"; $ws.Cells.Item($r, $c).Characters(1, 2).Font.Bold = $true }
        'rename-sheet' { $ws.Name = 'ちがう名前' }
        'add-sheet' { [void]$wb.Worksheets.Add([Type]::Missing, $wb.Worksheets.Item($wb.Worksheets.Count)) }
        'outside' { $ws.Cells.Item($r + 3, $cols + 3).Value2 = "離れた値" }
        'formula' { $ws.Cells.Item($r, $c).NumberFormatLocal = 'G/標準'; $ws.Cells.Item($r, $c).Formula = '=1+1' }
        'strict' { $ws.Cells.Item($r, $c).Value2 = $true }
      }
      $wb.Saved = $false
      $wb.Save()
      $wb.Close($false)
      "ok $($t.book)"
    } catch {
      "失敗 $($t.book): $_"
      try { $wb.Close($false) } catch { }
    }
  }
} finally {
  $excel.Quit()
  [void][Runtime.InteropServices.Marshal]::ReleaseComObject($excel)
  [GC]::Collect()
  [GC]::WaitForPendingFinalizers()
}
"""


def collect(paths):
    docs = []
    for path in paths:
        if os.path.isdir(path):
            for name in sorted(os.listdir(path)):
                if name.lower().endswith(".md"):
                    docs.append(os.path.join(path, name))
        else:
            docs.append(path)
    return docs


class Runner(object):
    def __init__(self, go, py_root):
        self.go = os.path.abspath(go)
        self.py_root = os.path.abspath(py_root)

    def run(self, impl, args, cwd):
        env = dict(os.environ)
        env.pop("NO_COLOR", None)
        env.pop("MDTABLE_DEBUG", None)
        if impl == "py":
            env["PYTHONPATH"] = self.py_root
            command = [sys.executable, "-B", "-m", "mdtable"] + args
        else:
            command = [self.go] + args
        proc = subprocess.run(command, cwd=cwd, env=env, stdin=subprocess.DEVNULL,
                              stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=TIMEOUT)
        return proc.returncode, normalize(proc.stdout, cwd), normalize(proc.stderr, cwd)


def normalize(data, cwd):
    for variant in sorted({cwd, cwd.replace("\\", "/"), json.dumps(cwd)[1:-1]}, key=len, reverse=True):
        data = data.replace(variant.encode("utf-8"), b"<DIR>")
    return data


def first_table(book):
    """ブックの最初の表のシート名と、行・列の数。"""
    wb = openpyxl.load_workbook(book)
    meta = [[c.value for c in row] for row in wb["_mdtable"].iter_rows()]
    header = next(i for i, row in enumerate(meta) if row and row[0] == "number")
    row = meta[header + 1]
    sheet = row[6]
    name = "mdtable_%s" % row[0]
    ref = wb.defined_names[name].attr_text.split("!")[1].replace("$", "")
    end = ref.split(":")[1]
    cols = openpyxl.utils.column_index_from_string("".join(ch for ch in end if ch.isalpha()))
    rows = int("".join(ch for ch in end if ch.isdigit()))
    return sheet, rows, cols


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--go", required=True)
    parser.add_argument("--py-root", required=True)
    parser.add_argument("--out", required=True)
    parser.add_argument("--edits", default=",".join(EDITS))
    parser.add_argument("paths", nargs="+")
    args = parser.parse_args()
    sys.stdout.reconfigure(encoding="utf-8")
    edits = args.edits.split(",")
    runner = Runner(args.go, args.py_root)
    out = os.path.abspath(args.out)
    shutil.rmtree(out, ignore_errors=True)
    os.makedirs(os.path.join(out, "mismatch"))
    mismatches = []
    counts = {}

    def record(label, args_, py, go, extra=""):
        counts.setdefault(label, [0, 0])
        counts[label][0] += 1
        if py == go:
            return
        counts[label][1] += 1
        mismatches.append(label)
        with open(os.path.join(out, "mismatch", "%04d.txt" % len(mismatches)), "w", encoding="utf-8",
                  newline="\n") as handle:
            handle.write("%s\nmdtable %s\n%s\n" % (label, " ".join(args_), extra))
            for name, result in (("Python", py), ("Go", go)):
                handle.write("\n=== %s（終了コード %s）===\n--- stdout ---\n%s\n--- stderr ---\n%s\n" % (
                    name, result[0], result[1].decode("utf-8", "replace"), result[2].decode("utf-8", "replace")))

    tasks = []
    cases = []
    for index, doc in enumerate(collect(args.paths)):
        name = os.path.basename(doc)
        stem = os.path.splitext(name)[0]
        base = os.path.join(out, "%04d" % index)
        exported = {}
        results = {}
        for impl in ("py", "go"):
            folder = os.path.join(base, "export-" + impl)
            os.makedirs(folder)
            shutil.copyfile(doc, os.path.join(folder, name))
            results[impl] = runner.run(impl, ["export", name], folder)
            book = os.path.join(folder, stem + ".tables.xlsx")
            if os.path.exists(book):
                exported[impl] = book
        record("export: " + name, ["export", name], results["py"], results["go"])
        for source, book in sorted(exported.items()):
            try:
                sheet, rows, cols = first_table(book)
            except Exception as error:  # 管理用シートが読めない形は、取り込みの比べで分かる
                print("最初の表を読めない: %s (%s)" % (book, error))
                continue
            row = 2 if rows >= 2 else 1
            col = 2 if cols >= 2 else 1
            for edit in edits:
                folder = os.path.join(base, "%s-%s" % (source, edit))
                os.makedirs(folder)
                shutil.copyfile(doc, os.path.join(folder, name))
                target = os.path.join(folder, stem + ".tables.xlsx")
                shutil.copyfile(book, target)
                tasks.append({"book": target, "sheet": sheet, "row": row, "col": col, "cols": cols, "edit": edit})
                cases.append((name, source, edit, folder, target))

    ps_file = os.path.join(out, "excel-edit.ps1")
    with open(ps_file, "w", encoding="utf-8-sig", newline="\r\n") as handle:
        handle.write(PS_SCRIPT)
    print("Excel で %d 冊を保存する" % len(tasks))
    log = ""
    for start in range(0, len(tasks), BATCH):
        task_file = os.path.join(out, "tasks-%04d.json" % start)
        with open(task_file, "w", encoding="utf-8") as handle:
            json.dump(tasks[start:start + BATCH], handle, ensure_ascii=False)
        proc = subprocess.run(["pwsh", "-NoProfile", "-File", ps_file, "-Tasks", task_file],
                              stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=3600)
        log += proc.stdout.decode("utf-8", "replace")
    with open(os.path.join(out, "excel.log"), "w", encoding="utf-8") as handle:
        handle.write(log)
    failed = [line for line in log.splitlines() if line.startswith("失敗")]
    for line in failed:
        print(line)

    for name, source, edit, folder, target in cases:
        book = os.path.basename(target)
        label = "import: %s（%s が書き出し、Excel で %s）" % (name, source, edit)
        options = []
        if edit in ("number-force", "date-force"):
            options = ["--force"]
        if edit == "strict":
            options = ["--strict-text"]
        command = ["import", book, "--md", name, "--dry-run"] + options
        record(label, command, runner.run("py", command, folder), runner.run("go", command, folder))
        if edit in ("none", "text", "insert-row"):
            results = {}
            for impl in ("py", "go"):
                copy = os.path.join(folder, "write-" + impl)
                os.makedirs(copy)
                shutil.copyfile(os.path.join(folder, name), os.path.join(copy, name))
                shutil.copyfile(target, os.path.join(copy, book))
                result = runner.run(impl, ["import", book, "--md", name], copy)
                with open(os.path.join(copy, name), "rb") as handle:
                    results[impl] = result + (handle.read(),)
            record("import（書き戻し）: %s（%s、%s）" % (name, source, edit), ["import", book, "--md", name],
                   results["py"], results["go"])

    print("比べた回数 %d、違い %d" % (sum(c[0] for c in counts.values()), len(mismatches)))
    kinds = {}
    for label, (count, bad) in counts.items():
        kind = label.split(":")[0]
        kinds.setdefault(kind, [0, 0])
        kinds[kind][0] += count
        kinds[kind][1] += bad
    for kind, (count, bad) in sorted(kinds.items()):
        print("  %-20s %5d 回中 %d 回違う" % (kind, count, bad))
    for label in mismatches[:30]:
        print("  違い: " + label)
    return 1 if mismatches or failed else 0


if __name__ == "__main__":
    sys.exit(main())
