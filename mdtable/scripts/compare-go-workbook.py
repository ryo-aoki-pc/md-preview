"""Python 0.5.2 と Go の export・import をブックの性質も含めて突き合わせる。

--out の下に作業用の写しと結果を置く。--limit は入力フォルダーごとの文書数、
--edit-limit は入力フォルダーごとの編集試験に使う文書数。
"""

import argparse
import concurrent.futures
import datetime
import json
import os
import pathlib
import re
import shutil
import subprocess
import sys
from collections import Counter

import openpyxl
from openpyxl.cell.rich_text import CellRichText, TextBlock
from openpyxl.cell.text import InlineFont
from openpyxl.styles import Alignment, Font, PatternFill
from openpyxl.worksheet.hyperlink import Hyperlink


def normalize(text, directory):
    for value in (str(directory), str(directory).replace("\\", "/")):
        text = text.replace(value, "<DIR>")
    return text


def color(value):
    return value.rgb[-6:] if value is not None and isinstance(value.rgb, str) else None


def font(value):
    return [value.name, value.sz, bool(value.b), bool(value.i), bool(value.strike), value.u, color(value.color)]


def align(value):
    return [value.horizontal, value.vertical, bool(value.wrap_text)]


def inspect_book(filename, directory):
    wb = openpyxl.load_workbook(filename, rich_text=True)
    out = {"active": wb.active.title,
           "names": sorted([name, value.attr_text] for name, value in wb.defined_names.items()), "sheets": []}
    for ws in wb:
        sheet = {"title": ws.title, "state": ws.sheet_state, "selected": bool(ws.sheet_view.tabSelected),
                 "freeze": ws.freeze_panes, "merges": sorted(str(m) for m in ws.merged_cells.ranges),
                 "cols": {}, "rows": {}, "cells": {}}
        for letter, dim in ws.column_dimensions.items():
            start = dim.min or openpyxl.utils.column_index_from_string(letter)
            end = dim.max or start
            for col in range(start, end + 1):
                sheet["cols"][str(col)] = [dim.width, dim.customWidth, dim.number_format,
                                          font(dim.font), align(dim.alignment)]
        for row, dim in ws.row_dimensions.items():
            if dim.height is not None:
                sheet["rows"][str(row)] = dim.height
        for row in ws:
            for cell in row:
                if not cell.has_style and cell.value is None:
                    continue
                value = cell.value
                if ws.title == "_mdtable" and cell.coordinate == "B3":
                    value = "<日時>"
                if isinstance(value, str):
                    value = normalize(value, directory)
                sides = [getattr(cell.border, name) for name in ("left", "right", "top", "bottom")]
                sheet["cells"][cell.coordinate] = {
                    "value": value, "type": cell.data_type, "num": cell.number_format,
                    "font": font(cell.font), "fill": [cell.fill.fill_type,
                                                       color(cell.fill.fgColor) if cell.fill.fill_type else None],
                    "border": [[s.style, color(s.color)] if s is not None and s.style else None for s in sides],
                    "align": align(cell.alignment),
                    "link": [cell.hyperlink.target, cell.hyperlink.location] if cell.hyperlink else None,
                    "comment": cell.comment.text if cell.comment else None,
                }
        out["sheets"].append(sheet)
    wb.close()
    return out


def collect(paths, limit):
    result = []
    for group, path in enumerate(paths):
        path = pathlib.Path(path)
        files = sorted(path.rglob("*.md")) if path.is_dir() else [path]
        result.extend((group, value) for value in (files[:limit] if limit else files))
    return result


def mutations(wb, kind):
    sheets = [ws for ws in wb if ws.title not in ("目次", "_mdtable")]
    ws = sheets[0]
    if kind == "text":
        ws["A1"] = "書き換えた文字"
    elif kind == "append-row":
        ws.cell(ws.max_row + 1, 1, "追加行")
    elif kind == "append-col":
        ws.cell(1, ws.max_column + 1, "追加列")
    elif kind == "insert-row":
        ws.insert_rows(2)
        ws["A2"] = "挿入行"
    elif kind == "insert-col":
        ws.insert_cols(2)
        ws["B1"] = "挿入列"
    elif kind == "delete-row":
        ws.delete_rows(1)
    elif kind == "delete-col":
        ws.delete_cols(1)
    elif kind == "alignment":
        ws["A1"].alignment = Alignment(horizontal="centerContinuous")
    elif kind == "merge":
        ws.merge_cells("A1:B2")
    elif kind == "header":
        ws["A1"].font = Font(bold=True, italic=True, strike=True, underline="double")
        ws["A1"].fill = PatternFill(patternType="solid", fgColor="FFAA00")
    elif kind == "numeric":
        for row, (value, fmt) in enumerate([(1.2345, "General"), (1e20, "@"), (12.4, "0.00"),
                                          (-1.0, "0%"), (9007199254740993, "General")], 1):
            ws.cell(row, 1, value).number_format = fmt
    elif kind == "datetime":
        for row, value in enumerate([datetime.datetime(2024, 2, 3, 4, 5, 6, 123456),
                                     datetime.date(2025, 6, 7), datetime.time(1, 2, 3, 456789),
                                     datetime.timedelta(days=-2, seconds=12345, microseconds=123500)], 1):
            ws.cell(row, 1, value)
    elif kind == "date-formats":
        for row, (value, fmt) in enumerate([(0.5, "h:mm:ss"), (59, "mm-dd-yy"), (60, "mm-dd-yy"),
                                          (61, "mm-dd-yy"), (-0.2, "yyyy-mm-dd hh:mm:ss"),
                                          (1.999999999, "[h]:mm:ss"), (12.3, '"days"0'),
                                          (12.3, "[Red]0.00"), (12.3, "_d0"), (12.3, r"\d0")], 1):
            ws.cell(row, 1, value).number_format = fmt
    elif kind == "boolean":
        ws["A1"] = True
    elif kind == "formula":
        ws["A1"] = "=SUM(1,2)"
    elif kind == "error":
        ws["A1"] = "#DIV/0!"
    elif kind == "rich":
        ws["A1"] = CellRichText("文字", TextBlock(InlineFont(b=True), "太字"))
    elif kind == "hyperlink":
        ws["A1"].hyperlink = "https://example.com/a%20b"
        ws["A1"].value = "別の文字"
    elif kind == "empty-hyperlink":
        ws["A1"].value = None
        ws["A1"].hyperlink = Hyperlink(ref="A1", location="目次!A1")
    elif kind == "location":
        ws["A1"].hyperlink = Hyperlink(ref="A1", location="行き先!A1")
    elif kind == "comment":
        ws["A1"].comment = openpyxl.comments.Comment("コメント", "確認")
    elif kind == "rename":
        old = ws.title
        ws.title = "名前 ' の変更"
        for value in wb.defined_names.values():
            value.attr_text = value.attr_text.replace("'" + old.replace("'", "''") + "'!",
                                                       "'名前 '' の変更'!")
    elif kind == "delete-sheet":
        wb.remove(ws)
    elif kind == "extra-sheet":
        wb.create_sheet("追加した無関係のシート")["A1"] = "値"
    elif kind == "far-value":
        ws["Z100"] = "離れた位置"
    elif kind == "delete-name":
        wb.defined_names.clear()
    elif kind == "broken-name":
        for value in wb.defined_names.values():
            value.attr_text = "#REF!"
    elif kind == "bad-version":
        wb["_mdtable"]["B1"] = 2
    elif kind == "text-version":
        wb["_mdtable"]["B1"] = "1"
    elif kind == "bad-meta":
        wb["_mdtable"]["A5"] = "壊した"
    elif kind == "delete-meta":
        wb.remove(wb["_mdtable"])
    elif kind == "bad-number":
        wb["_mdtable"]["A6"] = "bad"
    elif kind == "bad-start":
        wb["_mdtable"]["C6"] = None
    elif kind == "escape":
        ws["A1"] = re.sub(r"_(?=x[0-9A-Fa-f]{4}_)", "_x005F_",
                          "_x005F_ / x_x0041_x0042_ / _xD800_")
    elif kind == "empty-trailing":
        for row in ws.iter_rows():
            for cell in row:
                if cell.row == ws.max_row or cell.column == ws.max_column:
                    cell.value = None
    elif kind == "merge-block":
        for merge in list(ws.merged_cells.ranges):
            ws.unmerge_cells(str(merge))
        ws["A1"] = "```\nコード\n```"
    elif kind == "epoch1904":
        wb.epoch = openpyxl.utils.datetime.MAC_EPOCH
        ws["A1"] = datetime.datetime(2024, 1, 1, 12, 30)
    elif kind == "numeric-edges":
        for row, (value, fmt) in enumerate([(-0.0, "General"), (1e308, "General"),
                                          (1e308, "yyyy-mm-dd"), (-1e308, "[h]:mm:ss"),
                                          (1e9, "[h]:mm:ss"), (2958466, "yyyy-mm-dd")], 1):
            ws.cell(row, 1, value).number_format = fmt


KINDS = ["text", "append-row", "append-col", "insert-row", "insert-col", "delete-row", "delete-col",
         "alignment", "merge", "header", "numeric", "datetime", "date-formats", "boolean", "formula", "error",
         "rich", "hyperlink", "location", "comment", "rename", "delete-sheet", "extra-sheet", "far-value",
         "delete-name", "broken-name", "bad-version", "text-version", "bad-meta", "delete-meta", "escape",
         "empty-trailing", "merge-block", "epoch1904", "empty-hyperlink", "numeric-edges",
         "bad-number", "bad-start"]


# この一つのセルだけを正しい文字に戻した Python の結果とも比べる。
# 元ファイルは変更せず、当該プロセスの _cell_text だけを差し替える。
CORRECT_ESCAPE = r'''
import sys
from mdtable import workbook
from mdtable.cli import main
original = workbook._cell_text
def corrected(*args, **kwargs):
    value = original(*args, **kwargs)
    if value == "_ / x_x0041_x0042_ / _xD800_":
        return "_x005F_ / x_x0041_x0042_ / _xD800_"
    return value
workbook._cell_text = corrected
sys.exit(main())
'''


def compare_doc(args, index, group, source, edited):
    directory = pathlib.Path(args.out).resolve() / "work" / ("%04d" % index)
    directory.mkdir(parents=True, exist_ok=True)
    original = source.read_bytes()
    md = directory / "document.md"
    md.write_bytes(original)
    (directory / "book.xlsx").unlink(missing_ok=True)
    results = []

    def run(impl, command, content=original):
        md.write_bytes(content)
        env = dict(os.environ, PYTHONPATH=str(pathlib.Path(args.py_root).resolve()), NO_COLOR="1")
        env.pop("MDTABLE_DEBUG", None)
        if impl == "go":
            prefix = [args.go]
        elif impl == "py-corrected":
            prefix = [sys.executable, "-B", "-c", CORRECT_ESCAPE]
        else:
            prefix = [sys.executable, "-B", "-m", "mdtable"]
        proc = subprocess.run(prefix + command, cwd=directory, env=env, stdin=subprocess.DEVNULL,
                              capture_output=True, timeout=300)
        after = md.read_bytes()
        return {"code": proc.returncode, "out": normalize(proc.stdout.decode("utf-8"), directory),
                "err": normalize(proc.stderr.decode("utf-8"), directory),
                "md": after.hex() if after != content else None}

    def record(category, case, py, go, intentional=False):
        results.append({"category": category, "case": case, "same": py == go,
                        "intentional": intentional and py != go, "py": py, "go": go})

    books = {}
    for impl in ("py", "go"):
        result = run(impl, ["export", "document.md", "-o", "book.xlsx"])
        books[impl] = (directory / "book.xlsx").read_bytes() if (directory / "book.xlsx").exists() else None
        if impl == "py":
            py_export = result
            py_book = inspect_book(directory / "book.xlsx", directory) if books[impl] else None
        else:
            record("export", "出力", py_export, result)
            if books[impl] and py_book:
                record("properties", "ブックの性質", py_book, inspect_book(directory / "book.xlsx", directory))
        (directory / "book.xlsx").unlink(missing_ok=True)
    if books["py"] and books["go"]:
        for label, options, existing in [
                ("既存のブック", [], True), ("上書き", ["--force"], True),
                ("番号で選択", ["--table", "1"], False),
                ("行で選択", ["--table", "L1"], False),
                ("存在しない表", ["--table", "999"], False)]:
            observed = {}
            for impl in ("py", "go"):
                variant = directory / "variant.xlsx"
                variant.unlink(missing_ok=True)
                if existing:
                    variant.write_bytes(books["py"])
                observed[impl] = run(impl, ["export", "document.md", "-o", "variant.xlsx"] + options)
            record("export", label, observed["py"], observed["go"])
        for producer, book in books.items():
            (directory / "book.xlsx").write_bytes(book)
            for options in (["--dry-run"], []):
                command = ["import", "book.xlsx", "--md", "document.md"] + options
                record("cross", producer + " " + " ".join(options), run("py", command), run("go", command))
            command = ["import", "book.xlsx", "--dry-run"]
            record("cross", producer + " Markdown の指定省略", run("py", command), run("go", command))
        if edited:
            for kind in KINDS:
                (directory / "book.xlsx").write_bytes(books["py"])
                wb = openpyxl.load_workbook(directory / "book.xlsx", rich_text=True)
                try:
                    mutations(wb, kind)
                    wb.save(directory / "book.xlsx")
                except (AttributeError, ValueError):
                    wb.close()
                    continue
                wb.close()
                for options in ([], ["--force"], ["--strict-text"], ["--keep-kind"],
                                ["--table", "1"], ["--table", "999"]):
                    for dry in (True, False):
                        command = ["import", "book.xlsx", "--md", "document.md"] + options
                        if dry:
                            command.append("--dry-run")
                        py, go = run("py", command), run("go", command)
                        intentional = (kind == "escape" and py != go
                                       and run("py-corrected", command) == go)
                        record("edited", kind + " " + " ".join(command[4:]), py, go, intentional)
            # Markdown の変更・重複数の変更・再取り込みも比較する。
            (directory / "book.xlsx").write_bytes(books["py"])
            wb = openpyxl.load_workbook(directory / "book.xlsx")
            mutations(wb, "text")
            wb.save(directory / "book.xlsx")
            wb.close()
            for label, content in [("競合", original.replace(b"|", b"| changed ", 1)),
                                   ("前後の追加", b"before\n\n" + original + b"\n\nafter\n"),
                                   ("重複", original + b"\n\n" + original)]:
                for options in (["--dry-run"], ["--force", "--dry-run"]):
                    command = ["import", "book.xlsx", "--md", "document.md"] + options
                    record("conflict", label + " ".join(options), run("py", command, content),
                           run("go", command, content))
            command = ["import", "book.xlsx", "--md", "document.md"]
            py_first, go_first = run("py", command), run("go", command)
            record("repeat", "初回", py_first, go_first)
            record("repeat", "再取り込み",
                   run("py", command, bytes.fromhex(py_first["md"]) if py_first["md"] else original),
                   run("go", command, bytes.fromhex(go_first["md"]) if go_first["md"] else original))
    mismatch = [r for r in results if not r["same"] and not r["intentional"]]
    if not mismatch:
        shutil.rmtree(directory)
    return str(source), results


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--go", required=True)
    parser.add_argument("--py-root", required=True)
    parser.add_argument("--out", required=True)
    parser.add_argument("--jobs", type=int, default=8)
    parser.add_argument("--limit", type=int, default=8)
    parser.add_argument("--edit-limit", type=int, default=1)
    parser.add_argument("paths", nargs="+")
    args = parser.parse_args()
    args.go = str(pathlib.Path(args.go).resolve())
    pathlib.Path(args.out).mkdir(parents=True, exist_ok=True)
    counts, diffs, intended = Counter(), Counter(), Counter()
    mismatches, intentional = [], []
    per_group = Counter()
    tasks = []
    for index, (group, path) in enumerate(collect(args.paths, args.limit)):
        edited = per_group[group] < args.edit_limit
        if b"|" in path.read_bytes() or b"<table" in path.read_bytes():
            per_group[group] += 1
        else:
            edited = False
        tasks.append((index, group, path, edited))
    with concurrent.futures.ThreadPoolExecutor(max_workers=args.jobs) as pool:
        futures = [pool.submit(compare_doc, args, *task) for task in tasks]
        for future in concurrent.futures.as_completed(futures):
            source, results = future.result()
            result_path = pathlib.Path(args.out) / ("document-%s.json" % futures.index(future))
            summary = {"source": source, "counts": dict(Counter(r["category"] for r in results)),
                       "differences": sum(not r["same"] and not r["intentional"] for r in results),
                       "intentional": sum(r["intentional"] for r in results)}
            with open(result_path, "w", encoding="utf-8", newline="\n") as handle:
                json.dump(summary, handle, ensure_ascii=False, indent=1)
            for result in results:
                category = result["category"]
                counts[category] += 1
                if result["intentional"]:
                    intended[category] += 1
                    intentional.append(dict(source=source, **result))
                elif not result["same"]:
                    diffs[category] += 1
                    mismatches.append(dict(source=source, **result))
    report = {"documents": len(tasks), "counts": dict(counts), "differences": dict(diffs),
              "intentional_counts": dict(intended), "intentional": intentional, "mismatches": mismatches}
    with open(pathlib.Path(args.out) / "results.json", "w", encoding="utf-8", newline="\n") as handle:
        json.dump(report, handle, ensure_ascii=False, indent=1)
    print(json.dumps({key: report[key] for key in ("documents", "counts", "differences", "intentional_counts")},
                     ensure_ascii=False))
    return bool(mismatches)


if __name__ == "__main__":
    sys.exit(main())
