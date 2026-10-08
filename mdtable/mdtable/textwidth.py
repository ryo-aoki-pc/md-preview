"""文字の表示幅（全角は 2）。"""

import unicodedata


def char_width(ch):
    if unicodedata.combining(ch):
        return 0
    if unicodedata.east_asian_width(ch) in ("W", "F"):
        return 2
    return 1


def text_width(text):
    return sum(char_width(ch) for ch in text)


def wrapped_line_count(text, width):
    """幅 width で折り返したときの行数（おおよそ）。"""
    if width <= 0:
        return max(1, text.count("\n") + 1)
    total = 0
    for line in text.split("\n"):
        w = text_width(line.expandtabs(4))
        total += max(1, -(-w // width))
    return total
