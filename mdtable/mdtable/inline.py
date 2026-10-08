"""インラインの Markdown の走査と、エスケープ・強調の確かめ。

ここでは強調やリンクの構造までは作らない。改行や記号の扱いが変わる「ひとかたまり」
（コードスパン・生の HTML・自動リンク・数式・バックスラッシュのエスケープ）を
見つけることと、`**` などの区切りが CommonMark で強調として成り立つかの判定だけを持つ。
"""

import re
import unicodedata

ASCII_PUNCTUATION = "!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~"

_TAG_NAME = r"[A-Za-z][A-Za-z0-9-]*"
_WS = r"(?:[ \t]*\n?[ \t]*)"
_WS1 = r"(?:(?:[ \t]+\n?[ \t]*)|(?:[ \t]*\n[ \t]*))"
_ATTR_VALUE = r"(?:[^\"'=<>`\x00-\x20]+|'[^']*'|\"[^\"]*\")"
_ATTRIBUTE = _WS1 + r"[a-zA-Z_:][a-zA-Z0-9_.:-]*(?:" + _WS + "=" + _WS + _ATTR_VALUE + ")?"
_OPEN_TAG = re.compile(r"<(" + _TAG_NAME + r")((?:" + _ATTRIBUTE + r")*)" + _WS + r"(/?)>")
_CLOSE_TAG = re.compile(r"</(" + _TAG_NAME + r")" + _WS + r">")
_COMMENT = re.compile(r"<!-->|<!--->|<!--[\s\S]*?-->")
_PROCESSING = re.compile(r"<\?[\s\S]*?\?>")
_DECLARATION = re.compile(r"<![A-Za-z][^>]*>")
_CDATA = re.compile(r"<!\[CDATA\[[\s\S]*?\]\]>")
_URI_AUTOLINK = re.compile(r"<[A-Za-z][A-Za-z0-9+.-]{1,31}:[^<>\x00-\x20]*>")
_EMAIL_AUTOLINK = re.compile(
    r"<[a-zA-Z0-9.!#$%&'*+/=?^_`{|}~-]+@[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?"
    r"(?:\.[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*>"
)


class Token(object):
    """kind: text・escape・code・html・autolink・math のどれか。"""

    __slots__ = ("kind", "start", "end", "text", "tag", "closing")

    def __init__(self, kind, start, end, text, tag=None, closing=False):
        self.kind = kind
        self.start = start
        self.end = end
        self.text = text
        self.tag = tag            # html のタグ名（小文字）
        self.closing = closing    # html の終了タグか

    def __repr__(self):
        return "Token(%s, %r)" % (self.kind, self.text)


def match_raw_html(text, pos):
    """pos から始まる生の HTML（タグ・コメントなど）。(終わりの位置, タグ名, 終了タグか) か None。"""
    if not text.startswith("<", pos):
        return None
    for pattern in (_COMMENT, _PROCESSING, _CDATA, _DECLARATION):
        match = pattern.match(text, pos)
        if match:
            return match.end(), None, False
    match = _OPEN_TAG.match(text, pos)
    if match:
        return match.end(), match.group(1).lower(), False
    match = _CLOSE_TAG.match(text, pos)
    if match:
        return match.end(), match.group(1).lower(), True
    return None


def match_autolink(text, pos):
    if not text.startswith("<", pos):
        return None
    match = _URI_AUTOLINK.match(text, pos) or _EMAIL_AUTOLINK.match(text, pos)
    return match.end() if match else None


def _backtick_run(text, pos):
    end = pos
    while end < len(text) and text[end] == "`":
        end += 1
    return end - pos


def _find_code_span_end(text, pos, length):
    """pos から長さ length の ` の連なりで始まるコードスパンの終わり。無ければ None。"""
    search = pos + length
    while True:
        found = text.find("`" * length, search)
        if found < 0:
            return None
        run = _backtick_run(text, found)
        if run == length:
            return found + length
        search = found + run


def _match_math(text, pos):
    """GitLab の $…$ と $`…`$ の数式。終わりの位置か None。"""
    if text.startswith("$`", pos):
        end = text.find("`$", pos + 2)
        if end >= 0:
            return end + 2
        return None
    if text.startswith("$$", pos):
        end = text.find("$$", pos + 2)
        if end > pos + 2:
            return end + 2
        return None
    if not text.startswith("$", pos):
        return None
    nxt = text[pos + 1:pos + 2]
    if nxt == "" or nxt.isspace() or nxt == "$":
        return None
    search = pos + 1
    while True:
        found = text.find("$", search)
        if found < 0:
            return None
        before = text[found - 1]
        after = text[found + 1:found + 2]
        if before.isspace() or before == "\\" or (after and after in "0123456789"):
            search = found + 1
            continue
        return found + 1


def tokenize(text, math=True):
    """インラインの文字を、ひとかたまりごとの Token に分ける。"""
    tokens = []
    pos = 0
    text_start = 0
    n = len(text)

    def flush(upto):
        if upto > text_start:
            tokens.append(Token("text", text_start, upto, text[text_start:upto]))

    while pos < n:
        ch = text[pos]
        if ch == "\\" and pos + 1 < n and text[pos + 1] in ASCII_PUNCTUATION:
            flush(pos)
            tokens.append(Token("escape", pos, pos + 2, text[pos:pos + 2]))
            pos += 2
            text_start = pos
            continue
        if ch == "`":
            run = _backtick_run(text, pos)
            end = _find_code_span_end(text, pos, run)
            if end is not None:
                flush(pos)
                tokens.append(Token("code", pos, end, text[pos:end]))
                pos = end
                text_start = pos
            else:
                pos += run
            continue
        if ch == "<":
            end = match_autolink(text, pos)
            if end is not None:
                flush(pos)
                tokens.append(Token("autolink", pos, end, text[pos:end]))
                pos = end
                text_start = pos
                continue
            raw = match_raw_html(text, pos)
            if raw is not None:
                end, tag, closing = raw
                flush(pos)
                tokens.append(Token("html", pos, end, text[pos:end], tag, closing))
                pos = end
                text_start = pos
                continue
        if math and ch == "$":
            end = _match_math(text, pos)
            if end is not None:
                flush(pos)
                tokens.append(Token("math", pos, end, text[pos:end]))
                pos = end
                text_start = pos
                continue
        pos += 1
    flush(n)
    return tokens


def atom_spans(text):
    """改行やエスケープがそのまま意味を持たない範囲（コード・HTML・自動リンク・数式）。"""
    return [(t.start, t.end) for t in tokenize(text) if t.kind in ("code", "html", "autolink", "math")]


def position_in_spans(pos, spans):
    for start, end in spans:
        if start < pos < end:
            return True
    return False


# --- 強調の区切りが成り立つか ---

def is_unicode_whitespace(ch):
    return ch == "" or ch in "\t\n\x0b\x0c\r " or unicodedata.category(ch) == "Zs"


def is_punctuation(ch, include_symbols=True):
    """CommonMark の約物。0.31.2 は記号（S*）も含む。0.30 は P* と ASCII の記号だけ。"""
    if ch == "":
        return False
    if ch in ASCII_PUNCTUATION:
        return True
    category = unicodedata.category(ch)
    if category.startswith("P"):
        return True
    return include_symbols and category.startswith("S")


def _flanking(before, after, include_symbols):
    left = (not is_unicode_whitespace(after)
            and (not is_punctuation(after, include_symbols)
                 or is_unicode_whitespace(before) or is_punctuation(before, include_symbols)))
    right = (not is_unicode_whitespace(before)
             and (not is_punctuation(before, include_symbols)
                  or is_unicode_whitespace(after) or is_punctuation(after, include_symbols)))
    return left, right


def delimiters_work(before, inner, after, marker):
    """before + marker + inner + marker + after の marker が強調として開いて閉じるか。

    CommonMark 0.30 と 0.31.2 の両方の約物の定義で成り立つときだけ真にする。
    """
    if not inner or inner[0] in " \t\n" or inner[-1] in " \t\n":
        return False
    first = inner[0]
    last = inner[-1]
    # 隣や内側の端が区切りの記号だと、区切りの連なりがつながってしまう
    for ch in (first, last, before, after):
        if ch and ch in "*_~":
            return False
    for symbols in (False, True):
        open_left, open_right = _flanking(before, first, symbols)
        close_left, close_right = _flanking(last, after, symbols)
        if marker[0] == "_":
            can_open = open_left and (not open_right or is_punctuation(before, symbols))
            can_close = close_right and (not close_left or is_punctuation(after, symbols))
        else:
            can_open = open_left
            can_close = close_right
        if not (can_open and can_close):
            return False
    return True


# --- エスケープ ---

_LINE_START_BLOCK = re.compile(
    r"(?:[-+*](?=[ \t]|$)"           # 箇条書き
    r"|\d{1,9}[.)](?=[ \t]|$)"       # 番号付き
    r"|#{1,6}(?=[ \t]|$)"            # 見出し
    r"|>"                             # 引用
    r"|[:~](?=[ \t])"                 # 説明リスト
    r"|=+[ \t]*$"                     # setext の下線
    r"|-+[ \t]*$"
    r"|(?:\*[ \t]*){3,}$|(?:_[ \t]*){3,}$|(?:-[ \t]*){3,}$"   # 区切り線
    r"|`{3,}|~{3,}"                   # フェンス
    r"|\[\^?[^\]]+\]:"                # リンク参照・脚注の定義
    r")"
)


def escape_line_start(line):
    """行頭がブロックの記法として読まれないように、先頭の記号を 1 つエスケープする。"""
    match = _LINE_START_BLOCK.match(line)
    if not match:
        return line
    head = line[0]
    if head in "0123456789":
        digits = re.match(r"\d+", line).group(0)
        return digits + "\\" + line[len(digits):]
    return "\\" + line


def unescape_line_start(line):
    """escape_line_start で付けたエスケープを外す（インラインの文脈では要らないため）。"""
    if line.startswith("\\") and len(line) > 1:
        candidate = line[1:]
        if escape_line_start(candidate) == line:
            return candidate
    match = re.match(r"(\d{1,9})\\([.)])", line)
    if match:
        candidate = match.group(1) + line[len(match.group(1)) + 1:]
        if escape_line_start(candidate) == line:
            return candidate
    return line
