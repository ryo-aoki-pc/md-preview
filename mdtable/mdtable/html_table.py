"""HTML の表の検出・解析・書き戻し。

- 範囲: コンテナ（本文・引用・リスト項目・説明など）の直下の HTML ブロックにある <table> から、
  同じコンテナの HTML ブロックの中で対応する </table> まで。その間の HTML ブロック以外の
  ブロックは、セルの中の Markdown の部分
- 書き戻し: 格子の形が同じなら、変わったセルの中身とタグだけを元の文字の上で差し替える。
  形が変わったら「1 行に 1 タグ・字下げ無し」で作り直す（変わっていないセルの中身は元のまま）
"""

import difflib
import re

from . import blocks, htmltok, inline
from .cell_from_html import convert as convert_cell
from .cell_syntax import NotRepresentable, to_html_cell
from .model import Cell, TableModel

STRUCTURAL = frozenset(["table", "caption", "colgroup", "col", "thead", "tbody", "tfoot", "tr", "td", "th"])
_TEXT_ALIGN = re.compile(r"(^|;)\s*text-align\s*:\s*([a-zA-Z-]+)\s*(;|$)", re.I)
_ALIGN_VALUES = {"left": "left", "center": "center", "right": "right", "justify": "justify"}


# --- 検出 ---

def _block_text(md, node):
    """HTML ブロックの行（コンテナの接頭辞を除いたもの）と、各行の始まりの位置。"""
    parts = []
    starts = []
    position = 0
    for line_no, offset, _lazy in node.lines:
        starts.append((position, line_no, offset))
        body = md.lines[line_no][offset:]
        parts.append(body)
        position += len(body) + 1
    return "\n".join(parts), starts


def _locate(starts, position):
    """ブロックの文字の位置 → (行番号, 行の中の位置)。"""
    for index in range(len(starts) - 1, -1, -1):
        start, line_no, offset = starts[index]
        if position >= start:
            return line_no, offset + position - start
    return starts[0][1], starts[0][2]


def _chunk_has_structural(md, node):
    """Markdown の部分のブロックに、コードの外の表の構造のタグがあるか。"""
    for sub in node.walk():
        if sub.kind == blocks.CODE_BLOCK and sub.info.get("fenced"):
            # フェンスの中は意図して書いたコード
            continue
        if sub.kind in (blocks.PARAGRAPH, blocks.HEADING, blocks.HTML_BLOCK, blocks.CODE_BLOCK):
            # 字下げのコードブロックに入った </td> などは、字下げのしすぎで崩れたもの
            text = "\n".join(md.lines[n][o:] for n, o, _lazy in sub.lines)
            for token in inline.tokenize(text):
                if token.kind == "html" and token.tag in STRUCTURAL:
                    return True
        if sub.kind == blocks.TABLE:
            for line_no in range(sub.start_line, sub.end_line + 1):
                for token in inline.tokenize(md.lines[line_no]):
                    if token.kind == "html" and token.tag in STRUCTURAL:
                        return True
    return False


def _constructed_prefix(container):
    """コンテナの続きの行の接頭辞を組み立てる（続きの行が無いとき用）。"""
    parts = []
    node = container
    chain = []
    while node is not None and node.kind != blocks.DOCUMENT:
        chain.append(node)
        node = node.parent
    for node in reversed(chain):
        if node.kind in (blocks.BLOCK_QUOTE, blocks.ALERT):
            parts.append("> ")
        elif node.kind in (blocks.ITEM, blocks.DESCRIPTION_ITEM):
            parts.append(" " * (node.info["marker_offset"] + node.info["padding"]))
        elif node.kind == blocks.FOOTNOTE_DEFINITION:
            parts.append("    ")
    return "".join(parts)


class _Segment(object):
    __slots__ = ("kind", "node")

    def __init__(self, kind, node):
        self.kind = kind
        self.node = node


def _scan(md, container, children, index):
    from .document import TableRegion

    first = children[index]
    text, starts = _block_text(md, first)
    tokens = htmltok.tokenize(text)
    start_token = None
    for token in tokens:
        if token.kind == "start" and token.name == "table":
            start_token = token
            break
    if start_token is None:
        return None
    start_line, start_col = _locate(starts, start_token.start)
    depth = 0
    end = None
    segments = []
    unsupported = None
    current = index
    while current < len(children):
        node = children[current]
        if node.kind == blocks.HTML_BLOCK:
            node_text, node_starts = _block_text(md, node)
            node_tokens = htmltok.tokenize(node_text)
            for token in node_tokens:
                if current == index and token.start < start_token.start:
                    continue
                if token.kind == "start" and token.name == "table":
                    depth += 1
                elif token.kind == "end" and token.name == "table":
                    depth -= 1
                    if depth == 0:
                        end = _locate(node_starts, token.end)
                        break
            segments.append(_Segment("html", node))
            if end is not None:
                break
        else:
            if _chunk_has_structural(md, node):
                unsupported = "セルの中の Markdown の部分に表の構造のタグ（<td> など）がある（GitLab でも崩れる）"
            segments.append(_Segment("md", node))
        current += 1
    if end is None:
        last = children[-1]
        region = TableRegion("html", start_line, max(start_line, last.end_line))
        region.excluded = "対応する </table> が同じ場所（コンテナ）の HTML の中に無い（字下げや空行で GitLab でも崩れる）"
        _fill_lines(md, region, container)
        return region, len(children)
    end_line, end_col = end
    region = TableRegion("html", start_line, end_line)
    _fill_lines(md, region, container)
    # 行の中の位置を、接頭辞を除いた中身の中の位置にする
    start_col -= len(region.prefixes[0])
    end_col -= len(region.prefixes[-1])
    region.head = region.bodies[0][:start_col] if region.excluded is None else ""
    region.tail = region.bodies[-1][end_col:] if region.excluded is None else ""
    region.parsed = _Pending(segments, start_col, end_col)
    if unsupported and region.excluded is None:
        region.excluded = unsupported
    following = end_line + 1
    region.next_nonblank = (following < len(md.lines)
                            and md.doc.line_infos[following].offset_after(container) is not None
                            and md.lines[following][md.doc.line_infos[following].offset_after(container):].strip() != "")
    return region, current + 1


class _Pending(object):
    """検出したときの情報（analyze で解析結果に置き換える）。"""

    def __init__(self, segments, start_col, end_col):
        self.segments = segments
        self.start_col = start_col
        self.end_col = end_col


def _fill_lines(md, region, container):
    region.container = container
    infos = md.doc.line_infos
    for line_no in range(region.start, region.end + 1):
        line = md.lines[line_no]
        offset = infos[line_no].offset_after(container)
        if offset is None:
            region.excluded = region.excluded or "%d 行目が引用やリストの続きになっていない" % (line_no + 1)
            offset = 0
        region.prefixes.append(line[:offset])
        region.bodies.append(line[offset:])
    continuation = [p for p, b in zip(region.prefixes[1:], region.bodies[1:]) if b.strip()]
    region.cont_prefix = continuation[0] if continuation else _constructed_prefix(container)
    tabbed = [p for p in region.prefixes[1:] if "\t" in p]
    if tabbed and len(set(p for p, b in zip(region.prefixes[1:], region.bodies[1:]) if b.strip())) > 1:
        region.excluded = region.excluded or "タブを含む字下げが行ごとに違う"


def find_regions(md):
    regions = []
    for container in md.doc.root.walk():
        if container.kind not in blocks.BLOCK_CONTAINERS:
            continue
        children = list(container.children)
        index = 0
        while index < len(children):
            child = children[index]
            if child.kind == blocks.HTML_BLOCK:
                found = _scan(md, container, children, index)
                if found is not None:
                    region, index = found
                    regions.append(region)
                    continue
            index += 1
    # 外側の表の中（セルの Markdown の部分など）で見つけた表は除く
    regions.sort(key=lambda r: (r.start, -r.end))
    result = []
    for region in regions:
        if any(other.start <= region.start and region.end <= other.end and other is not region
               for other in result):
            continue
        result.append(region)
    return result


# --- 解析 ---

class HtmlCell(object):
    def __init__(self, token):
        self.tag = token.name
        self.start_token = token
        self.content_start = token.end
        self.content_end = token.end
        self.end_token = None
        self.tokens = []
        self.row = None
        self.col = None
        self.rowspan = 1
        self.colspan = 1
        self.text = ""

    def attribute(self, name):
        return self.start_token.attribute(name)


class HtmlRow(object):
    def __init__(self, token, section):
        self.token = token        # <tr> の開始タグ（省略なら None）
        self.end_token = None     # </tr>（省略なら None）
        self.section = section
        self.cells = []
        self.index = None


class HtmlSection(object):
    def __init__(self, token, tag):
        self.token = token        # 開始タグ（省略なら None）
        self.tag = tag            # thead・tbody・tfoot
        self.rows = []


class HtmlTable(object):
    def __init__(self):
        self.text = ""
        self.start = 0
        self.end = 0
        self.table_token = None
        self.caption = None       # 元の文字
        self.colgroups = []       # 元の文字のリスト
        self.sections = []
        self.explicit_sections = False
        self.rows = []
        self.cells = []
        self.interstitial = False
        self.align_style = False
        self.model = None
        self.cell_at = {}


def _parse_span(value, default, maximum):
    if value is None:
        return default, False
    match = re.match(r"\s*(\d+)", value)
    if not match:
        return default, False
    return min(int(match.group(1)), maximum), True


def _cell_align(cell):
    value = cell.attribute("align")
    if value is not None and value.strip().lower() in _ALIGN_VALUES:
        return _ALIGN_VALUES[value.strip().lower()], False
    style = cell.attribute("style")
    if style:
        match = _TEXT_ALIGN.search(style)
        if match and match.group(2).lower() in _ALIGN_VALUES:
            return _ALIGN_VALUES[match.group(2).lower()], True
    return None, False


def _tokens_for_region(region, pending):
    bodies = region.bodies
    text = "\n".join(bodies)
    line_starts = []
    position = 0
    for body in bodies:
        line_starts.append(position)
        position += len(body) + 1
    table_start = line_starts[0] + pending.start_col
    table_end = line_starts[-1] + pending.end_col
    md_ranges = []
    chunk = None
    for segment in pending.segments:
        node = segment.node
        first = node.start_line - region.start
        last = node.end_line - region.start
        if segment.kind == "md":
            if chunk is None:
                chunk = [first, last]
            else:
                chunk[1] = last
        else:
            if chunk is not None:
                md_ranges.append(tuple(chunk))
                chunk = None
    if chunk is not None:
        md_ranges.append(tuple(chunk))
    tokens = []
    position = table_start
    for first, last in md_ranges:
        start = line_starts[first]
        end = line_starts[last] + len(bodies[last])
        tokens.extend(htmltok.tokenize(text, position, start))
        tokens.append(htmltok.Token("md", start, end, "\n".join(bodies[first:last + 1])))
        position = end
    tokens.extend(htmltok.tokenize(text, position, table_end))
    return text, table_start, table_end, tokens


def analyze(md, region):
    pending = region.parsed
    table = HtmlTable()
    text, table_start, table_end, tokens = _tokens_for_region(region, pending)
    table.text = text
    table.start = table_start
    table.end = table_end
    try:
        _parse_structure(table, tokens)
        _build_grid(table)
    except NotRepresentable as error:
        region.excluded = str(error)
        region.parsed = None
        return
    region.parsed = table


def _parse_structure(table, tokens):
    if not tokens or tokens[0].kind != "start" or tokens[0].name != "table":
        raise NotRepresentable("<table> を読めない")
    table.table_token = tokens[0]
    section = None
    row = None
    cell = None
    nested = 0
    caption = None
    colgroup = None
    finished = False

    def close_cell(position):
        nonlocal cell
        if cell is not None:
            if cell.end_token is None:
                cell.content_end = position
            cell = None

    def ensure_section(token):
        nonlocal section
        if section is None:
            section = HtmlSection(None, "tbody")
            table.sections.append(section)
        return section

    for token in tokens[1:]:
        if finished:
            if token.kind == "text" and not token.text.strip():
                continue
            raise NotRepresentable("</table> の後に続きがある")
        if cell is not None and nested > 0:
            cell.tokens.append(token)
            if token.kind == "start" and token.name == "table":
                nested += 1
            elif token.kind == "end" and token.name == "table":
                nested -= 1
            continue
        if caption is not None:
            if token.kind == "end" and token.name == "caption":
                table.caption = table.text[caption:token.end]
                caption = None
            continue
        if colgroup is not None:
            if token.kind == "end" and token.name == "colgroup":
                table.colgroups.append(table.text[colgroup:token.end])
                colgroup = None
                continue
            if token.kind == "start" and token.name == "col":
                continue
            if token.kind == "text" and not token.text.strip():
                continue
            table.colgroups.append(table.text[colgroup:token.start])
            colgroup = None
        kind = token.kind
        name = token.name
        if kind == "start" and name in ("td", "th"):
            close_cell(token.start)
            if row is None:
                row = HtmlRow(None, ensure_section(token))
                row.section.rows.append(row)
            cell = HtmlCell(token)
            row.cells.append(cell)
            table.cells.append(cell)
            continue
        if kind == "end" and name in ("td", "th"):
            if cell is not None and cell.tag == name:
                cell.end_token = token
                cell.content_end = token.start
                cell = None
            elif cell is not None:
                cell.tokens.append(token)
            continue
        if kind == "start" and name == "tr":
            close_cell(token.start)
            row = HtmlRow(token, ensure_section(token))
            row.section.rows.append(row)
            continue
        if kind == "end" and name == "tr":
            close_cell(token.start)
            if row is not None:
                row.end_token = token
            row = None
            continue
        if kind == "start" and name in ("thead", "tbody", "tfoot"):
            close_cell(token.start)
            row = None
            section = HtmlSection(token, name)
            table.sections.append(section)
            table.explicit_sections = True
            continue
        if kind == "end" and name in ("thead", "tbody", "tfoot"):
            close_cell(token.start)
            row = None
            section = None
            continue
        if kind == "end" and name == "table":
            close_cell(token.start)
            finished = True
            continue
        if kind == "start" and name == "table":
            if cell is not None:
                cell.tokens.append(token)
                nested = 1
                continue
            raise NotRepresentable("セルの外に入れ子の <table> がある")
        if cell is not None:
            if kind == "start" and name in htmltok.RAW_TEXT_ELEMENTS:
                raise NotRepresentable("セルの中に <%s> がある" % name)
            if kind == "start" and name in ("caption", "colgroup", "col"):
                raise NotRepresentable("セルの中に <%s> がある" % name)
            cell.tokens.append(token)
            continue
        if kind == "start" and name == "caption" and not table.sections and table.caption is None:
            caption = token.start
            continue
        if kind == "start" and name == "colgroup" and not table.sections:
            colgroup = token.start
            continue
        if kind == "start" and name == "col" and not table.sections:
            table.colgroups.append(token.text)
            continue
        if kind == "text" and not token.text.strip():
            continue
        if kind in ("comment", "other"):
            table.interstitial = True
            continue
        raise NotRepresentable("表の構造（<tr> など）の間に文字や要素がある（GitLab では表の外に出る）")
    if not finished:
        raise NotRepresentable("</table> が無い")
    order = [s.tag for s in table.sections]
    if "tfoot" in order and "tbody" in order and order.index("tfoot") < len(order) - 1 \
            and any(t == "tbody" for t in order[order.index("tfoot") + 1:]):
        raise NotRepresentable("<tfoot> が <tbody> の前にある（表示の順が元の順と違う）")


def _check_markdown_parents(tree):
    """<pre> などの中に空行があって Markdown の部分になっていたら、GitLab でも崩れているので対象外にする。"""
    def walk(element, inside):
        for child in element.children:
            if isinstance(child, htmltok.Markdown) and inside:
                raise NotRepresentable("<%s> の中に空行がある（GitLab でも崩れる）" % inside)
            if isinstance(child, htmltok.Element):
                walk(child, inside or (child.tag if child.tag in ("pre", "code", "textarea") else None))
    walk(tree, None)


def _build_grid(table):
    occupied = {}
    row_index = 0
    for section in table.sections:
        section_end = row_index + len(section.rows)
        for row in section.rows:
            row.index = row_index
            table.rows.append(row)
            col = 0
            for cell in row.cells:
                while (row_index, col) in occupied:
                    col += 1
                colspan, _ok = _parse_span(cell.attribute("colspan"), 1, 1000)
                if colspan == 0:
                    colspan = 1
                rowspan, ok = _parse_span(cell.attribute("rowspan"), 1, 65534)
                if ok and rowspan == 0:
                    rowspan = section_end - row_index
                rowspan = max(1, min(rowspan, section_end - row_index))
                for r in range(row_index, row_index + rowspan):
                    for c in range(col, col + colspan):
                        if (r, c) in occupied:
                            raise NotRepresentable("セルが重なっている（rowspan / colspan）")
                        occupied[(r, c)] = cell
                cell.row = row_index
                cell.col = col
                cell.rowspan = rowspan
                cell.colspan = colspan
                col += colspan
            row_index += 1
    rows = row_index
    cols = max([c for _r, c in occupied] + [-1]) + 1
    if rows == 0 or cols == 0:
        raise NotRepresentable("セルが無い")
    holes = set()
    for r in range(rows):
        for c in range(cols):
            if (r, c) not in occupied:
                holes.add((r, c))
    styles = 0
    model_cells = []
    for cell in table.cells:
        tree = htmltok.build_tree(cell.tokens)
        _check_markdown_parents(tree)
        cell.text = convert_cell(tree.children, table.text)
        align, by_style = _cell_align(cell)
        if by_style:
            styles += 1
        model_cells.append(Cell(cell.row, cell.col, cell.text, cell.rowspan, cell.colspan,
                                header=cell.tag == "th", align=align))
        table.cell_at[(cell.row, cell.col)] = cell
    table.align_style = styles > 0
    table.model = TableModel("html", rows, cols, model_cells, holes)


# --- 書き戻し ---

def _inner_sources(table):
    """Excel の文字 → 元のセルの中身（同じ文字のセルが複数あれば最初のもの）。"""
    result = {}
    if table is None:
        return result
    for cell in table.cells:
        result.setdefault(cell.text, table.text[cell.content_start:cell.content_end])
    return result


def _new_inner(text, reuse, warnings):
    """セルの中身。(文字, 新しく作った Markdown の部分か, 元のセルの中身をそのまま使ったか)。"""
    if text in reuse:
        return reuse[text], False, True
    kind, payload, cell_warnings = to_html_cell(text)
    warnings.extend(cell_warnings)
    if kind == "inline":
        return payload, False, False
    return "\n\n" + "\n".join(payload) + "\n\n", True, False


def _without_text_align(style):
    parts = [part.strip() for part in style.split(";")]
    kept = [part for part in parts if part and not re.match(r"text-align\s*:", part, re.I)]
    return "; ".join(kept)


def _start_tag(attributes, tag, align, align_style, rowspan=None, colspan=None, keep_spans=False):
    """開始タグを組み立てる。align は None・left など。

    keep_spans が真なら rowspan / colspan は元の書き方のまま残す（差し替えのとき）。
    """
    parts = ["<" + tag]
    style_value = None
    for attribute in attributes:
        if attribute.name in ("rowspan", "colspan"):
            if keep_spans:
                parts.append(" " + attribute.raw)
            continue
        if attribute.name == "align":
            continue
        if attribute.name == "style":
            style_value = _without_text_align(attribute.value or "")
            continue
        parts.append(" " + attribute.raw)
    if not keep_spans:
        if rowspan and rowspan > 1:
            parts.append(' rowspan="%d"' % rowspan)
        if colspan and colspan > 1:
            parts.append(' colspan="%d"' % colspan)
    if align:
        if align_style:
            style_value = (style_value + "; " if style_value else "") + "text-align: %s" % align
        else:
            parts.append(' align="%s"' % align)
    if style_value:
        parts.append(' style="%s"' % style_value.replace('"', "&quot;"))
    parts.append(">")
    return "".join(parts)


def _apply_edits(region, table, edits):
    """元の表の文字（接頭辞を除いて行をつないだもの）に差し替えを当て、接頭辞を付けた行にする。"""
    text = table.text
    line_starts = []
    position = 0
    for body in region.bodies:
        line_starts.append(position)
        position += len(body) + 1
    edits = sorted(edits, key=lambda e: (e[0], e[1]))
    for earlier, later in zip(edits, edits[1:]):
        if later[0] < earlier[1]:
            raise NotRepresentable("差し替える範囲が重なる")
    survivors = {}
    for index, start in enumerate(line_starts):
        if index == 0:
            continue
        inside = any(s <= start < e or s == start == e for s, e, _r in edits)
        if not inside:
            delta = sum(len(r) - (e - s) for s, e, r in edits if e <= start)
            survivors[start + delta] = index
    for start, end, replacement in reversed(edits):
        text = text[:start] + replacement + text[end:]
    lines = []
    position = 0
    for index, body in enumerate(text.split("\n")):
        if index == 0:
            prefix = region.prefixes[0]
        elif position in survivors:
            prefix = region.prefixes[survivors[position]]
        else:
            prefix = region.cont_prefix if body else region.blank_prefix
        lines.append(prefix + body if body or index == 0 else prefix)
        position += len(body) + 1
    return lines


def _patch(region, model, warnings):
    table = region.parsed
    reuse = _inner_sources(table)
    edits = []
    new_cells = model.anchors()
    for cell in table.cells:
        new = new_cells[(cell.row, cell.col)]
        old_align, _by_style = _cell_align(cell)
        new_tag = "th" if new.header else "td"
        if new_tag != cell.tag or new.align != old_align:
            start_tag = _start_tag(cell.start_token.attributes, new_tag, new.align,
                                   table.align_style or _by_style, keep_spans=True)
            edits.append((cell.start_token.start, cell.start_token.end, start_tag))
            if cell.end_token is not None and new_tag != cell.tag:
                edits.append((cell.end_token.start, cell.end_token.end, "</%s>" % new_tag))
        if new.text != cell.text:
            inner, block, reused = _new_inner(new.text, reuse, warnings)
            block = block or (reused and "\n" in inner)
            start, end = cell.content_start, cell.content_end
            if block:
                # 続くタグは空行の後の行頭から始める（字下げするとコードブロックになる）
                while end < len(table.text) and table.text[end] in " \t\n":
                    end += 1
                if not inner.endswith("\n"):
                    inner += "\n"
            edits.append((start, end, inner))
    return _apply_edits(region, table, edits)


def _row_signature(cells, row):
    return tuple((c.col, c.rowspan, c.colspan, c.header, c.text) for c in cells if c.row == row)


def _sections_for(model, table):
    """新しい行ごとの区画（元の区画の番号か thead / tbody の名前）と、元の行の対応。"""
    rows = model.rows
    if table is None:
        names = ["thead"] + ["tbody"] * (rows - 1)
        return [(name, None) for name in names], [None] * rows
    old_rows = table.rows
    old_signatures = [_row_signature(table.model.cells, r.index) for r in old_rows]
    new_signatures = [_row_signature(model.cells, r) for r in range(rows)]
    matcher = difflib.SequenceMatcher(None, old_signatures, new_signatures, autojunk=False)
    mapping = [None] * rows
    for tag, i1, i2, j1, j2 in matcher.get_opcodes():
        if tag in ("equal", "replace"):
            for k in range(min(i2 - i1, j2 - j1)):
                mapping[j1 + k] = old_rows[i1 + k]
    sections = []
    previous = None
    for r in range(rows):
        old = mapping[r]
        if old is not None:
            section = table.sections.index(old.section)
        elif previous is not None:
            section = previous
        else:
            body = [i for i, s in enumerate(table.sections) if s.tag == "tbody"]
            section = body[0] if body else 0
        sections.append(section)
        previous = section
    # 結合したセルが区画をまたがないようにする
    changed = True
    while changed:
        changed = False
        for cell in model.cells:
            first = sections[cell.row]
            for r in range(cell.row + 1, cell.row + cell.rowspan):
                if sections[r] != first:
                    sections[r] = first
                    changed = True
    return [(index, None) for index in sections], mapping


def _original_cell(table, mapping, cell):
    """作り直すセルに対応する元のセル（同じ行の同じ列 → 同じ行の同じ中身 → 同じ中身）。"""
    if table is None:
        return None
    old_row = mapping[cell.row]
    if old_row is not None:
        for old in old_row.cells:
            if old.col == cell.col:
                return old
        for old in old_row.cells:
            if old.text == cell.text and cell.text:
                return old
    if cell.text:
        for old in table.cells:
            if old.text == cell.text:
                return old
    return None


def _regenerate(region, model, warnings, from_pipe):
    table = None if from_pipe else region.parsed
    reuse = _inner_sources(table)
    align_style = table.align_style if table is not None else False
    lines = []
    if table is not None:
        lines.append(table.table_token.text)
        if table.caption:
            lines.extend(table.caption.split("\n"))
        if table.colgroups and model.cols == table.model.cols:
            for colgroup in table.colgroups:
                lines.extend(colgroup.split("\n"))
    else:
        lines.append("<table>")
    sections, mapping = _sections_for(model, table)
    explicit = from_pipe or (table is not None and table.explicit_sections)
    if from_pipe and model.rows == 1:
        sections = [("thead", None)]
    groups = []
    for r in range(model.rows):
        key = sections[r][0]
        if groups and groups[-1][0] == key:
            groups[-1][1].append(r)
        else:
            groups.append((key, [r]))
    for key, rows in groups:
        if explicit:
            if isinstance(key, str):
                open_tag, close_tag = "<%s>" % key, "</%s>" % key
            else:
                section = table.sections[key]
                open_tag = section.token.text if section.token is not None else "<%s>" % section.tag
                close_tag = "</%s>" % section.tag
            lines.append(open_tag)
        for r in rows:
            old_row = mapping[r]
            if (old_row is not None and old_row.token is not None and old_row.end_token is not None
                    and _row_signature(table.model.cells, old_row.index) == _row_signature(model.cells, r)):
                # 変わっていない行は、元の行の文字をそのまま使う
                lines.extend(table.text[old_row.token.start:old_row.end_token.end].split("\n"))
                continue
            lines.append(old_row.token.text if old_row is not None and old_row.token is not None else "<tr>")
            anchors = sorted((c for c in model.cells if c.row == r), key=lambda c: c.col)
            for cell in anchors:
                tag = "th" if cell.header else "td"
                if from_pipe:
                    tag = "th" if cell.row == 0 else "td"
                original = _original_cell(table, mapping, cell)
                attributes = original.start_token.attributes if original is not None else []
                if original is not None and original.text != cell.text:
                    extra = [a for a in attributes if a.name not in ("rowspan", "colspan", "align", "style")]
                    if extra:
                        warnings.append("中身を変えたセル（%d 行 %d 列）の HTML の属性（%s）は引き継がない"
                                        % (cell.row + 1, cell.col + 1, " ".join(a.raw for a in extra)))
                    attributes = [a for a in attributes if a.name == "style"]
                start_tag = _start_tag(attributes, tag, cell.align, align_style, cell.rowspan, cell.colspan)
                inner, block, reused = _new_inner(cell.text, {} if from_pipe else reuse, warnings)
                # 新しく作った Markdown の部分は空行で終わるので、終了タグは行頭から始まる
                lines.extend((start_tag + inner + "</%s>" % tag).split("\n"))
            lines.append("</tr>")
        if explicit:
            lines.append(close_tag)
    lines.append("</table>")
    lines[0] = region.head + lines[0]
    lines[-1] = lines[-1] + region.tail
    result = []
    for index, line in enumerate(lines):
        if index == 0:
            result.append(region.first_prefix + line)
        elif line:
            result.append(region.cont_prefix + line)
        else:
            result.append(region.blank_prefix)
    return result


def render(region, model, warnings):
    table = region.parsed
    if model.shape() == table.model.shape():
        return _patch(region, model, warnings)
    if table.interstitial:
        raise NotRepresentable("表の構造の間にコメントなどがあるので、行・列・結合は変えられない")
    return _regenerate(region, model, warnings, False)


def render_from_pipe(region, model, warnings):
    lines = _regenerate(region, model, warnings, True)
    if getattr(region, "next_nonblank", False):
        lines.append(region.blank_prefix)
    return lines
