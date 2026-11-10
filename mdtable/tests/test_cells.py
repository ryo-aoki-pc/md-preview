"""セルの文字の変換（パイプ表 ↔ Excel、HTML の表 ↔ Excel）のテスト。"""

import unittest

import helpers  # noqa: F401  （mdtable を読めるようにする）

from mdtable import htmltok
from mdtable.cell_from_html import convert
from mdtable.cell_syntax import (NotRepresentable, chunk_to_excel, normalize_list_spacing,
                                 to_html_cell, to_pipe_cell)
from mdtable.pipe_table import cell_to_excel


def from_html(source):
    tree = htmltok.build_tree(htmltok.tokenize(source))
    return convert(tree.children, source)


class ListSpacingTest(unittest.TestCase):
    def test_interrupting_lists(self):
        for marker in ("- 項目", "* 項目", "+ 項目", "1. 一", "1) 一"):
            for blank in ("\n\n", "\n\n\n"):
                text = "前の文" + blank + marker + "\n\n後の文\n"
                expected = "前の文\n" + marker + "\n\n後の文\n"
                self.assertEqual(normalize_list_spacing(text), expected)
                self.assertEqual(normalize_list_spacing(expected), expected)

    def test_keeps_noninterrupting_and_nested_spacing(self):
        texts = ["手順:\n\n2. 二\n3. 三", "前の文\n\n-\n- 次", "前の文\n\n1.\n2. 二",
                 "前の文\n\n用語\n: 説明", "# 見出し\n\n- 項目", "> 前\n>\n> - 項目",
                 "- 前\n\n  - 内側", "```\n前\n\n- コード\n```", "    前\n\n    - コード"]
        for text in texts:
            with self.subTest(text=text):
                self.assertEqual(normalize_list_spacing(text), text)

    def test_alternating_paragraphs_and_lists(self):
        text = "段落 1\n\n- 項目 1\n\n段落 2\n\n- 項目 2"
        expected = "段落 1\n- 項目 1\n\n段落 2\n- 項目 2"
        self.assertEqual(normalize_list_spacing(text), expected)
        self.assertEqual(normalize_list_spacing(expected), expected)

    def test_keeps_blank_lines_inside_loose_list(self):
        text = "前\n\n1. 一\n\n   - 中\n\n2. 二"
        self.assertEqual(normalize_list_spacing(text), text.replace("前\n\n", "前\n", 1))

    def test_without_double_newline_does_not_parse(self):
        from unittest import mock
        with mock.patch("mdtable.cell_syntax.parse") as parse:
            self.assertEqual(normalize_list_spacing("前\n- 項目"), "前\n- 項目")
        parse.assert_not_called()

    def test_html_elements_use_same_spacing(self):
        self.assertEqual(from_html("<p>前</p><ul><li>項目</li></ul><p>後</p>"), "前\n- 項目\n\n後")

    def test_literal_task_markers_round_trip(self):
        for marker in ("[ ]", "[x]", "[X]", "[注]", "[1]"):
            for opening, closing, prefix in (("<ul>", "</ul>", "- "), ("<ol>", "</ol>", "1. ")):
                source = opening + "<li>" + marker + " 説明</li>" + closing
                text = prefix + "\\" + marker + " 説明"
                self.assertEqual(cell_to_excel(source), text)
                self.assertEqual(to_pipe_cell(text), source)
                self.assertIn("\\" + marker, "\n".join(to_html_cell(text)[1]))

    def test_non_task_brackets_stay_literal(self):
        for text in ("[注釈] 説明", "[x]続き", "[ ]続き", "[] 説明"):
            self.assertEqual(cell_to_excel("<ul><li>" + text + "</li></ul>"), "- " + text)

    def test_refuses_pipe_table_before_plain_fast_path(self):
        for text in ("| a | b |\n| --- | --- |\n| 1 | 2 |", "- | a |\n  | --- |\n  | 1 |"):
            with self.assertRaisesRegex(NotRepresentable, "セルの中に表"):
                to_html_cell(text)
        self.assertEqual(to_html_cell("```\n| a |\n| --- |\n``` ")[0], "chunk")


class PipeToExcelTest(unittest.TestCase):
    def test_breaks(self):
        # P1
        for source in ("a<br>b", "a<br/>b", "a<BR >b", "a<br />b"):
            self.assertEqual(cell_to_excel(source), "a\nb")
        self.assertEqual(cell_to_excel("`a<br>b`"), "`a<br>b`")

    def test_line_start_escapes(self):
        # P2: GitLab では文字として見えているので、ブロックの記法にならないようにする
        cases = {
            "a<br>- b": "a\n\\- b",
            "- a": "\\- a",
            "1. x": "1\\. x",
            "1) x": "1\\) x",
            "> 3": "\\> 3",
            "# x": "\\# x",
            "+1": "+1",
            "x<br>: y": "x\n\\: y",
            "x<br>~ y": "x\n\\~ y",
            "x<br>===": "x\n\\===",
            "x<br>---": "x\n\\---",
            "[a]: b": "\\[a]: b",
            "x<br><br>    y": "x\n\ny",
        }
        for source, expected in cases.items():
            with self.subTest(source=source):
                self.assertEqual(cell_to_excel(source), expected)

    def test_lists(self):
        # P3
        cases = {
            "<ul><li>a</li><li>b</li></ul>": "- a\n- b",
            "x<ul><li>a</li></ul>y": "x\n- a\n\ny",
            '<ol start="3"><li>a</li></ol>': "3. a",
            "<ul><li>a<br>b</li></ul>": "- a\n  b",
            "<dl><dt>T</dt><dd>d1</dd><dd>d2</dd></dl>": "T\n: d1\n: d2",
            '<ul class="x"><li>a</li></ul>': '<ul class="x"><li>a</li></ul>',
            "<ul><li>a</li></ul><ul><li>b</li></ul>": "- a\n* b",
        }
        for source, expected in cases.items():
            with self.subTest(source=source):
                self.assertEqual(cell_to_excel(source), expected)


class ExcelToPipeTest(unittest.TestCase):
    def test_breaks(self):
        # I1
        cases = {"a\nb": "a<br>b", "a\n\nb": "a<br><br>b", "a\\\nb": "a<br>b",
                 "a  \nb": "a<br>b", "`a\nb`": "`a b`"}
        for text, expected in cases.items():
            with self.subTest(text=text):
                self.assertEqual(to_pipe_cell(text), expected)

    def test_pipes(self):
        # I2: 直前の \ が偶数個なら 1 つ足す。コードの中の奇数個は書けない
        cases = {"a|b": "a\\|b", "a\\|b": "a\\|b", "a\\\\|b": "a\\\\\\|b",
                 "`x|y`": "`x\\|y`", "<https://h/a|b>": "<https://h/a\\|b>"}
        for text, expected in cases.items():
            with self.subTest(text=text):
                self.assertEqual(to_pipe_cell(text), expected)
        with self.assertRaises(NotRepresentable):
            to_pipe_cell("`x\\|y`")

    def test_blocks(self):
        # I3
        cases = {
            "- a\n- b": "<ul><li>a</li><li>b</li></ul>",
            "x\n- a": "x<ul><li>a</li></ul>",
            "- a\n\ny": "<ul><li>a</li></ul>y",
            "- a\ny": "<ul><li>a<br>y</li></ul>",
            "3. a\n4. b": '<ol start="3"><li>a</li><li>b</li></ol>',
            "1. a\n  - b": "<ol><li>a</li></ol><ul><li>b</li></ul>",
            "1. a\n   - b": "<ol><li>a<ul><li>b</li></ul></li></ol>",
            "T\n: d": "<dl><dt>T</dt><dd>d</dd></dl>",
            "p\nT\n: d": "<dl><dt>p<br>T</dt><dd>d</dd></dl>",
            "x\n2. y": "x<br>2. y",
            "<details><summary>s</summary>b</details>": "<details><summary>s</summary>b</details>",
            "\\- a": "- a",
        }
        for text, expected in cases.items():
            with self.subTest(text=text):
                self.assertEqual(to_pipe_cell(text), expected)

    def test_not_representable(self):
        for text in ("```\ncode\n```", "x\n-", "- [ ] task", "# h", "> q", "    code"):
            with self.subTest(text=text):
                with self.assertRaises(NotRepresentable):
                    to_pipe_cell(text)

    def test_round_trip_converges(self):
        # パイプ表のセル → Excel → パイプ表のセル → Excel が同じになる
        sources = ["a<br>- b", "<ul><li>a</li><li>b</li></ul>", "x<ul><li>a</li></ul>y",
                   "<dl><dt>T</dt><dd>d1</dd><dd>d2</dd></dl>", "`a\\|b` と a\\|b", "**太字**<br>2. 次"]
        from mdtable.blocks import unescape_pipes
        for source in sources:
            with self.subTest(source=source):
                # cell_to_excel には comrak が読んだ中身（\| を戻したもの）を渡す
                first = cell_to_excel(unescape_pipes(source))
                again = cell_to_excel(unescape_pipes(to_pipe_cell(first)))
                self.assertEqual(again, first)


class ChunkTest(unittest.TestCase):
    def test_chunk_to_excel(self):
        # M1・M2
        cases = {
            "a<br>\nb": "a\nb",
            "a\nb": "a b",
            "a\\\nb": "a\nb",
            "a  \nb": "a\nb",
            "`a<br>`\nb": "`a<br>` b",
            "`a\nb`": "`a b`",
            "- a<br>\n  b": "- a\n  b",
        }
        for text, expected in cases.items():
            with self.subTest(text=text):
                self.assertEqual(chunk_to_excel(text), expected)

    def test_verbatim_blocks(self):
        # M3: 段落以外はそのまま
        for text in ("<details>\n<summary>s</summary>\nx\n</details>", "```\na\nb\n```",
                     "| a |\n|---|\n| 1 |", "Title\n---", "    a\n    b"):
            with self.subTest(text=text):
                self.assertEqual(chunk_to_excel(text), text)

    def test_forward(self):
        # M4: 段落の中の改行に <br> を足す
        cases = {
            "x\n2. y": ["x<br>", "2. y"],
            "- a\nb": ["- a<br>", "b"],
            "x\n<span>": ["x<br>", "<span>"],
            "x\n<div>": ["x", "<div>"],
            "a<br>\nb": ["a<br>", "b"],
            "a\\\\\nb": ["a\\\\<br>", "b"],
            "a\\\nb": ["a\\", "b"],
        }
        for text, expected in cases.items():
            with self.subTest(text=text):
                kind, lines, _warnings = to_html_cell(text)
                self.assertEqual(kind, "chunk")
                self.assertEqual(lines, expected)

    def test_plain(self):
        # X1: 書式の無い文字は 1 行
        self.assertEqual(to_html_cell("a\nb")[:2], ("inline", "a<br>b"))
        self.assertEqual(to_html_cell("a < b")[0], "chunk")

    def test_lists_get_blank_lines(self):
        kind, lines, _warnings = to_html_cell("説明\n- a\n- b")
        self.assertEqual(lines, ["説明", "", "- a", "- b"])

    def test_unclosed_fence_is_closed(self):
        kind, lines, warnings = to_html_cell("```sh\necho 1")
        self.assertEqual(lines[-1], "```")
        self.assertTrue(warnings)

    def test_refuses_table_tags(self):
        with self.assertRaises(NotRepresentable):
            to_html_cell("a</td><td>b")
        with self.assertRaises(NotRepresentable):
            to_html_cell("<pre>\nx")

    def test_tasks_and_definitions(self):
        self.assertEqual(to_html_cell("- [注] x")[1], ["- \\[注] x"])
        self.assertEqual(to_html_cell("- [x] done")[1], ["- [x] done"])
        self.assertEqual(to_html_cell("[a]: https://example.com")[1], ["\\[a]: https://example.com"])

    def test_convergence(self):
        # M5: E1 = export(import(S)) のとき export(import(E1)) == E1
        for text in ("x\n2. y", "- a\nb", "a\\\nb", "**a**\nb", "T\n: d1\nd2", "x\n<span>", "`a\nb`"):
            with self.subTest(text=text):
                kind, payload, _w = to_html_cell(text)
                first = chunk_to_excel("\n".join(payload)) if kind == "chunk" else text
                kind2, payload2, _w = to_html_cell(first)
                second = chunk_to_excel("\n".join(payload2)) if kind2 == "chunk" else first
                self.assertEqual(second, first)


class HtmlToExcelTest(unittest.TestCase):
    def test_whitespace_and_entities(self):
        # H1・H5
        self.assertEqual(from_html("a  \n b"), "a b")
        self.assertEqual(from_html("a\u3000b"), "a\u3000b")
        self.assertEqual(from_html("a&nbsp;b"), "a&nbsp;b")
        self.assertEqual(from_html("&lt;div&gt; &amp; R&amp;D"), "\\<div> & R&D")
        self.assertEqual(from_html("&copy 2026"), "\u00a9 2026")
        self.assertEqual(from_html("&amp;amp;"), "&amp;amp;")

    def test_emphasis(self):
        # H2: 約物に接して閉じる強調は HTML のまま
        self.assertEqual(from_html("これは<strong>重要</strong>です"), "これは**重要**です")
        self.assertEqual(from_html("<strong>「重要」</strong>です"), "<strong>「重要」</strong>です")
        self.assertEqual(from_html("<b> x </b>y"), "**x** y")
        self.assertEqual(from_html("<strong>a</strong><strong>b</strong>"), "**ab**")
        self.assertEqual(from_html("<strong>a <em>b</em></strong>"), "**a <em>b</em>**")
        self.assertEqual(from_html("a<b>*</b>c"), "a<b>\\*</b>c")

    def test_tilde_and_dollar(self):
        # H3
        self.assertEqual(from_html("1~3 と 5~7"), "1\\~3 と 5\\~7")
        self.assertEqual(from_html("~bug を付ける"), "~bug を付ける")
        self.assertEqual(from_html("$a$ と $5"), "\\$a\\$ と \\$5")

    def test_escapes(self):
        # H4
        self.assertEqual(from_html("[注]: 説明"), "\\[注]: 説明")
        self.assertEqual(from_html("- a"), "\\- a")
        self.assertEqual(from_html("a<br>: b"), "a\n\\: b")
        self.assertEqual(from_html("a<br>--- "), "a\n\\---")
        self.assertEqual(from_html("<ul><li>[注] x</li></ul>"), "- \\[注] x")
        self.assertEqual(from_html("C:\\Users\\foo"), "C:\\Users\\foo")
        self.assertEqual(from_html("snake_case と (_x_)"), "snake_case と (\\_x\\_)")

    def test_code(self):
        # H6
        self.assertEqual(from_html("<code>`a`</code>"), "`` `a` ``")
        self.assertEqual(from_html("<code> a </code>"), "`  a  `")
        self.assertEqual(from_html("<code>&lt;br&gt;</code>"), "`<br>`")
        self.assertEqual(from_html("<code><b>x</b></code>"), "<code><b>x</b></code>")

    def test_links(self):
        # H7
        self.assertEqual(from_html('<a href="https://x/(a)">t</a>'), "[t](https://x/(a))")
        self.assertEqual(from_html('<a href="https://x/a)b">t</a>'), "[t](<https://x/a)b>)")
        self.assertEqual(from_html('<a href="a b">t</a>'), "[t](<a b>)")
        self.assertEqual(from_html("<a href=\"u\" title='say \"hi\"'>t</a>"), '[t](u "say \\"hi\\"")')
        self.assertEqual(from_html('<a href="u" target="_blank">t</a>'), '<a href="u" target="_blank">t</a>')
        self.assertEqual(from_html('<a href="https://x">https://x</a>'), "<https://x>")

    def test_raw_links_and_images(self):
        source = ('[リンク](https://example.com "題") と ![図](https://example.com/a.png)'
                  ' と **太字** と [相対](docs/usage.md) と [アンカー](#top)')
        expected = ('[リンク](https://example.com "題") と ![図](https://example.com/a.png)'
                    ' と \\*\\*太字\\*\\* と [相対](docs/usage.md) と [アンカー](#top)')
        self.assertEqual(from_html(source), expected)

    def test_raw_link_after_backslash(self):
        self.assertEqual(from_html(r'\[リンク](https://example.com/a)'),
                         r'\\[リンク](https://example.com/a)')
        self.assertEqual(from_html(r'\![図](a.png)'), r'\\![図](a.png)')

    def test_raw_reference_links_are_escaped(self):
        self.assertEqual(from_html('[参照][ref] と [ref] と ![図][image]'),
                         r'\[参照]\[ref] と \[ref] と !\[図]\[image]')

    def test_raw_links_in_excluded_elements(self):
        cases = {
            '<code>[a](b)</code>': '`[a](b)`',
            '<pre>[a](b)</pre>': '```\n[a](b)\n```',
            '<kbd>[a](b)</kbd>': r'<kbd>\[a](b)</kbd>',
            '<kbd><span>[a](b)</span></kbd>': r'<kbd><span>\[a](b)</span></kbd>',
            '<a href="https://example.com">[a](b)</a>': r'[\[a\](b)](https://example.com)',
            '<a href="u" target="_blank">[a](b)</a>': r'<a href="u" target="_blank">\[a](b)</a>',
            '<a>[a](b)</a>': r'<a>\[a](b)</a>',
            '<a href="u"><em>[a](b)</em></a>': r'[*\[a\](b)*](u)',
            '<script>[a](b)</script>': r'<script>\[a](b)</script>',
            '<style>[a](b)</style>': r'<style>\[a](b)</style>',
        }
        for source, expected in cases.items():
            with self.subTest(source=source):
                self.assertEqual(from_html(source), expected)

    def test_raw_links_do_not_cross_elements(self):
        cases = {
            '[<b>a</b>](https://example.com)': r'\[**a**](https://example.com)',
            '[a]<span>(b)</span>': r'\[a]<span>(b)</span>',
            '[a](<span>b</span>)': r'\[a](<span>b</span>)',
            '[a]<!-- 区切り -->(b)': r'\[a]<!-- 区切り -->(b)',
        }
        for source, expected in cases.items():
            with self.subTest(source=source):
                self.assertEqual(from_html(source), expected)

    def test_raw_links_decode_entities(self):
        self.assertEqual(from_html('[a](https://example.com/?x=1&amp;y=2)'),
                         '[a](https://example.com/?x=1&y=2)')
        self.assertEqual(from_html('&#91;*a*&#93;(b &quot;題&quot;)'), '[*a*](b "題")')

    def test_raw_links_escape_surrounding_text(self):
        self.assertEqual(from_html('*[a](b)* と _![c](d)_ と [f](g) と [e]'),
                         r'\*[a](b)\* と \_![c](d)\_ と [f](g) と \[e]')
        self.assertEqual(from_html('&lt;![a](b)'), r'\<![a](b)')
        self.assertEqual(from_html('$[a](b)$ と ~[c](d)~'), r'\$[a](b)\$ と \~[c](d)\~')

    def test_raw_links_keep_inner_markdown_and_destination_spaces(self):
        self.assertEqual(from_html('[*強調*](docs/a b.md "題") と ![**図**](a b.png)'),
                         '[*強調*](docs/a b.md "題") と ![**図**](a b.png)')
        self.assertEqual(from_html('[a](b(c)*後ろ*)'), r'[a](b(c)\*後ろ\*)')
        self.assertEqual(from_html('[](b) と [a]()'), r'\[](b) と \[a]()')

    def test_raw_links_do_not_cross_newlines(self):
        for source in ('[a\nb](c)', '[a](b\nc)', '[a](b "題\n名")', '[a&#10;b](c)'):
            with self.subTest(source=source):
                self.assertTrue(from_html(source).startswith(r'\['))
        self.assertEqual(from_html('[a](b)\n[c](d)'), '[a](b) [c](d)')

    def test_raw_links_keep_whitespace_rules(self):
        self.assertEqual(from_html('a  \t [x](b  \t c)  \n d'), 'a [x](b c) d')
        self.assertEqual(from_html('a&#9;b [x](c) a&nbsp;b'), 'a\tb [x](c) a&nbsp;b')

    def test_raw_links_in_flow_elements(self):
        cases = {
            '<p>[a](b) と **c**</p>': r'[a](b) と \*\*c\*\*',
            '<ul><li>[a](b)</li><li>![c](d)</li></ul>': '- [a](b)\n- ![c](d)',
            '<span>[a](b)</span>': '<span>[a](b)</span>',
        }
        for source, expected in cases.items():
            with self.subTest(source=source):
                self.assertEqual(from_html(source), expected)
        table = helpers.regions('<table><tr><th>[a](b)</th><td>![c](d)</td></tr></table>\n')[0]
        self.assertEqual(helpers.texts(table), [['[a](b)', '![c](d)']])

    def test_raw_links_do_not_change_markdown_chunks_or_pipe_cells(self):
        text = r'\[a](b) と **c** と [参照][ref]'
        table = helpers.regions('<table>\n<tr><td>\n\n' + text + '\n\n</td></tr>\n</table>\n')[0]
        self.assertEqual(helpers.texts(table), [[text]])
        self.assertEqual(cell_to_excel(text), text)

    def test_lists(self):
        # H8
        self.assertEqual(from_html("<ul><li>a<ul><li>b</li></ul></li></ul>"), "- a\n  - b")
        self.assertEqual(from_html('<ol start="9"><li>a</li><li>b<ul><li>c</li></ul></li></ol>'),
                         "9. a\n10. b\n    - c")
        self.assertEqual(from_html("<ul><li>a</li></ul><ul><li>b</li></ul>"), "- a\n* b")
        self.assertEqual(from_html("<ul><li><p>a</p><p>b</p></li></ul>"), "- a\n\n  b")
        self.assertEqual(from_html("<ul><li>a</li></ul>text"), "- a\n\ntext")
        self.assertEqual(from_html('<ol type="a"><li>x</li></ol>'), '<ol type="a"><li>x</li></ol>')
        self.assertEqual(from_html("<ul><li>1. x</li></ul>"), "- 1\\. x")
        self.assertEqual(from_html('<ul><li><input type="checkbox" checked> done</li></ul>'), "- [x] done")

    def test_description_lists(self):
        # H9
        self.assertEqual(from_html("<dl><dt>A</dt><dd>x</dd><dt>B</dt><dd>y</dd></dl>"), "A\n: x\n\nB\n: y")
        self.assertEqual(from_html("<dl><dt>A</dt><dt>B</dt><dd>x</dd></dl>"),
                         "<dl><dt>A</dt><dt>B</dt><dd>x</dd></dl>")
        self.assertEqual(from_html("<p>p</p><dl><dt>A</dt><dd>x</dd></dl>"), "p\n\nA\n: x")
        self.assertEqual(from_html("<dl><dt>A</dt><dd><p>x</p><p>y</p></dd></dl>"), "A\n: x\n\n  y")
        self.assertEqual(from_html("<dl><dt>A</dt><dd>x</dd><dd>y</dd></dl>"), "A\n: x\n: y")

    def test_pre(self):
        # H10
        self.assertEqual(from_html("<pre>\nx\n</pre>"), "```\nx\n```")
        self.assertEqual(from_html('<pre><code class="language-sh">a &amp;&amp; b\n</code></pre>'),
                         "```sh\na && b\n```")
        self.assertEqual(from_html("<pre><code>x\n```\ny</code></pre>"), "````\nx\n```\ny\n````")
        self.assertEqual(from_html("<pre><b>x</b></pre>"), "<pre><b>x</b></pre>")

    def test_other_blocks(self):
        # H11
        self.assertEqual(from_html("a<hr>b"), "a\n\n---\n\nb")
        self.assertEqual(from_html("<h3>C #</h3>"), "### C \\#")
        self.assertEqual(from_html("a<br>"), "a")

    def test_markdown_chunk(self):
        source = "<b>x</b>"
        tree = htmltok.build_tree([htmltok.Token("md", 0, 0, "- a\n- b")])
        self.assertEqual(convert(tree.children, source), "- a\n- b")


if __name__ == "__main__":
    unittest.main()
