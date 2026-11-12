"""Python の標準ライブラリで比較用の正解データを作る。"""

import difflib
import gzip
import html
import json
import math
from pathlib import Path
import random
import struct
import sys


ROOT = Path(__file__).resolve().parent / "testdata"
RNG = random.Random(20261110)


def save(name, values):
    data = json.dumps(values, ensure_ascii=False, separators=(",", ":")).encode("utf-8")
    (ROOT / (name + ".json.gz")).write_bytes(gzip.compress(data, mtime=0))
    print("%s: %d 組" % (name, len(values)))


def sequences():
    result = []
    for index in range(5000):
        size = index if index <= 400 else RNG.randrange(401)
        alphabet = ["", "a", "b", "c", "日本語", "😀"][:RNG.randrange(1, 7)]
        a = [RNG.choice(alphabet) for _ in range(size)]
        if index % 13 == 0:
            b = a[:]
        elif index % 13 == 1:
            b = []
        elif index % 13 == 2:
            b, a = a, []
        elif index % 3 == 0:
            b = a[:]
            for _ in range(RNG.randrange(1, 8)):
                start = RNG.randrange(len(b) + 1)
                b[start:start + RNG.randrange(8)] = [RNG.choice(alphabet) for _ in range(RNG.randrange(8))]
            b = b[:400]
        else:
            b = [RNG.choice(alphabet) for _ in range(RNG.randrange(401))]
        expected = [dict(Tag=tag, I1=i1, I2=i2, J1=j1, J2=j2)
                    for tag, i1, i2, j1, j2 in difflib.SequenceMatcher(None, a, b, autojunk=False).get_opcodes()]
        result.append(dict(a=a, b=b, expected=expected))
    save("opcodes", result)


def diffs():
    result = []
    def add(a, b):
        result.append(dict(a=a, b=b, fromfile="元\t😀.md", tofile="先.md",
                           expected="".join(difflib.unified_diff(a, b, fromfile="元\t😀.md", tofile="先.md"))))
    add([], [])
    add([], ["末尾"])
    add(["末尾"], [])
    for distance in range(1, 20):
        a = ["%d\n" % i for i in range(50)]
        b = a[:]
        b[5] = "変更\n"
        b[5 + distance] = "変更2\n"
        add(a, b)
    for _ in range(750):
        a = [RNG.choice(["a", "b", "c", "日本語", "😀", ""]) + RNG.choice(["\r\n", "\r", "\n"])
             for _ in range(RNG.randrange(401))]
        if a and RNG.randrange(2):
            a[-1] = a[-1].rstrip("\r\n")
            if a[-1] == "":
                a.pop()
        b = a[:]
        for _ in range(RNG.randrange(12)):
            start = RNG.randrange(len(b) + 1)
            b[start:start + RNG.randrange(10)] = [RNG.choice(["新\n", "新\r", "新\r\n", "\n"]) for _ in range(RNG.randrange(10))]
        add(a, b)
    # autojunk=True の既定動作は Opcodes と異なる。
    add(["a\n"] * 300, ["b\n"] + ["a\n"] * 300)
    save("diffs", result)


def strings():
    alphabet = list("AbΣσςİßǅ日本語😀_19") + list("\t\n\r\v\f\x1c\x1d\x1e\x1f\x85\u2028\u2029\u00a0\u2003\u200b\u0301'\u02b0")
    cases = ["", "ΟΣ", "ΟΣΑ", "AΣ'", "AΣ'A", "Σ", "İ", "A\u02b0Σ", "\r\n", "\x1f"]
    cases += ["".join(RNG.choice(alphabet) for _ in range(RNG.randrange(100))) for _ in range(2000)]
    result = []
    for s in cases:
        tabsize = RNG.choice([-8, -1, 0, 1, 2, 4, 8, 17])
        result.append(dict(s=s, lower=s.lower(), strip=s.strip(), lstrip=s.lstrip(), rstrip=s.rstrip(),
                           split=s.split(), lines=s.splitlines(), keep=s.splitlines(True),
                           alpha=s.isalpha(), length=len(s), tabsize=tabsize, expanded=s.expandtabs(tabsize)))
    save("strings", result)


def entities():
    cases = ["", "&", "&#", "&#x;", "&#X;", "&#-1;", "&#x-1;", "&notit;", "&notinva;tail", "&amp=1", "&AMPfoo", "&not\rA", "&&amp;"]
    for name in sorted(html.entities.html5):
        cases.extend(["&" + name, "&" + name + "tail=1", "&" + name.rstrip(";")])
    for cp in sorted(set(range(0x100)) | html._invalid_codepoints | {0xd7ff, 0xd800, 0xdfff, 0xe000, 0x10ffff, 0x110000, 0xffffffff}):
        cases.extend(["&#%d;" % cp, "&#x%x;" % cp, "&#X%X" % cp])
    cases += ["&#" + "9" * 4000 + ";", "&#x" + "f" * 10000 + ";", "&#" + "0" * 3999 + "1;", "&" + "not" + "😀" * 35 + ";"]
    save("html", [dict(s=s, expected=html.unescape(s)) for s in cases])


def node(value):
    if value is None:
        return dict(kind="null")
    if isinstance(value, bool):
        return dict(kind="bool", value=value)
    if isinstance(value, int):
        return dict(kind="int", value=str(value))
    if isinstance(value, float):
        return dict(kind="float", value=struct.pack(">d", value).hex())
    if isinstance(value, str):
        return dict(kind="string", value=value)
    if isinstance(value, list):
        return dict(kind="array", value=[node(v) for v in value])
    return dict(kind="object", value=[[k, node(v)] for k, v in value.items()])


def numbers_and_json():
    floats = [0., -0., 1., -1., 1e15, 1e16, 1e-4, 1e-5, 0.1 + 0.2,
              sys.float_info.max, sys.float_info.min, math.ulp(0.), math.inf, -math.inf, math.nan]
    for boundary in [1e15, 1e16, 1e-4, 1e-5]:
        floats += [math.nextafter(boundary, -math.inf), math.nextafter(boundary, math.inf)]
    floats += [struct.unpack(">d", RNG.getrandbits(64).to_bytes(8, "big"))[0] for _ in range(20000)]
    save("floats", [dict(bits=struct.pack(">d", f).hex(), expected=repr(f), json=json.dumps(f, ensure_ascii=False)) for f in floats])
    values = [None, True, False, 0, -1, 9223372036854775807, -9223372036854775808, [], {}, "\"\\" + "".join(chr(i) for i in range(32)) + "\u2028\u2029😀<>&/", {"z": [None, True, False, {"é": 1., "a": -0.}], "a": {}, "😀": []}]
    def random_value(depth):
        choices = [None, True, False, RNG.randrange(-100000, 100000), RNG.choice(floats), RNG.choice(["a", "\n\"\\", "😀\u2028", "日本語"])]
        if depth:
            choices += [[random_value(depth - 1) for _ in range(RNG.randrange(4))],
                        {k: random_value(depth - 1) for k in RNG.sample(["z", "a", "😀", "é", "\uffff"], RNG.randrange(6))}]
        return RNG.choice(choices)
    values += [random_value(3) for _ in range(600)]
    save("json", [dict(value=node(value), unsorted=json.dumps(value, ensure_ascii=False), sorted=json.dumps(value, ensure_ascii=False, sort_keys=True)) for value in values])


if __name__ == "__main__":
    ROOT.mkdir(exist_ok=True)
    sequences()
    diffs()
    strings()
    entities()
    numbers_and_json()
