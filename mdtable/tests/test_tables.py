"""表の構造の解析と、書き戻し（差し替え・作り直し・パイプ表から HTML の表へ）のテスト。"""

import unittest

from helpers import md_file, regions, texts

from mdtable import blocks, sync
from mdtable.document import Replacement, apply_replacements, find_tables
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
