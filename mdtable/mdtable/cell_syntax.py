"""Excel のセルに書いた Markdown（セル内の改行 = 表示上の改行）を、表のセルの書き方に変える。

- パイプ表のセル: 1 行のインラインの Markdown（改行は <br>、リストは <ul><li> など）
- HTML の表のセル: 書式の無い文字は 1 行、それ以外は <td> の中の Markdown の部分
  （段落の中の改行には行末に <br> を足す）
- その逆（HTML の表の Markdown の部分 → Excel のセル）
"""

import re

from . import blocks, inline
from .model import normalize_text


class NotRepresentable(Exception):
    """パイプ表のセルでは書けない（HTML の表にする必要がある）。"""


_BR_TAG = re.compile(r"<br[ \t]*/?>", re.I)
_TRAILING_BR = re.compile(r"(?:<br[ \t]*/?>)[ \t]*$", re.I)
_TASK_MARKER = re.compile(r"\[[ xX]\](?:[ \t]|$)")


def parse(text):
    return blocks.parse_text(text, front_matter=False, keep_definitions=True)


def _paragraph_lines(doc, node):
    return [doc.lines[n][o:] for n, o, _lazy in node.lines]


def _trailing_backslashes(text):
    count = 0
    while count < len(text) and text[len(text) - 1 - count] == "\\":
        count += 1
    return count


def strip_break_marker(line):
    """行末の改行マーク（2 つ以上の空白・奇数個の \\・<br>）を除く。(除いた行, 除いたか)。"""
    stripped = line.rstrip(" ")
    if len(line) - len(stripped) >= 2:
        return stripped, True
    if _trailing_backslashes(stripped) % 2 == 1:
        return stripped[:-1].rstrip(" \t"), True
    match = _TRAILING_BR.search(stripped)
    if match and not inline.position_in_spans(match.start(), inline.atom_spans(stripped)):
        return stripped[:match.start()].rstrip(" \t"), True
    return stripped.rstrip(" \t"), False


def _join_paragraph(lines, break_text):
    """段落の行を、改行（コードスパンなどの中は空白）でつなぐ。

    パイプ表のセル（インラインの文脈）では行頭の記法は意味を持たないので、
    行頭のエスケープ（Excel に出すときに付けたもの）は外す。
    """
    if not lines:
        return ""
    lines = [line.lstrip(" \t") if i else line for i, line in enumerate(lines)]
    spans = inline.atom_spans("\n".join(lines))
    position = 0
    unescaped = []
    for line in lines:
        if inline.position_in_spans(position, spans):
            unescaped.append(line)
        else:
            unescaped.append(inline.unescape_line_start(line))
        position += len(line) + 1
    joined = "\n".join(unescaped)
    spans = inline.atom_spans(joined)
    out = []
    pos = 0
    while True:
        newline = joined.find("\n", pos)
        if newline < 0:
            out.append(joined[pos:])
            break
        segment = joined[pos:newline]
        if inline.position_in_spans(newline, spans):
            out.append(segment + " ")
        else:
            segment, _marked = strip_break_marker(segment)
            if _trailing_backslashes(segment) % 2 == 1:
                segment += " "
            out.append(segment + break_text)
        pos = newline + 1
    return "".join(out).strip(" \t")


def escape_pipes(text):
    """パイプ表のセルの | をエスケープする（直前の \\ が偶数個なら 1 つ足す）。"""
    spans = inline.atom_spans(text)
    out = []
    for index, ch in enumerate(text):
        if ch == "|":
            count = 0
            while index - count - 1 >= 0 and text[index - count - 1] == "\\":
                count += 1
            if count % 2 == 0:
                out.append("\\|")
                continue
            if inline.position_in_spans(index, spans):
                raise NotRepresentable("コードの中の「\\|」はパイプ表のセルに書けない")
        out.append(ch)
    return "".join(out)


def _is_task_item(doc, item):
    if not item.children or item.children[0].kind != blocks.PARAGRAPH:
        return False
    first = _paragraph_lines(doc, item.children[0])[0]
    return bool(_TASK_MARKER.match(first))


def _blank_between(doc, first, second):
    """2 つのブロックの間に空行があるか。"""
    for line_no in range(first.end_line + 1, second.start_line):
        if not doc.lines[line_no].strip(" \t>"):
            return True
    return False


def _render_pipe_blocks(doc, nodes):
    out = []
    previous = None
    for node in nodes:
        piece = _render_pipe_block(doc, node)
        if previous is not None:
            if previous.kind in (blocks.LIST, blocks.DESCRIPTION_LIST) or \
                    node.kind in (blocks.LIST, blocks.DESCRIPTION_LIST):
                joiner = ""
            elif _blank_between(doc, previous, node):
                joiner = "<br><br>"
            else:
                joiner = "<br>"
            out.append(joiner)
        out.append(piece)
        previous = node
    return "".join(out)


def _render_pipe_block(doc, node):
    kind = node.kind
    if kind == blocks.PARAGRAPH:
        return _join_paragraph(_paragraph_lines(doc, node), "<br>")
    if kind == blocks.HTML_BLOCK:
        lines = [doc.lines[n][o:].strip(" \t") for n, o, _lazy in node.lines
                 if doc.lines[n][o:].strip(" \t")]
        return "<br>".join(lines)
    if kind == blocks.LIST:
        if node.info["type"] == "ordered":
            start = node.info["start"]
            open_tag = "<ol>" if start == 1 else '<ol start="%d">' % start
            close_tag = "</ol>"
        else:
            open_tag, close_tag = "<ul>", "</ul>"
        items = []
        for item in node.children:
            if _is_task_item(doc, item):
                raise NotRepresentable("タスクリストはパイプ表のセルに書けない")
            items.append("<li>" + _render_item_content(doc, item.children) + "</li>")
        return open_tag + "".join(items) + close_tag
    if kind == blocks.DESCRIPTION_LIST:
        parts = ["<dl>"]
        for item in node.children:
            for child in item.children:
                if child.kind == blocks.DESCRIPTION_TERM:
                    term = child.children[0] if child.children else None
                    text = _join_paragraph(_paragraph_lines(doc, term), "<br>") if term else ""
                    parts.append("<dt>" + text + "</dt>")
                elif child.kind == blocks.DESCRIPTION_DETAILS:
                    parts.append("<dd>" + _render_item_content(doc, child.children) + "</dd>")
        parts.append("</dl>")
        return "".join(parts)
    names = {
        blocks.CODE_BLOCK: "コードブロック",
        blocks.HEADING: "見出し",
        blocks.BLOCK_QUOTE: "引用",
        blocks.ALERT: "アラート",
        blocks.MULTILINE_BLOCK_QUOTE: "引用",
        blocks.THEMATIC_BREAK: "区切り線",
        blocks.TABLE: "表",
        blocks.FOOTNOTE_DEFINITION: "脚注の定義",
    }
    raise NotRepresentable("%sはパイプ表のセルに書けない" % names.get(kind, kind))


def _render_item_content(doc, children):
    parts = []
    previous = None
    for child in children:
        if child.kind == blocks.PARAGRAPH:
            piece = _join_paragraph(_paragraph_lines(doc, child), "<br>")
        elif child.kind in (blocks.LIST, blocks.DESCRIPTION_LIST, blocks.HTML_BLOCK):
            piece = _render_pipe_block(doc, child)
        else:
            piece = _render_pipe_block(doc, child)
        if previous is not None:
            if previous.kind in (blocks.LIST, blocks.DESCRIPTION_LIST) or \
                    child.kind in (blocks.LIST, blocks.DESCRIPTION_LIST):
                parts.append("")
            elif _blank_between(doc, previous, child):
                parts.append("<br><br>")
            else:
                parts.append("<br>")
        parts.append(piece)
        previous = child
    return "".join(parts)


def to_pipe_cell(text):
    """Excel のセルの文字を、パイプ表のセルの 1 行にする。書けなければ NotRepresentable。"""
    if not text.strip():
        return ""
    # リンク参照の定義に見える行も、パイプ表のセルの中ではただの文字になるので段落として扱う
    doc = parse(text)
    rendered = _render_pipe_blocks(doc, doc.root.children)
    return escape_pipes(rendered).strip(" \t")


# --- HTML の表のセル ---

_PLAIN_FORBIDDEN = re.compile(r"[\\`*_~\[\]<>&$]")
_STRUCTURAL_TAGS = frozenset(["table", "caption", "colgroup", "col", "thead", "tbody", "tfoot", "tr", "td", "th"])
_HTML_BLOCK_END = {
    1: re.compile(r"</(?:script|pre|textarea|style)>", re.I),
    2: re.compile(r"-->"),
    3: re.compile(r"\?>"),
    4: re.compile(r">"),
    5: re.compile(r"\]\]>"),
}
_RELAXED_TASK = re.compile(r"\[([^\]\s])\](?=[ \t]|$)")


def is_plain(text):
    """Markdown の記号もブロックの記法も無い文字か（<td>文字</td> の 1 行で書ける）。"""
    if not text or _PLAIN_FORBIDDEN.search(text):
        return False
    for line in text.split("\n"):
        if not line or line != line.strip(" \t\u3000"):
            return False
        if inline.escape_line_start(line) != line:
            return False
    return True


def _paragraph_like(doc):
    """段落と同じく、中の改行が表示上の改行にならない（空白になる）ブロック。"""
    for node in doc.root.walk():
        if node.kind == blocks.PARAGRAPH:
            yield node
        elif node.kind == blocks.HEADING and node.info.get("setext"):
            yield node


def _line_breaks(doc, node):
    """段落の中の改行ごとに (行番号, ひとかたまりの中か, 改行マークがあるか)。"""
    parts = [doc.lines[n][o:] for n, o, _lazy in node.lines]
    content = "\n".join(parts)
    spans = inline.atom_spans(content)
    result = []
    position = 0
    for index in range(len(parts) - 1):
        position += len(parts[index])
        line_no = node.lines[index][0]
        in_atom = inline.position_in_spans(position, spans)
        _stripped, marked = strip_break_marker(doc.lines[line_no])
        result.append((line_no, in_atom, marked))
        position += 1
    return result


def _structural_tags_outside(doc, text):
    """コードの外にある表の構造のタグ（入れ子の完全な <table> の中は除く）。"""
    code_lines = set()
    for node in doc.root.walk():
        if node.kind == blocks.CODE_BLOCK:
            for line_no in range(node.start_line, node.end_line + 1):
                code_lines.add(line_no)
    rest = "\n".join(line if i not in code_lines else "" for i, line in enumerate(doc.lines))
    depth = 0
    for token in inline.tokenize(rest):
        if token.kind != "html" or token.tag not in _STRUCTURAL_TAGS:
            continue
        if token.tag == "table":
            depth += -1 if token.closing else 1
            if depth < 0:
                return True
            continue
        if depth == 0:
            return True
    return depth != 0


def to_html_cell(text):
    """Excel のセルの文字を、HTML の表のセルの中身にする。

    ("inline", 文字) か ("chunk", 行のリスト) と、警告のリストを返す。
    書けなければ NotRepresentable。
    """
    warnings = []
    if not text:
        return "inline", "", warnings
    if is_plain(text):
        return "inline", "<br>".join(text.split("\n")), warnings
    doc = parse(text)
    lines = list(doc.lines)
    if _structural_tags_outside(doc, text):
        raise NotRepresentable("セルの中に表の構造のタグ（<td> など）がある")
    for node in doc.root.walk():
        if node.kind == blocks.HTML_BLOCK and node.info["block_type"] in _HTML_BLOCK_END:
            last_line, offset, _lazy = node.lines[-1]
            if not _HTML_BLOCK_END[node.info["block_type"]].search(lines[last_line][offset:]):
                raise NotRepresentable("セルの中に閉じていない HTML（<pre>・<!-- など）がある")

    suffix = {}
    for node in _paragraph_like(doc):
        for line_no, in_atom, marked in _line_breaks(doc, node):
            if in_atom or marked:
                continue
            body = lines[line_no].rstrip(" ")
            count = len(body) - len(body.rstrip("\\"))
            suffix[line_no] = " <br>" if count % 2 == 1 else "<br>"
    for line_no, ending in suffix.items():
        lines[line_no] = lines[line_no].rstrip(" ") + ending

    # リンク参照の定義（文書全体に効く）と、GitLab がタスクにする [注] などはエスケープする
    for node in doc.root.walk():
        if node.kind == blocks.PARAGRAPH and node.info.get("only_link_definitions"):
            for line_no, offset, _lazy in node.lines:
                if lines[line_no][offset:].startswith("["):
                    lines[line_no] = lines[line_no][:offset] + "\\" + lines[line_no][offset:]
        if node.kind == blocks.ITEM and node.children and node.children[0].kind == blocks.PARAGRAPH:
            line_no, offset, _lazy = node.children[0].lines[0]
            match = _RELAXED_TASK.match(lines[line_no], offset)
            if match and match.group(1) not in (" ", "x", "X"):
                lines[line_no] = lines[line_no][:offset] + "\\" + lines[line_no][offset:]

    # 閉じていないフェンスは閉じる
    closing = []
    for node in doc.root.walk():
        if node.kind == blocks.CODE_BLOCK and node.info.get("fenced") and not node.info.get("closed"):
            # 開きのフェンスと同じ位置（リストの中なら項目の本文の位置）で閉じる
            indent = " " * node.start_offset
            closing.append(indent + node.info["fence_char"] * node.info["fence_length"])
            warnings.append("閉じていないコードブロックを閉じた")
    lines.extend(reversed(closing))

    # markdownlint の自動修正（MD031・MD032）が入らないよう、一番外側のリストとフェンスの前後に空行を入れる
    blank_before = set()
    blank_after = set()
    children = doc.root.children
    for index, node in enumerate(children):
        if node.kind in (blocks.LIST, blocks.CODE_BLOCK) or (
                node.kind == blocks.CODE_BLOCK and node.info.get("fenced")):
            if index > 0 and node.start_line > 0 and lines[node.start_line - 1].strip():
                blank_before.add(node.start_line)
            if node.kind == blocks.CODE_BLOCK and index + 1 < len(children):
                following = children[index + 1].start_line
                if following == node.end_line + 1:
                    blank_after.add(node.end_line)
    result = []
    for index, line in enumerate(lines):
        if index in blank_before:
            result.append("")
        result.append(line)
        if index in blank_after:
            result.append("")
    return "chunk", result, warnings


def chunk_to_excel(text):
    """HTML の表のセルの Markdown の部分を、Excel のセルの文字にする（to_html_cell の逆）。"""
    doc = parse(text)
    lines = list(doc.lines)
    join_next = {}
    marker_lines = set()
    next_offset = {}
    for node in _paragraph_like(doc):
        for index, (line_no, in_atom, marked) in enumerate(_line_breaks(doc, node)):
            if marked and not in_atom:
                marker_lines.add(line_no)
            else:
                join_next[line_no] = True
                next_offset[line_no] = node.lines[index + 1][1]
    out = []
    index = 0
    while index < len(lines):
        line = lines[index]
        while join_next.get(index) and index + 1 < len(lines):
            following = lines[index + 1][next_offset[index]:].lstrip(" \t")
            line = line.rstrip(" \t") + " " + following
            index += 1
        if index in marker_lines:
            line, _marked = strip_break_marker(line)
        out.append(line)
        index += 1
    return normalize_text("\n".join(out))
