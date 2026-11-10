"""表のモデルを突き合わせ、利用者に出す変更の要約とセルの一覧を作る。"""

import difflib
from collections import Counter

from .model import Cell


class CellChange(object):
    """変更したセルの位置（0 起点）と、変更の種類。"""

    def __init__(self, row, col, old_text, new_text, kind):
        self.row = row
        self.col = col
        self.old_text = old_text
        self.new_text = new_text
        self.kind = kind


class Changes(object):
    def __init__(self):
        self.added_rows = 0
        self.deleted_rows = 0
        self.added_cols = 0
        self.deleted_cols = 0
        self.cells = []

    def summary(self):
        parts = []
        count = len({(cell.row, cell.col) for cell in self.cells})
        if count:
            parts.append("セル %d 個を変更" % count)
        for count, unit, action in (
                (self.added_rows, "行", "追加"), (self.deleted_rows, "行", "削除"),
                (self.added_cols, "列", "追加"), (self.deleted_cols, "列", "削除")):
            if count:
                parts.append("%d %sを%s" % (count, unit, action))
        return "（%s）" % "、".join(parts) if parts else ""

    def details(self, limit=20):
        cells = {}
        for cell in self.cells:
            descriptions = cells.setdefault((cell.row, cell.col), [])
            if cell.kind == "文字":
                descriptions.append("「%s」→「%s」" % _short_pair(cell.old_text, cell.new_text))
            else:
                descriptions.append("%sを変更" % cell.kind)
        lines = ["  %s: %s" % (_address(row, col), "、".join(descriptions))
                 for (row, col), descriptions in list(cells.items())[:limit]]
        if len(cells) > limit:
            lines.append("  ほか %d 個" % (len(cells) - limit))
        return lines


def _address(row, col):
    letters = ""
    number = col + 1
    while number:
        number, digit = divmod(number - 1, 26)
        letters = chr(ord("A") + digit) + letters
    return "%s%d" % (letters, row + 1)


def _short_text(text, start=0):
    text = text.replace("\r\n", "\n").replace("\r", "\n").replace("\n", "⏎")
    return ("…" if start and text else "") + text[start:start + 30] + ("…" if len(text) > start + 30 else "")


def _short_pair(before, after):
    """最初の違いが見える、同じ位置の窓で前後を切る。"""
    texts = [text.replace("\r\n", "\n").replace("\r", "\n").replace("\n", "⏎")
             for text in (before, after)]
    if max(map(len, texts)) <= 30:
        return tuple(texts)
    difference = 0
    while difference < min(map(len, texts)) and texts[0][difference] == texts[1][difference]:
        difference += 1
    start = max(0, difference - 10)
    return tuple(_short_text(text, start) for text in texts)


# 置換のかたまりを、似ているもの同士で対応させる大きさの上限（元の数 × 新しい数）
_ALIGN_LIMIT = 10000


def _similarity(first, second):
    """2 つの並びで、同じ位置に同じものがある数。"""
    return sum(1 for a, b in zip(first, second) if a == b)


def _overlap(first, second):
    """2 つの数え上げ（Counter）に、共通して入っているものの数。並びの順番は見ない。"""
    return sum((first & second).values())


def _align(old, new, i1, i2, j1, j2, similarity=_similarity):
    """置換のかたまりの中を、順番を保ったまま、似ているもの同士で対応させる。

    数が同じならそのままの順で対応させる。違うときは、少ないほうを全部対応させたうえで、
    似ている数（既定は、同じ位置に同じ文字がある数）の合計がいちばん多い対応を選ぶ（同点なら前のものから）。
    """
    count_old, count_new = i2 - i1, j2 - j1
    if count_old == count_new or count_old * count_new > _ALIGN_LIMIT:
        return list(zip(range(i1, i2), range(j1, j2)))
    swapped = count_old > count_new
    if swapped:
        short, long_ = new[j1:j2], old[i1:i2]
    else:
        short, long_ = old[i1:i2], new[j1:j2]
    rows, cols = len(short), len(long_)
    # best[i][j]: short[i:] を long_[j:] に全部対応させたときの、似ている数の合計の最大（無理なら None）
    best = [[None] * (cols + 1) for _ in range(rows + 1)]
    for j in range(cols + 1):
        best[rows][j] = 0
    for i in range(rows - 1, -1, -1):
        for j in range(cols - 1, -1, -1):
            if cols - j < rows - i:
                continue
            take = best[i + 1][j + 1]
            if take is not None:
                take += similarity(short[i], long_[j])
            skip = best[i][j + 1]
            best[i][j] = take if skip is None or (take is not None and take >= skip) else skip
    pairs = []
    i = j = 0
    while i < rows:
        take = best[i + 1][j + 1]
        if take is not None and take + similarity(short[i], long_[j]) == best[i][j]:
            pairs.append((i1 + j, j1 + i) if swapped else (i1 + i, j1 + j))
            i += 1
        j += 1
    return pairs


def _corresponding(old, new):
    """同じ並びを目印に対応を決め、置換の余りを追加・削除として数える。"""
    pairs = []
    added = deleted = 0
    matcher = difflib.SequenceMatcher(None, old, new, autojunk=False)
    for tag, i1, i2, j1, j2 in matcher.get_opcodes():
        if tag == "equal":
            pairs.extend(zip(range(i1, i2), range(j1, j2)))
        elif tag == "replace":
            pairs.extend(_align(old, new, i1, i2, j1, j2))
        added += max(0, (j2 - j1) - (i2 - i1))
        deleted += max(0, (i2 - i1) - (j2 - j1))
    return pairs, added, deleted


def _covered(model):
    """結合で隠れる位置（左上のセルの位置は含まない）。"""
    positions = set()
    for cell in model.cells:
        if cell.rowspan > 1 or cell.colspan > 1:
            for row in range(cell.row, cell.row + cell.rowspan):
                for col in range(cell.col, cell.col + cell.colspan):
                    if (row, col) != (cell.row, cell.col):
                        positions.add((row, col))
    return positions


def compare_models(old, new):
    """列、次に行を文字で突き合わせ、対応したセルの変更を返す。"""
    result = Changes()
    old_anchors = old.anchors()
    new_anchors = new.anchors()

    def text_at(anchors, row, col):
        cell = anchors.get((row, col))
        return cell.text if cell is not None else ""

    def match_rows(columns):
        old_rows = [tuple(text_at(old_anchors, row, col) for col, _new_col in columns)
                    for row in range(old.rows)]
        new_rows = [tuple(text_at(new_anchors, row, col) for _old_col, col in columns)
                    for row in range(new.rows)]
        return _corresponding(old_rows, new_rows)

    if old.cols == new.cols:
        columns = list(zip(range(old.cols), range(new.cols)))
        rows, result.added_rows, result.deleted_rows = match_rows(columns)
    else:
        # 仮の列の対応: 列ごとに、入っている文字が多く重なる列同士を対応させる（行の順番は見ないので、
        # 行を足した・消したときも決められる）
        columns = _align(
            [Counter(text_at(old_anchors, row, col) for row in range(old.rows)) for col in range(old.cols)],
            [Counter(text_at(new_anchors, row, col) for row in range(new.rows)) for col in range(new.cols)],
            0, old.cols, 0, new.cols, _overlap)
        rows, _added, _deleted = match_rows(columns)
        # 対応した行だけで列を突き合わせ直す（行を足した・消したときも、列の文字の並びの長さがそろう）
        columns, result.added_cols, result.deleted_cols = _corresponding(
            [tuple(text_at(old_anchors, row, col) for row, _new_row in rows) for col in range(old.cols)],
            [tuple(text_at(new_anchors, row, col) for _old_row, row in rows) for col in range(new.cols)])
        rows, result.added_rows, result.deleted_rows = match_rows(columns)
    old_covered = _covered(old)
    new_covered = _covered(new)
    for old_row, new_row in rows:
        for old_col, new_col in columns:
            previous = old_anchors.get((old_row, old_col))
            current = new_anchors.get((new_row, new_col))
            # 結合に隠れた位置は数えず、左上のセルで範囲の変更を比べる
            if ((previous is None and (old_row, old_col) in old_covered)
                    or (current is None and (new_row, new_col) in new_covered)):
                continue
            previous = previous if previous is not None else Cell(old_row, old_col)
            current = current if current is not None else Cell(new_row, new_col)
            for kind, before, after in (
                    ("文字", previous.text, current.text),
                    ("配置", previous.align, current.align),
                    ("見出し", previous.header, current.header),
                    ("結合", (previous.rowspan, previous.colspan), (current.rowspan, current.colspan))):
                if before != after:
                    result.cells.append(CellChange(new_row, new_col, previous.text, current.text, kind))
    result.cells.sort(key=lambda cell: (cell.row, cell.col))
    return result
