"""表の検出（comrak と同じ規則）と、字下げされた表の接頭辞のテスト。"""

import unittest

from helpers import regions, spans, texts

from mdtable import blocks


class TableDetectionTest(unittest.TestCase):
    def test_table_interrupts_paragraph(self):
        # D1: 段落の最後の行が見出し行になり、前の行は段落のまま
        self.assertEqual(spans("text\n| a | b |\n|---|---|\n| 1 | 2 |\n"), [(2, 4, "pipe", None)])

    def test_rows_without_pipes_continue(self):
        # D2: | の無い行も行になり、空行で終わる
        found = regions("| a |\n|---|\n| 1 |\nbar\n\nbaz\n")
        self.assertEqual((found[0].start + 1, found[0].end + 1), (1, 4))
        self.assertEqual(texts(found[0]), [["a"], ["1"], ["bar"]])

    def test_block_starts_end_table(self):
        # D3: 別のブロックの始まりで表が終わる
        for line in ["- x", "2. x", "-", "***", "---", "# h", "> q", "```", "<div>", "<span>",
                     "[^1]: n", "    | 2 |"]:
            with self.subTest(line=line):
                self.assertEqual(spans("| a |\n|---|\n| 1 |\n%s\n" % line)[0][:2], (1, 3))

    def test_lines_that_stay_rows(self):
        # D4: 説明リストやリンク参照の定義に見える行も行になる
        for line in ["===", ": x", "~ x", "[a]: b"]:
            with self.subTest(line=line):
                self.assertEqual(spans("| a |\n|---|\n| 1 |\n%s\n" % line)[0][:2], (1, 4))

    def test_cell_count_mismatch(self):
        # D5・D6: セル数が違えば表にならず、同じ段落ではもう表にしない
        self.assertEqual(spans("| a | b |\n|---|\n| 1 |\n"), [])
        self.assertEqual(spans("| a |\n|---|---|\n| a | b |\n|---|---|\n"), [])

    def test_setext_wins(self):
        # D7: --- だけの行は setext 見出し。:--- は 1 列の表
        self.assertEqual(spans("| a |\n---\n| 1 |\n"), [])
        self.assertEqual(spans("| a |\n:---\n")[0][:2], (1, 2))

    def test_fences_close_with_container(self):
        # D8・D9: コンテナが終わるとフェンスも終わる
        self.assertEqual(spans("> ```\n> x\n| a |\n|---|\n")[0][:2], (3, 4))
        self.assertEqual(spans("- a\n  ```\n  x\n\n| a |\n|---|\n")[0][:2], (5, 6))

    def test_html_blocks_hide_tables(self):
        # D10・D11: HTML ブロックの中は表にならない（空行で区切れば表）
        self.assertEqual(spans("<details>\n| a |\n|---|\n</details>\n"), [])
        self.assertEqual(spans("<details>\n\n| a |\n|---|\n\n</details>\n")[0][:2], (3, 4))
        self.assertEqual(spans("x <!-- c\n| a |\n|---|\n-->\n")[0][:2], (2, 4))
        self.assertEqual(spans("<!--\n| a |\n|---|\n-->\n"), [])

    def test_blockquote_and_lazy_lines(self):
        # D12: 表は遅延行では続かない。見出し行が遅延行の表は対象外
        self.assertEqual(spans("> | a |\n> |---|\n| 1 |\n")[0][:2], (1, 2))
        found = spans("> p\n| a |\n> |---|\n")
        self.assertEqual(len(found), 1)
        self.assertIsNotNone(found[0][3])

    def test_list_indentation(self):
        # D13: リスト項目の中は本文の位置まで字下げしたときだけ
        self.assertEqual(spans("- a\n| b |\n|---|\n"), [])
        found = regions("- a\n\n  | b |\n  |---|\n")
        self.assertEqual(found[0].prefixes, ["  ", "  "])
        self.assertEqual(spans("- a\n\n      | b |\n      |---|\n"), [])

    def test_pipe_escapes(self):
        # D14: comrak は \ の直後の | で区切らない（\ が何個でも）
        self.assertEqual(texts(regions("| a \\| b |\n|---|\n")[0]), [["a | b"]])
        self.assertEqual(spans("| a \\\\| b |\n|---|---|\n"), [])
        self.assertEqual(texts(regions("| a \\\\| b |\n|---|\n")[0]), [["a \\\\| b"]])
        self.assertEqual(texts(regions("| `x\\|y` |\n|---|\n")[0]), [["`x|y`"]])

    def test_hidden_cells(self):
        # D15: 見出しより多いセルは隠れて警告になる
        found = regions("| a | b |\n|---|---|\n| パイプ状態 | `false | true` の |\n")[0]
        self.assertEqual(texts(found)[1], ["パイプ状態", "`false"])
        self.assertTrue(found.warnings)

    def test_front_matter_and_multiline_quote(self):
        # D16
        self.assertEqual(spans("---\na: 1\n---\n| a |\n|---|\n")[0][:2], (4, 5))
        found = regions(">>>\n| a |\n|---|\n>>>\n")
        self.assertEqual((found[0].start + 1, found[0].end + 1), (2, 3))
        self.assertEqual(found[0].prefixes, ["", ""])


class IndentedTableTest(unittest.TestCase):
    def check(self, text, start, prefix, location=None):
        found = regions(text)
        self.assertEqual(len(found), 1, text)
        self.assertEqual(found[0].start + 1, start)
        self.assertEqual(found[0].cont_prefix, prefix)
        if location:
            self.assertIn(location, found[0].location)
        return found[0]

    def test_ordered_list(self):
        # N1・N2
        self.check("1. 手順\n\n   | a | b |\n   |---|---|\n   | 1 | 2 |\n", 3, "   ", "番号付きリスト")
        self.check("10. 手順\n\n    | a |\n    |---|\n", 3, "    ")
        self.check("9. 手順\n\n   | a |\n   |---|\n", 3, "   ")

    def test_nested_list(self):
        # N3
        self.check("- a\n  - b\n\n    | x |\n    |---|\n", 4, "    ", "箇条書き > 箇条書き")
        self.check("-   a\n\n    | x |\n    |---|\n", 3, "    ")

    def test_description_list(self):
        # N4: 説明の中は : の位置 + 「: 」と空白の幅まで
        self.check("用語\n: 説明\n\n  | a |\n  |---|\n", 4, "  ", "説明リスト")
        region = regions("用語\n: 説明\n\n| a |\n|---|\n")[0]
        self.assertEqual(region.location, "")
        self.check("用語\n:   説明\n\n    | a |\n    |---|\n", 4, "    ", "説明リスト")

    def test_blockquote_list(self):
        # N5
        region = self.check("> - a\n>\n>   | x |\n>   |---|\n", 3, ">   ", "引用 > 箇条書き")
        self.assertEqual(region.blank_prefix, ">")
        self.check("> [!NOTE]\n> 1. a\n>\n>    | x |\n>    |---|\n", 4, ">    ", "アラート > 番号付きリスト")

    def test_table_on_marker_line(self):
        # N6: 記号の行から始まる表
        region = self.check("- | a | b |\n  |---|---|\n  | 1 | 2 |\n", 1, "  ")
        self.assertEqual(region.first_prefix, "- ")

    def test_html_table_in_list(self):
        # N7
        found = regions("1. 手順\n\n   <table>\n   <tr><td>a</td></tr>\n   </table>\n2. 次\n")
        self.assertEqual(len(found), 1)
        self.assertEqual(found[0].kind, "html")
        self.assertEqual(found[0].cont_prefix, "   ")

    def test_footnote(self):
        # N9
        self.check("[^1]: 注\n\n    | x |\n    |---|\n", 3, "    ", "脚注")

    def test_extra_indentation(self):
        # N10: コンテナの中の追加の字下げ（1〜3）は表の一部
        self.check("- a\n\n   | x |\n   |---|\n", 3, "   ")

    def test_tabs(self):
        # N11
        self.check("-\ta\n\n\t| x |\n\t|---|\n", 3, "\t")

    def test_description_in_list(self):
        # N12
        self.check("- 用語\n  : 説明\n\n    | x |\n    |---|\n", 4, "    ", "箇条書き > 説明リスト")


class BlockParserTest(unittest.TestCase):
    def test_line_endings(self):
        lines, endings = blocks.split_lines("a\r\nb\rc\nd e")
        self.assertEqual(lines, ["a", "b", "c", "d e"])
        self.assertEqual(endings, ["\r\n", "\r", "\n", ""])

    def test_description_list_structure(self):
        doc = blocks.parse_text("T\n: d1\n: d2\n")
        kinds = [n.kind for n in doc.root.walk()]
        self.assertEqual(kinds.count(blocks.DESCRIPTION_DETAILS), 2)

    def test_unescape_pipes(self):
        for count in range(6):
            text = "a" + "\\" * count + "|b"
            expected = "a" + "\\" * (count - count % 2) + "|b"
            self.assertEqual(blocks.unescape_pipes(text), expected)


if __name__ == "__main__":
    unittest.main()
