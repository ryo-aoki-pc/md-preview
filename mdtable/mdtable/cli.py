"""mdtable コマンド: GLFM の Markdown にある表を Excel で編集して書き戻す。"""

import argparse
import hashlib
import json
import os
import queue
import re
import secrets
import shutil
import sys
import tempfile
import threading
import time

from . import __version__
from .document import MarkdownError
from .sync import MdTableError, default_output, export, import_workbook, list_tables
from .workbook import WorkbookError

USER_ERRORS = (MarkdownError, MdTableError, WorkbookError, OSError)
POLL_SECONDS = 0.3
EOF_GRACE_SECONDS = 30
# Excel が開くのを待つ秒。この間の Enter は、ブックが開いたか（~$ が現れたか）を見てから扱う
OPEN_GRACE_SECONDS = 3


def _prepare_streams():
    # パイプやファイルへ出すときは UTF-8 にする（端末へはそのまま Unicode で出る）
    for stream in (sys.stdout, sys.stderr):
        try:
            if not stream.isatty():
                stream.reconfigure(encoding="utf-8", errors="replace")
        except (AttributeError, ValueError):
            pass


def _err(text):
    print(text, file=sys.stderr)


def _enable_virtual_terminal(stream):
    """Windows の端末で ANSI の色を使えるようにする。失敗したら色を付けない。"""
    try:
        import ctypes
        import msvcrt
        from ctypes import wintypes

        kernel = ctypes.windll.kernel32
        kernel.GetConsoleMode.argtypes = (wintypes.HANDLE, ctypes.POINTER(wintypes.DWORD))
        kernel.GetConsoleMode.restype = wintypes.BOOL
        kernel.SetConsoleMode.argtypes = (wintypes.HANDLE, wintypes.DWORD)
        kernel.SetConsoleMode.restype = wintypes.BOOL
        handle = msvcrt.get_osfhandle(stream.fileno())
        mode = wintypes.DWORD()
        if not kernel.GetConsoleMode(handle, ctypes.byref(mode)):
            return False
        return bool(kernel.SetConsoleMode(handle, mode.value | 0x0004))
    except (AttributeError, ImportError, OSError, ValueError):
        return False


def _use_color(stream):
    if os.environ.get("NO_COLOR"):
        return False
    try:
        if not stream.isatty():
            return False
    except (AttributeError, OSError, ValueError):
        return False
    return os.name != "nt" or _enable_virtual_terminal(stream)


def _color_diff(diff):
    lines = []
    for line in diff.splitlines(True):
        color = ""
        if line.startswith(("---", "+++")):
            color = "\033[1m"
        elif line.startswith("@@"):
            color = "\033[36m"
        elif line.startswith("+"):
            color = "\033[32m"
        elif line.startswith("-"):
            color = "\033[31m"
        if color:
            body = line.rstrip("\r\n")
            line = color + body + "\033[0m" + line[len(body):]
        lines.append(line)
    return "".join(lines)


def _write_diff(diff):
    sys.stdout.write(_color_diff(diff) if _use_color(sys.stdout) else diff)


def _print_report(report):
    for message in report.messages:
        _err(message)
    for warning in report.warnings:
        _err("警告: " + warning)
    for error in report.errors:
        _err("エラー: " + error)


def cmd_list(args):
    regions = list_tables(args.markdown)
    if args.json:
        tables = []
        for region in regions:
            model = region.parsed.model if region.excluded is None else None
            tables.append({
                "number": region.number,
                "start_line": region.start + 1,
                "end_line": region.end + 1,
                "kind": region.kind,
                "rows": model.rows if model is not None else None,
                "cols": model.cols if model is not None else None,
                "excluded": region.excluded,
                "location": region.location,
                "heading": region.heading,
                "heading_line": region.heading_line + 1 if region.heading_line is not None else None,
            })
        print(json.dumps({"version": 1, "markdown": os.path.abspath(args.markdown), "tables": tables},
                         ensure_ascii=False))
        return 0
    if not regions:
        _err("表がありません")
        return 0
    for region in regions:
        kind = "Markdown" if region.kind == "pipe" else "HTML"
        if region.excluded:
            size = "対象外: " + region.excluded
        else:
            model = region.parsed.model
            size = "%d 行 × %d 列" % (model.rows, model.cols)
        parts = ["表 %d" % region.number, "%d〜%d 行" % (region.start + 1, region.end + 1), kind, size]
        if region.location:
            parts.append(region.location)
        if region.heading:
            parts.append("見出し: " + region.heading)
        print("\t".join(parts))
    return 0


def cmd_export(args):
    report = export(args.markdown, args.output, args.table, args.force)
    _print_report(report)
    print(report.output)
    return 0


def cmd_import(args):
    tables = None
    if args.table is not None:
        tables = []
        for selector in args.table:
            text = selector.strip()
            if not re.match(r"^\d+$", text):
                raise MdTableError("import の --table には、ブックの表の番号を指定してください: %s" % selector)
            tables.append(int(text))
    report = import_workbook(args.workbook, args.md, args.dry_run, args.force,
                             args.keep_kind, args.strict_text, tables=tables)
    if args.dry_run and report.diff:
        _write_diff(report.diff)
    _print_report(report)
    if report.errors:
        return 1
    if not report.changed:
        _err("Markdown は変わりません")
    elif args.dry_run:
        _err("（--dry-run なので書き戻していません）")
    else:
        _err("書き戻しました: %s" % report.output)
    return 0


def _file_hash(path):
    with open(path, "rb") as handle:
        return hashlib.sha256(handle.read()).hexdigest()


def _owner_files(directory):
    return [name for name in os.listdir(directory) if name.startswith("~$") or name.startswith(".~lock.")]


def _read_line():
    """標準入力の 1 行（改行は除く）。閉じていれば EOFError。

    端末でない標準入力（パイプなど）は、ファイル記述子から直接読む。input() は標準入力のバッファのロックを
    持ったまま待つので、読み待ちのスレッドを残して終わると、Python 3.9 は終了時に異常終了する
    （could not acquire lock for <stdin> at interpreter shutdown）。端末の input() はそのロックを使わない。
    """
    stream = sys.stdin
    if stream is None:
        raise EOFError
    try:
        descriptor = None if stream.isatty() else stream.fileno()
    except (AttributeError, OSError, ValueError):
        descriptor = None       # 差し替えられた標準入力は input() に任せる
    if descriptor is None:
        return input()
    data = bytearray()
    while True:
        chunk = os.read(descriptor, 1)
        if not chunk:
            if data:
                break
            raise EOFError
        if chunk == b"\n":
            break
        data.extend(chunk)
    return data.decode("utf-8", "replace").rstrip("\r")


def _wait_until_closed(directory, xlsx):
    """入力を待ちながら、開かれたブックが閉じられたかを確かめる。"""
    requests = queue.Queue()
    results = queue.Queue()
    read_line = _read_line

    def read_input():
        # 頼まれた 1 行だけ読む。自動で進んだ後に次の入力を読み始めないようにする
        while requests.get():
            try:
                answer = read_line()
            except EOFError:
                results.put(("eof", None))
                return
            except (Exception, KeyboardInterrupt) as error:
                results.put(("error", error))
                return
            results.put(("line", answer))

    threading.Thread(target=read_input, daemon=True).start()
    requests.put(True)
    started = time.monotonic()
    seen_owner = False
    confirming = False
    eof_deadline = None
    held = None     # Excel が開く前に届いた 1 行

    def answered(answer):
        """ブックが開いている間に届いた 1 行を扱う。このまま取り込むなら True。"""
        nonlocal confirming
        if confirming and answer.strip().lower() == "y":
            return True
        if not confirming:
            _err("ブックがまだ開いているようです。保存して閉じると自動で進みます"
                 "（このまま取り込むなら y を入力して Enter）")
            confirming = True
        # 入力がすぐ返る場合も、次の確認まで間隔を空ける
        time.sleep(POLL_SECONDS)
        requests.put(True)
        return False

    try:
        while True:
            owners = _owner_files(directory)
            if owners:
                seen_owner = True
            elif seen_owner:
                try:
                    with open(xlsx, "rb"):
                        pass
                except PermissionError:
                    pass
                else:
                    _err("ブックが閉じられたので取り込みます")
                    return
            if held is not None:
                # Excel が開く前の Enter は、~$ が現れるか、開くのを待つ時間が過ぎてから扱う
                if owners:
                    answer, held = held, None
                    if answered(answer):
                        return
                elif time.monotonic() - started >= OPEN_GRACE_SECONDS:
                    return
                else:
                    time.sleep(POLL_SECONDS)
                continue
            try:
                kind, answer = results.get(timeout=POLL_SECONDS)
            except queue.Empty:
                kind, answer = None, None
            if kind == "error":
                raise answer
            if kind == "eof":
                eof_deadline = time.monotonic() + EOF_GRACE_SECONDS
            elif kind == "line":
                if _owner_files(directory):
                    seen_owner = True
                    if answered(answer):
                        return
                elif seen_owner or time.monotonic() - started >= OPEN_GRACE_SECONDS:
                    return
                else:
                    held = answer
            if eof_deadline is not None and not seen_owner and time.monotonic() >= eof_deadline:
                raise EOFError
    finally:
        requests.put(False)


def cmd_edit(args):
    if os.name != "nt":
        raise MdTableError("edit は Windows だけで使えます（export と import を使ってください）")
    markdown = os.path.abspath(args.markdown)
    directory = tempfile.mkdtemp(prefix="mdtable-")
    stem = os.path.splitext(os.path.basename(markdown))[0]
    xlsx = os.path.join(directory, "%s.tables.%s.xlsx" % (stem, secrets.token_hex(4)))
    import_options = (" --keep-kind" if args.keep_kind else "") + (" --strict-text" if args.strict_text else "")
    keep = False
    opened = False
    try:
        report = export(markdown, xlsx, args.table, True)
        _print_report(report)
        before = _file_hash(xlsx)
        os.startfile(xlsx)
        opened = True
        keep = True
        _err("Excel で編集して保存し、ブックを閉じると自動で書き戻します（Enter でも進めます。中止は Ctrl+C）")
        _wait_until_closed(directory, xlsx)
        if not os.path.exists(markdown):
            raise MdTableError("書き戻す Markdown が見つかりません（--md で指定してください）: %s" % markdown)
        if _file_hash(xlsx) == before:
            _err("ブックは保存されていないので、Markdown は変えません")
            keep = False
            return 0
        report = None
        for attempt in range(2):
            try:
                report = import_workbook(xlsx, markdown, dry_run=args.dry_run, keep_kind=args.keep_kind,
                                         strict_text=args.strict_text)
                break
            except WorkbookError:
                if attempt == 1:
                    raise
                time.sleep(1)
        if args.dry_run and report.diff:
            _write_diff(report.diff)
        _print_report(report)
        if report.errors:
            _err("ブックを残しました。直してから次で取り込めます:")
            _err('  mdtable import "%s" --md "%s"%s' % (xlsx, markdown, import_options))
            return 1
        if report.changed:
            if args.dry_run:
                _err("（--dry-run なので書き戻していません）")
                _err("ブックを残しました。取り込むときは:")
                _err('  mdtable import "%s" --md "%s"%s' % (xlsx, markdown, import_options))
                return 0
            _err("書き戻しました: %s" % markdown)
        else:
            _err("Markdown は変わりません")
        keep = False
        return 0
    except (KeyboardInterrupt, EOFError):
        keep = True
        _err("")
        _err("中止しました。ブックを残しました: %s" % xlsx)
        _err('取り込むときは: mdtable import "%s" --md "%s"%s' % (xlsx, markdown, import_options))
        return 1
    except USER_ERRORS:
        if opened:
            _err("ブックを残しました: %s" % xlsx)
            _err('取り込むときは: mdtable import "%s" --md "%s"%s' % (xlsx, markdown, import_options))
        raise
    finally:
        if not keep:
            shutil.rmtree(directory, ignore_errors=True)
            if os.path.isdir(directory):
                # ブックを開いたまま取り込んだときは、Excel がブックを使っていて消せない
                _err("一時フォルダーを消せませんでした（ブックを閉じてから消してください）: %s" % directory)


def build_parser():
    parser = argparse.ArgumentParser(
        prog="mdtable",
        description="GLFM の Markdown にある表（パイプ表・HTML の表）を Excel で編集して書き戻す。")
    parser.add_argument("--version", action="version", version="mdtable " + __version__)
    sub = parser.add_subparsers(dest="command", metavar="コマンド")
    sub.required = True

    p = sub.add_parser("list", help="文書の表の一覧を出す")
    p.add_argument("markdown", help="Markdown のファイル")
    p.add_argument("--json", action="store_true", help="表の一覧を JSON で出す")
    p.set_defaults(func=cmd_list)

    p = sub.add_parser("export", help="表を Excel のブックに書き出す")
    p.add_argument("markdown", help="Markdown のファイル")
    p.add_argument("-o", "--output", help="書き出すブック（既定は <名前>.tables.xlsx）")
    p.add_argument("--table", action="append", metavar="番号|L行|見出し",
                   help="書き出す表（番号・L行番号・直前の見出しの文字。複数指定できる）")
    p.add_argument("--force", action="store_true", help="既にあるブックを上書きする")
    p.set_defaults(func=cmd_export)

    p = sub.add_parser("import", help="ブックの内容で Markdown の表を書き戻す")
    p.add_argument("workbook", help="export で書き出したブック")
    p.add_argument("--md", help="書き戻す Markdown（既定はブックに記録したもの）")
    p.add_argument("--table", action="append", metavar="番号", help="取り込むブックの表の番号（複数指定できる）")
    p.add_argument("--dry-run", action="store_true", help="書き戻さずに差分を出す")
    p.add_argument("--force", action="store_true",
                   help="衝突や、Excel が日付・数値に変えたセルがあっても書き戻す")
    p.add_argument("--keep-kind", action="store_true", help="パイプ表を HTML の表に変える必要があれば止める")
    p.add_argument("--strict-text", action="store_true", help="数値・真偽値・数式・エラー値のセルがあれば止める")
    p.set_defaults(func=cmd_import)

    p = sub.add_parser("edit", help="Excel で開いて編集し、閉じたら書き戻す（Windows）")
    p.add_argument("markdown", help="Markdown のファイル")
    p.add_argument("--table", action="append", metavar="番号|L行|見出し", help="編集する表（番号・L行番号・直前の見出しの文字）")
    p.add_argument("--dry-run", action="store_true", help="書き戻さずに差分を出す")
    p.add_argument("--keep-kind", action="store_true", help="パイプ表を HTML の表に変える必要があれば止める")
    p.add_argument("--strict-text", action="store_true", help="数値・真偽値・数式・エラー値のセルがあれば止める")
    p.set_defaults(func=cmd_edit)
    return parser


def main(argv=None):
    _prepare_streams()
    parser = build_parser()
    args = parser.parse_args(argv)
    if getattr(args, "strict_text", False) and getattr(args, "force", False):
        parser.error("--strict-text と --force は同時に指定できません")
    try:
        return args.func(args)
    except USER_ERRORS as error:
        if os.environ.get("MDTABLE_DEBUG") == "1":
            raise
        message = str(error)
        if isinstance(error, OSError) and not isinstance(error, (MarkdownError,)):
            message = "%s" % (error.strerror or error,)
            if getattr(error, "filename", None):
                message = "%s: %s" % (error.filename, message)
        _err("エラー: " + message)
        return 1
    except KeyboardInterrupt:
        _err("中止しました")
        return 1
    except Exception as error:  # 想定外の失敗でも Traceback は出さない
        if os.environ.get("MDTABLE_DEBUG") == "1":
            raise
        _err("エラー: 想定外の失敗です（MDTABLE_DEBUG=1 で詳細を出せます）: %r" % (error,))
        return 1


if __name__ == "__main__":
    sys.exit(main())
