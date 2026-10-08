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

    def test_usage_error(self):
        code, out, err = run_cli("export")
        self.assertEqual(code, 2)

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
