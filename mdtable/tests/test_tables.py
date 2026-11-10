"""表の構造の解析と、書き戻し（差し替え・作り直し・パイプ表から HTML の表へ）のテスト。"""

import unittest

from helpers import md_file, regions, texts

from mdtable import blocks, sync
from mdtable.document import MarkdownError, Replacement, apply_replacements, find_tables, select_tables
from mdtable.model import Cell, TableModel


def clone(model):
    cells = [Cell(c.row, c.col, c.text, c.rowspan, c.colspan, c.header, c.align) for c in model.cells]
    return TableModel(model.kind, model.rows, model.cols, cells, set(model.holes))


def set_text(model, row, col, text):
    model.anchors()[(row, col)].text = text


def rewrite(text, number, edit):
    """表 number の内容を edit で変えて書き戻した文書と、HTML に変えた理由・確認の問題を返す。"""
    md = md_file(text)
    region = find_tables(md)[number - 1]
    model = clone(region.parsed.model)
    edit(model)
    model = sync._normalized_sheet_model(model, region)
    warnings = []
    lines, converted = sync._render(region, model, warnings)
    replacement = Replacement(region, lines)
    new_text = apply_replacements(md, [replacement])
    problems = sync.verify(md, new_text, [replacement])
    return new_text, converted, problems


class ComplexCellTest(unittest.TestCase):
    def test_link_definitions_are_recorded_only_in_markdown(self):
        bodies = [("[a]: https://example.com/", True),
                  ("[a]: https://example.com/\n続く文章", True),
                  ("- [a]: https://example.com/", True),
                  ("```\n[a]: https://example.com/\n```", False),
                  ("    [a]: https://example.com/", False),
                  ("`[a]: https://example.com/`", False),
                  ("\\[a]: https://example.com/", False)]
        for body, expected in bodies:
            with self.subTest(body=body):
                source = "<table>\n<tr><td>\n\n" + body + "\n\n</td></tr>\n</table>\n"
                table = regions(source)[0].parsed
                self.assertEqual(table.cells[0].link_definitions, expected)
        table = regions("<table><tr><td>[a]: https://example.com/</td></tr></table>\n")[0].parsed
        self.assertFalse(table.cells[0].link_definitions)

    def test_unchanged_nested_table_cell_is_preserved(self):
        for nested in ("<td><table><tr><td>入れ子</td></tr></table></td>",
                       "<td>\n\n| a |\n| --- |\n| b |\n\n</td>"):
            source = "<table>\n<tr>" + nested + "<td>元</td></tr>\n</table>\n"
            result, converted, problems = rewrite(source, 1, lambda m: set_text(m, 0, 1, "変更"))
            self.assertEqual(result, source.replace("<td>元</td>", "<td>変更</td>"))
            self.assertIsNone(converted)
            self.assertEqual(problems, [])

            def add_row(model):
                set_text(model, 0, 1, "変更")
                model.rows += 1
                model.cells.extend([Cell(1, 0, "追加"), Cell(1, 1, "")])

            result, converted, problems = rewrite(source, 1, add_row)
            self.assertIn(nested, result)
            self.assertIsNone(converted)
            self.assertEqual(problems, [])

    def test_cell_write_errors_include_position_in_both_render_paths(self):
        from mdtable.cell_syntax import NotRepresentable
        source = "<table>\n<tr><td>元</td><td>値</td></tr>\n</table>\n"
        for body in ("| a | b |\n| --- | --- |\n| 1 | 2 |", "<td>中身</td>", "<!-- 未完"):
            for regenerate in (False, True):
                with self.subTest(body=body, regenerate=regenerate):
                    region = regions(source)[0]
                    model = clone(region.parsed.model)
                    set_text(model, 0, 1, body)
                    if regenerate:
                        model.rows += 1
                        model.cells.extend([Cell(1, 0, "追加"), Cell(1, 1, "")])
                    with self.assertRaises(NotRepresentable) as caught:
                        sync._render(region, model, [])
                    self.assertEqual((caught.exception.row, caught.exception.col), (1, 2))


class TableSelectionTest(unittest.TestCase):
    def test_heading_selects_one_or_several_tables(self):
        table = "| a |\n|---|\n| b |\n\n"
        found = regions("# 導入\n\n" + table + table + "# 更新\n\n" + table)
        self.assertEqual([r.number for r in select_tables(found, [" 導入 "])], [1, 2])
        self.assertEqual([r.number for r in select_tables(found, ["更新"])], [3])
        self.assertEqual([r.heading_line for r in found], [0, 0, 10])
        self.assertEqual([r.number for r in select_tables(found, ["導入", "2", "L13"])], [1, 2, 3])

    def test_duplicate_heading_is_ambiguous(self):
        table = "| a |\n|---|\n| b |\n\n"
        found = regions("# 導入\n\n" + table + "# 導入\n\n" + table)
        with self.assertRaises(MarkdownError) as raised:
            select_tables(found, ["導入"])
        self.assertEqual(str(raised.exception),
                         "見出し「導入」が複数あります（1 行目・7 行目）。番号か L行番号 で選んでください")
        self.assertEqual([r.number for r in select_tables(found, ["L10"])], [2])

    def test_duplicate_heading_without_table_is_not_ambiguous(self):
        found = regions("# 導入\n\n本文\n\n# 導入\n\n| a |\n|---|\n")
        self.assertEqual([r.number for r in select_tables(found, ["導入"])], [1])

    def test_missing_heading_requires_exact_match(self):
        found = regions("# 導入の手順\n\n| a |\n|---|\n")
        with self.assertRaises(MarkdownError) as raised:
            select_tables(found, ["導入"])
        self.assertEqual(str(raised.exception),
                         "直前の見出しが「導入」の表がありません（--table は番号・L行番号・直前の見出しの文字）")

    def test_numeric_and_line_like_headings(self):
        # 見出しが「2026」や「L12」のとき、その番号・行番号の表が無ければ、見出しの文字として選ぶ
        table = "| a |\n|---|\n| b |\n\n"
        found = regions("# 2026\n\n" + table + "# L99\n\n" + table + "# 2\n\n" + table)
        self.assertEqual([r.number for r in select_tables(found, ["2026"])], [1])
        self.assertEqual([r.number for r in select_tables(found, ["L99"])], [2])
        # 番号や行番号に当たる表があれば、そちらを選ぶ（見出し「2」の表 3 ではなく、表 2）
        self.assertEqual([r.number for r in select_tables(found, ["2"])], [2])
        self.assertEqual([r.number for r in select_tables(found, ["L3"])], [1])
        with self.assertRaises(MarkdownError) as raised:
            select_tables(found, ["9"])
        self.assertEqual(str(raised.exception), "表 9 はありません（表は 3 個）")
        with self.assertRaises(MarkdownError) as raised:
            select_tables(found, ["L500"])
        self.assertEqual(str(raised.exception), "500 行目を含む表がありません")

    def test_heading_selects_excluded_table_too(self):
        # 対象外の表も、見出しで選べる（書き出すときに、対象外の理由が出る）
        found = regions("# 崩れた表\n\n<table>\n<tr><td>a\n\n    </td></tr>\n</table>\n")
        self.assertIsNotNone(found[0].excluded)
        self.assertEqual(select_tables(found, ["崩れた表"]), [found[0]])

    def test_heading_line_without_heading_and_setext_heading(self):
        found = regions("| a |\n|---|\n\n題\n===\n\n| b |\n|---|\n")
        self.assertEqual([(r.heading, r.heading_line) for r in found], [("", None), ("題", 3)])
        self.assertEqual(select_tables(found, ["題"]), [found[1]])


class HtmlStructureTest(unittest.TestCase):
    def test_markdown_chunk(self):
        # T1
        found = regions("<table>\n<tr>\n<td>\n\n- a\n- b\n\n</td>\n</tr>\n</table>\n")
        self.assertEqual(texts(found[0]), [["- a\n- b"]])

    def test_indented_chunk_is_excluded(self):
        # T2: 字下げで </td> がコードブロックになる表は対象外
        text = "<table>\n  <tr>\n    <td>\n\n    - a\n\n    </td>\n  </tr>\n</table>\n"
        self.assertIsNotNone(regions(text)[0].excluded)

    def test_pre_with_blank_line(self):
        # T3・T4
        self.assertIsNotNone(regions("<table><tr><td><pre>\na\n\nb\n</pre></td></tr></table>\n")[0].excluded)
        self.assertIsNotNone(
            regions("<table><tr><td>\n<pre>\na\n\nb\n</pre>\n</td></tr></table>\n")[0].excluded)
        found = regions("<table><tr><td>\n\n<pre>\na\n\nb\n</pre>\n\n</td></tr></table>\n")
        self.assertIsNone(found[0].excluded)
        self.assertEqual(texts(found[0]), [["```\na\n\nb\n```"]])

    def test_fence_hides_tags(self):
        # T5
        found = regions("<table><tr><td>\n\n```\n</td>\n```\n\n</td></tr></table>\n")
        self.assertEqual(texts(found[0]), [["```\n</td>\n```"]])

    def test_end_tag_in_paragraph(self):
        # T6
        self.assertIsNotNone(regions("<table><tr><td>\n\n- a\n\n<b>x</b></td></tr></table>\n")[0].excluded)

    def test_implicit_end_tags(self):
        # T7・T8
        self.assertEqual(texts(regions("<table><tr><td>a<td>b<tr><td>c</table>\n")[0]),
                         [["a", "b"], ["c", None]])
        found = regions("<table><tr><th>x</td><td>y</td></tr></table>\n")[0]
        self.assertEqual(texts(found), [["x", "y"]])
        self.assertTrue(found.parsed.model.anchors()[(0, 0)].header)

    def test_spans(self):
        # T9
        text = ('<table><tbody><tr><td rowspan="0">a</td><td>b</td></tr><tr><td>c</td></tr>'
                '<tr><td>d</td></tr></tbody></table>\n')
        self.assertEqual(regions(text)[0].parsed.model.anchors()[(0, 0)].rowspan, 3)
        text = '<table><tr><td rowspan="5">a</td><td>b</td></tr><tr><td>c</td></tr></table>\n'
        self.assertEqual(regions(text)[0].parsed.model.anchors()[(0, 0)].rowspan, 2)
        text = '<table><tr><td colspan="2px">a</td></tr><tr><td colspan="0">b</td></tr></table>\n'
        anchors = regions(text)[0].parsed.model.anchors()
        self.assertEqual((anchors[(0, 0)].colspan, anchors[(1, 0)].colspan), (2, 1))
        text = ('<table><tr><td rowspan="2">a</td><td>b</td></tr>'
                '<tr><td colspan="2">c</td></tr></table>\n')
        self.assertIsNone(regions(text)[0].excluded)

    def test_text_between_rows(self):
        # T10
        self.assertIsNotNone(regions("<table><tr><td>a</td></tr>\nfoo\n<tr><td>b</td></tr></table>\n")[0].excluded)
        nested = "<table><tr><td><table><tr><td>x</td></tr></table></td></tr></table>\n"
        found = regions(nested)
        self.assertEqual(len(found), 1)
        self.assertIn("<table>", texts(found[0])[0][0])

    def test_text_around_table(self):
        # T11
        found = regions('<div align="center"><table>\n<tr><td>a</td></tr>\n</table></div>\n')[0]
        self.assertEqual(found.head, '<div align="center">')
        self.assertEqual(found.tail, "</div>")

    def test_blockquote(self):
        # T12
        found = regions("> <table>\n> <tr><td>\n>\n> - a\n>\n> </td></tr>\n> </table>\n")
        self.assertEqual(texts(found[0]), [["- a"]])

    def test_tricky_tokens(self):
        # T13
        found = regions('<table><tr><td title="<table>">a&ampb</td><!-- </td> --><td>c</td></tr></table>\n')
        self.assertEqual(texts(found[0])[0][0], "a&b")


class PatchTest(unittest.TestCase):
    def test_cell_becomes_chunk(self):
        # R1
        new, _converted, problems = rewrite(
            "<table>\n<tr><td>a</td><td>c</td></tr>\n</table>\n", 1, lambda m: set_text(m, 0, 0, "- x"))
        self.assertEqual(problems, [])
        self.assertEqual(new, "<table>\n<tr><td>\n\n- x\n\n</td><td>c</td></tr>\n</table>\n")

    def test_implicit_end(self):
        # R2
        new, _c, problems = rewrite("<table>\n<tr><td>a\n    <td>b\n</table>\n", 1,
                                    lambda m: set_text(m, 0, 0, "- x"))
        self.assertEqual(problems, [])
        self.assertEqual(new, "<table>\n<tr><td>\n\n- x\n\n<td>b\n</table>\n")

    def test_blockquote_prefixes(self):
        # R3
        new, _c, problems = rewrite("> <table>\n> <tr><td>a</td><td>c</td></tr>\n> </table>\n", 1,
                                    lambda m: set_text(m, 0, 0, "- x"))
        self.assertEqual(problems, [])
        self.assertEqual(new, "> <table>\n> <tr><td>\n>\n> - x\n>\n> </td><td>c</td></tr>\n> </table>\n")

    def test_header_to_data(self):
        # R4
        def edit(model):
            model.anchors()[(0, 0)].header = False
        new, _c, problems = rewrite('<table>\n<tr><th scope="col" class=x>a</th></tr>\n</table>\n', 1, edit)
        self.assertEqual(problems, [])
        self.assertIn('<td scope="col" class=x>a</td>', new)

    def test_alignment(self):
        def edit(model):
            model.anchors()[(0, 0)].align = "center"
        new, _c, problems = rewrite("<table>\n<tr><td>a</td></tr>\n</table>\n", 1, edit)
        self.assertEqual(problems, [])
        self.assertIn('<td align="center">a</td>', new)

    def test_ordered_list_container(self):
        # N7: リストの中の HTML の表の差し替え
        text = "1. 手順\n\n   <table>\n   <tr><td>a</td></tr>\n   </table>\n2. 次\n"
        new, _c, problems = rewrite(text, 1, lambda m: set_text(m, 0, 0, "- x"))
        self.assertEqual(problems, [])
        self.assertEqual(new, "1. 手順\n\n   <table>\n   <tr><td>\n\n   - x\n\n   </td></tr>\n   </table>\n2. 次\n")


class RegenerateTest(unittest.TestCase):
    def test_add_row_keeps_other_rows(self):
        text = "<table>\n<tr><td>a</td><td>b</td></tr>\n<tr><td>c</td><td>d</td></tr>\n</table>\n"

        def edit(model):
            model.cells.append(Cell(2, 0, "e"))
            model.cells.append(Cell(2, 1, "f"))
            model.rows = 3
        new, _c, problems = rewrite(text, 1, edit)
        self.assertEqual(problems, [])
        self.assertEqual(new, "<table>\n<tr><td>a</td><td>b</td></tr>\n<tr><td>c</td><td>d</td></tr>\n"
                              "<tr>\n<td>e</td>\n<td>f</td>\n</tr>\n</table>\n")

    def test_tfoot_stays(self):
        # R7
        text = ("<table>\n<tbody>\n<tr><td>a</td></tr>\n<tr><td>b</td></tr>\n</tbody>\n"
                "<tfoot>\n<tr><td>合計</td></tr>\n</tfoot>\n</table>\n")

        def edit(model):
            model.cells = [c for c in model.cells if c.row != 1]
            for cell in model.cells:
                if cell.row == 2:
                    cell.row = 1
            model.rows = 2
        new, _c, problems = rewrite(text, 1, edit)
        self.assertEqual(problems, [])
        self.assertIn("<tfoot>\n<tr><td>合計</td></tr>\n</tfoot>", new)
        self.assertNotIn("<td>b</td>", new)


class PipeToHtmlTest(unittest.TestCase):
    def test_merge_converts_and_keeps_following_list(self):
        # R5: 表の直後に空行なしでリストが続く文書
        text = "| a | b |\n|---|---|\n| 1 | 2 |\n| 3 | 4 |\n- **list**: item\n"

        def edit(model):
            model.cells = [c for c in model.cells if (c.row, c.col) != (2, 0)]
            model.anchors()[(1, 0)].rowspan = 2
        new, converted, problems = rewrite(text, 1, edit)
        self.assertEqual(problems, [])
        self.assertIn("セル結合", converted)
        self.assertIn('<td rowspan="2">1</td>', new)
        self.assertIn("</table>\n\n- **list**: item\n", new)
        doc = blocks.parse_text(new)
        self.assertEqual([n.kind for n in doc.root.children], [blocks.HTML_BLOCK, blocks.LIST])

    def test_pipes_are_unescaped_in_chunks(self):
        # R6: パイプ表の \| は HTML の表では | に戻る
        text = "| a | b |\n|---|---|\n| `x\\|y` | 2 |\n"

        def edit(model):
            set_text(model, 1, 1, "```\ncode\n```")
        new, converted, problems = rewrite(text, 1, edit)
        self.assertEqual(problems, [])
        self.assertIn("`x|y`", new)
        self.assertNotIn("\\|", new)

    def test_in_list_item(self):
        # N8
        text = "- a\n\n  | x |\n  |---|\n  | 1 |\n- b\n"
        new, _converted, problems = rewrite(text, 1, lambda m: set_text(m, 1, 0, "```\nc\n```"))
        self.assertEqual(problems, [])
        self.assertTrue(new.startswith("- a\n\n  <table>\n"))
        self.assertTrue(new.endswith("  </table>\n- b\n"))
        for line in new.split("\n")[2:-2]:
            self.assertTrue(line == "" or line.startswith("  "), line)


class PipeRenderTest(unittest.TestCase):
    def test_edit_keeps_other_rows(self):
        # G1
        text = "> | a | b |\n> |---|---|\n> | 1 | 2 |\n> | 3 | 4 |\n"
        new, converted, problems = rewrite(text, 1, lambda m: set_text(m, 1, 1, "x"))
        self.assertIsNone(converted)
        self.assertEqual(problems, [])
        self.assertEqual(new, "> | a | b |\n> |---|---|\n> | 1 | x |\n> | 3 | 4 |\n")

    def test_new_column(self):
        # G2: 列を足すと区切り行も作り直す
        text = "| a | b |\n| --- | --- |\n| 1 | 2 |\n"

        def edit(model):
            model.cells.append(Cell(0, 2, "c", header=True))
            model.cells.append(Cell(1, 2, "3"))
            model.cols = 3
        new, _c, problems = rewrite(text, 1, edit)
        self.assertEqual(problems, [])
        self.assertEqual(new, "| a | b | c |\n| --- | --- | --- |\n| 1 | 2 | 3 |\n")

    def test_identical_rows(self):
        # G3: 同じ行が多くても、挿入した行だけが新しい
        rows = "".join("| |\n" for _ in range(300))
        text = "| a |\n|---|\n" + rows

        def edit(model):
            for cell in model.cells:
                if cell.row >= 150:
                    cell.row += 1
            model.cells.append(Cell(150, 0, "new"))
            model.rows += 1
        new, _c, problems = rewrite(text, 1, edit)
        self.assertEqual(problems, [])
        self.assertEqual(new.count("| new |"), 1)
        self.assertEqual(new.count("| |\n"), 300)

    def test_without_leading_pipes(self):
        # G4
        text = "a | b\n--- | ---\n1 | 2\n"

        def edit(model):
            model.cells.append(Cell(2, 0, ""))
            model.cells.append(Cell(2, 1, "x"))
            model.rows = 3
        new, _c, problems = rewrite(text, 1, edit)
        self.assertEqual(problems, [])
        self.assertTrue(new.endswith("1 | 2\n| | x\n"), new)

    def test_alignment_majority(self):
        text = "| a | b |\n|---|---|\n| 1 | 2 |\n| 3 | 4 |\n"

        def edit(model):
            for cell in model.cells:
                if cell.col == 1:
                    cell.align = "right"
        new, _c, problems = rewrite(text, 1, edit)
        self.assertEqual(problems, [])
        self.assertIn("|---|---:|", new)


if __name__ == "__main__":
    unittest.main()
