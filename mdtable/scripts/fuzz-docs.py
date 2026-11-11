"""突き合わせ用に、表の入った乱数の Markdown の文書を作る（Python 版と Go 版の違いを探すため）。

使い方: python scripts/fuzz-docs.py <出力のフォルダー> [文書の数（既定 300）] [種（既定 1）]

文書は壊れた形や、どちらの実装も断る形も含む（断るときの文も突き合わせる）。
"""

import os
import random
import sys

WORDS = ["表", "セル", "abc", "x", "1", "２", "日本語の文", "a b", "  前後  ", "末尾\t", "😀", "é", "ｱｲｳ",
         "snake_case", "2*3*4", "a~b", "　全角空白", " ", "Ⅻ", "١٢"]
INLINE = ["**太字**", "*斜体*", "_下線_", "__x__", "~~消す~~", "`code`", "`a | b`", "`` `x` ``", "\\|", "|",
          "<br>", "<br/>", "<BR />", "<b>b</b>", "<span title=\"a|b\">s</span>", "&amp;", "&#124;", "&lt;td&gt;",
          "&nbsp;", "&copy", "[link](https://example.com/a_b)", "<https://example.com>", "https://example.com/x",
          "![img](a.png)", "$x^2$", "$`y`$", "[^1]", "\\*", "\\\\", "\\", "<!-- c -->", "<kbd>K</kbd>",
          "<ul><li>a</li><li>b</li></ul>", "<ol start=\"3\"><li>c</li></ol>", "<dl><dt>t</dt><dd>d</dd></dl>",
          "- x", "1. y", "> q", "# h", "<p>p</p>", "<code>c</code>", "<pre>p</pre>", "_x0041_", "\t", "~~~", "```"]


def cell_text(rng):
    parts = []
    for _ in range(rng.randint(0, 4)):
        parts.append(rng.choice(WORDS if rng.random() < 0.55 else INLINE))
    return rng.choice(["", " "]).join(parts)


def pipe_table(rng):
    cols = rng.randint(1, 5)
    rows = rng.randint(0, 5)
    lead = rng.random() < 0.85
    trail = rng.random() < 0.8

    def line(cells):
        body = " | ".join(cells) if rng.random() < 0.8 else "|".join(cells)
        return ("| " if lead else "") + body + (" |" if trail else "")

    header = [cell_text(rng) or "h%d" % i for i in range(cols)]
    aligns = [rng.choice(["---", ":--", "--:", ":-:", "-", ":---:", "----"]) for _ in range(cols)]
    if rng.random() < 0.05:
        aligns = aligns[:-1] or aligns
    out = [line(header), line(aligns)]
    for _ in range(rows):
        n = cols + rng.choice([0, 0, 0, 0, -1, 1, 2])
        out.append(line([cell_text(rng) for _ in range(max(1, n))]))
    return out


def html_cell(rng, tag):
    attrs = ""
    if rng.random() < 0.2:
        attrs += " %s=\"%d\"" % (rng.choice(["rowspan", "colspan"]), rng.randint(1, 3))
    if rng.random() < 0.15:
        attrs += rng.choice([" align=\"right\"", " style=\"text-align: center;\"", " ALIGN=left",
                             " style='color:red;text-align:right'", " class=\"x\""])
    body = cell_text(rng)
    if rng.random() < 0.15:
        body = "\n\n" + rng.choice(["- a\n- b", "1. x\n2. y", "**太字** の段落", "```\ncode\n```", "> 引用",
                                     "| a | b |\n| - | - |\n| 1 | 2 |"]) + "\n\n"
    close = "" if rng.random() < 0.05 else "</%s>" % tag
    return "<%s%s>%s%s" % (tag.upper() if rng.random() < 0.05 else tag, attrs, body, close)


def html_table(rng):
    cols = rng.randint(1, 4)
    rows = rng.randint(1, 5)
    one_line = rng.random() < 0.3
    out = []
    head = rng.random() < 0.6
    out.append("<table>" if rng.random() < 0.9 else "<table border=\"1\">")
    for r in range(rows):
        tag = "th" if r == 0 and head else "td"
        if r == 0 and head and rng.random() < 0.5:
            out.append("<thead>")
        cells = [html_cell(rng, tag) for _ in range(cols + rng.choice([0, 0, 0, -1, 1]))]
        if one_line:
            out.append("<tr>" + "".join(cells) + "</tr>")
        else:
            out.append("<tr>")
            out.extend("  " + c for c in cells)
            out.append("</tr>")
        if r == 0 and head and out[-1 - (1 if one_line else len(cells) + 1)] == "<thead>":
            out.append("</thead>")
    out.append("</table>")
    text = "\n".join(out)
    if rng.random() < 0.15:
        text = text.replace("\n", "")
    return text.split("\n")


def wrap(rng, lines):
    kind = rng.choice(["none", "none", "none", "quote", "list", "olist", "nested", "tab", "multi", "footnote", "alert"])
    if kind == "quote":
        return ["> " + l if l or rng.random() < 0.5 else ">" for l in lines]
    if kind == "list":
        return ["- 項目", ""] + ["  " + l if l else "" for l in lines] if rng.random() < 0.5 else \
            ["- " + lines[0]] + ["  " + l for l in lines[1:]]
    if kind == "olist":
        return ["1. 項目", ""] + ["   " + l if l else "" for l in lines]
    if kind == "nested":
        return ["> - 項目", ">"] + [">   " + l for l in lines]
    if kind == "tab":
        return ["- 項目", ""] + ["\t" + l for l in lines]
    if kind == "multi":
        return [">>>"] + lines + [">>>"]
    if kind == "footnote":
        return ["[^1]: 脚注", ""] + ["    " + l for l in lines]
    if kind == "alert":
        return ["> [!note]"] + ["> " + l for l in lines]
    return lines


def noise(rng):
    return rng.choice([
        ["# 見出し %d" % rng.randint(1, 99)], ["見出し", "===="], ["段落の文。", "続きの行"], ["```", "| a | b |", "| - | - |", "```"],
        ["~~~md", "<table><tr><td>x</td></tr></table>", "~~~"], ["<div>", "html block", "</div>"], ["---"], ["- a", "- b"],
        ["    | indented | code |", "    | - | - |"], ["<!-- comment -->"], ["## 2026"], ["## L3 #"], [""], ["text | with | pipes"],
    ])


def document(rng):
    lines = []
    for _ in range(rng.randint(1, 5)):
        lines.extend(noise(rng))
        if rng.random() < 0.7:
            lines.append("")
        table = pipe_table(rng) if rng.random() < 0.55 else html_table(rng)
        lines.extend(wrap(rng, table))
        if rng.random() < 0.7:
            lines.append("")
    ending = rng.choice(["\n", "\n", "\r\n", "mixed"])
    if ending == "mixed":
        text = "".join(l + rng.choice(["\n", "\r\n"]) for l in lines)
    else:
        text = ending.join(lines) + (ending if rng.random() < 0.85 else "")
    data = text.encode("utf-8")
    if rng.random() < 0.1:
        data = b"\xef\xbb\xbf" + data
    return data


def main():
    out = sys.argv[1]
    count = int(sys.argv[2]) if len(sys.argv) > 2 else 300
    seed = int(sys.argv[3]) if len(sys.argv) > 3 else 1
    rng = random.Random(seed)
    os.makedirs(out, exist_ok=True)
    for i in range(count):
        with open(os.path.join(out, "fuzz-%04d.md" % i), "wb") as handle:
            handle.write(document(rng))


if __name__ == "__main__":
    main()
