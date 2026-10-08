"""テストの補助。"""

import os
import subprocess
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
if ROOT not in sys.path:
    sys.path.insert(0, ROOT)

from mdtable.document import MarkdownFile, find_tables  # noqa: E402


def md_file(text, path="<test>"):
    return MarkdownFile(path, text)


def regions(text):
    return find_tables(md_file(text))


def spans(text):
    """表の範囲（1 起点の行）・種類・対象外の理由のリスト。"""
    return [(r.start + 1, r.end + 1, r.kind, r.excluded) for r in regions(text)]


def texts(region):
    """表のモデルのセルの文字を、行ごとのリストにする。"""
    model = region.parsed.model
    rows = [[None] * model.cols for _ in range(model.rows)]
    for cell in model.cells:
        rows[cell.row][cell.col] = cell.text
    return rows


def run_cli(*args, cwd=None, env=None, stdin=None):
    """mdtable を別のプロセスで動かす。(終了コード, 標準出力, 標準エラー)。"""
    command = [sys.executable, "-B", "-m", "mdtable"] + list(args)
    environment = dict(os.environ)
    environment["PYTHONPATH"] = ROOT
    environment.pop("MDTABLE_DEBUG", None)
    if env:
        environment.update(env)
    result = subprocess.run(command, cwd=cwd, env=environment, capture_output=True,
                            input=stdin.encode("utf-8") if stdin is not None else None)
    return (result.returncode, result.stdout.decode("utf-8", "replace"),
            result.stderr.decode("utf-8", "replace"))
