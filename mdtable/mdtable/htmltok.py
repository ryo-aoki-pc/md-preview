"""HTML の字句解析（元の文書での位置付き）と、ゆるい木の組み立て。

Python の html.parser は版によって生の文字として読む要素などが違うので使わない。
"""

import re

_COMMENT = re.compile(r"<!--(?:>|->|[\s\S]*?-->)")
_CDATA = re.compile(r"<!\[CDATA\[[\s\S]*?\]\]>")
_DECLARATION = re.compile(r"<![A-Za-z][^>]*>")
_PROCESSING = re.compile(r"<\?[\s\S]*?\?>")
_START = re.compile(
    r"<([A-Za-z][A-Za-z0-9-]*)"
    r"((?:\s+[^\s\"'>/=]+(?:\s*=\s*(?:\"[^\"]*\"|'[^']*'|[^\s\"'=<>`]+))?)*)"
    r"\s*(/?)>")
_END = re.compile(r"</([A-Za-z][A-Za-z0-9-]*)\s*>")
_ATTRIBUTE = re.compile(r"\s*([^\s\"'>/=]+)(?:(\s*=\s*)(\"[^\"]*\"|'[^']*'|[^\s\"'=<>`]+))?")

VOID_ELEMENTS = frozenset([
    "area", "base", "br", "col", "embed", "hr", "img", "input", "link", "meta", "param",
    "source", "track", "wbr",
])
RAW_TEXT_ELEMENTS = frozenset([
    "script", "style", "textarea", "title", "xmp", "iframe", "noembed", "noframes", "plaintext",
])


class Attribute(object):
    __slots__ = ("name", "value", "raw")

    def __init__(self, name, value, raw):
        self.name = name            # 小文字の名前
        self.value = value          # 引用符を外した値（値が無ければ None）
        self.raw = raw              # 元の文字（前の空白を除く）


def parse_attributes(text):
    attributes = []
    for match in _ATTRIBUTE.finditer(text):
        if not match.group(1):
            continue
        value = match.group(3)
        if value is not None and value[:1] in ("'", '"'):
            value = value[1:-1]
        raw = match.group(0).lstrip()
        attributes.append(Attribute(match.group(1).lower(), value, raw))
    return attributes


class Token(object):
    """kind: start・end・comment・other・text・md。"""

    __slots__ = ("kind", "start", "end", "text", "name", "attributes", "self_closing")

    def __init__(self, kind, start, end, text, name=None, attributes=None, self_closing=False):
        self.kind = kind
        self.start = start
        self.end = end
        self.text = text
        self.name = name
        self.attributes = attributes or []
        self.self_closing = self_closing

    def attribute(self, name):
        for attribute in self.attributes:
            if attribute.name == name:
                return attribute.value if attribute.value is not None else ""
        return None

    def __repr__(self):
        return "Token(%s, %r)" % (self.kind, self.text)


def tokenize(text, start=0, end=None):
    """text[start:end] を字句に分ける。位置は text の中の位置。"""
    if end is None:
        end = len(text)
    tokens = []
    pos = start
    text_start = start

    def flush(upto):
        if upto > text_start:
            tokens.append(Token("text", text_start, upto, text[text_start:upto]))

    while pos < end:
        lt = text.find("<", pos, end)
        if lt < 0:
            break
        token = None
        for pattern, kind in ((_COMMENT, "comment"), (_CDATA, "other"),
                              (_DECLARATION, "other"), (_PROCESSING, "other")):
            match = pattern.match(text, lt, end)
            if match:
                token = Token(kind, lt, match.end(), match.group(0))
                break
        if token is None:
            match = _START.match(text, lt, end)
            if match:
                token = Token("start", lt, match.end(), match.group(0), match.group(1).lower(),
                              parse_attributes(match.group(2)), bool(match.group(3)))
        if token is None:
            match = _END.match(text, lt, end)
            if match:
                token = Token("end", lt, match.end(), match.group(0), match.group(1).lower())
        if token is None:
            pos = lt + 1
            continue
        flush(lt)
        tokens.append(token)
        pos = token.end
        text_start = pos
    flush(end)
    return tokens


# --- ゆるい木 ---

class Element(object):
    __slots__ = ("tag", "attributes", "start_text", "children", "start", "end", "closed", "parent")

    def __init__(self, tag, attributes=None, start_text="", start=0):
        self.tag = tag
        self.attributes = attributes or []
        self.start_text = start_text      # 元の開始タグ
        self.children = []
        self.start = start                # 開始タグの始まり
        self.end = start                  # 終了タグ（無ければ暗黙に閉じた位置）の終わり
        self.closed = False               # 終了タグがあったか
        self.parent = None

    def attribute(self, name):
        for attribute in self.attributes:
            if attribute.name == name:
                return attribute.value if attribute.value is not None else ""
        return None

    def __repr__(self):
        return "<%s>" % self.tag


class Text(object):
    __slots__ = ("text", "start", "end")

    def __init__(self, text, start, end):
        self.text = text
        self.start = start
        self.end = end


class Markdown(object):
    """HTML の中の Markdown の部分（空行で区切られた行）。"""

    __slots__ = ("text", "start", "end")

    def __init__(self, text, start, end):
        self.text = text
        self.start = start
        self.end = end


class Raw(object):
    """コメントなど、そのまま残すもの。"""

    __slots__ = ("text", "start", "end")

    def __init__(self, text, start, end):
        self.text = text
        self.start = start
        self.end = end


_P_CLOSERS = frozenset([
    "address", "article", "aside", "blockquote", "details", "dialog", "div", "dl", "fieldset",
    "figcaption", "figure", "footer", "form", "h1", "h2", "h3", "h4", "h5", "h6", "header",
    "hgroup", "hr", "main", "menu", "nav", "ol", "p", "pre", "search", "section", "table", "ul",
])


def build_tree(tokens):
    """字句から木を作る（セルの中身用。li・dt・dd・p の省略された終了タグも扱う）。"""
    root = Element("#root")
    stack = [root]

    def close_to(index, position):
        for element in stack[index:]:
            if not element.closed:
                element.end = position
        del stack[index:]

    def open_index(*names, **kwargs):
        boundary = kwargs.get("boundary", ())
        for index in range(len(stack) - 1, 0, -1):
            if stack[index].tag in names:
                return index
            if stack[index].tag in boundary:
                return None
        return None

    for token in tokens:
        top = stack[-1]
        if token.kind == "text":
            top.children.append(Text(token.text, token.start, token.end))
        elif token.kind == "md":
            if top.tag == "p":
                close_to(len(stack) - 1, token.start)
                top = stack[-1]
            top.children.append(Markdown(token.text, token.start, token.end))
        elif token.kind in ("comment", "other"):
            top.children.append(Raw(token.text, token.start, token.end))
        elif token.kind == "start":
            name = token.name
            if name == "li":
                index = open_index("li", boundary=("ul", "ol"))
                if index is not None:
                    close_to(index, token.start)
            elif name in ("dt", "dd"):
                index = open_index("dt", "dd", boundary=("dl",))
                if index is not None:
                    close_to(index, token.start)
            if name in _P_CLOSERS:
                index = open_index("p", boundary=("li", "dd", "dt", "td", "th", "blockquote", "div"))
                if index is not None:
                    close_to(index, token.start)
            element = Element(name, token.attributes, token.text, token.start)
            element.end = token.end
            element.parent = stack[-1]
            stack[-1].children.append(element)
            if name not in VOID_ELEMENTS and not token.self_closing:
                stack.append(element)
            else:
                element.closed = True
        elif token.kind == "end":
            index = open_index(token.name)
            if index is not None:
                close_to(index + 1, token.start)
                element = stack[index]
                element.end = token.end
                element.closed = True
                del stack[index:]
            # 開いていない要素の終了タグは無視する（HTML と同じ）
    if len(stack) > 1:
        last = tokens[-1].end if tokens else 0
        close_to(1, last)
    return root
