"""mdtable コマンドのテスト（別のプロセスで動かす）。"""

import builtins
import io
import os
import shutil
import sys
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
from unittest import mock

from helpers import run_cli

import openpyxl

from mdtable import cli

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
                mock.patch.object(builtins, "input", lambda *a: ""), \
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
                        mock.patch.object(builtins, "input", lambda *a: ""), \
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
                mock.patch.object(builtins, "input", lambda *a: ""), \
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
                mock.patch.object(builtins, "input", interrupt), \
                redirect_stderr(stderr), redirect_stdout(io.StringIO()):
            code = cli.main(["edit", self.md])
        self.assertEqual(code, 1)
        self.assertIn("mdtable import", stderr.getvalue())
        kept = [line for line in stderr.getvalue().splitlines() if line.startswith("中止しました。ブックを残しました: ")]
        path = kept[0].split(": ", 1)[1]
        self.assertTrue(os.path.exists(path))
        shutil.rmtree(os.path.dirname(path), ignore_errors=True)

    def test_edit_needs_windows(self):
        stderr = io.StringIO()
        with mock.patch.object(cli.os, "name", "posix"), redirect_stderr(stderr):
            code = cli.main(["edit", self.md])
        self.assertEqual(code, 1)
        self.assertIn("Windows", stderr.getvalue())


if __name__ == "__main__":
    unittest.main()
