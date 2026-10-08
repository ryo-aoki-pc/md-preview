"""表のモデル（Excel と Markdown の間で受け渡す形）と、セルの文字の正規化。"""

import hashlib
import json
import re

ALIGNMENTS = (None, "left", "center", "right", "justify")


class Cell(object):
    """格子の 1 つのセル（結合したセルは左上の位置に 1 つ）。"""

    __slots__ = ("row", "col", "rowspan", "colspan", "text", "header", "align")

    def __init__(self, row, col, text="", rowspan=1, colspan=1, header=False, align=None):
        self.row = row
        self.col = col
        self.rowspan = rowspan
        self.colspan = colspan
        self.text = text          # Excel に書く Markdown（正規化したもの）
        self.header = header      # 見出しセル（<th>）か
        self.align = align        # None・left・center・right・justify

    def key(self):
        return (self.row, self.col, self.rowspan, self.colspan, self.header, self.align, self.text)

    def __repr__(self):
        return "Cell(%d,%d,%r)" % (self.row, self.col, self.text)


class TableModel(object):
    """表の格子。kind は pipe か html。"""

    def __init__(self, kind, rows, cols, cells, holes=None):
        self.kind = kind
        self.rows = rows
        self.cols = cols
        self.cells = sorted(cells, key=lambda c: (c.row, c.col))
        # HTML の表で、セルの無い位置（行のセルが足りない）
        self.holes = set(holes or ())

    def cell_at(self, row, col):
        for cell in self.cells:
            if cell.row <= row < cell.row + cell.rowspan and cell.col <= col < cell.col + cell.colspan:
                return cell
        return None

    def anchors(self):
        return {(c.row, c.col): c for c in self.cells}

    def shape(self):
        """格子の形（セルの位置と結合範囲、セルの無い位置）。"""
        return (self.rows, self.cols,
                tuple((c.row, c.col, c.rowspan, c.colspan) for c in self.cells),
                tuple(sorted(self.holes)))

    def signature(self):
        return {
            "rows": self.rows,
            "cols": self.cols,
            "cells": [list(c.key()) for c in self.cells],
        }

    def fingerprint(self):
        data = json.dumps(self.signature(), ensure_ascii=False, sort_keys=True)
        return hashlib.sha256(data.encode("utf-8")).hexdigest()

    def same_content(self, other):
        return self.signature() == other.signature()


_TRAILING_BLANK = re.compile(r"[ \t]*(?:\n[ \t]*)*$")
_LEADING_BLANK_LINES = re.compile(r"^(?:[ \t]*\n)+")


def normalize_text(text):
    """セルの文字を正規化する（Excel への書き出しと読み込みの両方で同じものを使う）。

    改行は LF にそろえ、値の末尾の空白と前後の空行を除く。行の先頭の字下げと、
    途中の行の末尾の空白はそのまま残す。
    """
    if text is None:
        return ""
    text = text.replace("_x000D_\n", "\n")
    text = text.replace("\r\n", "\n").replace("\r", "\n")
    text = _LEADING_BLANK_LINES.sub("", text)
    text = _TRAILING_BLANK.sub("", text)
    return text


def sha256_text(text):
    return hashlib.sha256(text.encode("utf-8")).hexdigest()
