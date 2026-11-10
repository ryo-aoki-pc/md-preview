"""表の種類だけを変える処理と、書き戻す前の確認。"""

import os
import tempfile
import unittest
from unittest import mock

from helpers import ROOT, md_file, regions
from mdtable import html_table, pipe_table
from mdtable.document import Replacement
from mdtable.sync import MdTableError, convert, verify


PIPE = "| 左 | 中央 | 右 |\n| :--- | :---: | ---: |\n| a | b | c |\n"
HTML = ("<table>\n<tr><th>見出し</th><th>説明</th></tr>\n"
        "<tr><td>a</td><td>b</td></tr>\n</table>\n")


class ConvertTest(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory(prefix="mdtable-convert-", dir=ROOT)
        self.addCleanup(self.directory.cleanup)
        self.path = os.path.join(self.directory.name, "文書.md")

    def write(self, text, bom=False):
        with open(self.path, "wb") as handle:
            handle.write((b"\xef\xbb\xbf" if bom else b"") + text.encode("utf-8"))

    def read_bytes(self):
        with open(self.path, "rb") as handle:
            return handle.read()

    def read(self):
        return self.read_bytes().decode("utf-8-sig")

    def assert_refused(self, text, reason):
        self.write(text)
        before = self.read_bytes()
        report = convert(self.path, ["1"], "pipe")
        self.assertTrue(report.errors)
        self.assertIn(reason, "\n".join(report.errors))
        self.assertEqual(report.messages[-1], "問題があるので書き戻していません")
        self.assertFalse(report.changed)
        self.assertEqual(report.diff, "")
        self.assertEqual(self.read_bytes(), before)
        return report

    def test_pipe_html_pipe_preserves_models_and_other_lines(self):
        before = "# 文書\r\n\r\n前の文章  \n\n"
        after = "\n後の文章  \r\n\r\n| 選ばない |\n|---|\n| そのまま |"
        source = PIPE.replace("| a | b | c |", "| **強調** | `code` | a\\|b |")
        self.write(before + source + after, bom=True)
        model = regions(source)[0].parsed.model
        for target in ("html", "pipe"):
            report = convert(self.path, ["1"], target)
            self.assertFalse(report.errors, report.errors)
            self.assertTrue(report.changed)
            self.assertTrue(self.read_bytes().startswith(b"\xef\xbb\xbf"))
            current = self.read()
            self.assertTrue(current.startswith(before))
            self.assertTrue(current.endswith(after))
            region = regions(current)[0]
            self.assertEqual(region.kind, target)
            self.assertTrue(region.parsed.model.same_content(model))

    def test_html_cells_breaks_lists_pipes_and_alignment(self):
        text = ("<table>\n<thead><tr><th align='left'>左</th>"
                "<th style='text-align: center;'>中央</th><th align='right'>右</th></tr></thead>\n"
                "<tbody><tr><td align='left'>a<br>b</td><td style='TEXT-ALIGN: center'>"
                "<ul><li>項目</li><li>次</li></ul></td><td align='right'>a|b</td></tr></tbody>\n"
                "</table>\n")
        self.write(text)
        report = convert(self.path, ["1"], "pipe")
        self.assertFalse(report.errors, report.errors)
        self.assertEqual(self.read(), "| 左 | 中央 | 右 |\n| :--- | :---: | ---: |\n"
                         "| a<br>b | <ul><li>項目</li><li>次</li></ul> | a\\|b |\n")
        self.assertTrue(regions(self.read())[0].parsed.model.same_content(regions(text)[0].parsed.model))

    def test_empty_cells_and_header_only(self):
        self.write("<table><tr><th></th><th>列</th></tr></table>")
        report = convert(self.path, ["1"], "pipe")
        self.assertFalse(report.errors, report.errors)
        self.assertEqual(self.read(), "| | 列 |\n| --- | --- |")

    def test_container_prefixes_and_tight_warning(self):
        cases = [("- ", "  "), ("> ", "> "), ("> - ", ">   "), ("1. ", "   ")]
        for first, continuation in cases:
            with self.subTest(first=first):
                source = "\n".join((first if i == 0 else continuation) + line
                                   for i, line in enumerate(PIPE.splitlines())) + "\n"
                self.write(source)
                original = regions(source)[0]
                for target in ("html", "pipe"):
                    report = convert(self.path, ["1"], target)
                    self.assertFalse(report.errors, report.errors)
                    self.assertTrue(self.read().startswith(first))
                    self.assertTrue(all(line.startswith(continuation) for line in self.read().splitlines()[1:]))
                    again = regions(self.read())[0]
                    self.assertEqual(again.location, original.location)
                    self.assertTrue(again.parsed.model.same_content(original.parsed.model))
                    if target == "html" and "箇条書き" in original.location:
                        self.assertIn("詰めたリスト", "\n".join(report.warnings))

    def test_html_on_marker_line_uses_continuation_prefix(self):
        self.write("- <table><tr><th>a</th></tr><tr><td>b</td></tr></table>\n")
        report = convert(self.path, ["1"], "pipe")
        self.assertFalse(report.errors, report.errors)
        self.assertEqual(self.read(), "- | a |\n  | --- |\n  | b |\n")

    def test_refuses_merges(self):
        report = self.assert_refused("<table><tr><th colspan='2'>a</th></tr></table>\n", "セル結合")
        self.assertIn("1 行 1 列", report.errors[0])
        self.assert_refused("<table><tr><th>a</th></tr><tr><td rowspan='2'>b</td></tr>"
                            "<tr></tr></table>\n", "セル結合")

    def test_refuses_holes(self):
        report = self.assert_refused("<table><tr><th>a</th><th>b</th></tr><tr><td>c</td></tr></table>\n",
                                     "セルの無い位置")
        self.assertIn("2 行 2 列", report.errors[0])

    def test_refuses_header_shape(self):
        for row in ("<tr><td>a</td></tr>", "<tr><th>a</th></tr><tr><th>b</th></tr>",
                    "<tr><th>a</th><td>b</td></tr>"):
            with self.subTest(row=row):
                self.assert_refused("<table>" + row + "</table>\n", "1 行目はすべて見出しセル")

    def test_refuses_mixed_alignment(self):
        report = self.assert_refused("<table><tr><th align='left'>a</th></tr>"
                                     "<tr><td align='right'>b</td></tr></table>\n", "配置が違う")
        self.assertIn("2 行 1 列", report.errors[0])
        self.assert_refused("<table><tr><th>a</th></tr><tr><td align='left'>b</td></tr></table>\n",
                            "配置が違う")

    def test_refuses_justify(self):
        for attribute in ("align='justify'", "style='text-align: justify'"):
            self.assert_refused("<table><tr><th " + attribute + ">a</th></tr></table>\n", "justify")

    def test_refuses_caption(self):
        self.assert_refused("<table><caption>題</caption><tr><th>a</th></tr></table>\n", "<caption>")

    def test_refuses_colgroups(self):
        for tags in ("<colgroup><col></colgroup>", "<col>"):
            self.assert_refused("<table>" + tags + "<tr><th>a</th></tr></table>\n", "<colgroup>")

    def test_refuses_tfoot(self):
        self.assert_refused("<table><tr><th>a</th></tr><tfoot><tr><td>b</td></tr></tfoot></table>\n",
                            "<tfoot>")

    def test_refuses_attributes_on_each_structural_element(self):
        source = ("<table><thead><tr><th>a</th></tr></thead>"
                  "<tbody><tr><td>b</td></tr></tbody></table>\n")
        for tag in ("table", "thead", "tbody", "tr", "th", "td"):
            with self.subTest(tag=tag):
                report = self.assert_refused(source.replace("<" + tag + ">", "<" + tag + " class='x'>", 1),
                                             "属性（class）")
                if tag in ("tr", "th", "td"):
                    self.assertRegex(report.errors[0], r"\d 行 \d 列")
        for attribute in ("style='color: red'", "style='text-align: left; color: red'", "rowspan='0'",
                          "colspan='1x'"):
            self.assert_refused("<table><tr><th " + attribute + ">a</th></tr></table>\n", "属性")

    def test_allows_unit_spans_and_alignment_attributes(self):
        self.write("<table><thead><tr><th rowspan='1' colspan='1' align='center'>a</th></tr>"
                   "</thead><tbody><tr><td style='text-align: center'>b</td></tr></tbody></table>\n")
        report = convert(self.path, ["1"], "pipe")
        self.assertFalse(report.errors, report.errors)
        self.assertEqual(self.read(), "| a |\n| :---: |\n| b |\n")

    def test_refuses_alignment_on_table_section_and_row(self):
        # 配置として読むのはセルの属性だけ。<table>・<tbody>・<tr> の配置は、パイプ表にすると消えるので変えない
        cases = [
            ("<table align='center'>\n<tr><th>h</th></tr>\n<tr><td>a</td></tr>\n</table>\n", "<table>"),
            ("<table>\n<tr><th>h</th></tr>\n<tbody style='text-align: right'><tr><td>a</td></tr></tbody>\n</table>\n",
             "<tbody>"),
            ("<table>\n<tr align='right'><th>h</th></tr>\n<tr><td>a</td></tr>\n</table>\n", "<tr>"),
        ]
        for text, tag in cases:
            with self.subTest(tag=tag):
                self.assert_refused(text, "%s にパイプ表では残せない属性" % tag)

    def test_refuses_nested_table_in_cell(self):
        report = self.assert_refused(
            "<table>\n<tr><th>h</th></tr>\n"
            "<tr><td><table><tr><th>入れ子</th></tr><tr><td>データ</td></tr></table></td></tr>\n</table>\n",
            "セルの中に入れ子の表（<table> など）がある")
        self.assertIn("2 行 1 列", report.errors[0])

    def test_refuses_two_rows_in_thead(self):
        self.assert_refused(
            "<table>\n<thead>\n<tr><th>見出し 1</th></tr>\n<tr><td>見出し 2</td></tr>\n</thead>\n"
            "<tbody>\n<tr><td>データ</td></tr>\n</tbody>\n</table>\n", "<thead> に 2 行以上ある")

    def test_code_with_pipe_and_table_tag_text(self):
        # コードの中の | は \| と書け、読み直すと同じ文字になる。コードの中の <table> の文字は入れ子の表ではない
        self.write("<table>\n<tr><th>h</th></tr>\n<tr><td><code>a|b</code> と <code>&lt;table&gt;</code></td></tr>\n</table>\n")
        report = convert(self.path, ["1"], "pipe")
        self.assertFalse(report.errors, report.errors)
        self.assertEqual(self.read(), "| h |\n| --- |\n| `a\\|b` と `<table>` |\n")

    def test_table_in_description_list(self):
        # 説明リストの説明の中の表も、接頭辞を保って変える
        text = "用語\n: <table>\n  <tr><th>h</th></tr>\n  <tr><td>a</td></tr>\n  </table>\n"
        self.write(text)
        location = regions(text)[0].location
        self.assertIn("説明リスト", location)
        report = convert(self.path, ["1"], "pipe")
        self.assertFalse(report.errors, report.errors)
        self.assertEqual(self.read(), "用語\n: | h |\n  | --- |\n  | a |\n")
        self.assertEqual(regions(self.read())[0].location, location)

    def test_refuses_interstitial_comments(self):
        for text in ("<table><!-- 注 --><tr><th>a</th></tr></table>\n",
                     "<table><tr><!-- 注 --><th>a</th></tr></table>\n"):
            self.assert_refused(text, "表の構造の間にコメント")

    def test_refuses_text_before_or_after_table_on_same_line(self):
        for text in ("<div><table><tr><th>a</th></tr></table>\n",
                     "<table><tr><th>a</th></tr></table>後ろ\n"):
            self.assert_refused(text, "同じ行の文字")

    def test_refuses_code_block_and_task_cells(self):
        for body in ("<pre><code>echo test\n</code></pre>",
                     "<ul><li><input type='checkbox'>未着手</li></ul>"):
            report = self.assert_refused("<table><tr><th>a</th></tr><tr><td>" + body + "</td></tr></table>\n",
                                         "パイプ表のセルに書けない")
            self.assertIn("2 行 1 列", report.errors[0])

    def test_refuses_when_blank_line_changes_following_block(self):
        text = HTML + "後ろの行\n"
        region = regions(text)[0]
        self.assertTrue(region.next_nonblank)
        self.assertEqual(pipe_table.render_new(region, region.parsed.model, [])[-1], "")
        self.assert_refused(text, "表の前後の Markdown の解釈が変わる")

    def test_adds_blank_after_new_html_when_next_block_stays_the_same(self):
        text = "| a |\n| --- |\n# 次の見出し\n"
        self.write(text)
        report = convert(self.path, ["1"], "html")
        self.assertFalse(report.errors, report.errors)
        self.assertIn("</table>\n\n# 次の見出し\n", self.read())

    def test_already_same_kind_does_not_rewrite(self):
        for text, target, name in ((PIPE, "pipe", "Markdown"), (HTML, "html", "HTML")):
            self.write(text)
            with mock.patch("mdtable.sync.write_text_atomic") as write:
                report = convert(self.path, ["1"], target)
            self.assertFalse(report.errors)
            self.assertFalse(report.changed)
            self.assertIn("もう " + name + " の表", report.messages[0])
            write.assert_not_called()

    def test_excluded_table_is_a_problem_even_with_same_kind(self):
        text = "<table><tr><td><pre>\na\n\nb\n</pre></td></tr></table>\n"
        self.assertTrue(regions(text)[0].excluded)
        for target in ("html", "pipe"):
            self.write(text)
            report = convert(self.path, ["1"], target)
            self.assertIn("対象外なので変えられない", report.errors[0])
            self.assertFalse(report.changed)
            self.assertEqual(self.read(), text)

    def test_one_problem_prevents_all_replacements(self):
        source = HTML + "\n<table><tr><td>見出しなし</td></tr></table>\n"
        self.write(source)
        report = convert(self.path, ["1", "2"], "pipe")
        self.assertIn("Markdown の表に変える", report.messages[0])
        self.assertIn("表 2:", report.errors[0])
        self.assertFalse(report.changed)
        self.assertEqual(self.read(), source)

    def test_dry_run_outputs_diff_and_preserves_bom_crlf(self):
        source = HTML.replace("\n", "\r\n")
        self.write(source, bom=True)
        before = self.read_bytes()
        report = convert(self.path, ["1"], "pipe", dry_run=True)
        self.assertFalse(report.errors, report.errors)
        self.assertTrue(report.changed)
        self.assertIn("-<table>\r\n", report.diff)
        self.assertIn("+| 見出し | 説明 |\r\n", report.diff)
        self.assertEqual(self.read_bytes(), before)
        report = convert(self.path, ["1"], "pipe")
        self.assertFalse(report.errors, report.errors)
        self.assertEqual(self.read_bytes(), b"\xef\xbb\xbf" +
                         "| 見出し | 説明 |\r\n| --- | --- |\r\n| a | b |\r\n".encode("utf-8"))

    def test_multiple_selectors_and_unselected_problem(self):
        source = "# 一組\n\n" + HTML + "\n" + HTML + "\n# 別\n\n<table><tr><td>x</td></tr></table>\n"
        self.write(source)
        report = convert(self.path, ["一組", "L4", "2"], "pipe")
        self.assertFalse(report.errors, report.errors)
        self.assertEqual(len(report.messages), 2)
        self.assertEqual([r.kind for r in regions(self.read())], ["pipe", "pipe", "html"])
        self.assertTrue(self.read().endswith("# 別\n\n<table><tr><td>x</td></tr></table>\n"))

    def test_api_requires_selector_and_known_target(self):
        for selectors, target in (([], "pipe"), (["1"], "other")):
            with self.assertRaises(MdTableError):
                convert(self.path, selectors, target)


class ConversionVerifyTest(unittest.TestCase):
    def test_model_differences_block_conversion(self):
        md = md_file("| a | b |\n| --- | --- |\n| c | d |\n")
        region = regions(md.text())[0]
        lines = html_table.render_from_pipe(region, region.parsed.model, [])
        replacement = Replacement(region, lines, region.parsed.model, target_kind="html")
        self.assertEqual(verify(md, "\n".join(lines) + "\n", [replacement]), [])
        mutations = [lambda text: text.replace("<td>c</td>", "<td>違う</td>"),
                     lambda text: text.replace("<td>c</td>", "<th>c</th>"),
                     lambda text: text.replace("<td>c</td>", '<td align="right">c</td>'),
                     lambda text: text.replace("<td>c</td>", '<td colspan="2">c</td>').replace("<td>d</td>\n", ""),
                     lambda text: text.replace("<td>d</td>\n", ""),
                     lambda text: text.replace("<tr>\n<td>c</td>\n<td>d</td>\n</tr>\n", "")]
        for mutate in mutations:
            with self.subTest(mutate=mutate):
                text = mutate("\n".join(lines) + "\n")
                replacement = Replacement(region, text.splitlines(), region.parsed.model, target_kind="html")
                problems = verify(md, text, [replacement])
                self.assertEqual(len(problems), 1, problems)
                # 何が変わるか（セルの番地と前後の文字か、行・列の数）を括弧で付ける
                self.assertRegex(problems[0], r"^表 1: 種類を変えると、表の中身が変わる（.+）$")

    def test_collapsed_spaces_are_reported_with_the_cell(self):
        # HTML の表は、読み直すと連続した空白が 1 つにまとまる。中身が変わるので変えず、どのセルかを出す
        with tempfile.TemporaryDirectory(prefix="mdtable-verify-", dir=ROOT) as directory:
            path = os.path.join(directory, "文書.md")
            original = "| 名前 | 値 |\n| --- | --- |\n| /dev/sda  16G | x |\n"
            with open(path, "w", encoding="utf-8", newline="") as handle:
                handle.write(original)
            report = convert(path, ["1"], "html")
            self.assertEqual(report.errors, [
                "表 1: 種類を変えると、表の中身が変わる（A2: 「/dev/sda  16G」→「/dev/sda 16G」）"])
            self.assertFalse(report.changed)
            with open(path, encoding="utf-8", newline="") as handle:
                self.assertEqual(handle.read(), original)

    def test_extra_cells_are_warned(self):
        # 見出しより多いセルは、種類を変えると消えるので、表を読んだときの警告を出す
        with tempfile.TemporaryDirectory(prefix="mdtable-verify-", dir=ROOT) as directory:
            path = os.path.join(directory, "文書.md")
            with open(path, "w", encoding="utf-8", newline="") as handle:
                handle.write("| a |\n| --- |\n| b | 余り |\n")
            report = convert(path, ["1"], "html", dry_run=True)
            self.assertEqual(report.errors, [])
            self.assertTrue(any("見出しより多いセルがある" in warning for warning in report.warnings), report.warnings)

    def test_conversion_requires_target_kind(self):
        md = md_file(PIPE)
        region = regions(PIPE)[0]
        replacement = Replacement(region, PIPE.splitlines(), region.parsed.model, target_kind="html")
        self.assertIn("表 1: 書き戻した表を読み直せない", verify(md, PIPE, [replacement]))

    def test_changed_cell_text_prevents_file_write(self):
        with tempfile.TemporaryDirectory(prefix="mdtable-verify-", dir=ROOT) as directory:
            path = os.path.join(directory, "文書.md")
            with open(path, "w", encoding="utf-8", newline="") as handle:
                handle.write(PIPE)
            render = html_table.render_from_pipe

            def changed(region, model, warnings):
                return [line.replace(">a</td>", ">違う</td>") for line in render(region, model, warnings)]

            with mock.patch("mdtable.sync.html_table.render_from_pipe", side_effect=changed), \
                    mock.patch("mdtable.sync.write_text_atomic") as write:
                report = convert(path, ["1"], "html")
            self.assertEqual(report.errors, ["表 1: 種類を変えると、表の中身が変わる（A2: 「a」→「違う」）"])
            self.assertFalse(report.changed)
            write.assert_not_called()
