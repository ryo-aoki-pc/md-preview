"""ブックの書き出し・読み込み・取り込みの判定のテスト。"""

import datetime
import os
import re
import shutil
import tempfile
import unittest
import zipfile
from copy import copy
from unittest import mock
from xml.sax.saxutils import escape

import helpers

import openpyxl

from mdtable import sync, workbook
from mdtable.model import Cell, TableModel, normalize_text, sheet_form


class TempDir(object):
    def __enter__(self):
        self.path = tempfile.mkdtemp(prefix="mdtable-test-")
        return self.path

    def __exit__(self, *args):
        shutil.rmtree(self.path, ignore_errors=True)


def write(path, text):
    with open(path, "w", encoding="utf-8", newline="") as handle:
        handle.write(text)


def read(path):
    with open(path, encoding="utf-8", newline="") as handle:
        return handle.read()


def edit_sheet(xlsx, title, edits):
    """openpyxl でセルを書き換える（Excel で入力したのと同じく文字列として）。"""
    book = openpyxl.load_workbook(xlsx)
    sheet = book[title]
    for ref, value in edits.items():
        sheet[ref].value = value
        if isinstance(value, str):
            sheet[ref].data_type = "s"
    book.save(xlsx)


def excel_shape(xlsx):
    """openpyxl の書いたブックを、Excel が保存したときの形（共有文字列）に書き換える。

    Excel は CR を _x000D_ に、文字の _xHHHH_ を _x005F_xHHHH_ にして共有文字列に入れる。
    """
    with zipfile.ZipFile(xlsx) as source:
        entries = [(info, source.read(info.filename)) for info in source.infolist()]
    strings = []

    def to_shared(match):
        attrs, text = match.group(1), match.group(2)
        strings.append(text)
        attrs = attrs.replace(' t="inlineStr"', ' t="s"')
        return "<c%s><v>%d</v></c>" % (attrs, len(strings) - 1)

    pattern = re.compile(r'<c([^>]*t="inlineStr"[^>]*)><is><t(?: [^>]*)?>([\s\S]*?)</t></is></c>')
    out = []
    for info, data in entries:
        if info.filename.startswith("xl/worksheets/sheet"):
            data = pattern.sub(to_shared, data.decode("utf-8")).encode("utf-8")
        out.append((info, data))
    items = []
    for text in strings:
        # Windows の openpyxl はセル内の改行を XML に CRLF で書くので、先に LF にそろえる
        text = text.replace("\r\n", "\n")
        text = text.replace("_x000D_", "_x005F_x000D_").replace("\n", "_x000D_\n")
        items.append('<si><t xml:space="preserve">%s</t><rPh sb="0" eb="1"><t>ふりがな</t></rPh></si>' % text)
    shared = ('<?xml version="1.0" encoding="UTF-8" standalone="yes"?>\n'
              '<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" count="%d" '
              'uniqueCount="%d">%s</sst>' % (len(strings), len(strings), "".join(items)))
    with zipfile.ZipFile(xlsx, "w", zipfile.ZIP_DEFLATED) as target:
        for info, data in out:
            if info.filename == "[Content_Types].xml":
                data = data.decode("utf-8").replace(
                    "</Types>",
                    '<Override PartName="/xl/sharedStrings.xml" ContentType="application/'
                    'vnd.openxmlformats-officedocument.spreadsheetml.sharedStrings+xml"/></Types>'
                ).encode("utf-8")
            if info.filename == "xl/_rels/workbook.xml.rels":
                data = data.decode("utf-8").replace(
                    "</Relationships>",
                    '<Relationship Id="rIdShared" Type="http://schemas.openxmlformats.org/'
                    'officeDocument/2006/relationships/sharedStrings" Target="sharedStrings.xml"/>'
                    "</Relationships>").encode("utf-8")
            target.writestr(info, data)
        target.writestr("xl/sharedStrings.xml", shared)


SAMPLE = """# 文書

| ツール | 説明 |
|---|---|
| lazygit | `scoop install lazygit` |
| yazi | 設定<br>- `yazi.toml` |

<table>
<tr><th>項目</th><th>値</th></tr>
<tr><td>版</td><td>3.4</td></tr>
</table>
"""


class NormalizeTest(unittest.TestCase):
    def test_sheet_form_copies_cells_and_fills_holes(self):
        model = TableModel("html", 2, 2, [Cell(0, 0, "a", colspan=2), Cell(1, 0, "b")], {(1, 1)})
        sheet = sheet_form(model)
        self.assertIsNone(sheet.kind)
        self.assertEqual((sheet.rows, sheet.cols), (2, 2))
        self.assertEqual(sheet.anchors()[(1, 1)].text, "")
        sheet.cells[0].text = "x"
        self.assertEqual(model.cells[0].text, "a")
        self.assertEqual(model.holes, {(1, 1)})

    def test_normalize(self):
        self.assertEqual(normalize_text("a\r\nb\rc  \n\n"), "a\nb\nc")
        self.assertEqual(normalize_text("\n\n  - a"), "  - a")
        self.assertEqual(normalize_text("a_x000D_\nb"), "a\nb")

    def test_escape_round_trip(self):
        # W1: 文字の _x000D_ は _x005F_ を付けて書き、読み戻すと元に戻る
        for text in ("_x000D_", "a_x0041_b", "plain"):
            self.assertEqual(workbook.unescape_from_excel(workbook.escape_for_excel(text)), text)

    def test_cell_checks(self):
        # W2
        self.assertIsNotNone(workbook.check_cell_text("a\x0cb"))
        self.assertIsNotNone(workbook.check_cell_text("x" * 32768))
        self.assertIsNone(workbook.check_cell_text("x" * 32767))

    def test_sheet_names(self):
        used = set()
        name = workbook.sheet_name_for(3, "見出し: [とても] 長い/見出し*の名前" * 3, used)
        self.assertLessEqual(len(name.encode("utf-16-le")) // 2, 31)
        self.assertNotRegex(name, r"[\\/?*\[\]:]")
        self.assertNotEqual(workbook.sheet_name_for(3, "見出し: [とても] 長い/見出し*の名前" * 3, used), name)


class FontTest(unittest.TestCase):
    def test_font_entry_matches(self):
        family = "HackGen Console NF"
        for entry in ("HackGen Console NF Regular (TrueType)", "HackGen Console NF Bold (TrueType)",
                      "Foo & HackGen Console NF (TrueType)", "hackgen console nf"):
            self.assertTrue(workbook.font_entry_matches(entry, family), entry)
        for entry in ("HackGen35 Console NF Regular (TrueType)", "HackGen NF Regular (TrueType)",
                      "HackGen Console NFX Regular (TrueType)", "BIZ UDGothic & BIZ UDPGothic (TrueType)"):
            self.assertFalse(workbook.font_entry_matches(entry, family), entry)

    def test_font_name(self):
        # 環境変数が最優先。無ければ HackGen Console NF が入っているかで決める
        with mock.patch.dict(os.environ, {"MDTABLE_FONT": "Consolas"}):
            with mock.patch.object(workbook, "font_installed", return_value=True):
                self.assertEqual(workbook.font_name(), "Consolas")
        with mock.patch.dict(os.environ):
            os.environ.pop("MDTABLE_FONT", None)
            with mock.patch.object(workbook, "font_installed", return_value=True):
                self.assertEqual(workbook.font_name(), "HackGen Console NF")
            with mock.patch.object(workbook, "font_installed", return_value=False):
                self.assertEqual(workbook.font_name(), "BIZ UDゴシック")

    @unittest.skipUnless(os.name == "nt", "Windows のフォントの登録を見る")
    def test_registered_fonts(self):
        self.assertFalse(workbook.font_installed("mdtable に無い書体 0123"))
        self.assertIsInstance(workbook.font_installed(workbook.PREFERRED_FONT), bool)


class ExportImportTest(unittest.TestCase):
    def setUp(self):
        self.dir = tempfile.mkdtemp(prefix="mdtable-test-")
        self.md = os.path.join(self.dir, "文書 1.md")
        self.xlsx = os.path.join(self.dir, "文書 1.tables.xlsx")
        write(self.md, SAMPLE)

    def tearDown(self):
        shutil.rmtree(self.dir, ignore_errors=True)

    def export(self, **kwargs):
        return sync.export(self.md, self.xlsx, force=True, **kwargs)

    def sheet(self, number):
        book = openpyxl.load_workbook(self.xlsx)
        return [t for t in book.sheetnames if t.startswith("表%d" % number)][0]

    def test_import_selected_tables_only(self):
        original = "| h |\n|---|\n| a |\n\n| h2 |\n|---|\n| b |\n\n| h3 |\n|---|\n| c |\n"
        write(self.md, original)
        self.export()
        book = openpyxl.load_workbook(self.xlsx)
        book[self.sheet(1)]["A2"] = "aa"
        book[self.sheet(2)]["A2"] = "bb"
        book[self.sheet(3)]["A2"] = "cc"
        book.save(self.xlsx)
        report = sync.import_workbook(self.xlsx, tables=[1, 3, 1])
        self.assertEqual(report.errors, [])
        self.assertEqual(report.warnings, [])
        self.assertEqual(read(self.md), original.replace("| a |", "| aa |").replace("| c |", "| cc |"))
        self.assertEqual(len(report.messages), 2)
        self.assertFalse(any(message.startswith("表 2") for message in report.messages))

    def test_unselected_sheet_errors_and_warnings_are_ignored(self):
        original = "| h |\n|---|\n| a |\n\n| h2 |\n|---|\n| b |\n"
        write(self.md, original)
        self.export()
        book = openpyxl.load_workbook(self.xlsx)
        book[self.sheet(1)]["A2"] = "aa"
        other = book[self.sheet(2)]
        other["A2"] = datetime.date(2026, 11, 10)
        other["A2"].number_format = "yyyy-mm-dd"
        other["A1"].comment = openpyxl.comments.Comment("試験", "試験")
        book.save(self.xlsx)
        self.assertTrue(sync.import_workbook(self.xlsx, dry_run=True).errors)
        report = sync.import_workbook(self.xlsx, tables=[1])
        self.assertEqual(report.errors, [])
        self.assertEqual(report.warnings, [])
        self.assertEqual(read(self.md), original.replace("| a |", "| aa |"))

    def test_unselected_missing_sheet_is_ignored(self):
        original = "| h |\n|---|\n| a |\n\n| h2 |\n|---|\n| b |\n"
        write(self.md, original)
        self.export()
        book = openpyxl.load_workbook(self.xlsx)
        book[self.sheet(1)]["A2"] = "aa"
        del book[self.sheet(2)]
        book.save(self.xlsx)
        report = sync.import_workbook(self.xlsx, tables=[1])
        self.assertEqual(report.errors, [])
        self.assertEqual(report.warnings, [])
        self.assertEqual(read(self.md), original.replace("| a |", "| aa |"))

    def test_selected_import_still_reports_unrelated_sheets(self):
        # 表を絞って取り込むときも、足したシートの警告は出し、選んでいない表のシートは警告しない
        original = "| h |\n|---|\n| a |\n\n| h2 |\n|---|\n| b |\n"
        write(self.md, original)
        self.export()
        book = openpyxl.load_workbook(self.xlsx)
        book[self.sheet(1)]["A2"] = "aa"
        book.create_sheet("メモ")
        book.save(self.xlsx)
        data = workbook.read(self.xlsx, tables=[1])
        self.assertEqual([table.number for table in data.tables], [1])
        self.assertEqual(len(data.source_hashes), 2)
        self.assertEqual(data.warnings, ["シート「メモ」はどの表とも対応しないので読まない"])

    def test_selected_import_of_swapped_tables(self):
        # 表を絞って取り込むときも、文書の中で表を入れ替えたことを、ブックの全部の表の元の本文で見分ける
        first = "| a |\n|---|\n| 1 |\n"
        second = "| b |\n|---|\n| 2 |\n"
        write(self.md, first + "\n" + second)
        self.export()
        edit_sheet(self.xlsx, self.sheet(1), {"A1": "b", "A2": "2"})
        write(self.md, second + "\n" + first)
        report = sync.import_workbook(self.xlsx, tables=[1])
        self.assertEqual(report.errors, [])
        self.assertEqual(read(self.md), second + "\n" + second)

    def test_import_missing_number_lists_exported_numbers(self):
        original = "| h |\n|---|\n| a |\n\n| h2 |\n|---|\n| b |\n\n| h3 |\n|---|\n| c |\n"
        write(self.md, original)
        self.export(selectors=["1", "3"])
        with self.assertRaises(workbook.WorkbookError) as raised:
            sync.import_workbook(self.xlsx, tables=[5])
        self.assertEqual(str(raised.exception), "ブックに表 5 はありません（ブックの表: 1, 3）")
        self.assertEqual(read(self.md), original)

    def test_dry_run_summary_and_cells_follow_each_table(self):
        original = "| h |\n|---|\n| a |\n\n| h2 |\n|---|\n| b |\n"
        write(self.md, original)
        self.export()
        book = openpyxl.load_workbook(self.xlsx)
        book[self.sheet(1)]["A2"] = "aa"
        book[self.sheet(2)]["A2"] = "bb"
        book.save(self.xlsx)
        report = sync.import_workbook(self.xlsx, dry_run=True)
        self.assertEqual(report.errors, [])
        self.assertEqual(report.messages, ["表 1（1〜3 行）: 更新（セル 1 個を変更）", "  A2: 「a」→「aa」",
                                           "表 2（5〜7 行）: 更新（セル 1 個を変更）", "  A2: 「b」→「bb」"])
        self.assertEqual(read(self.md), original)
        applied = sync.import_workbook(self.xlsx)
        self.assertEqual(applied.errors, [])
        self.assertEqual(applied.messages, [report.messages[0], report.messages[2]])

    def test_summary_counts_inserted_row_only(self):
        original = "| h |\n|---|\n| a |\n| b |\n"
        write(self.md, original)
        self.export()
        book = openpyxl.load_workbook(self.xlsx)
        sheet = book[self.sheet(1)]
        sheet.insert_rows(3)
        sheet["A3"] = "追加"
        book.save(self.xlsx)
        report = sync.import_workbook(self.xlsx, dry_run=True)
        self.assertEqual(report.errors, [])
        self.assertEqual(report.messages, ["表 1（1〜4 行）: 更新（1 行を追加）"])
        self.assertEqual(read(self.md), original)

    def test_dry_run_cell_limit_per_table(self):
        original = "| h |\n|---|\n" + "| a |\n" * 25
        write(self.md, original + "\n" + original)
        self.export()
        book = openpyxl.load_workbook(self.xlsx)
        for number in (1, 2):
            sheet = book[self.sheet(number)]
            for row in range(2, 27):
                sheet.cell(row, 1, "b")
        book.save(self.xlsx)
        report = sync.import_workbook(self.xlsx, dry_run=True)
        self.assertEqual(report.errors, [])
        self.assertEqual(len(report.messages), 44)
        self.assertEqual(report.messages[21], "  ほか 5 個")
        self.assertEqual(report.messages[43], "  ほか 5 個")

    def test_no_edit_round_trip(self):
        self.export()
        report = sync.import_workbook(self.xlsx)
        self.assertFalse(report.changed)
        self.assertEqual(report.warnings, [])
        self.assertEqual(read(self.md), SAMPLE)

    def test_single_table_opens_its_sheet(self):
        for number in ("1", "2"):
            with self.subTest(number=number):
                self.export(selectors=[number])
                book = openpyxl.load_workbook(self.xlsx)
                self.assertEqual(book.active.title, self.sheet(int(number)))
                self.assertEqual([sheet.title for sheet in book if sheet.sheet_view.tabSelected],
                                 [book.active.title])
                book.close()

    def test_single_table_document_opens_its_sheet(self):
        write(self.md, "| a |\n|---|\n| b |\n")
        self.export()
        book = openpyxl.load_workbook(self.xlsx)
        self.assertEqual(book.active.title, "表1")
        self.assertEqual([sheet.title for sheet in book if sheet.sheet_view.tabSelected], ["表1"])
        book.close()

    def test_multiple_tables_open_index_sheet(self):
        self.export()
        book = openpyxl.load_workbook(self.xlsx)
        self.assertEqual(book.active.title, "目次")
        self.assertEqual([sheet.title for sheet in book if sheet.sheet_view.tabSelected], ["目次"])
        book.close()

    def test_discarded_cell_features(self):
        self.export()
        book = openpyxl.load_workbook(self.xlsx)
        sheet = book[self.sheet(1)]
        sheet["A2"].font = openpyxl.styles.Font(i=True)
        sheet["B2"].font = openpyxl.styles.Font(b=True)
        sheet["A3"].font = openpyxl.styles.Font(strike=True)
        sheet["B3"].font = openpyxl.styles.Font(u="single")
        sheet["A1"].hyperlink = "https://example.com/"
        sheet["B1"].value = "https://example.org/"
        sheet["B1"].hyperlink = "https://example.org/"
        sheet["B1"].font = openpyxl.styles.Font(u="single")
        sheet["B3"].comment = openpyxl.comments.Comment("補足", "試験")
        book.save(self.xlsx)
        table = workbook.read(self.xlsx).tables[0]
        self.assertEqual(table.warnings, [
            "シート「表1 文書」: セル全体の書式（太字・斜体・取り消し線・下線）は書き戻さない: A2, B2, A3, B3（強調は ** などで書く）",
            "シート「表1 文書」: ハイパーリンクは書き戻さない: A1（[文字](URL) の形で書く）",
            "シート「表1 文書」: コメント（メモ）は書き戻さない: B3"])
        report = sync.import_workbook(self.xlsx)
        self.assertEqual(report.errors, [])
        self.assertEqual(report.warnings, ["表 1: " + warning for warning in table.warnings])
        self.assertIn("| lazygit | `scoop install lazygit` |", read(self.md))
        self.assertNotIn("[ツール]", read(self.md))
        self.assertNotIn("**", read(self.md))

    def test_discarded_features_limit_positions(self):
        write(self.md, "| a | b |\n|---|---|\n| c | d |\n| e | f |\n| g | h |\n")
        self.export()
        book = openpyxl.load_workbook(self.xlsx)
        sheet = book[self.sheet(1)]
        for row in sheet.iter_rows(min_row=2):
            for cell in row:
                cell.font = openpyxl.styles.Font(i=True, b=True, strike=True, u="single")
                cell.hyperlink = "https://example.com/"
                cell.comment = openpyxl.comments.Comment("補足", "試験")
        book.save(self.xlsx)
        warnings = workbook.read(self.xlsx).tables[0].warnings
        self.assertEqual(len(warnings), 3)
        for warning in warnings:
            self.assertIn("A2, B2, A3, B3, A4 ほか 1 個", warning)
            self.assertNotIn("B4", warning)

    def test_expected_cell_formats_do_not_warn(self):
        self.export()
        book = openpyxl.load_workbook(self.xlsx)
        sheet = book[self.sheet(1)]
        # パイプ表の見出しの太字と、見出しから写った本文の書式は警告しない
        sheet["A1"].fill = openpyxl.styles.PatternFill()
        sheet["A2"].font = copy(sheet["B1"].font)
        sheet["A2"].fill = copy(sheet["B1"].fill)
        sheet["B3"].value = None
        sheet["B3"].font = openpyxl.styles.Font(i=True, b=True, strike=True, u="double")
        book.save(self.xlsx)
        report = sync.import_workbook(self.xlsx)
        self.assertEqual(report.errors, [])
        self.assertEqual(report.warnings, [])

    def test_hyperlinks_with_visible_targets_do_not_warn(self):
        cases = [
            ("https://example.com", "https://example.com/"),
            ("連絡: user@example.com", "mailto:user@example.com"),
            ("[案内](https://example.com/guide)", "https://example.com/guide/"),
            # Excel はスキームとホストを小文字にして覚える
            ("HTTPS://EXAMPLE.COM/Path", "https://example.com/Path"),
            ("https://example.com/案内", "https://example.com/%E6%A1%88%E5%86%85"),
        ]
        for text, target in cases:
            with self.subTest(target=target):
                self.export()
                book = openpyxl.load_workbook(self.xlsx)
                sheet = book[self.sheet(1)]
                sheet["A2"] = text
                sheet["A2"].hyperlink = target
                sheet["A2"].font = openpyxl.styles.Font(u="single")
                book.save(self.xlsx)
                self.assertEqual(workbook.read(self.xlsx).tables[0].warnings, [])

    def test_internal_and_empty_hyperlinks(self):
        from openpyxl.worksheet.hyperlink import Hyperlink

        self.export()
        book = openpyxl.load_workbook(self.xlsx)
        sheet = book[self.sheet(1)]
        sheet["A2"].hyperlink = Hyperlink(ref="A2", location="目次!A1")
        sheet["B2"].hyperlink = Hyperlink(ref="B2")
        book.save(self.xlsx)
        self.assertEqual(workbook.read(self.xlsx).tables[0].warnings, [
            "シート「表1 文書」: ハイパーリンクは書き戻さない: A2（[文字](URL) の形で書く）"])

    def test_html_header_format_and_bold_only(self):
        # HTML の表: 太字 + 塗りつぶしは見出しなので警告しない。太字だけのセルは警告する
        write(self.md, "<table>\n<tr><th>h</th><td>a</td></tr>\n<tr><td>b</td><td>c</td></tr>\n</table>\n")
        self.export()
        self.assertEqual(workbook.read(self.xlsx).tables[0].warnings, [])
        book = openpyxl.load_workbook(self.xlsx)
        sheet = book[self.sheet(1)]
        sheet["B1"].font = openpyxl.styles.Font(b=True)
        sheet["A2"].font = copy(sheet["A1"].font)
        sheet["A2"].fill = copy(sheet["A1"].fill)
        book.save(self.xlsx)
        table = workbook.read(self.xlsx).tables[0]
        self.assertEqual(table.warnings, [
            "シート「表1」: セル全体の書式（太字・斜体・取り消し線・下線）は書き戻さない: B1（強調は ** などで書く）"])
        self.assertTrue(table.model.anchors()[(1, 0)].header)
        self.assertFalse(table.model.anchors()[(0, 1)].header)

    def test_rich_text_warning_is_preserved(self):
        from openpyxl.cell.rich_text import CellRichText, TextBlock
        from openpyxl.cell.text import InlineFont

        self.export()
        book = openpyxl.load_workbook(self.xlsx)
        sheet = book[self.sheet(1)]
        sheet["A2"] = CellRichText([TextBlock(InlineFont(i=True), "一部"), "だけ"])
        book.save(self.xlsx)
        table = workbook.read(self.xlsx).tables[0]
        self.assertEqual(table.warnings, [
            "シート「表1 文書」の A2: セルの一部だけの書式は書き戻さない（文字だけを使う）"])
        self.assertEqual(table.model.anchors()[(1, 0)].text, "一部だけ")

    def test_cleared_trailing_row_warns(self):
        self.export()
        edit_sheet(self.xlsx, self.sheet(1), {"A3": None, "B3": " \n\t "})
        report = sync.import_workbook(self.xlsx)
        self.assertEqual(report.errors, [])
        self.assertEqual(report.warnings, [
            "表 1: 末尾の 1 行が空になっている（行ごと消すなら、Excel で行を削除する）"])
        model = sync.list_tables(self.md)[0].parsed.model
        self.assertEqual(model.rows, 3)
        self.assertEqual(model.cell_at(2, 0).text, "")
        self.assertEqual(sync.import_workbook(self.xlsx).warnings, [])

    def test_cleared_trailing_column_warns(self):
        self.export()
        edit_sheet(self.xlsx, self.sheet(1), {"B1": None, "B2": None, "B3": None})
        report = sync.import_workbook(self.xlsx)
        self.assertEqual(report.errors, [])
        self.assertEqual(report.warnings, [
            "表 1: 末尾の 1 列が空になっている（列ごと消すなら、Excel で列を削除する）"])
        self.assertEqual(sync.list_tables(self.md)[0].parsed.model.cols, 2)

    def test_deleted_rows_do_not_warn_about_columns(self):
        # 見出しが空の表で本文の行を行ごと消しても、列が空になったとは言わない
        write(self.md, "|   |   |\n|---|---|\n| 1 | 2 |\n")
        self.export()
        book = openpyxl.load_workbook(self.xlsx)
        title = self.sheet(1)
        book[title].delete_rows(2, 1)
        book.defined_names["mdtable_1"].attr_text = "'%s'!$A$1:$B$1" % title
        book.save(self.xlsx)
        report = sync.import_workbook(self.xlsx)
        self.assertEqual(report.errors, [])
        self.assertEqual(report.warnings, [])
        self.assertEqual(read(self.md), "|   |   |\n|---|---|\n")

    def test_original_empty_trailing_rows_and_columns_do_not_warn(self):
        write(self.md, "| a | |\n|---|---|\n| b | |\n| | |\n")
        self.export()
        edit_sheet(self.xlsx, self.sheet(1), {"A2": "変更"})
        report = sync.import_workbook(self.xlsx)
        self.assertEqual(report.errors, [])
        self.assertEqual(report.warnings, [])

    def test_only_increased_trailing_empty_rows_warn(self):
        write(self.md, "| a | b |\n|---|---|\n| c | d |\n| | |\n")
        self.export()
        edit_sheet(self.xlsx, self.sheet(1), {"A2": None, "B2": None})
        report = sync.import_workbook(self.xlsx)
        self.assertEqual(report.errors, [])
        self.assertEqual(report.warnings, [
            "表 1: 末尾の 1 行が空になっている（行ごと消すなら、Excel で行を削除する）"])

    def test_cleared_trailing_row_does_not_warn_on_conflict(self):
        self.export()
        edit_sheet(self.xlsx, self.sheet(1), {"A3": None, "B3": None})
        write(self.md, SAMPLE.replace("lazygit", "変更"))
        report = sync.import_workbook(self.xlsx)
        self.assertTrue(report.errors)
        self.assertEqual(report.warnings, [])

    def test_empty_merged_trailing_cells_are_not_empty_rows_or_columns(self):
        original = "<table>\n<tr><td>a</td><td>b</td></tr>\n<tr><td>c</td><td>d</td></tr>\n</table>\n"
        for merge, edits in (
                ("A2:B2", {"A1": "変更", "A2": None, "B2": None}),
                ("A1:A2", {"A1": None, "B1": "変更", "A2": None, "B2": None}),
                ("B1:B2", {"A1": "変更", "B1": None, "B2": None})):
            with self.subTest(merge=merge):
                write(self.md, original)
                self.export()
                book = openpyxl.load_workbook(self.xlsx)
                sheet = book[self.sheet(1)]
                for ref, value in edits.items():
                    sheet[ref] = value
                sheet.merge_cells(merge)
                book.save(self.xlsx)
                report = sync.import_workbook(self.xlsx)
                self.assertEqual(report.errors, [])
                self.assertEqual(report.warnings, [])

    def test_excel_shaped_round_trip(self):
        # Excel が保存した形（共有文字列・_x000D_・ふりがな）でも変わらない
        self.export()
        excel_shape(self.xlsx)
        report = sync.import_workbook(self.xlsx)
        self.assertEqual(report.errors, [])
        self.assertFalse(report.changed)

    def test_text_cells(self):
        self.export()
        book = openpyxl.load_workbook(self.xlsx)
        sheet = book[self.sheet(1)]
        self.assertEqual(sheet["B3"].value, "設定\n\\- `yazi.toml`")
        self.assertEqual(sheet["B3"].number_format, "@")
        self.assertTrue(sheet["A1"].font.b)
        self.assertEqual(book[workbook.META_SHEET].sheet_state, "veryHidden")

    def test_font(self):
        with mock.patch.dict(os.environ, {"MDTABLE_FONT": "Consolas"}):
            self.export()
        book = openpyxl.load_workbook(self.xlsx)
        sheet = book[self.sheet(1)]
        index = book[workbook.INDEX_SHEET]
        for cell in (sheet["A1"], sheet["B2"], index["A1"], index["B4"]):
            self.assertEqual(cell.font.name, "Consolas", cell.coordinate)
        self.assertEqual(sheet.column_dimensions["A"].font.name, "Consolas")

    def test_edit_and_import(self):
        self.export()
        edit_sheet(self.xlsx, self.sheet(1), {"B2": "`scoop install lazygit`\n- 確認する"})
        report = sync.import_workbook(self.xlsx)
        self.assertEqual(report.errors, [])
        self.assertIn("| lazygit | `scoop install lazygit`<ul><li>確認する</li></ul> |", read(self.md))

    def test_numbers_and_dates(self):
        # W4: 標準の書式の数値は文字にして警告、日付は止める（--force で書き戻す）
        self.export()
        book = openpyxl.load_workbook(self.xlsx)
        sheet = book[self.sheet(2)]
        sheet["B2"].value = 3.5
        sheet["B2"].number_format = "General"
        book.save(self.xlsx)
        report = sync.import_workbook(self.xlsx)
        self.assertEqual(report.errors, [])
        self.assertTrue(any("数値" in w for w in report.warnings))
        self.assertIn("<td>3.5</td>", read(self.md))

        self.export()
        book = openpyxl.load_workbook(self.xlsx)
        sheet = book[self.sheet(2)]
        sheet["B2"].value = datetime.datetime(2026, 10, 9)
        book.save(self.xlsx)
        before = read(self.md)
        report = sync.import_workbook(self.xlsx)
        self.assertTrue(report.errors)
        self.assertEqual(read(self.md), before)
        report = sync.import_workbook(self.xlsx, force=True)
        self.assertIn("<td>2026-10-09</td>", read(self.md))

    def test_formula_like_text(self):
        self.export()
        edit_sheet(self.xlsx, self.sheet(2), {"B2": "=A1"})
        book = openpyxl.load_workbook(self.xlsx)
        self.assertEqual(book[self.sheet(2)]["B2"].data_type, "s")
        report = sync.import_workbook(self.xlsx)
        self.assertEqual(report.errors, [])
        self.assertIn("<td>=A1</td>", read(self.md))

    def test_renamed_and_deleted_sheets(self):
        self.export()
        book = openpyxl.load_workbook(self.xlsx)
        old = self.sheet(1)
        sheet = book[old]
        sheet.title = "名前を変えた"
        # Excel はシート名を変えると名前の定義も直す（openpyxl は直さないので合わせる）
        book.defined_names["mdtable_1"].attr_text = "'名前を変えた'!$A$1:$B$3"
        sheet["A2"].value = "lazygit2"
        sheet["A2"].data_type = "s"
        del book[self.sheet(2)]
        book.save(self.xlsx)
        report = sync.import_workbook(self.xlsx)
        self.assertIn("| lazygit2 |", read(self.md))
        self.assertTrue(any("シートが見つからない" in w for w in report.warnings))

    def test_far_values_are_ignored(self):
        self.export()
        edit_sheet(self.xlsx, self.sheet(1), {"Z100": "stray"})
        report = sync.import_workbook(self.xlsx)
        self.assertFalse(report.changed)
        self.assertTrue(any("離れた位置" in w for w in report.warnings))

    def test_adjacent_row_extends(self):
        self.export()
        edit_sheet(self.xlsx, self.sheet(1), {"A4": "delta", "B4": "使う"})
        sync.import_workbook(self.xlsx)
        self.assertIn("| delta | 使う |", read(self.md))

    def test_conflict_and_force(self):
        # C3: 書き出した後に Markdown の表を変えると止まる
        self.export()
        write(self.md, SAMPLE.replace("| yazi |", "| yazi2 |"))
        edit_sheet(self.xlsx, self.sheet(1), {"A2": "lazygit3"})
        report = sync.import_workbook(self.xlsx)
        self.assertTrue(any("衝突" in e for e in report.errors))
        self.assertNotIn("lazygit3", read(self.md))
        report = sync.import_workbook(self.xlsx, force=True)
        self.assertIn("lazygit3", read(self.md))

    def test_applied_twice(self):
        # C2: 同じブックをもう一度取り込むと「適用済み」
        self.export()
        edit_sheet(self.xlsx, self.sheet(1), {"A2": "lazygit4"})
        sync.import_workbook(self.xlsx)
        report = sync.import_workbook(self.xlsx)
        self.assertEqual(report.errors, [])
        self.assertFalse(report.changed)
        self.assertTrue(any("適用済み" in m for m in report.messages))

    def test_applied_twice_after_normalizing(self):
        # 表の下に足した行のセルの配置が列と違う（多いほうにそろえる）、セルの文字が収束する形に
        # 直る（- y⏎z → - y⏎  z）のどちらがあっても、もう一度取り込むと「適用済み」
        write(self.md, "| a | b |\n|---|:---:|\n| 1 | 2 |\n")
        self.export()
        edit_sheet(self.xlsx, self.sheet(1), {"A3": "x", "B3": "- y\nz"})
        report = sync.import_workbook(self.xlsx)
        self.assertEqual(report.errors, [])
        self.assertTrue(any("配置が混在" in w for w in report.warnings))
        self.assertIn("| x | <ul><li>y<br>z</li></ul> |", read(self.md))
        after = read(self.md)
        report = sync.import_workbook(self.xlsx)
        self.assertEqual(report.errors, [])
        self.assertFalse(report.changed)
        self.assertTrue(any("適用済み" in m for m in report.messages))
        self.assertEqual(read(self.md), after)

    def test_many_tables_with_body(self):
        # 本文と 7 つの表がある見本（docs/verification/multi-tables.md）で 5 つの表を編集しても、
        # 表の外の行（足した空行を除く）と、編集していない表は 1 文字も変わらない
        with open(os.path.join(helpers.ROOT, "docs", "verification", "multi-tables.md"),
                  encoding="utf-8", newline="") as handle:
            original = handle.read()
        write(self.md, original)
        self.export()
        edit_sheet(self.xlsx, self.sheet(1), {"B3": "0.2.0", "A4": "フォント", "B4": "HackGen Console NF",
                                              "C4": "- 入っていれば使う\n- 無ければ BIZ UDゴシック"})
        edit_sheet(self.xlsx, self.sheet(2), {"B2": "`python scripts/setup-mdtable.py`\n（リポジトリの外に入れる）"})
        edit_sheet(self.xlsx, self.sheet(3), {"B3": "セル結合ができる\n\n```html\n<td rowspan=\"2\">結合</td>\n```"})
        edit_sheet(self.xlsx, self.sheet(4), {"C3": "```sh\nsudo dnf install -y epel-release\n```",
                                              "C4": "ln\n: シンボリックリンクを作る"})
        edit_sheet(self.xlsx, self.sheet(5), {"B2": "編集（**Excel** で開く）"})
        report = sync.import_workbook(self.xlsx)
        self.assertEqual(report.errors, [])
        edited = read(self.md)

        def split(text):
            regions = helpers.regions(text)
            lines = text.split("\n")
            covered = {i for r in regions for i in range(r.start, r.end + 1)}
            outside = [line for i, line in enumerate(lines) if i not in covered and line.strip()]
            tables = ["\n".join(lines[r.start:r.end + 1]) for r in regions]
            return outside, tables, [r.kind for r in regions]

        outside_before, tables_before, _ = split(original)
        outside_after, tables_after, kinds = split(edited)
        self.assertEqual(outside_after, outside_before)
        self.assertEqual(len(tables_after), 7)
        self.assertEqual([i + 1 for i in range(7) if tables_before[i] != tables_after[i]], [1, 2, 3, 4, 5])
        self.assertEqual(kinds, ["pipe", "pipe", "html", "html", "pipe", "pipe", "pipe"])
        # HTML にした表 3 の直後のリストは、空行を足してリストのまま
        self.assertIn("</table>\n\n- 表のすぐ後のリスト（空行なし）", edited)
        report = sync.import_workbook(self.xlsx)
        self.assertFalse(report.changed)
        self.assertEqual(len([m for m in report.messages if "適用済み" in m]), 5)

    def test_merged_cells_keep_text_format(self):
        # 結合で隠れるセルも、左上のセルと同じ書式（文字列の書式）にする。Excel で結合を解いて
        # 入力しても文字列のまま。openpyxl は読むときに隠れるセルの書式を捨てるので、XML で確かめる
        write(self.md, '<table>\n<tr><th rowspan="2">a</th><th>b</th></tr>\n<tr><td>c</td></tr>\n</table>\n')
        self.export()
        with zipfile.ZipFile(self.xlsx) as book:
            sheet = book.read("xl/worksheets/sheet2.xml").decode("utf-8")
            styles = book.read("xl/styles.xml").decode("utf-8")
        self.assertIn('<mergeCell ref="A1:A2"', sheet)
        top = re.search(r'<c r="A1" s="(\d+)"', sheet).group(1)
        covered = re.search(r'<c r="A2" s="(\d+)"', sheet).group(1)
        self.assertEqual(covered, top)
        xfs = re.findall(r"<xf [^>]*>", re.search(r"<cellXfs[^>]*>(.*?)</cellXfs>", styles, re.S).group(1))
        self.assertIn('numFmtId="49"', xfs[int(covered)])
        report = sync.import_workbook(self.xlsx)
        self.assertEqual(report.errors, [])
        self.assertFalse(report.changed)

    def test_column_style_alignment(self):
        # パイプ表の列の配置を列のスタイルにも付け、表の下の行に入力したセルも同じ配置にする
        write(self.md, "| a | b |\n|---|:---:|\n| 1 | 2 |\n")
        self.export()
        sheet = openpyxl.load_workbook(self.xlsx)[self.sheet(1)]
        self.assertEqual(sheet.column_dimensions["B"].alignment.horizontal, "center")
        self.assertIsNone(sheet.column_dimensions["A"].alignment.horizontal)

    def test_line_endings_and_bom(self):
        # C4・D17: 改行コードと BOM を保ち、改行コードを変えても衝突しない
        text = SAMPLE.replace("\n", "\r\n")
        with open(self.md, "wb") as handle:
            handle.write(b"\xef\xbb\xbf" + text.encode("utf-8"))
        self.export()
        edit_sheet(self.xlsx, self.sheet(1), {"A2": "lazygit5"})
        sync.import_workbook(self.xlsx)
        with open(self.md, "rb") as handle:
            data = handle.read()
        self.assertTrue(data.startswith(b"\xef\xbb\xbf"))
        self.assertNotIn(b"\n", data.replace(b"\r\n", b""))
        self.assertIn("lazygit5".encode("utf-8"), data)

    def test_identical_tables(self):
        # C1: 同じ表が 2 つあっても、編集したほうだけが変わる
        table = "| a | b |\n|---|---|\n| 1 | 2 |\n"
        write(self.md, table + "\n" + table)
        self.export()
        edit_sheet(self.xlsx, self.sheet(2), {"A2": "x"})
        sync.import_workbook(self.xlsx)
        self.assertEqual(read(self.md), table + "\n" + table.replace("| 1 |", "| x |"))

    def test_identical_tables_applied_twice(self):
        # 先頭の表だけを書き出して繰り返し取り込んでも、後ろの同じ表を変えない
        table = "| a | b |\n|---|---|\n| 1 | 2 |\n"
        write(self.md, table + "\n" + table)
        self.export(selectors=["1"])
        metadata = workbook.read(self.xlsx).tables[0]
        self.assertEqual(metadata.same, 2)
        self.assertEqual(metadata.occurrence, 0)
        edit_sheet(self.xlsx, self.sheet(1), {"A2": "x"})
        report = sync.import_workbook(self.xlsx)
        self.assertEqual(report.errors, [])
        after = table.replace("| 1 |", "| x |") + "\n" + table
        self.assertEqual(read(self.md), after)
        report = sync.import_workbook(self.xlsx)
        self.assertEqual(report.errors, [])
        self.assertFalse(report.changed)
        self.assertIn("表 1: 適用済み（変更なし）", report.messages)
        self.assertEqual(read(self.md), after)

    def test_unedited_sheet_preserves_document_edit(self):
        original = "| h |\n|---|\n| 元 |\n\n| k |\n|---|\n| 1 |\n"
        for force in (False, True):
            with self.subTest(force=force):
                write(self.md, original)
                self.export()
                edit_sheet(self.xlsx, self.sheet(2), {"A2": "2"})
                edited = original.replace("元", "文書側の編集")
                write(self.md, edited)
                report = sync.import_workbook(self.xlsx, force=force)
                self.assertEqual(report.errors, [])
                self.assertIn("表 1: 変更なし", report.messages)
                self.assertEqual(read(self.md), edited.replace("| 1 |", "| 2 |"))

    def test_old_metadata_without_same(self):
        table = "| a | b |\n|---|---|\n| 1 | 2 |\n"
        write(self.md, table + "\n" + table)
        self.export()
        book = openpyxl.load_workbook(self.xlsx)
        book[workbook.META_SHEET].delete_cols(9)
        book[self.sheet(2)]["A2"] = "x"
        book.save(self.xlsx)
        self.assertIsNone(workbook.read(self.xlsx).tables[0].same)
        report = sync.import_workbook(self.xlsx)
        self.assertEqual(report.errors, [])
        after = table + "\n" + table.replace("| 1 |", "| x |")
        self.assertEqual(read(self.md), after)
        report = sync.import_workbook(self.xlsx)
        self.assertEqual(report.errors, [])
        self.assertFalse(report.changed)
        self.assertEqual(read(self.md), after)

    def test_old_metadata_applied_twice(self):
        # same の列が無いブックでも、取り込み直しで後ろの同じ表を変えない
        table = "| a | b |\n|---|---|\n| 1 | 2 |\n"
        write(self.md, table + "\n" + table)
        self.export(selectors=["1"])
        book = openpyxl.load_workbook(self.xlsx)
        book[workbook.META_SHEET].delete_cols(9)
        book[self.sheet(1)]["A2"] = "x"
        book.save(self.xlsx)
        after = table.replace("| 1 |", "| x |") + "\n" + table
        for _ in range(2):
            report = sync.import_workbook(self.xlsx)
            self.assertEqual(report.errors, [])
            self.assertEqual(read(self.md), after)
        self.assertIn("表 1: 適用済み（変更なし）", report.messages)

    def test_swapped_tables_in_document(self):
        # 文書の中で表を入れ替えた後でも、ブックで変えた表は元の本文で探して更新する
        first = "| a |\n|---|\n| 1 |\n"
        second = "| b |\n|---|\n| 2 |\n"
        write(self.md, first + "\n" + second)
        self.export()
        # 表 1 を、表 2 と同じ中身にする
        edit_sheet(self.xlsx, self.sheet(1), {"A1": "b", "A2": "2"})
        write(self.md, second + "\n" + first)
        report = sync.import_workbook(self.xlsx)
        self.assertEqual(report.errors, [])
        self.assertIn("表 2: 変更なし", report.messages)
        self.assertEqual(read(self.md), second + "\n" + second)

    def test_identical_table_count_conflict_and_force(self):
        table = "| h |\n|---|\n| 1 |\n"
        for current_count in (1, 3):
            with self.subTest(current_count=current_count):
                write(self.md, table + "\n" + table)
                self.export(selectors=["1"])
                edit_sheet(self.xlsx, self.sheet(1), {"A2": "x"})
                current = "\n".join([table] * current_count)
                write(self.md, current)
                report = sync.import_workbook(self.xlsx)
                self.assertFalse(report.changed)
                self.assertEqual(read(self.md), current)
                self.assertEqual(report.errors, [
                    "表 1: 衝突: 同じ内容の表の数が、書き出したとき（2 個）と違う（今は %d 個）ので、どの表か決められない（同じ番号の表をブックの内容で上書きするなら --force）"
                    % current_count])
                report = sync.import_workbook(self.xlsx, force=True)
                self.assertEqual(report.errors, [])
                self.assertTrue(report.warnings)
                self.assertEqual(read(self.md), current.replace("| 1 |", "| x |", 1))

    def test_unedited_html_with_holes(self):
        # セルの無い位置が、表の最後にある表と、途中（1 行目の右）にある表
        for original in ("<table>\n<tr><td>a</td><td>b</td></tr>\n<tr><td>c</td></tr>\n</table>\n",
                         "<table>\n<tr><td>c</td></tr>\n<tr><td>a</td><td>b</td></tr>\n</table>\n"):
            write(self.md, original)
            self.export()
            self.assertEqual(workbook.read(self.xlsx).tables[0].fingerprint,
                             workbook.read(self.xlsx).tables[0].model.fingerprint())
            report = sync.import_workbook(self.xlsx)
            self.assertEqual(report.errors, [])
            self.assertIn("表 1: 変更なし", report.messages)
            self.assertFalse(report.changed)
            for force in (False, True):
                with self.subTest(original=original, force=force):
                    edited = original.replace("<td>c</td>", "<td>文書側の編集</td>")
                    write(self.md, edited)
                    report = sync.import_workbook(self.xlsx, force=force)
                    self.assertEqual(report.errors, [])
                    self.assertIn("表 1: 変更なし", report.messages)
                    self.assertEqual(read(self.md), edited)

    def test_html_alignment_with_added_row(self):
        write(self.md, "<table>\n<tr><td>a</td></tr>\n</table>\n")
        self.export()
        book = openpyxl.load_workbook(self.xlsx)
        sheet = book[self.sheet(1)]
        sheet["A1"].alignment = openpyxl.styles.Alignment(wrap_text=True, vertical="top", horizontal="right")
        sheet["A2"] = "b"
        book.save(self.xlsx)
        report = sync.import_workbook(self.xlsx)
        self.assertEqual(report.errors, [])
        self.assertTrue(report.changed)
        self.assertIn('<td align="right">a</td>', read(self.md))
        model = helpers.regions(read(self.md))[0].parsed.model
        self.assertEqual((model.rows, model.cols), (2, 1))
        self.assertEqual(model.anchors()[(0, 0)].align, "right")

    def test_html_header_with_added_column(self):
        write(self.md, "<table>\n<tr><td>a</td></tr>\n</table>\n")
        self.export()
        book = openpyxl.load_workbook(self.xlsx)
        sheet = book[self.sheet(1)]
        sheet["A1"].font = openpyxl.styles.Font(bold=True)
        sheet["A1"].fill = openpyxl.styles.PatternFill(fill_type="solid", fgColor=workbook.HEADER_FILL)
        sheet["B1"] = "b"
        book.save(self.xlsx)
        report = sync.import_workbook(self.xlsx)
        self.assertEqual(report.errors, [])
        self.assertIn("<th>a</th>", read(self.md))
        model = helpers.regions(read(self.md))[0].parsed.model
        self.assertEqual((model.rows, model.cols), (1, 2))
        self.assertTrue(model.anchors()[(0, 0)].header)

    def test_html_verification_rejects_lost_structure(self):
        original = "<table>\n<tr><td>a</td></tr>\n</table>\n"
        for force in (False, True):
            with self.subTest(force=force):
                write(self.md, original)
                self.export()
                book = openpyxl.load_workbook(self.xlsx)
                book[self.sheet(1)]["A1"] = "x"
                book[self.sheet(1)]["A1"].alignment = openpyxl.styles.Alignment(horizontal="right")
                book.save(self.xlsx)
                # 配置を落とした出力でも収束はするため、ブックとの構造の比較で止める
                render = sync._render

                def lose_alignment(region, model, warnings, keep_kind=False):
                    lines, converted = render(region, model, warnings, keep_kind=keep_kind)
                    return [line.replace(' align="right"', "") for line in lines], converted

                with mock.patch.object(sync, "_render", side_effect=lose_alignment):
                    report = sync.import_workbook(self.xlsx, force=force)
                self.assertEqual(report.errors, [
                    "表 1: 書き戻した表の形・見出し・配置が、ブックの内容と違う"])
                self.assertIn("書き戻した結果の確認に失敗したので書き戻していません", report.messages)
                self.assertFalse(report.changed)
                self.assertEqual(read(self.md), original)

    def test_edit_header_only_pipe(self):
        for prefix, continuation in (("", ""), ("- ", "  ")):
            with self.subTest(prefix=prefix):
                write(self.md, prefix + "| a | b |\n" + continuation + "|---|---|\n")
                self.export()
                edit_sheet(self.xlsx, self.sheet(1), {"A1": "x"})
                report = sync.import_workbook(self.xlsx)
                self.assertEqual(report.errors, [])
                self.assertEqual(read(self.md), prefix + "| x | b |\n" + continuation + "|---|---|\n")

    def test_delete_all_pipe_body_rows(self):
        for prefix, continuation in (("", ""), ("- ", "  ")):
            with self.subTest(prefix=prefix):
                write(self.md, prefix + "| a |\n" + continuation + "|---|\n"
                      + continuation + "| 1 |\n" + continuation + "| 2 |\n")
                self.export()
                book = openpyxl.load_workbook(self.xlsx)
                title = self.sheet(1)
                book[title].delete_rows(2, 2)
                # Excel は行を消すと名前の定義も直す（openpyxl は直さないので合わせる）
                book.defined_names["mdtable_1"].attr_text = "'%s'!$A$1:$A$1" % title
                book.save(self.xlsx)
                report = sync.import_workbook(self.xlsx)
                self.assertEqual(report.errors, [])
                self.assertEqual(read(self.md), prefix + "| a |\n" + continuation + "|---|\n")

    def test_mixed_line_endings_inside_table(self):
        original = b"| h |\r\n|---|\n| a |\r\n| b |\n"
        with open(self.md, "wb") as handle:
            handle.write(original)
        self.export()
        edit_sheet(self.xlsx, self.sheet(1), {"A2": "aa"})
        report = sync.import_workbook(self.xlsx)
        self.assertEqual(report.errors, [])
        with open(self.md, "rb") as handle:
            self.assertEqual(handle.read(), b"| h |\r\n|---|\n| aa |\r\n| b |\n")

    def test_delete_last_row_with_mixed_line_endings(self):
        # 最後に残る行は変えていない行なので、消した行の改行コードに変えない
        with open(self.md, "wb") as handle:
            handle.write(b"| h |\r\n|---|\n| a |\n| b |\r\n")
        self.export()
        book = openpyxl.load_workbook(self.xlsx)
        title = self.sheet(1)
        book[title].delete_rows(3, 1)
        book.defined_names["mdtable_1"].attr_text = "'%s'!$A$1:$A$2" % title
        book.save(self.xlsx)
        report = sync.import_workbook(self.xlsx)
        self.assertEqual(report.errors, [])
        with open(self.md, "rb") as handle:
            self.assertEqual(handle.read(), b"| h |\r\n|---|\n| a |\n")

    def test_delete_last_row_without_final_newline(self):
        write(self.md, "| h |\n|---|\n| a |\n| b |")
        self.export()
        book = openpyxl.load_workbook(self.xlsx)
        title = self.sheet(1)
        book[title].delete_rows(3, 1)
        book.defined_names["mdtable_1"].attr_text = "'%s'!$A$1:$A$2" % title
        book.save(self.xlsx)
        report = sync.import_workbook(self.xlsx)
        self.assertEqual(report.errors, [])
        self.assertEqual(read(self.md), "| h |\n|---|\n| a |")

    def test_append_row_without_final_newline(self):
        original = "| h |\r\n|---|\n| a |"
        write(self.md, original)
        self.export()
        edit_sheet(self.xlsx, self.sheet(1), {"A3": "b"})
        report = sync.import_workbook(self.xlsx)
        self.assertEqual(report.errors, [])
        self.assertEqual(read(self.md), "| h |\r\n|---|\n| a |\r\n| b |")

    def test_conversion_then_reimport(self):
        # C5: パイプ表を HTML の表にした後、同じブックをもう一度取り込んでも変わらない
        self.export()
        book = openpyxl.load_workbook(self.xlsx)
        book[self.sheet(1)].merge_cells("A2:A3")
        book.save(self.xlsx)
        report = sync.import_workbook(self.xlsx)
        self.assertEqual(report.errors, [])
        self.assertIn('<td rowspan="2">lazygit</td>', read(self.md))
        after = read(self.md)
        report = sync.import_workbook(self.xlsx)
        self.assertFalse(report.changed)
        self.assertEqual(read(self.md), after)

    def test_control_character_is_excluded(self):
        # W2: 書き出せない表は対象外にして、ほかの表は書き出す
        write(self.md, "| a |\n|---|\n| x\x0cy |\n\n| b |\n|---|\n| 1 |\n")
        report = self.export()
        self.assertTrue(any("対象外" in w for w in report.warnings))
        book = openpyxl.load_workbook(self.xlsx)
        self.assertEqual(len([t for t in book.sheetnames if t.startswith("表")]), 1)


if __name__ == "__main__":
    unittest.main()
