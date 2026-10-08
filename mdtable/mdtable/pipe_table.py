"""パイプ表（GFM の表）の解析、Excel 用の文字への変換、書き戻し。"""

import difflib
import re
from collections import Counter

from . import blocks, inline
from .cell_syntax import NotRepresentable, to_pipe_cell
from .model import Cell, TableModel, normalize_text

_BR = re.compile(r"<br[ \t]*/?>\Z", re.I)
_CANON_OPEN = re.compile(r"<(ul|ol|li|dl|dt|dd)>\Z", re.I)
_CANON_OL_START = re.compile(r"<ol start=(?:\"(\d{1,9})\"|'(\d{1,9})'|(\d{1,9}))>\Z", re.I)
_CANON_CLOSE = re.compile(r"</(ul|ol|li|dl|dt|dd)>\Z", re.I)


# --- パイプ表のセル → Excel のセル ---

def _tag(token):
    if token.kind != "html":
        return None
    match = _CANON_OPEN.match(token.text)
    if match:
        return match.group(1).lower(), False, None
    match = _CANON_OL_START.match(token.text)
    if match:
        return "ol", False, int(next(g for g in match.groups() if g))
    match = _CANON_CLOSE.match(token.text)
    if match:
        return match.group(1).lower(), True, None
    return None


def _is_br(token):
    return token.kind == "html" and bool(_BR.match(token.text))


def _skip_blank(tokens, i):
    while i < len(tokens) and tokens[i].kind == "text" and not tokens[i].text.strip(" \t"):
        i += 1
    return i


def _parse_sequence(tokens, i, stop):
    """tokens[i:] を、stop の終了タグの手前まで読む。(節のリスト, 位置)。"""
    nodes = []
    while i < len(tokens):
        token = tokens[i]
        tag = _tag(token)
        if tag and tag[1] and tag[0] == stop:
            return nodes, i
        if _is_br(token):
            nodes.append(("br",))
            i += 1
            continue
        if tag and not tag[1] and tag[0] in ("ul", "ol", "dl"):
            previous_br = bool(nodes) and nodes[-1][0] == "br"
            parsed = None if previous_br else (
                _parse_list(tokens, i) if tag[0] != "dl" else _parse_dl(tokens, i))
            if parsed is not None:
                node, j = parsed
                if not (j < len(tokens) and _is_br(tokens[j])):
                    nodes.append(node)
                    i = j
                    continue
        nodes.append(("inline", token))
        i += 1
    return nodes, i


def _parse_list(tokens, i):
    name, _closing, start = _tag(tokens[i])
    i += 1
    items = []
    while True:
        i = _skip_blank(tokens, i)
        if i >= len(tokens):
            return None
        tag = _tag(tokens[i])
        if tag == (name, True, None):
            i += 1
            break
        if tag != ("li", False, None):
            return None
        content, j = _parse_sequence(tokens, i + 1, "li")
        if j >= len(tokens) or _tag(tokens[j]) != ("li", True, None):
            return None
        items.append(content)
        i = j + 1
    if not items:
        return None
    return ("list", name == "ol", start if start is not None else 1, items), i


def _parse_dl(tokens, i):
    i += 1
    entries = []
    while True:
        i = _skip_blank(tokens, i)
        if i >= len(tokens):
            return None
        tag = _tag(tokens[i])
        if tag == ("dl", True, None):
            i += 1
            break
        if tag not in (("dt", False, None), ("dd", False, None)):
            return None
        name = tag[0]
        content, j = _parse_sequence(tokens, i + 1, name)
        if j >= len(tokens) or _tag(tokens[j]) != (name, True, None):
            return None
        if name == "dt":
            if entries and entries[-1][0] == "dt":
                return None
            if any(node[0] in ("list", "dl") for node in content):
                return None
        elif not entries:
            return None
        entries.append((name, content))
        i = j + 1
    if not entries or entries[-1][0] == "dt":
        return None
    return ("dl", entries), i


def _inline_lines(nodes):
    """インラインと <br> の並びを行に分ける。各行はトークンのリスト。"""
    lines = [[]]
    for node in nodes:
        if node[0] == "br":
            lines.append([])
        else:
            lines[-1].append(node[1])
    return lines


def _line_text(tokens):
    """行のトークンを文字にし、行頭の空白を除いて、ブロックの記法になる先頭をエスケープする。"""
    text = "".join(t.text for t in tokens)
    stripped = text.lstrip(" \t")
    if not stripped:
        return ""
    leading = len(text) - len(stripped)
    first_kind = None
    position = 0
    for token in tokens:
        if position + len(token.text) > leading:
            first_kind = token.kind
            break
        position += len(token.text)
    if first_kind == "text":
        stripped = inline.escape_line_start(stripped)
    return stripped.rstrip(" \t")


def _list_can_interrupt(node):
    _kind, ordered, start, items = node
    if ordered and start != 1:
        return False
    first = _render_nodes(items[0]) if items else []
    return bool(first and first[0])


def _render_list(node, marker_char):
    _kind, ordered, start, items = node
    lines = []
    for index, item in enumerate(items):
        if ordered:
            marker = "%d%s " % (start + index, marker_char)
        else:
            marker = marker_char + " "
        body = _render_nodes(item) or [""]
        lines.append((marker + body[0]).rstrip(" ") if body[0] else marker.rstrip(" "))
        for line in body[1:]:
            lines.append(" " * len(marker) + line if line else "")
    return lines


def _render_dl(node):
    _kind, entries = node
    lines = []
    for name, content in entries:
        body = _render_nodes(content) or [""]
        if name == "dt":
            if lines:
                lines.append("")
            lines.extend(body)
        else:
            lines.append(": " + body[0] if body[0] else ":")
            for line in body[1:]:
                lines.append("  " + line if line else "")
    return lines


def _render_nodes(nodes):
    """セルの中身の節を、Excel の行のリストにする。"""
    groups = []
    current = []
    for node in nodes:
        if node[0] in ("list", "dl"):
            if current:
                groups.append(("para", current))
                current = []
            groups.append((node[0], node))
        else:
            current.append(node)
    if current:
        groups.append(("para", current))

    lines = []
    previous = None
    previous_list = None
    for kind, value in groups:
        if kind == "para":
            if previous is not None:
                lines.append("")
            lines.extend(_line_text(tokens) for tokens in _inline_lines(value))
            previous_list = None
        elif kind == "list":
            ordered = value[1]
            options = (".", ")") if ordered else ("-", "*")
            marker = options[0]
            if previous_list == (ordered, marker):
                # 隣り合う同じ種類のリストは、記号を変えないと 1 つにつながる
                marker = options[1]
            if previous == "para" and not _list_can_interrupt(value):
                lines.append("")
            elif previous == "dl":
                lines.append("")
            lines.extend(_render_list(value, marker))
            previous_list = (ordered, marker)
        else:
            if previous is not None:
                lines.append("")
            lines.extend(_render_dl(value))
            previous_list = None
        previous = kind
    return lines


def cell_to_excel(content):
    """パイプ表のセルの中身（comrak が読んだもの）を、Excel に書く Markdown にする。"""
    tokens = inline.tokenize(content)
    nodes, _ = _parse_sequence(tokens, 0, None)
    return normalize_text("\n".join(_render_nodes(nodes)))


# --- 元の表の解析 ---

class PipeRow(object):
    def __init__(self, line, prefix, body, raw_cells, contents):
        self.line = line
        self.prefix = prefix
        self.body = body
        self.raw_cells = raw_cells      # セルの元の文字（前後の空白を除く）
        self.contents = contents        # comrak が読んだセルの中身
        self.texts = [cell_to_excel(c) for c in contents]


class PipeTable(object):
    def __init__(self):
        self.header = None
        self.delimiter_prefix = ""
        self.delimiter_body = ""
        self.rows = []
        self.alignments = []
        self.leading_pipe = True
        self.trailing_pipe = True
        self.delimiter_spaced = False
        self.spaced_cells = True
        self.model = None

    def all_rows(self):
        return [self.header] + self.rows


def _parse_row(line, prefix, body):
    parsed = blocks.parse_table_row(body + "\n")
    raw_cells = [blocks._strip_ascii_space(c.raw) for c in parsed.cells]
    contents = [c.content for c in parsed.cells]
    return PipeRow(line, prefix, body, raw_cells, contents)


def analyze(md, region):
    table = PipeTable()
    lines = list(range(region.start, region.end + 1))
    table.header = _parse_row(lines[0], region.prefixes[0], region.bodies[0])
    table.delimiter_prefix = region.prefixes[1]
    table.delimiter_body = region.bodies[1]
    for index in range(2, len(lines)):
        table.rows.append(_parse_row(lines[index], region.prefixes[index], region.bodies[index]))
    table.alignments = list(region.node.info["alignments"])
    columns = len(table.alignments)

    header_body = region.bodies[0].rstrip(" \t")
    table.leading_pipe = region.bodies[0].lstrip(" \t").startswith("|")
    table.trailing_pipe = header_body.endswith("|") and not header_body.endswith("\\|")
    delimiter_cells = blocks.parse_table_row(table.delimiter_body + "\n").cells
    # comrak の区切りは「| と後ろの空白」なので、セルの前の空白は区切りに含まれ、
    # 後ろの空白がセルに残る。後ろの空白で、セルを空白で囲む書き方かを見分ける
    table.delimiter_spaced = any(c.raw[-1:] in (" ", "\t") for c in delimiter_cells)
    raw = [c.raw for c in blocks.parse_table_row(region.bodies[0] + "\n").cells if c.raw.strip()]
    table.spaced_cells = not raw or any(r[-1:] in (" ", "\t") for r in raw)

    cells = []
    for row_index, row in enumerate(table.all_rows()):
        if len(row.contents) > columns:
            region.warnings.append(
                "%d 行目: 見出しより多いセルがある（GitLab では表示されない）。この行を編集すると消える"
                % (row.line + 1))
        for col in range(columns):
            text = row.texts[col] if col < len(row.texts) else ""
            cells.append(Cell(row_index, col, text, header=row_index == 0, align=table.alignments[col]))
    table.model = TableModel("pipe", len(table.rows) + 1, columns, cells)
    region.parsed = table


# --- 書き戻し ---

_DELIMITER = {None: "---", "left": ":---", "right": "---:", "center": ":---:"}


def column_alignments(model, warnings, number):
    """列ごとの配置（列の中で多いもの。混在は警告）。"""
    result = []
    for col in range(model.cols):
        counts = Counter(c.align if c.align != "justify" else None
                         for c in model.cells if c.col == col)
        if not counts:
            result.append(None)
            continue
        best = max(counts.items(), key=lambda item: (item[1], item[0] is None))[0]
        if len(counts) > 1:
            warnings.append("表 %d の %d 列目: 配置が混在しているので、多いほうにそろえた" % (number, col + 1))
        result.append(best)
    return result


def _join_cells(parts, leading, trailing):
    body = "|".join(parts)
    if not leading:
        body = body.lstrip(" ")
    if not trailing:
        body = body.rstrip(" ")
    return ("|" if leading else "") + body + ("|" if trailing else "")


def _format_row(cells, table):
    if table.spaced_cells:
        parts = [" %s " % cell if cell else " " for cell in cells]
    else:
        parts = list(cells)
    leading = (table.leading_pipe or not cells[0]
               or inline.escape_line_start(cells[0]) != cells[0])
    trailing = table.trailing_pipe or not cells[-1]
    return _join_cells(parts, leading, trailing)


def _format_delimiter(alignments, table):
    specs = [_DELIMITER[a] for a in alignments]
    if table.delimiter_spaced:
        parts = [" %s " % spec for spec in specs]
    else:
        parts = specs
    return _join_cells(parts, table.leading_pipe, table.trailing_pipe)


def render(region, model, warnings):
    """新しいモデルからパイプ表の行（接頭辞を含む）を作る。書けなければ NotRepresentable。"""
    table = region.parsed
    if model.rows < 2 or model.cols < 1:
        raise NotRepresentable("見出し行と 1 行以上の行が要る")
    for cell in model.cells:
        if cell.rowspan > 1 or cell.colspan > 1:
            raise NotRepresentable("セル結合がある")
    columns = model.cols
    grid = [[""] * columns for _ in range(model.rows)]
    for cell in model.cells:
        grid[cell.row][cell.col] = cell.text

    old_rows = table.all_rows()
    original_columns = len(table.alignments)

    def old_key(row):
        texts = list(row.texts[:columns])
        return tuple(texts + [""] * (columns - len(texts)))

    def reusable(row):
        # 列を減らしたときに、消した列のセルが行に残らないようにする
        return len(row.texts) <= columns or columns == original_columns

    matcher = difflib.SequenceMatcher(
        None, [old_key(r) for r in old_rows], [tuple(r) for r in grid], autojunk=False)
    reused = {}
    paired = {}
    for tag, i1, i2, j1, j2 in matcher.get_opcodes():
        if tag == "equal":
            for k in range(i2 - i1):
                if reusable(old_rows[i1 + k]):
                    reused[j1 + k] = old_rows[i1 + k]
                else:
                    paired[j1 + k] = old_rows[i1 + k]
        elif tag == "replace":
            for k in range(min(i2 - i1, j2 - j1)):
                paired[j1 + k] = old_rows[i1 + k]

    anywhere = {}
    for row in old_rows:
        for raw, text in zip(row.raw_cells, row.texts):
            anywhere.setdefault(text, raw)

    def cell_source(new_row, col, text):
        old = paired.get(new_row)
        if old is not None:
            if col < len(old.texts) and old.texts[col] == text:
                return old.raw_cells[col]
            for raw, old_text in zip(old.raw_cells, old.texts):
                if old_text == text:
                    return raw
        if text in anywhere:
            return anywhere[text]
        return to_pipe_cell(text)

    rendered_rows = []
    for index, row in enumerate(grid):
        if index in reused:
            rendered_rows.append(("reuse", reused[index]))
        else:
            cells = [cell_source(index, col, text) for col, text in enumerate(row)]
            rendered_rows.append(("new", _format_row(cells, table)))

    alignments = column_alignments(model, warnings, region.number)
    if alignments == list(table.alignments) and columns == len(table.alignments):
        delimiter = table.delimiter_prefix + table.delimiter_body
    else:
        delimiter = region.cont_prefix + _format_delimiter(alignments, table)

    out = []
    for index, (kind, value) in enumerate(rendered_rows):
        if kind == "reuse":
            prefix = value.prefix
            if index == 0:
                prefix = region.first_prefix
            elif value is old_rows[0]:
                prefix = region.cont_prefix
            out.append(prefix + value.body)
        else:
            prefix = region.first_prefix if index == 0 else region.cont_prefix
            out.append(prefix + value)
        if index == 0:
            out.append(delimiter)
    return out
