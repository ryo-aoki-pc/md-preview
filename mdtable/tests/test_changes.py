"""行・列を突き合わせた変更の要約と、セルの表示のテスト。"""

import unittest

import helpers

from mdtable.changes import compare_models
from mdtable.model import Cell, TableModel


def model(values):
    return TableModel("html", len(values), len(values[0]),
                      [Cell(row, col, text) for row, values_row in enumerate(values)
                       for col, text in enumerate(values_row)])


class ChangesTest(unittest.TestCase):
    def test_single_cell_and_no_change(self):
        old = model([["a", "b"], ["c", "d"]])
        new = model([["a", "b"], ["c", "変更"]])
        changes = compare_models(old, new)
        self.assertEqual(changes.summary(), "（セル 1 個を変更）")
        cell = changes.cells[0]
        self.assertEqual((cell.row, cell.col, cell.old_text, cell.new_text, cell.kind),
                         (1, 1, "d", "変更", "文字"))
        self.assertEqual(changes.details(), ["  B2: 「d」→「変更」"])
        self.assertEqual(compare_models(old, old).summary(), "")
        self.assertEqual(compare_models(old, old).details(), [])

    def test_insert_middle_row_does_not_change_later_cells(self):
        old = model([["a", "b"], ["c", "d"], ["e", "f"]])
        new = model([["a", "b"], ["追加", "行"], ["c", "d"], ["e", "f"]])
        changes = compare_models(old, new)
        self.assertEqual(changes.summary(), "（1 行を追加）")
        self.assertEqual(changes.cells, [])

    def test_rows_and_columns_changed_together(self):
        # 行と列を同時に足しても・消しても、元のセルを「変更」と数えない
        cases = [
            ("途中に 1 列、末尾に 1 行", [["a", "b"], ["c", "d"]],
             [["a", "x", "b"], ["c", "y", "d"], ["e", "z", "f"]], "（1 行を追加、1 列を追加）"),
            ("先頭に 1 列、先頭に 1 行", [["a", "b"], ["c", "d"]],
             [["p", "q", "r"], ["s", "a", "b"], ["t", "c", "d"]], "（1 行を追加、1 列を追加）"),
            ("1 行と 1 列を消す", [["h", "k", "m"], ["1", "2", "3"], ["4", "5", "6"]],
             [["h", "m"], ["4", "6"]], "（1 行を削除、1 列を削除）"),
        ]
        for name, old, new, summary in cases:
            with self.subTest(name=name):
                changes = compare_models(model(old), model(new))
                self.assertEqual(changes.summary(), summary)
                self.assertEqual(changes.cells, [])

    def test_added_column_next_to_edited_column(self):
        # 列を足すのと同時に隣の列の文字を変えても、足した列と変えた列を取り違えない
        old = model([["h1", "h2"], ["a", "b"], ["c", "d"]])
        new = model([["h1", "new", "h2"], ["a", "x", "B"], ["c", "y", "d"]])
        changes = compare_models(old, new)
        self.assertEqual(changes.summary(), "（セル 1 個を変更、1 列を追加）")
        self.assertEqual(changes.details(), ["  C2: 「b」→「B」"])

    def test_repeated_rows_and_empty_table(self):
        old = model([["x"], ["same"], ["same"], ["y"]])
        new = model([["x"], ["same"], ["new"], ["same"], ["y"]])
        self.assertEqual(compare_models(old, new).summary(), "（1 行を追加）")
        empty = TableModel("html", 0, 0, [])
        self.assertEqual(compare_models(empty, model([["a"]])).summary(), "（1 行を追加、1 列を追加）")
        self.assertEqual(compare_models(model([["a"]]), empty).summary(), "（1 行を削除、1 列を削除）")
        self.assertEqual(compare_models(empty, empty).summary(), "")

    def test_large_table_with_merges_is_fast(self):
        # 結合のある大きな表でも、結合で隠れる位置を毎回探し直さない
        import time

        def big(rows, changed):
            cells = []
            for row in range(rows):
                cells.append(Cell(row, 0, "項目 %d" % row if row != changed else "変えた"))
                if row % 2 == 0:
                    cells.append(Cell(row, 1, "結合 %d" % row, rowspan=2))
            return TableModel("html", rows, 2, cells)

        started = time.perf_counter()
        changes = compare_models(big(2000, -1), big(2000, 1000))
        self.assertEqual(changes.summary(), "（セル 1 個を変更）")
        self.assertLess(time.perf_counter() - started, 5)

    def test_delete_middle_row(self):
        old = model([["a"], ["b"], ["c"]])
        new = model([["a"], ["c"]])
        changes = compare_models(old, new)
        self.assertEqual(changes.summary(), "（1 行を削除）")
        self.assertEqual(changes.cells, [])

    def test_insert_and_delete_columns(self):
        old = model([["a", "b"], ["c", "d"]])
        new = model([["a", "追加", "b"], ["c", "列", "d"]])
        self.assertEqual(compare_models(old, new).summary(), "（1 列を追加）")
        self.assertEqual(compare_models(new, old).summary(), "（1 列を削除）")

    def test_columns_then_rows(self):
        old = model([["a", "b"], ["c", "d"], ["e", "f"]])
        new = model([["a"], ["c"], ["追加"], ["e"]])
        changes = compare_models(old, new)
        self.assertEqual(changes.summary(), "（1 行を追加、1 列を削除）")
        self.assertEqual(changes.cells, [])

    def test_changed_cell_uses_new_row(self):
        old = model([["a", "b"], ["c", "d"], ["e", "f"]])
        new = model([["a", "b"], ["追加", "行"], ["c", "d"], ["e", "変更"]])
        changes = compare_models(old, new)
        self.assertEqual(changes.summary(), "（セル 1 個を変更、1 行を追加）")
        self.assertEqual(changes.details(), ["  B4: 「f」→「変更」"])

    def test_alignment_and_header_only(self):
        old = model([["a", "b"]])
        for field, value, kind in (("align", "center", "配置"), ("header", True, "見出し")):
            with self.subTest(kind=kind):
                new = model([["a", "b"]])
                setattr(new.cells[1], field, value)
                changes = compare_models(old, new)
                self.assertEqual(changes.summary(), "（セル 1 個を変更）")
                self.assertEqual(changes.details(), ["  B1: %sを変更" % kind])

    def test_multiple_kinds_count_cell_once(self):
        old = model([["a"]])
        new = model([["b"]])
        new.cells[0].align = "right"
        new.cells[0].header = True
        changes = compare_models(old, new)
        self.assertEqual(changes.summary(), "（セル 1 個を変更）")
        self.assertEqual(changes.details(), ["  A1: 「a」→「b」、配置を変更、見出しを変更"])

    def test_merge_and_unmerge_at_anchor(self):
        old = model([["a", "b"], ["c", "d"]])
        new = TableModel("html", 2, 2, [Cell(0, 0, "a", rowspan=2, colspan=2)])
        for previous, current in ((old, new), (new, old)):
            changes = compare_models(previous, current)
            self.assertEqual(changes.summary(), "（セル 1 個を変更）")
            self.assertEqual(changes.details(), ["  A1: 結合を変更"])

    def test_merged_cell_text_and_extent(self):
        old = TableModel("html", 1, 3, [Cell(0, 0, "a", colspan=2), Cell(0, 2, "")])
        new = TableModel("html", 1, 3, [Cell(0, 0, "b", colspan=3)])
        changes = compare_models(old, new)
        self.assertEqual(changes.summary(), "（セル 1 個を変更）")
        self.assertEqual(changes.details(), ["  A1: 「a」→「b」、結合を変更"])

    def test_fill_or_clear_hole(self):
        old = TableModel("html", 1, 2, [Cell(0, 0, "a")], {(0, 1)})
        new = model([["a", "b"]])
        self.assertEqual(compare_models(old, new).details(), ["  B1: 「」→「b」"])
        self.assertEqual(compare_models(new, old).details(), ["  B1: 「b」→「」"])

    def test_shortening_newlines_and_empty_text(self):
        changes = compare_models(model([["a\nb", "", "x" * 31]]),
                                 model([["", "y" * 30, "z" * 31]]))
        self.assertEqual(changes.details(), ["  A1: 「a⏎b」→「」", "  B1: 「」→「%s」" % ("y" * 30),
                                             "  C1: 「%s…」→「%s…」" % ("x" * 30, "z" * 30)])

    def test_long_text_shows_first_difference(self):
        before = "同" * 40 + "前" + "終" * 40
        after = "同" * 40 + "後" + "終" * 40
        self.assertEqual(compare_models(model([[before]]), model([[after]])).details(),
                         ["  A1: 「…%s前%s…」→「…%s後%s…」" % ("同" * 10, "終" * 19, "同" * 10, "終" * 19)])

    def test_long_text_append_and_delete_use_same_window(self):
        before = "同" * 40
        after = before + "追加"
        for old, new, first, second in ((before, after, "同" * 10, "同" * 10 + "追加"),
                                       (after, before, "同" * 10 + "追加", "同" * 10)):
            self.assertEqual(compare_models(model([[old]]), model([[new]])).details(),
                             ["  A1: 「…%s」→「…%s」" % (first, second)])

    def test_long_text_window_normalizes_newlines(self):
        before = "前\r\n" + "同" * 35 + "前"
        after = "前\n" + "同" * 35 + "後"
        self.assertEqual(compare_models(model([[before]]), model([[after]])).details(),
                         ["  A1: 「…%s前」→「…%s後」" % ("同" * 10, "同" * 10)])

    def test_limit_and_excel_column_address(self):
        changes = compare_models(model([["a"] * 28]), model([["b"] * 28]))
        self.assertEqual(changes.summary(), "（セル 28 個を変更）")
        self.assertEqual(len(changes.details()), 21)
        self.assertEqual(changes.details()[-1], "  ほか 8 個")
        self.assertEqual(changes.details(28)[26:], ["  AA1: 「a」→「b」", "  AB1: 「a」→「b」"])

    def test_detail_limit_counts_cells_with_several_changes_once(self):
        old = model([["a"] * 21])
        new = model([["b"] * 21])
        for cell in new.cells:
            cell.align = "right"
        changes = compare_models(old, new)
        self.assertEqual(changes.summary(), "（セル 21 個を変更）")
        self.assertEqual(len(changes.details()), 21)
        self.assertEqual(changes.details()[0], "  A1: 「a」→「b」、配置を変更")
        self.assertEqual(changes.details()[-1], "  ほか 1 個")

    def test_repeated_rows_are_not_junk(self):
        old = model([["目印"]] + [["同じ"]] * 210 + [["末尾"]])
        new = model([["目印"], ["追加"]] + [["同じ"]] * 210 + [["末尾"]])
        self.assertEqual(compare_models(old, new).summary(), "（1 行を追加）")
