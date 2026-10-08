"""Markdown の文書を読み書きし、表の位置（パイプ表と HTML の表）を見つけて置き換える。"""

import os
import re
import shutil
import tempfile
import time

from . import blocks
from .model import sha256_text


class MarkdownError(Exception):
    pass


class MarkdownFile(object):
    """文書の行と改行コード。BOM の有無を保ち、行は \\r\\n・\\r・\\n だけで分ける。"""

    def __init__(self, path, text, bom=False):
        self.path = path
        self.bom = bom
        self.lines, self.endings = blocks.split_lines(text)
        self._doc = None

    @classmethod
    def load(cls, path):
        try:
            with open(path, "rb") as handle:
                data = handle.read()
        except OSError as error:
            raise MarkdownError("Markdown を読めません: %s (%s)" % (path, error.strerror or error))
        bom = data.startswith(b"\xef\xbb\xbf")
        if bom:
            data = data[3:]
        try:
            text = data.decode("utf-8")
        except UnicodeDecodeError as error:
            raise MarkdownError("Markdown が UTF-8 ではありません: %s（%d バイト目）" % (path, error.start + 1))
        return cls(path, text, bom)

    def text(self):
        return "".join(line + ending for line, ending in zip(self.lines, self.endings))

    @property
    def doc(self):
        if self._doc is None:
            self._doc = blocks.parse(self.lines)
        return self._doc

    def default_ending(self):
        for ending in self.endings:
            if ending:
                return ending
        return "\n"


class TableRegion(object):
    """文書の中の 1 つの表の範囲。"""

    def __init__(self, kind, start, end):
        self.number = 0
        self.kind = kind          # pipe か html
        self.start = start        # 最初の行（0 起点）
        self.end = end            # 最後の行（含む）
        self.prefixes = []        # 行ごとの接頭辞（コンテナの記号と字下げ）
        self.bodies = []          # 行ごとの、接頭辞を除いた中身
        self.cont_prefix = ""     # 新しく足す行の接頭辞
        self.head = ""            # 最初の行で、表より前にある文字（HTML の表）
        self.tail = ""            # 最後の行で、表より後にある文字（HTML の表）
        self.location = ""
        self.heading = ""
        self.excluded = None      # 対象外の理由
        self.warnings = []
        self.node = None          # blocks の表の節（パイプ表）
        self.container = None     # 表を含むコンテナの節
        self.parsed = None        # 種類ごとの解析結果
        self.tight_container = False
        self.next_nonblank = False

    @property
    def first_prefix(self):
        return self.prefixes[0] if self.prefixes else ""

    @property
    def blank_prefix(self):
        return self.cont_prefix.rstrip(" \t")

    def source_text(self):
        """接頭辞を除いた行を LF でつないだもの（衝突の確かめに使う）。"""
        return "\n".join(self.bodies)

    def source_hash(self):
        return sha256_text(self.source_text())

    def line_count(self):
        return self.end - self.start + 1


_KIND_NAMES = {
    blocks.BLOCK_QUOTE: "引用",
    blocks.ALERT: "アラート",
    blocks.MULTILINE_BLOCK_QUOTE: "引用（>>>）",
    blocks.DESCRIPTION_DETAILS: "説明リスト",
    blocks.FOOTNOTE_DEFINITION: "脚注",
}


def describe_location(container, cont_prefix):
    names = []
    node = container
    chain = []
    while node is not None and node.kind != blocks.DOCUMENT:
        chain.append(node)
        node = node.parent
    for node in reversed(chain):
        if node.kind == blocks.ITEM:
            list_type = node.info.get("type")
            names.append("番号付きリスト" if list_type == "ordered" else "箇条書き")
        elif node.kind in _KIND_NAMES:
            names.append(_KIND_NAMES[node.kind])
    if not names:
        return ""
    width = len(cont_prefix.expandtabs(4))
    return "%sの中（字下げ %d）" % (" > ".join(names), width)


def _block_container(node):
    node = node.parent
    while node is not None and node.kind not in blocks.BLOCK_CONTAINERS:
        node = node.parent
    return node


def _container_is_tight(container):
    """コンテナが詰めたリスト（tight）の項目か。"""
    if container is None:
        return False
    if container.kind == blocks.ITEM and container.parent is not None:
        return bool(container.parent.info.get("tight", True))
    if container.kind == blocks.DESCRIPTION_DETAILS and container.parent is not None:
        return bool(container.parent.info.get("tight"))
    return False


def _headings(md):
    result = []
    for node in md.doc.nodes(blocks.HEADING):
        if node.info.get("setext"):
            text = " ".join(md.lines[n][o:].strip() for n, o, _l in node.lines)
        else:
            line_no, offset, _lazy = node.lines[0] if node.lines else (node.start_line, 0, False)
            text = md.lines[line_no][offset:].strip()
            text = re.sub(r"(?:^|[ \t]+)#+[ \t]*$", "", text).strip()
        result.append((node.start_line, node.end_line, text))
    return result


def _pipe_region(md, node):
    doc = md.doc
    header_line, header_offset, header_lazy = node.info["header"]
    region = TableRegion("pipe", header_line, node.end_line)
    region.node = node
    region.container = _block_container(node)
    offsets = {header_line: header_offset}
    delimiter_line, delimiter_offset = node.info["delimiter"]
    offsets[delimiter_line] = delimiter_offset
    for line_no, offset in node.info["rows"]:
        offsets[line_no] = offset
    for line_no in range(region.start, region.end + 1):
        text = md.lines[line_no]
        offset = offsets[line_no]
        region.prefixes.append(text[:offset])
        region.bodies.append(text[offset:])
    region.cont_prefix = region.prefixes[1] if len(region.prefixes) > 1 else region.prefixes[0]
    if header_lazy or doc.line_infos[header_line].lazy:
        region.excluded = "見出し行が引用やリストの続きの行になっていない（遅延行）"
    tabbed = [p for p in region.prefixes[1:] if "\t" in p]
    if tabbed and len(set(region.prefixes[1:])) > 1:
        region.excluded = "タブを含む字下げが行ごとに違う"
    return region


def find_tables(md):
    """文書の表を、出てくる順に番号を付けて返す。"""
    from . import html_table, pipe_table

    regions = []
    for node in md.doc.nodes(blocks.TABLE):
        regions.append(_pipe_region(md, node))
    regions.extend(html_table.find_regions(md))
    regions.sort(key=lambda r: r.start)
    headings = _headings(md)
    infos = md.doc.line_infos
    for number, region in enumerate(regions, 1):
        region.number = number
        region.location = describe_location(region.container, region.cont_prefix)
        region.tight_container = _container_is_tight(region.container)
        # 表の次の行が、同じコンテナの空でない行か（HTML の表にしたときに空行を足す）
        following = region.end + 1
        offset = infos[following].offset_after(region.container) \
            if following < len(md.lines) and region.container is not None else None
        region.next_nonblank = offset is not None and md.lines[following][offset:].strip() != ""
        before = [text for start, end, text in headings if end < region.start]
        region.heading = before[-1] if before else ""
        if region.excluded is None:
            if region.kind == "pipe":
                pipe_table.analyze(md, region)
            else:
                html_table.analyze(md, region)
    return regions


def select_tables(regions, selectors):
    """--table の指定（番号か L行番号）に合う表。指定が無ければすべて。"""
    if not selectors:
        return list(regions)
    chosen = []
    for selector in selectors:
        text = selector.strip()
        if re.match(r"^[Ll]\d+$", text):
            line = int(text[1:]) - 1
            found = [r for r in regions if r.start <= line <= r.end]
            if not found:
                raise MarkdownError("%d 行目を含む表がありません" % (line + 1))
        elif re.match(r"^\d+$", text):
            number = int(text)
            found = [r for r in regions if r.number == number]
            if not found:
                raise MarkdownError("表 %d はありません（表は %d 個）" % (number, len(regions)))
        else:
            raise MarkdownError("--table には表の番号か L行番号 を指定してください: %s" % selector)
        for region in found:
            if region not in chosen:
                chosen.append(region)
    chosen.sort(key=lambda r: r.number)
    return chosen


class Replacement(object):
    """表の範囲を置き換える新しい行（接頭辞を含む完全な行）。"""

    def __init__(self, region, lines):
        self.region = region
        self.lines = lines


def apply_replacements(md, replacements):
    """置き換えた後の文書の文字を返す。変えない行は元の改行コードのまま。"""
    lines = list(md.lines)
    endings = list(md.endings)
    for replacement in sorted(replacements, key=lambda r: r.region.start, reverse=True):
        region = replacement.region
        start, end = region.start, region.end
        first_ending = endings[start] or md.default_ending()
        last_ending = endings[end]
        new_lines = replacement.lines
        new_endings = [first_ending] * (len(new_lines) - 1) + [last_ending]
        lines[start:end + 1] = new_lines
        endings[start:end + 1] = new_endings
    return "".join(line + ending for line, ending in zip(lines, endings))


def write_text_atomic(path, text, bom=False, retries=5):
    """一時ファイルに書いてから置き換える。権限は元のファイルに合わせる。"""
    target = os.path.realpath(path)
    directory = os.path.dirname(target) or "."
    data = text.encode("utf-8")
    if bom:
        data = b"\xef\xbb\xbf" + data
    handle, temp_path = tempfile.mkstemp(prefix=".mdtable-", suffix=".tmp", dir=directory)
    try:
        with os.fdopen(handle, "wb") as temp:
            temp.write(data)
        if os.path.exists(target):
            try:
                shutil.copymode(target, temp_path)
            except OSError:
                pass
        replace_with_retry(temp_path, target, retries)
    except BaseException:
        if os.path.exists(temp_path):
            os.remove(temp_path)
        raise


def replace_with_retry(source, target, retries=5):
    for attempt in range(retries):
        try:
            os.replace(source, target)
            return
        except PermissionError:
            if attempt == retries - 1:
                raise
            time.sleep(0.2 * (attempt + 1))
