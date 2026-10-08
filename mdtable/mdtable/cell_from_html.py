"""HTML の表のセルの中身（HTML と Markdown の部分）を、Excel に書く Markdown にする。

規則:
- <p> は段落、<br> は改行、<ul>/<ol>/<li> はリスト、<dl> は説明リスト、<pre> はフェンス、
  <code> はコードスパン、<strong>/<b>・<em>/<i>・<del>/<s> は ** ・ * ・ ~~、<a> はリンク、
  <img> は画像、<blockquote> は引用、<h1>〜 は見出し、<hr> は区切り線
- ** などを約物に接して閉じると強調にならない位置では、元の HTML のタグのまま残す
- 知らないインラインの要素は HTML のまま中を変換し、知らないブロックは元の HTML を行のまま入れる
- HTML の文字は空白をまとめ、Markdown で意味を持つ文字だけをエスケープする
- Markdown の部分は、段落の中の改行の扱いだけを Excel 用にそろえ、ほかはそのまま
"""

import html
import re

from . import blocks, inline
from .cell_syntax import chunk_to_excel
from .htmltok import Element, Markdown, Raw, Text
from .model import normalize_text

_WS = re.compile(r"[ \t\n\r\f]+")
_ENTITY_LIKE = re.compile(r"&#?[A-Za-z0-9]+;")
_LANGUAGE_CLASS = re.compile(r"(?:^|\s)(?:language|lang)-(\S+)")
_PLACEHOLDER_BASE = 0xE000

BLOCK_TAGS = frozenset([
    "p", "div", "ul", "ol", "dl", "pre", "blockquote", "h1", "h2", "h3", "h4", "h5", "h6",
    "hr", "table", "details", "summary", "figure", "figcaption", "section", "article", "aside",
    "nav", "header", "footer", "center", "form", "fieldset", "address", "main", "menu", "dir",
    "dialog", "hgroup", "search", "noscript", "video", "audio", "li", "dt", "dd", "caption",
    "thead", "tbody", "tfoot", "tr", "td", "th", "colgroup", "col", "iframe", "object",
])
EMPHASIS = {
    "strong": "**", "b": "**", "em": "*", "i": "*", "del": "~~", "s": "~~", "strike": "~~",
}


class _Context(object):
    """1 つのセルの変換の状態（強調の仮の印と、元の文字）。"""

    def __init__(self, source):
        self.source = source
        self.emphasis = []

    def raw(self, element):
        return self.source[element.start:element.end]

    def placeholder(self, inner, marker, open_text, close_text, depth):
        index = len(self.emphasis)
        opening = chr(_PLACEHOLDER_BASE + 2 * index)
        closing = chr(_PLACEHOLDER_BASE + 2 * index + 1)
        self.emphasis.append((opening, closing, marker, open_text, close_text, depth))
        stripped = inner.strip(" ")
        if not stripped:
            return inner
        lead = inner[:len(inner) - len(inner.lstrip(" "))]
        trail = inner[len(inner.rstrip(" ")):]
        return lead + opening + stripped + closing + trail


def _is_placeholder(ch):
    return ch != "" and _PLACEHOLDER_BASE <= ord(ch) < _PLACEHOLDER_BASE + 0x1800


def _resolve_emphasis(text, context):
    """仮の印を、強調として成り立つなら ** などに、成り立たなければ元のタグにする。"""
    order = sorted(range(len(context.emphasis)), key=lambda i: -context.emphasis[i][5])
    for index in order:
        opening, closing, marker, open_text, close_text, _depth = context.emphasis[index]
        i = text.find(opening)
        j = text.find(closing)
        if i < 0 or j < 0:
            continue
        inner = text[i + 1:j]
        before = text[i - 1] if i > 0 else ""
        after = text[j + 1] if j + 1 < len(text) else ""

        def visible(ch):
            return "*" if _is_placeholder(ch) else ch

        check_inner = "".join(visible(ch) for ch in inner)
        if inline.delimiters_work(visible(before), check_inner, visible(after), marker):
            text = text[:i] + marker + inner + marker + text[j + 1:]
        else:
            text = text[:i] + open_text + inner + close_text + text[j + 1:]
    return text


def _paragraph_flags(nodes):
    """段落の文字に、組になる ~ と数式になる $ があるか。"""
    plain = []

    def collect(node):
        if isinstance(node, Text):
            plain.append(html.unescape(_WS.sub(" ", node.text)))
        elif isinstance(node, Element) and node.tag not in ("code", "pre"):
            for child in node.children:
                collect(child)

    for node in nodes:
        collect(node)
    joined = "".join(plain)
    tilde = joined.count("~") >= 2
    dollar = any(t.kind == "math" for t in inline.tokenize(joined))
    return tilde, dollar


def escape_text(text, tilde=False, dollar=False):
    """HTML の文字（実体参照を戻したもの）を、Markdown で同じに見えるようにエスケープする。"""
    out = []
    n = len(text)
    for i, ch in enumerate(text):
        nxt = text[i + 1] if i + 1 < n else ""
        prev = text[i - 1] if i > 0 else ""
        if ch == "\\":
            out.append("\\\\" if nxt == "" or nxt in inline.ASCII_PUNCTUATION else "\\")
        elif ch in "`*[":
            out.append("\\" + ch)
        elif ch == "_":
            inside_word = (prev and nxt and not inline.is_unicode_whitespace(prev)
                           and not inline.is_unicode_whitespace(nxt)
                           and not inline.is_punctuation(prev) and not inline.is_punctuation(nxt))
            out.append("_" if inside_word else "\\_")
        elif ch == "~" and tilde:
            out.append("\\~")
        elif ch == "$" and dollar:
            out.append("\\$")
        elif ch == "<":
            out.append("\\<" if nxt and (nxt.isalpha() or nxt in "/!?") else "<")
        elif ch == "&":
            out.append("&amp;" if _ENTITY_LIKE.match(text, i) else "&")
        elif ch == " ":
            out.append("&nbsp;")
        else:
            out.append(ch)
    return "".join(out)


def _code_span(content):
    content = content.replace("\n", " ")
    if not content.strip(" "):
        return None
    longest = max([len(m.group(0)) for m in re.finditer(r"`+", content)] or [0])
    fence = "`" * (longest + 1)
    if content.startswith("`") or content.endswith("`") or (
            content.startswith(" ") and content.endswith(" ")):
        content = " " + content + " "
    return fence + content + fence


def _text_of(element):
    parts = []
    for child in element.children:
        if isinstance(child, Text):
            parts.append(child.text)
        elif isinstance(child, Element):
            if child.tag == "br":
                parts.append("\n")
            else:
                parts.append(_text_of(child))
    return "".join(parts)


def _only_text(element):
    return all(isinstance(child, Text) for child in element.children)


def _merge_adjacent(nodes):
    """隣り合う同じ強調の要素（属性なし）をまとめる。"""
    result = []
    for node in nodes:
        if (isinstance(node, Element) and node.tag in EMPHASIS and not node.attributes
                and result and isinstance(result[-1], Element) and result[-1].tag == node.tag
                and not result[-1].attributes):
            merged = Element(node.tag, [], result[-1].start_text, result[-1].start)
            merged.children = list(result[-1].children) + list(node.children)
            merged.end = node.end
            merged.closed = node.closed
            result[-1] = merged
        else:
            result.append(node)
    return result


def _link_destination(href):
    dest = href.replace("<", "%3C").replace(">", "%3E").replace("\n", "%0A")
    dest = re.sub(r"\\(?=[" + re.escape(inline.ASCII_PUNCTUATION) + "])", r"\\\\", dest)
    balanced = dest.count("(") == dest.count(")")
    if " " in dest or not balanced or dest == "":
        return "<" + dest + ">"
    return dest


def _render_inline(nodes, context, flags, depth=0):
    out = []
    for node in _merge_adjacent(nodes):
        if isinstance(node, Text):
            out.append(escape_text(html.unescape(_WS.sub(" ", node.text)), *flags))
        elif isinstance(node, Raw):
            out.append(node.text)
        elif isinstance(node, Markdown):
            out.append(node.text)
        elif isinstance(node, Element):
            out.append(_render_inline_element(node, context, flags, depth))
    return "".join(out)


def _render_inline_element(element, context, flags, depth):
    tag = element.tag
    if tag == "br":
        return "\n"
    if tag in EMPHASIS and not element.attributes:
        inner = _render_inline(element.children, context, flags, depth + 1)
        if not inner.strip(" "):
            return inner
        return context.placeholder(inner, EMPHASIS[tag], element.start_text, "</%s>" % tag, depth)
    if tag == "code":
        if not element.attributes and _only_text(element):
            span = _code_span(html.unescape(_text_of(element)))
            if span is not None:
                return span
        # 中に要素がある・属性があるコードは、元の HTML のまま
        return context.raw(element)
    if tag == "a":
        names = {a.name for a in element.attributes}
        href = element.attribute("href")
        if href is not None and names <= {"href", "title"}:
            text = _render_inline(element.children, context, flags, depth + 1)
            title = element.attribute("title")
            if (text == escape_text(href) and title is None
                    and re.match(r"[A-Za-z][A-Za-z0-9+.-]{1,31}:[^<>\s]*$", href)):
                return "<" + href + ">"
            text = text.replace("]", "\\]")
            dest = _link_destination(href)
            if title is not None:
                return '[%s](%s "%s")' % (text, dest, title.replace("\\", "\\\\").replace('"', '\\"'))
            return "[%s](%s)" % (text, dest)
        return element.start_text + _render_inline(element.children, context, flags, depth + 1) + \
            ("</a>" if element.closed else "")
    if tag == "img":
        names = {a.name for a in element.attributes}
        src = element.attribute("src")
        if src is not None and names <= {"src", "alt", "title"}:
            alt = (element.attribute("alt") or "").replace("[", "\\[").replace("]", "\\]")
            title = element.attribute("title")
            if title is not None:
                return '![%s](%s "%s")' % (alt, _link_destination(src), title.replace('"', '\\"'))
            return "![%s](%s)" % (alt, _link_destination(src))
        return element.start_text
    if tag in ("input",):
        return element.start_text
    if tag in BLOCK_TAGS:
        return context.raw(element)
    if tag in ("img", "wbr"):
        return element.start_text
    inner = _render_inline(element.children, context, flags, depth + 1)
    return element.start_text + inner + ("</%s>" % tag if element.closed else "")


def _finish_paragraph(text, context):
    text = _resolve_emphasis(text, context)
    lines = [line.strip(" ") for line in text.split("\n")]
    while lines and not lines[0]:
        lines.pop(0)
    while lines and not lines[-1]:
        lines.pop()
    if any(blocks.is_delimiter_row(line) and "|" in line for line in lines):
        lines = [line.replace("|", "\\|") for line in lines]
    result = []
    for line in lines:
        if line and not line.startswith("`"):
            line = inline.escape_line_start(line)
        result.append(line)
    return result


class _Block(object):
    __slots__ = ("kind", "lines", "ordered", "marker", "interrupts", "loose")

    def __init__(self, kind, lines, ordered=False, marker=None, interrupts=False):
        self.kind = kind
        self.lines = lines
        self.ordered = ordered
        self.marker = marker
        self.interrupts = interrupts


def _render_flow(nodes, context):
    """セルや li の中身を、ブロックのリストにする。"""
    result = []
    group = []

    def flush():
        if not group:
            return
        flags = _paragraph_flags(group)
        text = _render_inline(group, context, flags)
        lines = _finish_paragraph(text, context)
        if lines:
            result.append(_Block("para", lines))
        del group[:]

    for node in nodes:
        if isinstance(node, Markdown):
            flush()
            lines = chunk_to_excel(node.text).split("\n")
            result.append(_Block("md", lines))
        elif isinstance(node, Element) and node.tag in BLOCK_TAGS:
            flush()
            previous = result[-1] if result else None
            result.extend(_render_block(node, context, previous))
        else:
            group.append(node)
    flush()
    return result


def _join(blocks_):
    lines = []
    previous = None
    for block in blocks_:
        if previous is not None:
            if previous.kind == "para" and block.kind == "list" and block.interrupts:
                pass
            elif previous.kind == "list" and block.kind == "list":
                pass
            else:
                lines.append("")
        lines.extend(block.lines)
        previous = block
    return lines


def _raw_block(element, context):
    return [_Block("raw", context.raw(element).split("\n"))]


def _list_items(element):
    items = []
    for child in element.children:
        if isinstance(child, Text) and not child.text.strip(" \t\n\r\f"):
            continue
        if isinstance(child, Raw):
            continue
        if not (isinstance(child, Element) and child.tag == "li"):
            return None
        items.append(child)
    return items


def _render_list(element, context, previous):
    ordered = element.tag == "ol"
    if element.attribute("type") is not None or element.attribute("reversed") is not None:
        return _raw_block(element, context)
    names = {a.name for a in element.attributes} - {"start"}
    if names:
        return _raw_block(element, context)
    items = _list_items(element)
    if not items:
        return _raw_block(element, context)
    start = 1
    if ordered and element.attribute("start") is not None:
        match = re.match(r"\s*(\d{1,9})", element.attribute("start"))
        if not match:
            return _raw_block(element, context)
        start = int(match.group(1))
    options = (".", ")") if ordered else ("-", "*")
    marker = options[0]
    if previous is not None and previous.kind == "list" and previous.ordered == ordered \
            and previous.marker == marker:
        marker = options[1]
    loose = any(isinstance(c, Element) and c.tag == "p" for item in items for c in item.children)
    lines = []
    for index, item in enumerate(items):
        if item.attributes:
            return _raw_block(element, context)
        children = list(item.children)
        task = ""
        while children and isinstance(children[0], Text) and not children[0].text.strip(" \t\n\r\f"):
            children.pop(0)
        if children and isinstance(children[0], Element) and children[0].tag == "input" \
                and (children[0].attribute("type") or "").lower() == "checkbox":
            task = "[x] " if children[0].attribute("checked") is not None else "[ ] "
            children.pop(0)
        body = _join(_render_flow(children, context))
        if task and body and body[0].startswith("\\["):
            pass
        label = ("%d%s " % (start + index, marker)) if ordered else marker + " "
        if index > 0 and loose:
            lines.append("")
        first = (task + body[0]) if body else task
        lines.append((label + first).rstrip(" ") if first else label.rstrip(" "))
        for line in body[1:]:
            lines.append(" " * len(label) + line if line else "")
    interrupts = (not ordered or start == 1) and bool(lines and lines[0].strip(" -*.)0123456789"))
    return [_Block("list", lines, ordered, marker, interrupts)]


def _render_dl(element, context):
    entries = []
    for child in element.children:
        if isinstance(child, Text) and not child.text.strip(" \t\n\r\f"):
            continue
        if isinstance(child, Raw):
            continue
        if not (isinstance(child, Element) and child.tag in ("dt", "dd")) or child.attributes:
            return _raw_block(element, context)
        entries.append(child)
    if not entries or entries[0].tag != "dt" or entries[-1].tag != "dd" or element.attributes:
        return _raw_block(element, context)
    lines = []
    previous = None
    for entry in entries:
        rendered = _render_flow(entry.children, context)
        if entry.tag == "dt":
            if previous == "dt":
                return _raw_block(element, context)
            if len(rendered) != 1 or rendered[0].kind != "para":
                return _raw_block(element, context)
            if lines:
                lines.append("")
            lines.extend(rendered[0].lines)
        else:
            body = _join(rendered) or [""]
            lines.append(": " + body[0] if body[0] else ":")
            for line in body[1:]:
                lines.append("  " + line if line else "")
        previous = entry.tag
    return [_Block("dl", lines)]


def _render_pre(element, context):
    names = {a.name for a in element.attributes} - {"lang"}
    if names:
        return _raw_block(element, context)
    language = element.attribute("lang") or ""
    children = [c for c in element.children]
    content_element = element
    meaningful = [c for c in children if not (isinstance(c, Text) and not c.text.strip(" \t\n\r\f"))]
    if len(meaningful) == 1 and isinstance(meaningful[0], Element) and meaningful[0].tag == "code":
        code = meaningful[0]
        if not _only_text(code) or {a.name for a in code.attributes} - {"class"}:
            return _raw_block(element, context)
        match = _LANGUAGE_CLASS.search(code.attribute("class") or "")
        if match:
            language = match.group(1)
        elif code.attribute("class"):
            return _raw_block(element, context)
        content_element = code
        if any(isinstance(c, Text) and c.text.strip(" \t\n\r\f") for c in children if c is not code):
            return _raw_block(element, context)
    elif not _only_text(element):
        return _raw_block(element, context)
    content = html.unescape(_text_of(content_element))
    if content_element is element and content.startswith("\n"):
        content = content[1:]
    elif content_element is not element:
        before = "".join(c.text for c in children if isinstance(c, Text) and c.end <= content_element.start)
        if not before and content.startswith("\n"):
            pass
    if content.endswith("\n"):
        content = content[:-1]
    runs = [len(m.group(1)) for m in re.finditer(r"(?m)^[ \t]*(`{3,})", content)]
    fence = "`" * max(3, max(runs or [0]) + 1)
    lines = [fence + language] + content.split("\n") + [fence]
    return [_Block("code", lines)]


def _render_block(element, context, previous):
    tag = element.tag
    if tag in ("p", "div"):
        if element.attributes:
            return _raw_block(element, context)
        blocks_ = _render_flow(element.children, context)
        return blocks_
    if tag in ("ul", "ol"):
        return _render_list(element, context, previous)
    if tag == "dl":
        return _render_dl(element, context)
    if tag == "pre":
        return _render_pre(element, context)
    if tag == "blockquote" and not element.attributes:
        body = _join(_render_flow(element.children, context))
        return [_Block("quote", ["> " + line if line else ">" for line in body])]
    if tag in ("h1", "h2", "h3", "h4", "h5", "h6") and not element.attributes:
        rendered = _render_flow(element.children, context)
        if len(rendered) != 1 or rendered[0].kind != "para" or len(rendered[0].lines) != 1:
            return _raw_block(element, context)
        text = rendered[0].lines[0]
        text = re.sub(r"(#+)$", r"\\\1", text)
        return [_Block("heading", ["#" * int(tag[1]) + " " + text])]
    if tag == "hr" and not element.attributes:
        return [_Block("hr", ["---"])]
    return _raw_block(element, context)


def convert(nodes, source):
    """セルの中身の節（htmltok の木の子）を、Excel に書く Markdown にする。"""
    context = _Context(source)
    return normalize_text("\n".join(_join(_render_flow(nodes, context))))
