"""comrak（GitLab の Markdown の解析器）と同じ規則で、Markdown のブロックの構造を読む。

インラインの解析はしない。表の検出、HTML の表の中の「HTML ブロック」と「Markdown の部分」の
区別、セル内の Markdown の行の役割の判定に使う、ブロックの木と行ごとの位置だけを作る。
規則は comrak の src/parser/mod.rs・table.rs・scanners.re をそのまま写している
（GitLab が有効にしている拡張: 表・説明リスト・脚注・アラート・>>> の引用）。
"""

import re

TAB_STOP = 4
CODE_INDENT = 4
MAX_LIST_DEPTH = 100

DOCUMENT = "document"
FRONT_MATTER = "front_matter"
BLOCK_QUOTE = "block_quote"
ALERT = "alert"
MULTILINE_BLOCK_QUOTE = "multiline_block_quote"
LIST = "list"
ITEM = "item"
DESCRIPTION_LIST = "description_list"
DESCRIPTION_ITEM = "description_item"
DESCRIPTION_TERM = "description_term"
DESCRIPTION_DETAILS = "description_details"
FOOTNOTE_DEFINITION = "footnote_definition"
PARAGRAPH = "paragraph"
HEADING = "heading"
THEMATIC_BREAK = "thematic_break"
CODE_BLOCK = "code_block"
HTML_BLOCK = "html_block"
TABLE = "table"
TABLE_ROW = "table_row"

# ブロックを子に持てる入れ物（リスト項目以外のブロックを入れられる）
BLOCK_CONTAINERS = frozenset([
    DOCUMENT, BLOCK_QUOTE, ALERT, MULTILINE_BLOCK_QUOTE, FOOTNOTE_DEFINITION,
    DESCRIPTION_TERM, DESCRIPTION_DETAILS, ITEM,
])
ACCEPTS_LINES = frozenset([PARAGRAPH, HEADING, CODE_BLOCK])

_SPACECHARS = " \t\x0b\x0c\r\n"
_ASCII_DIGITS = "0123456789"

_BLOCK_TAG_NAMES = (
    "address|article|aside|base|basefont|blockquote|body|caption|center|col|colgroup|dd|"
    "details|dialog|dir|div|dl|dt|fieldset|figcaption|figure|footer|form|frame|frameset|"
    "h1|h2|h3|h4|h5|h6|head|header|hr|html|iframe|legend|li|link|main|menu|menuitem|nav|"
    "noframes|ol|optgroup|option|p|param|search|section|title|summary|table|tbody|td|tfoot|"
    "th|thead|tr|track|ul"
)
_ATTRIBUTE = (
    r"(?:[ \t\x0b\x0c\r\n]+[a-zA-Z_:][a-zA-Z0-9:._-]*"
    r"(?:[ \t\x0b\x0c\r\n]*=[ \t\x0b\x0c\r\n]*"
    r"(?:[^ \t\r\n\x0b\x0c\"'=<>`]+|'[^']*'|\"[^\"]*\"))?)"
)
_OPEN_TAG = r"[A-Za-z][A-Za-z0-9-]*" + _ATTRIBUTE + r"*[ \t\x0b\x0c\r\n]*/?>"
_CLOSE_TAG = r"/[A-Za-z][A-Za-z0-9-]*[ \t\x0b\x0c\r\n]*>"

_HTML_BLOCK_START = [
    (1, re.compile(r"<(?:script|pre|textarea|style)(?:[ \t\x0b\x0c\r\n]|>)", re.I)),
    (2, re.compile(r"<!--")),
    (3, re.compile(r"<\?")),
    (4, re.compile(r"<![A-Za-z]")),
    (5, re.compile(r"<!\[CDATA\[")),
    (6, re.compile(r"</?(?:" + _BLOCK_TAG_NAMES + r")(?:[ \t\x0b\x0c\r\n]|/?>)", re.I)),
]
_HTML_BLOCK_START_7 = re.compile(r"<(?:" + _OPEN_TAG + "|" + _CLOSE_TAG + r")[\t\n\x0c ]*[\r\n]")
_HTML_BLOCK_END = {
    1: re.compile(r"[^\n]*</(?:script|pre|textarea|style)>", re.I),
    2: re.compile(r"[^\n]*-->"),
    3: re.compile(r"[^\n]*\?>"),
    4: re.compile(r"[^\n]*>"),
    5: re.compile(r"[^\n]*\]\]>"),
}

_ATX_HEADING_START = re.compile(r"#{1,6}(?:[ \t]+|[\r\n])")
_OPEN_CODE_FENCE = re.compile(r"(`{3,})(?=[^`\r\n]*[\r\n])|(~{3,})(?=[^\r\n]*[\r\n])")
_CLOSE_CODE_FENCE = re.compile(r"(`{3,}|~{3,})(?=[ \t]*[\r\n])")
_SETEXT_HEADING_LINE = re.compile(r"(=+|-+)[ \t]*[\r\n]")
_FOOTNOTE_DEFINITION = re.compile(r"\[\^[^\] \r\n\t]+\]:[ \t]*")
_DESCRIPTION_ITEM_START = re.compile(r"[:~][ \t]+")
_MULTILINE_FENCE = re.compile(r">{3,}(?=[ \t]*[\r\n])")
_ALERT_START = re.compile(r">+ \[!(?:note|tip|important|warning|caution)\]", re.I)
_TABLE_START = re.compile(
    r"\|?[ \t\x0b\x0c]*:?-+:?[ \t\x0b\x0c]*"
    r"(?:\|[ \t\x0b\x0c]*:?-+:?[ \t\x0b\x0c]*)*"
    r"\|?[ \t\x0b\x0c]*(?:\r\n|\r|\n|$)"
)
_FRONT_MATTER_OPEN = re.compile(r"(---|\+\+\+|;;;)[A-Za-z0-9_-]*[ \t]*$")


class Node(object):
    """ブロックの木の節。行番号と位置は 0 起点。"""

    __slots__ = (
        "kind", "parent", "children", "open", "start_line", "start_offset", "end_line",
        "lines", "info", "last_line_blank", "table_visited",
    )

    def __init__(self, kind, start_line, start_offset, info=None):
        self.kind = kind
        self.parent = None
        self.children = []
        self.open = True
        self.start_line = start_line
        self.start_offset = start_offset
        self.end_line = start_line
        # 葉のブロックが受け取った行: (行番号, 中身の開始位置, 遅延行か)
        self.lines = []
        self.info = info if info is not None else {}
        self.last_line_blank = False
        self.table_visited = False

    def __repr__(self):
        return "<%s %d-%d>" % (self.kind, self.start_line, self.end_line)

    def append(self, child):
        child.parent = self
        self.children.append(child)

    def detach(self):
        if self.parent is not None:
            self.parent.children.remove(self)
            self.parent = None

    def insert_before(self, node):
        siblings = self.parent.children
        node.parent = self.parent
        siblings.insert(siblings.index(self), node)

    def insert_after(self, node):
        siblings = self.parent.children
        node.parent = self.parent
        siblings.insert(siblings.index(self) + 1, node)

    def walk(self):
        yield self
        for child in self.children:
            for node in child.walk():
                yield node

    def ancestors(self):
        node = self.parent
        while node is not None:
            yield node
            node = node.parent


class LineInfo(object):
    """1 行を読んだときの、コンテナの接頭辞などの情報。"""

    __slots__ = ("matched", "all_matched", "prefix_end", "lazy", "offsets")

    def __init__(self):
        self.matched = None      # 続きとして一致した一番深いコンテナ
        self.all_matched = False
        self.prefix_end = 0      # 一致したコンテナの接頭辞の終わり（行の中の位置）
        self.lazy = False        # 段落の遅延行（コンテナの接頭辞が足りないまま続いた行）
        # コンテナごとの、その接頭辞（記号を含む）を読み終えた位置: [(節, 位置), ...]
        self.offsets = []

    def offset_after(self, node):
        """node の接頭辞の終わりの位置。この行で node が続いていなければ None。"""
        if node.kind == DOCUMENT:
            return 0
        for candidate, offset in self.offsets:
            if candidate is node:
                return offset
        return None


class TableCell(object):
    __slots__ = ("start", "end", "raw", "content")

    def __init__(self, start, end, raw, content):
        self.start = start
        self.end = end
        self.raw = raw            # セルの元の文字（前後の空白とエスケープを含む）
        self.content = content    # comrak が使う中身（\| を戻し、前後の空白を除いたもの）


class TableRowParse(object):
    __slots__ = ("paragraph_offset", "cells")

    def __init__(self, paragraph_offset, cells):
        self.paragraph_offset = paragraph_offset
        self.cells = cells


def _is_space(ch):
    return ch != "" and ch in _SPACECHARS


def _strip_ascii_space(text):
    return text.strip(" \t\n\r\x0b\x0c")


def _table_cell_length(s, i):
    # comrak の table_cell = (escaped_char | [^|\r\n])+ の最長一致。バックスラッシュは
    # 普通の文字としても読めるので、直前に \ がある | は（\ が何個でも）区切りにならない
    j = i
    n = len(s)
    while j < n:
        ch = s[j]
        if ch == "\r" or ch == "\n":
            break
        if ch == "|":
            if j > i and s[j - 1] == "\\":
                j += 1
                continue
            break
        j += 1
    return j - i


def _table_cell_end_length(s, i):
    # [|] table_spacechar*
    if i < len(s) and s[i] == "|":
        j = i + 1
        while j < len(s) and s[j] in " \t\x0b\x0c":
            j += 1
        return j - i
    return 0


def _table_row_end_length(s, i):
    # table_spacechar* table_newline
    j = i
    while j < len(s) and s[j] in " \t\x0b\x0c":
        j += 1
    if s.startswith("\r\n", j):
        return j + 2 - i
    if j < len(s) and s[j] in "\r\n":
        return j + 1 - i
    return 0


def unescape_pipes(text):
    """comrak と同じく、奇数個の \\ の直後の | から \\ を 1 つ除く。"""
    out = []
    i = 0
    n = len(text)
    while i < n:
        ch = text[i]
        if ch == "\\":
            if i + 1 < n and text[i + 1] == "|":
                out.append("|")
            else:
                out.append(text[i:i + 2])
            i += 2
            continue
        out.append(ch)
        i += 1
    return "".join(out)


def parse_table_row(s):
    """comrak の table.rs の row()。s は行末の改行を含む。読めなければ None。"""
    n = len(s)
    cells = []
    offset = _table_cell_end_length(s, 0)
    paragraph_offset = 0
    while offset < n:
        cell_len = _table_cell_length(s, offset)
        pipe_len = _table_cell_end_length(s, offset + cell_len)
        if cell_len > 0 or pipe_len > 0:
            raw = s[offset:offset + cell_len]
            cells.append(TableCell(offset, offset + cell_len, raw,
                                   _strip_ascii_space(unescape_pipes(raw))))
        offset += cell_len + pipe_len
        if pipe_len == 0:
            row_end = _table_row_end_length(s, offset)
            offset += row_end
            if row_end == 0 or offset == n:
                break
            paragraph_offset = offset
            cells = []
            offset += _table_cell_end_length(s, offset)
    if offset != n or not cells:
        return None
    return TableRowParse(paragraph_offset, cells)


def is_delimiter_row(text):
    """表の区切り行の形か（text は行末の改行を含まなくてよい）。"""
    return _TABLE_START.match(text) is not None


def parse_list_marker(line, pos, interrupts_paragraph):
    """comrak の parse_list_marker。(記号の長さ, 情報) か None。"""
    n = len(line)
    if pos >= n:
        return None
    ch = line[pos]
    start = pos
    if ch in "*-+":
        pos += 1
        if pos < n and not _is_space(line[pos]):
            return None
        if interrupts_paragraph:
            i = pos
            if i == n:
                return None
            while line[i] in " \t":
                i += 1
                if i == n:
                    return None
            if line[i] in "\r\n":
                return None
        return pos - start, {"type": "bullet", "bullet_char": ch, "delimiter": None, "start": 1}
    if ch in _ASCII_DIGITS:
        number = 0
        digits = 0
        while True:
            number = number * 10 + int(line[pos])
            pos += 1
            digits += 1
            if pos == n:
                return None
            if not (digits < 9 and line[pos] in _ASCII_DIGITS):
                break
        if interrupts_paragraph and number != 1:
            return None
        ch = line[pos]
        if ch not in ".)":
            return None
        pos += 1
        if pos == n or not _is_space(line[pos]):
            return None
        if interrupts_paragraph:
            i = pos
            while line[i] in " \t":
                i += 1
                if i == n:
                    return None
            if line[i] in "\r\n":
                return None
        return pos - start, {"type": "ordered", "bullet_char": None, "delimiter": ch, "start": number}
    return None


def _lists_match(a, b):
    return (a["type"] == b["type"] and a["delimiter"] == b["delimiter"]
            and a["bullet_char"] == b["bullet_char"])


def _can_contain(parent_kind, child_kind):
    if child_kind == DOCUMENT:
        return False
    if child_kind == FRONT_MATTER:
        return parent_kind == DOCUMENT
    if parent_kind in BLOCK_CONTAINERS:
        return child_kind != ITEM
    if parent_kind == LIST:
        return child_kind == ITEM
    if parent_kind == DESCRIPTION_LIST:
        return child_kind == DESCRIPTION_ITEM
    if parent_kind == DESCRIPTION_ITEM:
        return child_kind in (DESCRIPTION_TERM, DESCRIPTION_DETAILS)
    if parent_kind == TABLE:
        return child_kind == TABLE_ROW
    return False


_LINK_LABEL = r"\[(?:[^\\\[\]]|\\.){0,999}\]"
_LINK_REFERENCE_DEFINITION = re.compile(
    r"[ ]{0,3}" + _LINK_LABEL + r":[ \t]*(?:\n[ \t]*)?"
    r"(?:<[^<>\n]*>|[^\s<][^\s]*)"
    r"(?:(?:[ \t]+|[ \t]*\n[ \t]*)(?:\"(?:[^\"\\]|\\.)*\"|'(?:[^'\\]|\\.)*'|\((?:[^()\\]|\\.)*\)))?"
    r"[ \t]*(?:\n|$)"
)


def strip_link_reference_definitions(content):
    """段落の先頭のリンク参照定義を除いた残りを返す（comrak の解決の近似）。"""
    pos = 0
    while pos < len(content):
        match = _LINK_REFERENCE_DEFINITION.match(content, pos)
        if not match or match.end() == pos:
            break
        if not re.search(r"\S", content[pos:match.end()]):
            break
        label = re.match(r"[ ]{0,3}(" + _LINK_LABEL + ")", content[pos:]).group(1)
        if not re.search(r"\S", label[1:-1]):
            break
        pos = match.end()
    return content[pos:]


class _Parser(object):
    def __init__(self, lines, front_matter=True, keep_definitions=False):
        self.keep_definitions = keep_definitions
        self.source_lines = lines
        self.root = Node(DOCUMENT, 0, 0)
        self.current = self.root
        self.line_number = -1
        self.line_infos = []
        self.offset = 0
        self.column = 0
        self.first_nonspace = 0
        self.first_nonspace_column = 0
        self.indent = 0
        self.blank = False
        self.partially_consumed_tab = False
        self.thematic_break_kill_pos = 0
        self.front_matter = front_matter

    # --- 位置の移動 ---

    def find_first_nonspace(self, line):
        chars_to_tab = TAB_STOP - (self.column % TAB_STOP)
        if self.first_nonspace <= self.offset:
            self.first_nonspace = self.offset
            self.first_nonspace_column = self.column
            while True:
                ch = line[self.first_nonspace] if self.first_nonspace < len(line) else ""
                if ch == " ":
                    self.first_nonspace += 1
                    self.first_nonspace_column += 1
                    chars_to_tab -= 1
                    if chars_to_tab == 0:
                        chars_to_tab = TAB_STOP
                elif ch == "\t":
                    self.first_nonspace += 1
                    self.first_nonspace_column += chars_to_tab
                    chars_to_tab = TAB_STOP
                else:
                    break
        self.indent = self.first_nonspace_column - self.column
        ch = line[self.first_nonspace] if self.first_nonspace < len(line) else ""
        self.blank = ch == "" or ch in "\r\n"

    def advance_offset(self, line, count, columns):
        while count > 0:
            ch = line[self.offset]
            if ch == "\t":
                chars_to_tab = TAB_STOP - (self.column % TAB_STOP)
                if columns:
                    self.partially_consumed_tab = chars_to_tab > count
                    chars_to_advance = min(count, chars_to_tab)
                    self.column += chars_to_advance
                    if not self.partially_consumed_tab:
                        self.offset += 1
                    count -= chars_to_advance
                else:
                    self.partially_consumed_tab = False
                    self.column += chars_to_tab
                    self.offset += 1
                    count -= 1
            else:
                self.partially_consumed_tab = False
                self.offset += 1
                self.column += 1
                count -= 1

    def _char_at(self, line, pos):
        return line[pos] if 0 <= pos < len(line) else ""

    # --- 木の操作 ---

    def add_child(self, parent, kind, start_offset, info=None):
        while not _can_contain(parent.kind, kind):
            parent = self.finalize(parent)
        node = Node(kind, self.line_number, start_offset, info)
        parent.append(node)
        return node

    def add_line(self, node, line, lazy=False):
        if self.partially_consumed_tab:
            self.offset += 1
        node.lines.append((self.line_number, self.offset, lazy))
        node.end_line = self.line_number

    def finalize(self, node):
        node.open = False
        parent = node.parent
        if node.kind == PARAGRAPH:
            content = self.node_content(node)
            if not strip_link_reference_definitions(content).strip(" \t\n\r\x0b\x0c"):
                node.info["only_link_definitions"] = True
                if not self.keep_definitions:
                    node.detach()
        elif node.kind == LIST:
            node.info["tight"] = self._list_tight(node)
        return parent

    def node_content(self, node):
        parts = []
        for line_no, offset, _lazy in node.lines:
            parts.append(self.source_lines[line_no][offset:] + "\n")
        return "".join(parts)

    def _ends_with_blank_line(self, node):
        while node is not None:
            if node.last_line_blank:
                return True
            if node.kind in (LIST, ITEM):
                node = node.children[-1] if node.children else None
            else:
                node = None
        return False

    def _list_tight(self, node):
        items = node.children
        for index, item in enumerate(items):
            has_next = index + 1 < len(items)
            if item.last_line_blank and has_next:
                return False
            for sub_index, sub in enumerate(item.children):
                sub_has_next = sub_index + 1 < len(item.children)
                if (has_next or sub_has_next) and self._ends_with_blank_line(sub):
                    return False
        return True

    # --- 行の処理 ---

    def parse(self):
        lines = self.source_lines
        start = 0
        if self.front_matter and lines:
            match = _FRONT_MATTER_OPEN.match(lines[0])
            if match:
                delimiter = match.group(1)
                for index in range(1, len(lines)):
                    if lines[index].rstrip(" \t") == delimiter:
                        node = Node(FRONT_MATTER, 0, 0)
                        node.end_line = index
                        node.open = False
                        self.root.append(node)
                        start = index + 1
                        break
        for index in range(start):
            info = LineInfo()
            info.matched = self.root
            info.all_matched = True
            self.line_infos.append(info)
        self.line_number = start - 1
        for index in range(start, len(lines)):
            self.process_line(lines[index] + "\n")
        while self.current is not self.root:
            self.current = self.finalize(self.current)
        self.finalize(self.root)
        _compute_end_lines(self.root, self.source_lines)
        return self.root

    def process_line(self, line):
        self.offset = 0
        self.column = 0
        self.first_nonspace = 0
        self.first_nonspace_column = 0
        self.indent = 0
        self.thematic_break_kill_pos = 0
        self.blank = False
        self.partially_consumed_tab = False
        self.line_number += 1
        info = LineInfo()
        self.line_infos.append(info)
        self.info = info
        result = self.check_open_blocks(line)
        if result is None:
            info.prefix_end = self.offset
            return
        last_matched, all_matched = result
        info.matched = last_matched
        info.all_matched = all_matched
        info.prefix_end = self.offset
        current = self.current
        container = self.open_new_blocks(last_matched, line, all_matched)
        if current is self.current:
            self.add_text_to_container(container, last_matched, line, info)

    def check_open_blocks(self, line):
        container = self.root
        all_matched = False
        while True:
            last = container.children[-1] if container.children else None
            if last is None or not last.open:
                all_matched = True
                break
            container = last
            self.find_first_nonspace(line)
            kind = container.kind
            if kind == BLOCK_QUOTE or (kind == ALERT and not container.info["multiline"]):
                if not self.parse_block_quote_prefix(line):
                    break
            elif kind == ITEM or kind == DESCRIPTION_ITEM:
                if not self.parse_item_prefix(line, container):
                    break
            elif kind == CODE_BLOCK:
                matched = self.parse_code_block_prefix(line, container)
                if matched is None:
                    return None
                if not matched:
                    break
            elif kind == HTML_BLOCK:
                if container.info["block_type"] in (6, 7) and self.blank:
                    break
            elif kind == PARAGRAPH:
                if self.blank:
                    break
            elif kind == TABLE:
                if parse_table_row(line[self.first_nonspace:]) is None:
                    break
            elif kind in (HEADING, TABLE_ROW):
                break
            elif kind == FOOTNOTE_DEFINITION:
                if not self.parse_footnote_definition_prefix(line):
                    break
            elif kind == MULTILINE_BLOCK_QUOTE or (kind == ALERT and container.info["multiline"]):
                if self.parse_multiline_block_quote_prefix(line, container) is None:
                    return None
            self.info.offsets.append((container, self.offset))
        if not all_matched:
            container = container.parent
        return container, all_matched

    def parse_block_quote_prefix(self, line):
        if self.indent <= 3 and self._char_at(line, self.first_nonspace) == ">":
            self.advance_offset(line, self.indent + 1, True)
            if self._char_at(line, self.offset) in (" ", "\t"):
                self.advance_offset(line, 1, True)
            return True
        return False

    def parse_item_prefix(self, line, container):
        need = container.info["marker_offset"] + container.info["padding"]
        if self.indent >= need:
            self.advance_offset(line, need, True)
            return True
        if self.blank and container.children:
            self.advance_offset(line, self.first_nonspace - self.offset, False)
            return True
        return False

    def parse_code_block_prefix(self, line, container):
        info = container.info
        if not info["fenced"]:
            if self.indent >= CODE_INDENT:
                self.advance_offset(line, CODE_INDENT, True)
                return True
            if self.blank:
                self.advance_offset(line, self.first_nonspace - self.offset, False)
                return True
            return False
        matched = 0
        if self.indent <= 3 and self._char_at(line, self.first_nonspace) == info["fence_char"]:
            match = _CLOSE_CODE_FENCE.match(line, self.first_nonspace)
            if match:
                matched = len(match.group(1))
        if matched >= info["fence_length"]:
            info["closed"] = True
            info["close_line"] = self.line_number
            container.end_line = self.line_number
            self.advance_offset(line, matched, False)
            self.current = self.finalize(container)
            return None
        remaining = info["fence_offset"]
        while remaining > 0 and self._char_at(line, self.offset) in (" ", "\t"):
            self.advance_offset(line, 1, True)
            remaining -= 1
        return True

    def parse_footnote_definition_prefix(self, line):
        if self.indent >= 4:
            self.advance_offset(line, 4, True)
            return True
        return line in ("\n", "\r\n")

    def parse_multiline_block_quote_prefix(self, line, container):
        info = container.info
        matched = 0
        if self.indent <= 3 and self._char_at(line, self.first_nonspace) == ">":
            match = _MULTILINE_FENCE.match(line, self.first_nonspace)
            if match:
                matched = len(match.group(0))
        if matched >= info["fence_length"]:
            self.advance_offset(line, matched, False)
            child = container.children[-1] if container.children else None
            if child is not None and child.open:
                while (child.children and child.children[-1].open and child.kind != LIST):
                    child = child.children[-1]
                self.finalize(child)
            container.end_line = self.line_number
            self.current = self.finalize(container)
            return None
        remaining = info["fence_offset"]
        while remaining > 0 and self._char_at(line, self.offset) in (" ", "\t"):
            self.advance_offset(line, 1, True)
            remaining -= 1
        return True

    def open_new_blocks(self, container, line, all_matched):
        maybe_lazy = self.current.kind == PARAGRAPH
        depth = 0
        while container.kind not in (CODE_BLOCK, HTML_BLOCK):
            depth += 1
            self.find_first_nonspace(line)
            indented = self.indent >= CODE_INDENT
            new = None
            if not indented:
                new = (self.handle_alert(container, line)
                       or self.handle_multiline_blockquote(container, line)
                       or self.handle_blockquote(container, line)
                       or self.handle_atx_heading(container, line)
                       or self.handle_code_fence(container, line)
                       or self.handle_html_block(container, line)
                       or self.handle_setext_heading(container, line)
                       or self.handle_thematic_break(container, line, all_matched)
                       or self.handle_footnote(container, line, depth)
                       or self.handle_description_list(container, line))
            if new is None:
                new = (self.handle_list(container, line, indented, depth)
                       or self.handle_code_block(container, line, indented, maybe_lazy)
                       or self.handle_table(container, line, indented))
            if new is None:
                break
            container = new
            self.info.offsets.append((container, self.offset))
            if container.kind in ACCEPTS_LINES:
                break
            maybe_lazy = False
        return container

    def handle_alert(self, container, line):
        if self._char_at(line, self.first_nonspace) != ">":
            return None
        if not _ALERT_START.match(line, self.first_nonspace):
            return None
        pos = self.first_nonspace
        fence_length = 0
        while line[pos] != "]":
            if line[pos] == ">":
                fence_length += 1
            pos += 1
        if fence_length == 2:
            return None
        start = self.first_nonspace
        self.advance_offset(line, len(line) - 1 - self.offset, False)
        return self.add_child(container, ALERT, start, {
            "multiline": fence_length >= 3, "fence_length": fence_length, "fence_offset": self.indent,
        })

    def handle_multiline_blockquote(self, container, line):
        match = _MULTILINE_FENCE.match(line, self.first_nonspace)
        if not match:
            return None
        matched = len(match.group(0))
        start = self.first_nonspace
        node = self.add_child(container, MULTILINE_BLOCK_QUOTE, start, {
            "fence_length": matched, "fence_offset": self.indent,
        })
        self.advance_offset(line, self.first_nonspace + matched - self.offset, False)
        return node

    def handle_blockquote(self, container, line):
        if self._char_at(line, self.first_nonspace) != ">":
            return None
        start = self.first_nonspace
        self.advance_offset(line, self.first_nonspace + 1 - self.offset, False)
        if self._char_at(line, self.offset) in (" ", "\t"):
            self.advance_offset(line, 1, True)
        return self.add_child(container, BLOCK_QUOTE, start)

    def handle_atx_heading(self, container, line):
        match = _ATX_HEADING_START.match(line, self.first_nonspace)
        if not match:
            return None
        start = self.first_nonspace
        level = len(re.match(r"#+", line[start:]).group(0))
        matched = match.end() - match.start()
        if line[match.end() - 1] in "\r\n":
            # 改行は残す（行の終わりの位置に止める）
            matched -= 1
        self.advance_offset(line, start + matched - self.offset, False)
        return self.add_child(container, HEADING, start, {"level": level, "setext": False})

    def handle_code_fence(self, container, line):
        match = _OPEN_CODE_FENCE.match(line, self.first_nonspace)
        if not match:
            return None
        fence = match.group(1) or match.group(2)
        start = self.first_nonspace
        node = self.add_child(container, CODE_BLOCK, start, {
            "fenced": True, "fence_char": fence[0], "fence_length": len(fence),
            "fence_offset": self.indent, "closed": False,
        })
        self.advance_offset(line, start + len(fence) - self.offset, False)
        return node

    def handle_html_block(self, container, line):
        if self._char_at(line, self.first_nonspace) != "<":
            return None
        block_type = None
        for kind, pattern in _HTML_BLOCK_START:
            if pattern.match(line, self.first_nonspace):
                block_type = kind
                break
        if block_type is None and container.kind != PARAGRAPH:
            if _HTML_BLOCK_START_7.match(line, self.first_nonspace):
                block_type = 7
        if block_type is None:
            return None
        return self.add_child(container, HTML_BLOCK, self.first_nonspace, {"block_type": block_type})

    def handle_setext_heading(self, container, line):
        if container.kind != PARAGRAPH:
            return None
        match = _SETEXT_HEADING_LINE.match(line, self.first_nonspace)
        if not match:
            return None
        content = self.node_content(container)
        if strip_link_reference_definitions(content).strip(" \t\n\r\x0b\x0c"):
            container.kind = HEADING
            container.info = {"level": 1 if match.group(1)[0] == "=" else 2, "setext": True,
                              "underline": self.line_number}
            container.end_line = self.line_number
            self.advance_offset(line, len(line) - 1 - self.offset, False)
        return container

    def handle_thematic_break(self, container, line, all_matched):
        if container.kind == PARAGRAPH and not all_matched:
            return None
        if self.thematic_break_kill_pos > self.first_nonspace:
            return None
        pos = self.first_nonspace
        ch = self._char_at(line, pos)
        if ch not in ("*", "_", "-") or ch == "":
            self.thematic_break_kill_pos = pos
            return None
        count = 1
        nxt = ""
        while True:
            pos += 1
            if pos >= len(line):
                nxt = ""
                break
            nxt = line[pos]
            if nxt == ch:
                count += 1
            elif nxt not in (" ", "\t"):
                break
        if not (count >= 3 and (nxt == "" or nxt in "\r\n")):
            self.thematic_break_kill_pos = pos
            return None
        node = self.add_child(container, THEMATIC_BREAK, self.first_nonspace)
        self.advance_offset(line, len(line) - 1 - self.offset, False)
        return node

    def handle_footnote(self, container, line, depth):
        if depth >= MAX_LIST_DEPTH:
            return None
        match = _FOOTNOTE_DEFINITION.match(line, self.first_nonspace)
        if not match:
            return None
        start = self.first_nonspace
        self.advance_offset(line, match.end() - self.offset, False)
        return self.add_child(container, FOOTNOTE_DEFINITION, start)

    def handle_description_list(self, container, line):
        match = _DESCRIPTION_ITEM_START.match(line, self.first_nonspace)
        if not match:
            return None
        matched = match.end() - match.start()
        details = self._parse_description_details(container, matched)
        if details is None:
            return None
        self.advance_offset(line, self.first_nonspace + matched - self.offset, False)
        if self._char_at(line, self.offset) in (" ", "\t"):
            self.advance_offset(line, 1, True)
        return details

    def _parse_description_details(self, container, matched):
        tight = False
        last_child = container.children[-1] if container.children else None
        if last_child is None:
            # 用語の行の直後（空行なし）の説明
            if container.kind != PARAGRAPH or container.parent is None:
                return None
            tight = True
            container = container.parent
            last_child = container.children[-1]
        if last_child.kind == PARAGRAPH:
            paragraph = last_child
            paragraph.detach()
            paragraph.open = False
            previous = container.children[-1] if container.children else None
            if previous is not None and previous.kind == DESCRIPTION_LIST:
                description_list = previous
                node = description_list
                while node is not None:
                    node.open = True
                    node = node.parent
            else:
                description_list = Node(DESCRIPTION_LIST, paragraph.start_line, paragraph.start_offset)
                container.append(description_list)
            item = Node(DESCRIPTION_ITEM, paragraph.start_line, paragraph.start_offset, {
                "marker_offset": self.indent, "padding": matched, "tight": tight,
            })
            description_list.append(item)
            term = Node(DESCRIPTION_TERM, paragraph.start_line, paragraph.start_offset)
            item.append(term)
            term.append(paragraph)
            details = Node(DESCRIPTION_DETAILS, self.line_number, self.first_nonspace)
            item.append(details)
            return details
        if last_child.kind == DESCRIPTION_ITEM:
            parent = last_child.parent
            item = Node(DESCRIPTION_ITEM, self.line_number, self.first_nonspace, {
                "marker_offset": self.indent, "padding": matched, "tight": last_child.info["tight"],
            })
            parent.append(item)
            details = Node(DESCRIPTION_DETAILS, self.line_number, self.first_nonspace)
            item.append(details)
            return details
        return None

    def handle_list(self, container, line, indented, depth):
        if not ((not indented or container.kind == LIST) and self.indent < 4
                and depth < MAX_LIST_DEPTH):
            return None
        parsed = parse_list_marker(line, self.first_nonspace, container.kind == PARAGRAPH)
        if parsed is None:
            return None
        matched, data = parsed
        marker_start = self.first_nonspace
        self.advance_offset(line, self.first_nonspace + matched - self.offset, False)
        saved = (self.partially_consumed_tab, self.offset, self.column)
        while self.column - saved[2] <= 5 and self._char_at(line, self.offset) in (" ", "\t"):
            self.advance_offset(line, 1, True)
        spaces = self.column - saved[2]
        if not (1 <= spaces < 5) or self._char_at(line, self.offset) in ("\r", "\n", ""):
            data["padding"] = matched + 1
            self.partially_consumed_tab, self.offset, self.column = saved
            if spaces > 0:
                self.advance_offset(line, 1, True)
        else:
            data["padding"] = matched + spaces
        data["marker_offset"] = self.indent
        data["marker_length"] = matched
        if container.kind != LIST or not _lists_match(container.info, data):
            container = self.add_child(container, LIST, marker_start, dict(data))
        return self.add_child(container, ITEM, marker_start, dict(data))

    def handle_code_block(self, container, line, indented, maybe_lazy):
        if not (indented and not maybe_lazy and not self.blank):
            return None
        self.advance_offset(line, CODE_INDENT, True)
        return self.add_child(container, CODE_BLOCK, self.offset, {"fenced": False})

    def handle_table(self, container, line, indented):
        if indented:
            return None
        if container.kind == PARAGRAPH:
            result = self._try_opening_table_header(container, line)
        elif container.kind == TABLE:
            result = self._try_opening_table_row(container, line)
        else:
            return None
        if result is None:
            return None
        new_container, replace, mark_visited = result
        if replace:
            container.insert_after(new_container)
            container.detach()
        if mark_visited:
            new_container.table_visited = True
        return new_container

    def _try_opening_table_header(self, container, line):
        if container.table_visited:
            return container, False, False
        rest = line[self.first_nonspace:]
        if not _TABLE_START.match(rest):
            return container, False, False
        delimiter = parse_table_row(rest)
        if delimiter is None:
            return container, False, True
        header = parse_table_row(self.node_content(container))
        if header is None or len(header.cells) != len(delimiter.cells):
            return container, False, True
        header_line, header_offset, header_lazy = container.lines[-1]
        if header.paragraph_offset > 0:
            # 段落の最後の行より前は、表の前の段落として残す
            preface = Node(PARAGRAPH, container.start_line, container.start_offset)
            preface.lines = container.lines[:-1]
            preface.end_line = preface.lines[-1][0]
            preface.open = False
            container.insert_before(preface)
        alignments = []
        for cell in delimiter.cells:
            text = cell.content
            left = text.startswith(":")
            right = text.endswith(":")
            alignments.append("center" if left and right else "left" if left
                              else "right" if right else None)
        table = Node(TABLE, header_line, header_offset, {
            "alignments": alignments,
            "num_columns": len(header.cells),
            "header": (header_line, header_offset, header_lazy),
            "delimiter": (self.line_number, self.first_nonspace),
            "rows": [],
        })
        table.end_line = self.line_number
        self.advance_offset(line, len(line) - 1 - self.offset, False)
        return table, True, False

    def _try_opening_table_row(self, container, line):
        if self.blank:
            return None
        if parse_table_row(line[self.first_nonspace:]) is None:
            return None
        row = self.add_child(container, TABLE_ROW, self.first_nonspace)
        container.info["rows"].append((self.line_number, self.first_nonspace))
        container.end_line = self.line_number
        self.advance_offset(line, len(line) - 1 - self.offset, False)
        return row, False, False

    def add_text_to_container(self, container, last_matched, line, info):
        self.find_first_nonspace(line)
        if self.blank and container.children:
            container.children[-1].last_line_blank = True
        kind = container.kind
        if kind in (BLOCK_QUOTE, HEADING, THEMATIC_BREAK, MULTILINE_BLOCK_QUOTE, ALERT):
            last_line_blank = False
        elif kind == CODE_BLOCK:
            last_line_blank = not container.info["fenced"]
        elif kind == ITEM:
            last_line_blank = bool(container.children) or container.start_line != self.line_number
        else:
            last_line_blank = True
        container.last_line_blank = self.blank and last_line_blank
        node = container.parent
        while node is not None:
            node.last_line_blank = False
            node = node.parent

        if (self.current is not last_matched and container is last_matched and not self.blank
                and self.current.kind == PARAGRAPH):
            info.lazy = True
            self.add_line(self.current, line, lazy=True)
            return

        while self.current is not last_matched:
            self.current = self.finalize(self.current)

        if kind == CODE_BLOCK:
            self.add_line(container, line)
        elif kind == HTML_BLOCK:
            self.add_line(container, line)
            block_type = container.info["block_type"]
            if block_type in _HTML_BLOCK_END and _HTML_BLOCK_END[block_type].match(line, self.first_nonspace):
                container = self.finalize(container)
        elif self.blank:
            pass
        elif kind in ACCEPTS_LINES:
            self.advance_offset(line, self.first_nonspace - self.offset, False)
            self.add_line(container, line)
        else:
            container = self.add_child(container, PARAGRAPH, self.first_nonspace)
            self.advance_offset(line, self.first_nonspace - self.offset, False)
            self.add_line(container, line)
        self.current = container


def _compute_end_lines(node, source_lines):
    """ブロックの終わりの行を、受け取った行と子から求める（comrak の sourcepos に合わせる）。"""
    kind = node.kind
    if kind in (PARAGRAPH, HEADING, CODE_BLOCK, HTML_BLOCK):
        end = node.start_line
        fenced = kind == CODE_BLOCK and node.info.get("fenced")
        for line_no, offset, _lazy in node.lines:
            if kind == HTML_BLOCK and not source_lines[line_no][offset:].strip(" \t"):
                # HTML ブロックの末尾の空行は含めない（字下げのコードは comrak と同じく含める）
                continue
            end = max(end, line_no)
        if fenced:
            # 閉じていれば閉じフェンスの行まで。閉じていなければ受け取った最後の行まで
            if "close_line" in node.info:
                end = max(end, node.info["close_line"])
            elif node.lines:
                end = max(end, node.lines[-1][0])
        if kind == HEADING and node.info.get("setext"):
            end = max(end, node.info["underline"])
        node.end_line = end
        return end
    end = max(node.start_line, node.end_line)
    for child in node.children:
        end = max(end, _compute_end_lines(child, source_lines))
    if node.kind == TABLE:
        end = max(end, node.info["delimiter"][0])
    node.end_line = end
    return end


class Document(object):
    """解析した結果。root は木、lines は行（改行を除く）、line_infos は行ごとの情報。"""

    def __init__(self, root, lines, line_infos):
        self.root = root
        self.lines = lines
        self.line_infos = line_infos

    def nodes(self, kind=None):
        for node in self.root.walk():
            if kind is None or node.kind == kind:
                yield node

    def text_of(self, node):
        """葉のブロックの中身（行ごとの接頭辞を除いて改行でつないだもの）。"""
        return "".join(self.lines[n][o:] + "\n" for n, o, _lazy in node.lines)


def parse(lines, front_matter=True, keep_definitions=False):
    """改行を除いた行のリストを解析する。

    keep_definitions が真なら、リンク参照の定義だけの段落も消さずに残す（セルの解析用）。
    """
    parser = _Parser(list(lines), front_matter=front_matter, keep_definitions=keep_definitions)
    root = parser.parse()
    return Document(root, parser.source_lines, parser.line_infos)


def parse_text(text, front_matter=True, keep_definitions=False):
    """文字列を解析する（\\r\\n・\\r・\\n で行に分ける）。"""
    return parse(split_lines(text)[0], front_matter=front_matter, keep_definitions=keep_definitions)


_LINE_SPLIT = re.compile(r"\r\n|\r|\n")


def split_lines(text):
    """行と改行コードのリストに分ける。最後の行に改行が無ければ、その改行コードは空。"""
    lines = []
    endings = []
    pos = 0
    for match in _LINE_SPLIT.finditer(text):
        lines.append(text[pos:match.start()])
        endings.append(match.group(0))
        pos = match.end()
    if pos < len(text):
        lines.append(text[pos:])
        endings.append("")
    return lines, endings
