"""mdtable コマンドのテスト（別のプロセスで動かす）。"""

import builtins
import io
import json
import os
import shutil
import subprocess
import sys
import tempfile
import threading
import time
import unittest
from contextlib import redirect_stderr, redirect_stdout
from unittest import mock

from helpers import ROOT, run_cli

import openpyxl

from mdtable import cli
from mdtable.workbook import WorkbookError

DOCUMENT = """# 文書

| ツール | 説明 |
|---|---|
| lazygit | `scoop install lazygit` |
"""


class CliTest(unittest.TestCase):
    def setUp(self):
        self.dir = tempfile.mkdtemp(prefix="mdtable 試験 ")
        self.md = os.path.join(self.dir, "日本語 の 文書.md")
        with open(self.md, "w", encoding="utf-8", newline="") as handle:
            handle.write(DOCUMENT)
        self.xlsx = os.path.join(self.dir, "日本語 の 文書.tables.xlsx")

    def tearDown(self):
        shutil.rmtree(self.dir, ignore_errors=True)

    def test_list(self):
        code, out, err = run_cli("list", self.md)
        self.assertEqual(code, 0, err)
        self.assertIn("表 1\t3〜5 行\tMarkdown\t2 行 × 2 列", out)

    def test_list_json(self):
        code, out, err = run_cli("list", self.md, "--json")
        self.assertEqual(code, 0, err)
        self.assertEqual(err, "")
        self.assertIn('"heading": "文書"', out)
        self.assertTrue(out.endswith("\n"))
        self.assertEqual(json.loads(out), {
            "version": 1, "markdown": os.path.abspath(self.md), "tables": [{
                "number": 1, "start_line": 3, "end_line": 5, "kind": "pipe", "rows": 2, "cols": 2,
                "excluded": None, "location": "", "heading": "文書", "heading_line": 1,
            }],
        })

    def test_list_json_excluded_and_without_heading(self):
        document = ("| a |\n|---|\n\n"
                    "<table><tr><td><pre>\na\n\nb\n</pre></td></tr></table>\n")
        with open(self.md, "w", encoding="utf-8", newline="") as handle:
            handle.write(document)
        code, out, err = run_cli("list", self.md, "--json")
        self.assertEqual(code, 0, err)
        self.assertEqual(err, "")
        tables = json.loads(out)["tables"]
        self.assertEqual(len(tables), 2)
        self.assertEqual((tables[0]["heading"], tables[0]["heading_line"]), ("", None))
        self.assertEqual(tables[1]["kind"], "html")
        self.assertEqual((tables[1]["rows"], tables[1]["cols"]), (None, None))
        self.assertTrue(tables[1]["excluded"])

    def test_list_json_without_tables(self):
        with open(self.md, "w", encoding="utf-8", newline="") as handle:
            handle.write("# 文書\n\n本文\n")
        code, out, err = run_cli("list", self.md, "--json")
        self.assertEqual(code, 0, err)
        self.assertEqual(err, "")
        self.assertEqual(json.loads(out), {"version": 1, "markdown": self.md, "tables": []})
        code, out, err = run_cli("list", self.md)
        self.assertEqual((code, out, err.splitlines()), (0, "", ["表がありません"]))

    def test_export_with_heading_and_import_with_number(self):
        document = DOCUMENT + "\n# 別の表\n\n| a |\n|---|\n| 1 |\n"
        with open(self.md, "w", encoding="utf-8", newline="") as handle:
            handle.write(document)
        code, out, err = run_cli("export", self.md, "--table", " 文書 ", "--table", "別の表")
        self.assertEqual(code, 0, err)
        book = openpyxl.load_workbook(self.xlsx)
        book[book.sheetnames[1]]["A2"] = "lazygit2"
        book[book.sheetnames[2]]["A2"] = "2"
        book.save(self.xlsx)
        code, out, err = run_cli("import", self.xlsx, "--table", "2")
        self.assertEqual(code, 0, err)
        self.assertNotIn("表 1", err)
        with open(self.md, encoding="utf-8", newline="") as handle:
            self.assertEqual(handle.read(), document.replace("| 1 |", "| 2 |"))

    def test_import_rejects_heading_or_line_selector(self):
        for selector in ("文書", "L3", "1.0"):
            code, out, err = run_cli("import", self.xlsx, "--table", selector)
            self.assertEqual(code, 1)
            self.assertEqual(out, "")
            self.assertEqual(err.splitlines(),
                             ["エラー: import の --table には、ブックの表の番号を指定してください: %s" % selector])

    def test_convert_dry_run_then_write_and_back(self):
        code, out, err = run_cli("convert", self.md, "--table", "1", "--to", "html", "--dry-run")
        self.assertEqual(code, 0, err)
        self.assertIn("+<table>", out)
        self.assertNotIn("\033", out)
        self.assertIn("表 1（3〜5 行）: HTML の表に変える", err)
        self.assertIn("（--dry-run なので書き戻していません）", err)
        with open(self.md, encoding="utf-8") as handle:
            self.assertEqual(handle.read(), DOCUMENT)
        code, out, err = run_cli("convert", self.md, "--table", "文書", "--to", "html")
        self.assertEqual(code, 0, err)
        self.assertEqual(out, "")
        self.assertIn("書き戻しました: " + self.md, err)
        code, out, err = run_cli("convert", self.md, "--table", "L3", "--to", "pipe")
        self.assertEqual(code, 0, err)
        self.assertIn("Markdown の表に変える", err)
        with open(self.md, encoding="utf-8") as handle:
            self.assertEqual(handle.read(), DOCUMENT.replace("|---|---|", "| --- | --- |"))

    def test_convert_already_same_kind(self):
        code, out, err = run_cli("convert", self.md, "--table", "1", "--to", "pipe")
        self.assertEqual(code, 0, err)
        self.assertEqual(out, "")
        self.assertIn("もう Markdown の表", err)
        self.assertIn("Markdown は変わりません", err)

    def test_convert_failure_does_not_change_any_table(self):
        document = ("<table><tr><th>a</th></tr></table>\n\n"
                    "<table><tr><td>b</td></tr></table>\n")
        with open(self.md, "w", encoding="utf-8", newline="") as handle:
            handle.write(document)
        for options in ([], ["--dry-run"]):
            code, out, err = run_cli("convert", self.md, "--table", "1", "--table", "2", "--to", "pipe", *options)
            self.assertEqual(code, 1)
            self.assertEqual(out, "")
            self.assertIn("表 2: Markdown の表にできない:", err)
            self.assertIn("問題があるので書き戻していません", err)
            self.assertNotIn("Traceback", err)
            with open(self.md, encoding="utf-8") as handle:
                self.assertEqual(handle.read(), document)

    def test_convert_requires_table_and_target(self):
        for options in ([], ["--to", "pipe"], ["--table", "1"], ["--table", "1", "--to", "other"]):
            with self.subTest(options=options):
                code, out, err = run_cli("convert", self.md, *options)
                self.assertEqual(code, 2)
                self.assertEqual(out, "")
                self.assertIn("--to" if "--to" not in options else "--table" if "--table" not in options else "other", err)
                self.assertNotIn("Traceback", err)

    def test_convert_dry_run_uses_colored_diff_writer(self):
        with mock.patch("mdtable.cli._use_color", return_value=True), \
                redirect_stdout(io.StringIO()) as out, redirect_stderr(io.StringIO()) as err:
            code = cli.main(["convert", self.md, "--table", "1", "--to", "html", "--dry-run"])
        self.assertEqual(code, 0, err.getvalue())
        self.assertIn("\033[32m+<table>", out.getvalue())
        self.assertIn("--dry-run なので書き戻していません", err.getvalue())

    def test_export_import(self):
        code, out, err = run_cli("export", self.md)
        self.assertEqual(code, 0, err)
        self.assertEqual(out.strip(), self.xlsx)
        self.assertTrue(os.path.exists(self.xlsx))
        # 既にあれば --force が無い限り上書きしない
        code, out, err = run_cli("export", self.md)
        self.assertEqual(code, 1)
        self.assertIn("--force", err)

        book = openpyxl.load_workbook(self.xlsx)
        sheet = book[book.sheetnames[1]]
        sheet["B2"].value = "`scoop install lazygit`\n- 確認する"
        sheet["B2"].data_type = "s"
        book.save(self.xlsx)

        code, out, err = run_cli("import", self.xlsx, "--dry-run")
        self.assertEqual(code, 0, err)
        self.assertIn("+| lazygit | `scoop install lazygit`<ul><li>確認する</li></ul> |", out)
        self.assertNotIn("\033", out)
        self.assertIn("更新（セル 1 個を変更）", err)
        self.assertIn("  B2: 「`scoop install lazygit`」→「`scoop install lazygit`⏎- 確認する」", err)
        with open(self.md, encoding="utf-8") as handle:
            self.assertEqual(handle.read(), DOCUMENT)

        code, out, err = run_cli("import", self.xlsx)
        self.assertEqual(code, 0, err)
        self.assertIn("書き戻しました", err)
        with open(self.md, encoding="utf-8") as handle:
            self.assertIn("<ul><li>確認する</li></ul>", handle.read())

    def test_errors_without_traceback(self):
        cases = [
            ("list", os.path.join(self.dir, "無い.md")),
            ("import", os.path.join(self.dir, "無い.xlsx")),
            ("import", self.md),
            ("export", self.md, "--table", "9"),
        ]
        for args in cases:
            with self.subTest(args=args):
                code, out, err = run_cli(*args)
                self.assertEqual(code, 1)
                self.assertIn("エラー:", err)
                self.assertNotIn("Traceback", err)

    def test_force_does_not_write_when_table_cannot_be_rendered(self):
        original = "<table>\n<tr><td>a</td><td>b</td></tr>\n</table>\n\n| x |\n|---|\n| 1 |\n"
        with open(self.md, "w", encoding="utf-8", newline="") as handle:
            handle.write(original)
        code, out, err = run_cli("export", self.md)
        self.assertEqual(code, 0, err)
        book = openpyxl.load_workbook(self.xlsx)
        book[book.sheetnames[1]]["A1"] = "<td>入れ子"
        book[book.sheetnames[2]]["A2"] = "2"
        book.save(self.xlsx)
        code, out, err = run_cli("import", self.xlsx, "--force")
        self.assertEqual(code, 1, err)
        self.assertIn("エラー: 表 1: 書き戻せない:", err)
        self.assertIn("問題があるので書き戻していません", err)
        self.assertNotIn("Traceback", err)
        with open(self.md, encoding="utf-8", newline="") as handle:
            self.assertEqual(handle.read(), original)

    def test_usage_error(self):
        code, out, err = run_cli("export")
        self.assertEqual(code, 2)

    def test_keep_kind_blocks_conversion_and_other_tables(self):
        original = DOCUMENT + "\n| a |\n|---|\n| 1 |\n"
        with open(self.md, "w", encoding="utf-8", newline="") as handle:
            handle.write(original)
        code, out, err = run_cli("export", self.md)
        self.assertEqual(code, 0, err)
        book = openpyxl.load_workbook(self.xlsx)
        book[book.sheetnames[1]]["B2"] = "```sh\necho test\n```"
        book[book.sheetnames[2]]["A2"] = "2"
        book.save(self.xlsx)
        for options in (("--keep-kind",), ("--keep-kind", "--force"), ("--keep-kind", "--dry-run")):
            with self.subTest(options=options):
                code, out, err = run_cli("import", self.xlsx, *options)
                self.assertEqual(code, 1, err)
                self.assertIn(
                    "表 1: パイプ表のままでは書けない（2 行 2 列: コードブロックはパイプ表のセルに書けない）。HTML の表に変えてよければ --keep-kind を外す",
                    err)
                self.assertNotIn("Traceback", err)
                with open(self.md, encoding="utf-8", newline="") as handle:
                    self.assertEqual(handle.read(), original)
        code, out, err = run_cli("import", self.xlsx)
        self.assertEqual(code, 0, err)
        self.assertIn("コードブロックはパイプ表のセルに書けないため HTML の表に変えて更新", err)
        self.assertIn("HTML の表に変えて更新（セル 1 個を変更）", err)
        with open(self.md, encoding="utf-8") as handle:
            updated = handle.read()
        self.assertIn("<table>", updated)
        self.assertIn("```sh\necho test\n```", updated)
        self.assertIn("| 2 |", updated)

    def test_keep_kind_blocks_merged_cell(self):
        code, out, err = run_cli("export", self.md)
        self.assertEqual(code, 0, err)
        book = openpyxl.load_workbook(self.xlsx)
        book[book.sheetnames[1]].merge_cells("A2:B2")
        book.save(self.xlsx)
        code, out, err = run_cli("import", self.xlsx, "--keep-kind")
        self.assertEqual(code, 1, err)
        self.assertIn("パイプ表のままでは書けない（2 行 1 列: セル結合がある）", err)
        with open(self.md, encoding="utf-8") as handle:
            self.assertEqual(handle.read(), DOCUMENT)
        code, out, err = run_cli("import", self.xlsx)
        self.assertEqual(code, 0, err)
        self.assertIn("セル結合があるため HTML の表に変えて更新", err)
        with open(self.md, encoding="utf-8") as handle:
            self.assertIn('colspan="2"', handle.read())

    def test_strict_text_blocks_non_text_values(self):
        cases = [
            (3.1, "General", "数値", "3.1", "数値を文字にした"),
            (123, "@", "数値", "123", "数値を文字にした"),
            (True, "General", "真偽値", "TRUE", "真偽値を文字にした"),
            ("=1+1", "@", "数式", "=1+1", "数式は式の文字のまま書き戻す"),
            ("#N/A", "@", "エラー値", "#N/A", "エラー値は文字のまま書き戻す"),
        ]
        for value, number_format, kind, text, warning in cases:
            with self.subTest(kind=kind, number_format=number_format):
                with open(self.md, "w", encoding="utf-8", newline="") as handle:
                    handle.write(DOCUMENT)
                code, out, err = run_cli("export", self.md, "--force")
                self.assertEqual(code, 0, err)
                book = openpyxl.load_workbook(self.xlsx)
                sheet = book[book.sheetnames[1]]
                sheet["B2"] = value
                sheet["B2"].number_format = number_format
                book.save(self.xlsx)
                code, out, err = run_cli("import", self.xlsx, "--strict-text")
                self.assertEqual(code, 1, err)
                self.assertIn(
                    "シート「表1 文書」の B2: Excel が%sに変えたセルがある（%s）。セルを文字列の書式にして入力し直す"
                    % (kind, text), err)
                self.assertNotIn("Traceback", err)
                with open(self.md, encoding="utf-8") as handle:
                    self.assertEqual(handle.read(), DOCUMENT)
                code, out, err = run_cli("import", self.xlsx)
                self.assertEqual(code, 0, err)
                self.assertIn("警告: 表 1: シート「表1 文書」の B2: " + warning, err)

    def test_strict_text_blocks_value_matching_original_text(self):
        original = DOCUMENT.replace("`scoop install lazygit`", "3.1")
        with open(self.md, "w", encoding="utf-8", newline="") as handle:
            handle.write(original)
        code, out, err = run_cli("export", self.md)
        self.assertEqual(code, 0, err)
        book = openpyxl.load_workbook(self.xlsx)
        book[book.sheetnames[1]]["B2"] = 3.1
        book.save(self.xlsx)
        code, out, err = run_cli("import", self.xlsx, "--strict-text")
        self.assertEqual(code, 1, err)
        self.assertIn("Excel が数値に変えたセルがある", err)
        with open(self.md, encoding="utf-8") as handle:
            self.assertEqual(handle.read(), original)

    def test_strict_text_and_force_are_incompatible(self):
        for options in (("--strict-text", "--force"), ("--force", "--strict-text")):
            with self.subTest(options=options):
                code, out, err = run_cli("import", self.xlsx, *options)
                self.assertEqual(code, 2, err)
                self.assertIn("--strict-text と --force は同時に指定できません", err)
                self.assertNotIn("Traceback", err)

    def test_table_selection(self):
        with open(self.md, "a", encoding="utf-8", newline="") as handle:
            handle.write("\n| a |\n|---|\n| 1 |\n")
        code, out, err = run_cli("export", self.md, "--table", "L9")
        self.assertEqual(code, 0, err)
        book = openpyxl.load_workbook(self.xlsx)
        self.assertEqual([t for t in book.sheetnames if t.startswith("表")], ["表2 文書"])

    def _change_edit_workbook(self, path):
        book = openpyxl.load_workbook(path)
        book[book.sheetnames[1]]["A2"] = "lazygit2"
        book.save(path)
        book.close()

    def _run_edit(self, startfile, read_input=None, options=()):
        directory = tempfile.mkdtemp(dir=self.dir)
        stdout, stderr = io.StringIO(), io.StringIO()
        if read_input is None:
            read_input = lambda: ""
        with mock.patch.object(cli.os, "name", "nt"), \
                mock.patch.object(cli.os, "startfile", startfile, create=True), \
                mock.patch.object(cli.tempfile, "mkdtemp", return_value=directory), \
                mock.patch.object(cli, "_read_line", read_input), \
                mock.patch.object(cli, "OPEN_GRACE_SECONDS", 0), \
                mock.patch.object(cli, "POLL_SECONDS", 0.002), \
                mock.patch.object(cli, "EOF_GRACE_SECONDS", 0.02), \
                redirect_stderr(stderr), redirect_stdout(stdout):
            code = cli.main(["edit", self.md] + list(options))
        return code, stdout.getvalue(), stderr.getvalue(), directory

    def _close_edit_in_background(self, path, proceed, delay=0):
        ready = threading.Event()
        errors = []

        def close_book():
            try:
                owner = os.path.join(os.path.dirname(path), "~$" + os.path.basename(path))
                with open(owner, "w"):
                    pass
                ready.set()
                if not proceed.wait(2):
                    raise AssertionError("ブックを閉じる前の待ち合わせが終わりませんでした")
                time.sleep(delay)
                self._change_edit_workbook(path)
                os.remove(owner)
            except BaseException as error:
                errors.append(error)
                ready.set()

        thread = threading.Thread(target=close_book, daemon=True)
        thread.start()
        self.addCleanup(lambda: self.assertEqual(errors, []))
        self.addCleanup(thread.join, 2)
        self.assertTrue(ready.wait(2))

    def test_edit_imports_when_closed_without_enter(self):
        owner_seen, release_input = threading.Event(), threading.Event()
        self.addCleanup(release_input.set)
        original_owner_files = cli._owner_files

        def owners(directory):
            names = original_owner_files(directory)
            if names:
                owner_seen.set()
            return names

        def read_input():
            release_input.wait(2)
            raise KeyboardInterrupt

        reader = mock.Mock(side_effect=read_input)
        with mock.patch.object(cli, "_owner_files", side_effect=owners):
            code, out, err, directory = self._run_edit(
                lambda path: self._close_edit_in_background(path, owner_seen), reader)
        release_input.set()
        self.assertEqual(code, 0, err)
        self.assertIn("ブックが閉じられたので取り込みます", err)
        self.assertEqual(reader.call_count, 1)
        self.assertFalse(os.path.exists(directory))
        with open(self.md, encoding="utf-8") as handle:
            self.assertIn("| lazygit2 |", handle.read())

    def test_edit_enter_with_owner_then_closes_automatically(self):
        waiting, release_input = threading.Event(), threading.Event()
        self.addCleanup(release_input.set)
        answers = iter(["", "n"])

        def read_input():
            try:
                return next(answers)
            except StopIteration:
                waiting.set()
                release_input.wait(2)
                raise KeyboardInterrupt

        reader = mock.Mock(side_effect=read_input)
        code, out, err, directory = self._run_edit(
            lambda path: self._close_edit_in_background(path, waiting), reader)
        release_input.set()
        self.assertEqual(code, 0, err)
        self.assertIn("保存して閉じると自動で進みます（このまま取り込むなら y を入力して Enter）", err)
        self.assertEqual(err.count("ブックがまだ開いているようです"), 1)
        self.assertIn("ブックが閉じられたので取り込みます", err)
        self.assertEqual(reader.call_count, 3)
        self.assertFalse(os.path.exists(directory))

    def test_edit_confirms_import_while_owner_exists(self):
        def startfile(path):
            with open(os.path.join(os.path.dirname(path), "~$" + os.path.basename(path)), "w"):
                pass
            self._change_edit_workbook(path)

        reader = mock.Mock(side_effect=["", " Y "])
        code, out, err, directory = self._run_edit(startfile, reader)
        self.assertEqual(code, 0, err)
        self.assertIn("ブックがまだ開いているようです", err)
        self.assertNotIn("ブックが閉じられたので取り込みます", err)
        self.assertEqual(reader.call_count, 2)
        self.assertFalse(os.path.exists(directory))
        with open(self.md, encoding="utf-8") as handle:
            self.assertIn("| lazygit2 |", handle.read())

    def test_edit_eof_without_owner_keeps_workbook(self):
        reader = mock.Mock(side_effect=EOFError)
        code, out, err, directory = self._run_edit(
            lambda path: None, reader, ("--keep-kind", "--strict-text"))
        self.assertEqual(code, 1, err)
        self.assertIn("中止しました。ブックを残しました:", err)
        self.assertIn("取り込むときは: mdtable import", err)
        self.assertIn('--md "%s" --keep-kind --strict-text' % self.md, err)
        self.assertEqual(reader.call_count, 1)
        self.assertTrue(any(name.endswith(".xlsx") for name in os.listdir(directory)))

    def test_edit_eof_with_owner_waits_past_grace(self):
        eof_seen = threading.Event()

        def read_input():
            eof_seen.set()
            raise EOFError

        reader = mock.Mock(side_effect=read_input)
        code, out, err, directory = self._run_edit(
            lambda path: self._close_edit_in_background(path, eof_seen, delay=0.06), reader)
        self.assertEqual(code, 0, err)
        self.assertIn("ブックが閉じられたので取り込みます", err)
        self.assertNotIn("中止しました", err)
        self.assertEqual(reader.call_count, 1)
        self.assertFalse(os.path.exists(directory))

    def test_edit_export_failure_removes_temporary_directory(self):
        with open(self.md, "w", encoding="utf-8", newline="") as handle:
            handle.write("表がない文書\n")
        startfile = mock.Mock()
        code, out, err, directory = self._run_edit(startfile)
        self.assertEqual(code, 1, err)
        self.assertIn("エラー: 表がありません: " + self.md, err)
        self.assertNotIn("ブックを残しました", err)
        self.assertFalse(os.path.exists(directory))
        startfile.assert_not_called()

    def test_edit_startfile_failure_removes_temporary_directory(self):
        code, out, err, directory = self._run_edit(mock.Mock(side_effect=OSError("開けません")))
        self.assertEqual(code, 1, err)
        self.assertIn("エラー:", err)
        self.assertNotIn("ブックを残しました", err)
        self.assertFalse(os.path.exists(directory))

    def test_edit_input_error_keeps_workbook_and_command(self):
        code, out, err, directory = self._run_edit(
            lambda path: None, mock.Mock(side_effect=OSError("入力を読めません")))
        self.assertEqual(code, 1, err)
        self.assertIn("ブックを残しました:", err)
        self.assertIn("取り込むときは: mdtable import", err)
        self.assertIn("入力を読めません", err)
        self.assertTrue(any(name.endswith(".xlsx") for name in os.listdir(directory)))

    def test_edit_waits_for_readable_book_after_owner_disappears(self):
        with open(self.xlsx, "wb") as handle:
            handle.write(b"book")
        real_open = open
        attempts = []

        def open_book(*args, **kwargs):
            attempts.append(None)
            if len(attempts) == 1:
                raise PermissionError("まだ閉じられていません")
            return real_open(*args, **kwargs)

        names = iter([["~$book.xlsx"], [], [], []])
        stderr = io.StringIO()
        with mock.patch.object(cli, "_owner_files", side_effect=lambda directory: next(names, [])), \
                mock.patch.object(cli, "_read_line", side_effect=EOFError), \
                mock.patch.object(cli, "OPEN_GRACE_SECONDS", 0), \
                mock.patch.object(builtins, "open", side_effect=open_book), \
                mock.patch.object(cli, "POLL_SECONDS", 0.002), \
                redirect_stderr(stderr):
            cli._wait_until_closed(self.dir, self.xlsx)
        self.assertEqual(len(attempts), 2)
        self.assertIn("ブックが閉じられたので取り込みます", stderr.getvalue())

    def test_edit_owner_appears_during_eof_grace(self):
        with open(self.xlsx, "wb") as handle:
            handle.write(b"book")
        names = iter([[], [], ["~$book.xlsx"], []])
        stderr = io.StringIO()
        reader = mock.Mock(side_effect=EOFError)
        with mock.patch.object(cli, "_owner_files", side_effect=lambda directory: next(names, [])), \
                mock.patch.object(cli, "_read_line", reader), \
                mock.patch.object(cli, "OPEN_GRACE_SECONDS", 0), \
                mock.patch.object(cli, "POLL_SECONDS", 0.002), \
                mock.patch.object(cli, "EOF_GRACE_SECONDS", 30), \
                redirect_stderr(stderr):
            cli._wait_until_closed(self.dir, self.xlsx)
        self.assertIn("ブックが閉じられたので取り込みます", stderr.getvalue())
        self.assertEqual(reader.call_count, 1)

    def test_edit_enter_before_excel_opens_waits_for_owner(self):
        # Excel が開く前に Enter を押しても終わらず、ブックが開いたら「開いている間の Enter」として扱う
        with open(self.xlsx, "wb") as handle:
            handle.write(b"book")
        held, release_input = threading.Event(), threading.Event()
        self.addCleanup(release_input.set)
        answers = iter([""])

        def read_input():
            try:
                return next(answers)
            except StopIteration:
                release_input.wait(5)
                raise KeyboardInterrupt

        calls = []

        def owners(directory):
            calls.append(None)
            # 最初の何回かはまだ開いていない。開いた後、確かめの文を出してから閉じる
            if len(calls) <= 3:
                return []
            return ["~$book.xlsx"] if len(calls) <= 8 else []

        stderr = io.StringIO()
        with mock.patch.object(cli, "_owner_files", side_effect=owners), \
                mock.patch.object(cli, "_read_line", side_effect=read_input), \
                mock.patch.object(cli, "POLL_SECONDS", 0.002), \
                mock.patch.object(cli, "OPEN_GRACE_SECONDS", 30), \
                redirect_stderr(stderr):
            cli._wait_until_closed(self.dir, self.xlsx)
        release_input.set()
        self.assertEqual(stderr.getvalue().count("ブックがまだ開いているようです"), 1)
        self.assertIn("ブックが閉じられたので取り込みます", stderr.getvalue())

    def test_edit_enter_without_owner_proceeds_after_open_grace(self):
        # ~$ が現れない環境では、開くのを待つ時間が過ぎたら Enter で進む
        stderr = io.StringIO()
        started = time.monotonic()
        with mock.patch.object(cli, "_owner_files", return_value=[]), \
                mock.patch.object(cli, "_read_line", return_value=""), \
                mock.patch.object(cli, "POLL_SECONDS", 0.002), \
                mock.patch.object(cli, "OPEN_GRACE_SECONDS", 0.2), \
                redirect_stderr(stderr):
            cli._wait_until_closed(self.dir, self.xlsx)
        self.assertGreaterEqual(time.monotonic() - started, 0.2)
        self.assertEqual(stderr.getvalue(), "")

    def test_edit_missing_markdown_keeps_workbook_and_command(self):
        code, out, err, directory = self._run_edit(
            lambda path: os.remove(self.md), options=("--keep-kind", "--strict-text"))
        self.assertEqual(code, 1, err)
        xlsx = os.path.join(directory, next(name for name in os.listdir(directory) if name.endswith(".xlsx")))
        self.assertIn("ブックを残しました: " + xlsx, err)
        self.assertIn('取り込むときは: mdtable import "%s" --md "%s" --keep-kind --strict-text'
                      % (xlsx, self.md), err)
        self.assertIn("エラー: 書き戻す Markdown が見つかりません", err)
        self.assertNotIn("Traceback", err)

    def test_edit_dry_run_keeps_diff_and_workbook(self):
        code, out, err, directory = self._run_edit(
            self._change_edit_workbook, options=("--dry-run", "--keep-kind", "--strict-text"))
        self.assertEqual(code, 0, err)
        self.assertIn("-| lazygit |", out)
        self.assertIn("+| lazygit2 |", out)
        self.assertNotIn("\033", out)
        self.assertIn("更新（セル 1 個を変更）", err)
        self.assertIn("  A2: 「lazygit」→「lazygit2」", err)
        self.assertIn("（--dry-run なので書き戻していません）", err)
        self.assertIn("ブックを残しました。取り込むときは:", err)
        xlsx = os.path.join(directory, next(name for name in os.listdir(directory) if name.endswith(".xlsx")))
        self.assertIn('  mdtable import "%s" --md "%s" --keep-kind --strict-text' % (xlsx, self.md), err)
        with open(self.md, encoding="utf-8") as handle:
            self.assertEqual(handle.read(), DOCUMENT)

    def test_edit_dry_run_without_content_changes_removes_workbook(self):
        def startfile(path):
            book = openpyxl.load_workbook(path)
            book[book.sheetnames[1]].column_dimensions["A"].width = 42
            book.save(path)
            book.close()

        code, out, err, directory = self._run_edit(startfile, options=("--dry-run",))
        self.assertEqual(code, 0, err)
        self.assertEqual(out, "")
        self.assertIn("Markdown は変わりません", err)
        self.assertNotIn("ブックを残しました", err)
        self.assertFalse(os.path.exists(directory))

    def test_edit_retries_unreadable_workbook_once(self):
        original_import = cli.import_workbook
        attempts = []

        def import_workbook(*args, **kwargs):
            attempts.append(None)
            if len(attempts) == 1:
                raise WorkbookError("ブックを読み込めません")
            return original_import(*args, **kwargs)

        with mock.patch.object(cli, "import_workbook", side_effect=import_workbook), \
                mock.patch.object(cli.time, "sleep"):
            code, out, err, directory = self._run_edit(self._change_edit_workbook)
        self.assertEqual(code, 0, err)
        self.assertEqual(len(attempts), 2)
        self.assertFalse(os.path.exists(directory))

    def test_edit_repeated_workbook_error_keeps_workbook(self):
        with mock.patch.object(cli, "import_workbook", side_effect=WorkbookError("ブックを読み込めません")), \
                mock.patch.object(cli.time, "sleep"):
            code, out, err, directory = self._run_edit(self._change_edit_workbook)
        self.assertEqual(code, 1, err)
        self.assertIn("ブックを残しました:", err)
        self.assertIn("取り込むときは: mdtable import", err)
        self.assertIn("エラー: ブックを読み込めません", err)
        self.assertTrue(os.path.exists(directory))

    def test_edit_flow(self):
        # edit: Excel で開く代わりにブックを書き換え、Enter の代わりに空の入力を返す
        def fake_startfile(path):
            book = openpyxl.load_workbook(path)
            sheet = book[book.sheetnames[1]]
            sheet["A2"].value = "lazygit2"
            sheet["A2"].data_type = "s"
            book.save(path)

        stderr = io.StringIO()
        with mock.patch.object(cli.os, "name", "nt"), \
                mock.patch.object(cli.os, "startfile", fake_startfile, create=True), \
                mock.patch.object(cli, "_read_line", lambda *a: ""), \
                mock.patch.object(cli, "OPEN_GRACE_SECONDS", 0), \
                redirect_stderr(stderr), redirect_stdout(io.StringIO()):
            code = cli.main(["edit", self.md])
        self.assertEqual(code, 0, stderr.getvalue())
        with open(self.md, encoding="utf-8") as handle:
            self.assertIn("| lazygit2 |", handle.read())

    def test_edit_forwards_import_protections(self):
        cases = [
            ("--keep-kind", "```\ncode\n```", "パイプ表のままでは書けない"),
            ("--strict-text", 3.1, "Excel が数値に変えたセルがある"),
        ]
        for option, value, error in cases:
            with self.subTest(option=option):
                directory = tempfile.mkdtemp(dir=self.dir)

                def fake_startfile(path):
                    book = openpyxl.load_workbook(path)
                    book[book.sheetnames[1]]["B2"] = value
                    book.save(path)

                stderr = io.StringIO()
                with mock.patch.object(cli.os, "name", "nt"), \
                        mock.patch.object(cli.os, "startfile", fake_startfile, create=True), \
                        mock.patch.object(cli.tempfile, "mkdtemp", return_value=directory), \
                        mock.patch.object(cli, "_read_line", lambda *a: ""), \
                        mock.patch.object(cli, "OPEN_GRACE_SECONDS", 0), \
                        redirect_stderr(stderr), redirect_stdout(io.StringIO()):
                    code = cli.main(["edit", self.md, option])
                self.assertEqual(code, 1, stderr.getvalue())
                self.assertIn(error, stderr.getvalue())
                self.assertIn("ブックを残しました", stderr.getvalue())
                self.assertIn('--md "%s"%s' % (self.md, " " + option), stderr.getvalue())
                self.assertTrue(any(name.endswith(".xlsx") for name in os.listdir(directory)))
                with open(self.md, encoding="utf-8") as handle:
                    self.assertEqual(handle.read(), DOCUMENT)

    def test_edit_not_saved(self):
        stderr = io.StringIO()
        with mock.patch.object(cli.os, "name", "nt"), \
                mock.patch.object(cli.os, "startfile", lambda path: None, create=True), \
                mock.patch.object(cli, "_read_line", lambda *a: ""), \
                mock.patch.object(cli, "OPEN_GRACE_SECONDS", 0), \
                redirect_stderr(stderr), redirect_stdout(io.StringIO()):
            code = cli.main(["edit", self.md])
        self.assertEqual(code, 0)
        self.assertIn("保存されていない", stderr.getvalue())

    def test_edit_interrupted_keeps_workbook(self):
        def interrupt(*args):
            raise KeyboardInterrupt

        stderr = io.StringIO()
        with mock.patch.object(cli.os, "name", "nt"), \
                mock.patch.object(cli.os, "startfile", lambda path: None, create=True), \
                mock.patch.object(cli, "_read_line", interrupt), \
                mock.patch.object(cli, "OPEN_GRACE_SECONDS", 0), \
                redirect_stderr(stderr), redirect_stdout(io.StringIO()):
            code = cli.main(["edit", self.md])
        self.assertEqual(code, 1)
        self.assertIn("mdtable import", stderr.getvalue())
        kept = [line for line in stderr.getvalue().splitlines() if line.startswith("中止しました。ブックを残しました: ")]
        path = kept[0].split(": ", 1)[1]
        self.assertTrue(os.path.exists(path))
        shutil.rmtree(os.path.dirname(path), ignore_errors=True)

    def test_edit_reports_temporary_directory_it_cannot_remove(self):
        # ブックを開いたまま取り込むと、Excel がブックを使っていて一時フォルダーを消せない
        with mock.patch.object(cli.shutil, "rmtree"):
            code, out, err, directory = self._run_edit(self._change_edit_workbook)
        self.assertEqual(code, 0, err)
        self.assertIn("一時フォルダーを消せませんでした（ブックを閉じてから消してください）: " + directory, err)

    def test_edit_ends_normally_with_open_stdin_pipe(self):
        # 標準入力が、何も届かないまま開いているパイプでも、ブックを閉じたら取り込んで普通に終わる
        # （読み待ちのスレッドを残したまま、別のプロセスとして終われること）
        driver = "\n".join([
            "import os, sys, threading, time",
            "sys.path.insert(0, sys.argv[1])",
            "import openpyxl",
            "from mdtable import cli",
            "cli.os.name = 'nt'",
            "cli.POLL_SECONDS = 0.05",
            "seen = threading.Event()",
            "original = cli._owner_files",
            "def owners(directory):",
            "    names = original(directory)",
            "    if names:",
            "        seen.set()",
            "    return names",
            "cli._owner_files = owners",
            "def startfile(path):",
            "    def work():",
            "        owner = os.path.join(os.path.dirname(path), '~$' + os.path.basename(path))",
            "        open(owner, 'w').close()",
            "        book = openpyxl.load_workbook(path)",
            "        sheet = book[book.sheetnames[1]]",
            "        sheet['A2'].value = 'lazygit2'",
            "        sheet['A2'].data_type = 's'",
            "        book.save(path)",
            "        seen.wait(30)",
            "        os.remove(owner)",
            "    threading.Thread(target=work).start()",
            "os.startfile = startfile",
            "sys.exit(cli.main(['edit', sys.argv[2]]))",
        ])
        environment = dict(os.environ)
        environment.pop("MDTABLE_DEBUG", None)
        process = subprocess.Popen([sys.executable, "-B", "-c", driver, ROOT, self.md],
                                   stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                   env=environment)
        try:
            code = process.wait(timeout=60)
            err = process.stderr.read().decode("utf-8", "replace")
        finally:
            if process.poll() is None:
                process.kill()
            for stream in (process.stdin, process.stdout, process.stderr):
                stream.close()
        self.assertEqual(code, 0, err)
        self.assertIn("ブックが閉じられたので取り込みます", err)
        self.assertNotIn("Fatal Python error", err)
        self.assertNotIn("Traceback", err)
        with open(self.md, encoding="utf-8") as handle:
            self.assertIn("| lazygit2 |", handle.read())

    def test_read_line_from_terminal_uses_input(self):
        terminal = mock.Mock()
        terminal.isatty.return_value = True
        with mock.patch.object(cli.sys, "stdin", terminal), \
                mock.patch.object(builtins, "input", return_value="y") as reader:
            self.assertEqual(cli._read_line(), "y")
        reader.assert_called_once_with()

    def test_read_line_from_pipe_reads_the_descriptor(self):
        # 端末でない標準入力は、ファイル記述子から 1 行ずつ読む（改行と CR を除く）
        read_end, write_end = os.pipe()
        stream = os.fdopen(read_end, "r", encoding="utf-8")
        try:
            os.write(write_end, "あ y\r\n次の行\n終わり".encode("utf-8"))
            os.close(write_end)
            with mock.patch.object(cli.sys, "stdin", stream), \
                    mock.patch.object(builtins, "input", side_effect=AssertionError("input を使った")):
                self.assertEqual(cli._read_line(), "あ y")
                self.assertEqual(cli._read_line(), "次の行")
                self.assertEqual(cli._read_line(), "終わり")
                with self.assertRaises(EOFError):
                    cli._read_line()
        finally:
            stream.close()

    def test_read_line_without_stdin(self):
        with mock.patch.object(cli.sys, "stdin", None):
            with self.assertRaises(EOFError):
                cli._read_line()

    def test_os_error_message(self):
        # ファイル名の無い OSError でも、先頭に「: 」だけが付かない
        for error, expected in ((OSError("壊れた"), "エラー: 壊れた"),
                                (OSError(2, "見つからない", "x.md"), "エラー: x.md: 見つからない")):
            with self.subTest(expected=expected):
                stderr = io.StringIO()
                with mock.patch.object(cli, "list_tables", side_effect=error), redirect_stderr(stderr):
                    code = cli.main(["list", self.md])
                self.assertEqual(code, 1)
                self.assertEqual(stderr.getvalue().strip(), expected)

    def test_edit_needs_windows(self):
        stderr = io.StringIO()
        with mock.patch.object(cli.os, "name", "posix"), redirect_stderr(stderr):
            code = cli.main(["edit", self.md])
        self.assertEqual(code, 1)
        self.assertIn("Windows", stderr.getvalue())


class DiffColorTest(unittest.TestCase):
    def test_color_diff(self):
        diff = "--- 元\n+++ 新\n@@ -1 +1 @@\n-削除\n+追加\n そのまま\n+末尾"
        self.assertEqual(cli._color_diff(diff),
                         "\033[1m--- 元\033[0m\n\033[1m+++ 新\033[0m\n"
                         "\033[36m@@ -1 +1 @@\033[0m\n\033[31m-削除\033[0m\n"
                         "\033[32m+追加\033[0m\n そのまま\n\033[32m+末尾\033[0m")
        self.assertEqual(cli._color_diff("+追加\r\n"), "\033[32m+追加\033[0m\r\n")

    def test_pipe_and_no_color_disable_colors(self):
        stream = mock.Mock()
        with mock.patch.dict(os.environ, {"NO_COLOR": ""}):
            stream.isatty.return_value = False
            self.assertFalse(cli._use_color(stream))
        with mock.patch.dict(os.environ, {"NO_COLOR": "1"}):
            stream.isatty.return_value = True
            self.assertFalse(cli._use_color(stream))

    def test_terminal_enables_colors(self):
        stream = mock.Mock()
        stream.isatty.return_value = True
        with mock.patch.dict(os.environ, {"NO_COLOR": ""}), mock.patch.object(cli.os, "name", "posix"):
            self.assertTrue(cli._use_color(stream))
        with mock.patch.dict(os.environ, {"NO_COLOR": ""}), mock.patch.object(cli.os, "name", "nt"):
            for enabled in (True, False):
                with mock.patch.object(cli, "_enable_virtual_terminal", return_value=enabled):
                    self.assertEqual(cli._use_color(stream), enabled)

    def test_windows_console_mode(self):
        import ctypes

        stream = mock.Mock()
        stream.fileno.return_value = 1
        handle = 2 ** 33 + 7
        for readable, writable, expected in ((True, True, True), (False, True, False), (True, False, False)):
            with self.subTest(readable=readable, writable=writable):
                kernel = mock.Mock()

                def get_mode(console_handle, mode):
                    self.assertEqual(console_handle, handle)
                    mode._obj.value = 0x0001
                    return readable

                kernel.GetConsoleMode.side_effect = get_mode
                kernel.SetConsoleMode.return_value = writable
                runtime = mock.Mock()
                runtime.get_osfhandle.return_value = handle
                with mock.patch.object(ctypes, "windll", mock.Mock(kernel32=kernel), create=True), \
                        mock.patch.dict(sys.modules, {"msvcrt": runtime}):
                    self.assertEqual(cli._enable_virtual_terminal(stream), expected)
                if readable:
                    kernel.SetConsoleMode.assert_called_once_with(handle, 0x0005)
                else:
                    kernel.SetConsoleMode.assert_not_called()

    def test_diff_color_does_not_reach_stderr(self):
        stdout, stderr = io.StringIO(), io.StringIO()
        with redirect_stdout(stdout), redirect_stderr(stderr), mock.patch.object(cli, "_use_color", return_value=True):
            cli._write_diff("+追加\n")
            cli._err("更新")
        self.assertEqual(stdout.getvalue(), "\033[32m+追加\033[0m\n")
        self.assertEqual(stderr.getvalue(), "更新\n")


if __name__ == "__main__":
    unittest.main()
