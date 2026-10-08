"""mdtable コマンド: GLFM の Markdown にある表を Excel で編集して書き戻す。"""

import argparse
import hashlib
import os
import secrets
import shutil
import sys
import tempfile
import time

from . import __version__
from .document import MarkdownError
from .sync import MdTableError, default_output, export, import_workbook, list_tables
from .workbook import WorkbookError

USER_ERRORS = (MarkdownError, MdTableError, WorkbookError, OSError)


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


def _print_report(report):
    for message in report.messages:
        _err(message)
    for warning in report.warnings:
        _err("警告: " + warning)
    for error in report.errors:
        _err("エラー: " + error)


def cmd_list(args):
    regions = list_tables(args.markdown)
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
    report = import_workbook(args.workbook, args.md, args.dry_run, args.force)
    if args.dry_run and report.diff:
        sys.stdout.write(report.diff)
    _print_report(report)
    if report.errors and not args.force:
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


def cmd_edit(args):
    if os.name != "nt":
        raise MdTableError("edit は Windows だけで使えます（export と import を使ってください）")
    markdown = os.path.abspath(args.markdown)
    directory = tempfile.mkdtemp(prefix="mdtable-")
    stem = os.path.splitext(os.path.basename(markdown))[0]
    xlsx = os.path.join(directory, "%s.tables.%s.xlsx" % (stem, secrets.token_hex(4)))
    keep = True
    try:
        report = export(markdown, xlsx, args.table, True)
        _print_report(report)
        before = _file_hash(xlsx)
        os.startfile(xlsx)
        _err("Excel で編集して保存し、閉じたら Enter を押してください（中止は Ctrl+C）")
        while True:
            input()
            if _owner_files(directory):
                _err("ブックがまだ開いているようです。保存して閉じてから Enter を押してください"
                     "（このまま取り込むなら y を入力して Enter）")
                answer = input()
                if answer.strip().lower() != "y":
                    continue
            break
        if _file_hash(xlsx) == before:
            _err("ブックは保存されていないので、Markdown は変えません")
            keep = False
            return 0
        report = None
        for attempt in range(2):
            try:
                report = import_workbook(xlsx, markdown, False, False)
                break
            except WorkbookError:
                if attempt == 1:
                    raise
                time.sleep(1)
        _print_report(report)
        if report.errors:
            _err("ブックを残しました。直してから次で取り込めます:")
            _err('  mdtable import "%s" --md "%s"' % (xlsx, markdown))
            return 1
        if report.changed:
            _err("書き戻しました: %s" % markdown)
        else:
            _err("Markdown は変わりません")
        keep = False
        return 0
    except (KeyboardInterrupt, EOFError):
        _err("")
        _err("中止しました。ブックを残しました: %s" % xlsx)
        _err('取り込むときは: mdtable import "%s" --md "%s"' % (xlsx, markdown))
        return 1
    finally:
        if not keep:
            shutil.rmtree(directory, ignore_errors=True)


def build_parser():
    parser = argparse.ArgumentParser(
        prog="mdtable",
        description="GLFM の Markdown にある表（パイプ表・HTML の表）を Excel で編集して書き戻す。")
    parser.add_argument("--version", action="version", version="mdtable " + __version__)
    sub = parser.add_subparsers(dest="command", metavar="コマンド")
    sub.required = True

    p = sub.add_parser("list", help="文書の表の一覧を出す")
    p.add_argument("markdown", help="Markdown のファイル")
    p.set_defaults(func=cmd_list)

    p = sub.add_parser("export", help="表を Excel のブックに書き出す")
    p.add_argument("markdown", help="Markdown のファイル")
    p.add_argument("-o", "--output", help="書き出すブック（既定は <名前>.tables.xlsx）")
    p.add_argument("--table", action="append", metavar="番号|L行",
                   help="書き出す表（番号か、その行を含む表の L行番号。複数指定できる）")
    p.add_argument("--force", action="store_true", help="既にあるブックを上書きする")
    p.set_defaults(func=cmd_export)

    p = sub.add_parser("import", help="ブックの内容で Markdown の表を書き戻す")
    p.add_argument("workbook", help="export で書き出したブック")
    p.add_argument("--md", help="書き戻す Markdown（既定はブックに記録したもの）")
    p.add_argument("--dry-run", action="store_true", help="書き戻さずに差分を出す")
    p.add_argument("--force", action="store_true",
                   help="衝突や、Excel が日付・数値に変えたセルがあっても書き戻す")
    p.set_defaults(func=cmd_import)

    p = sub.add_parser("edit", help="Excel で開いて編集し、閉じたら書き戻す（Windows）")
    p.add_argument("markdown", help="Markdown のファイル")
    p.add_argument("--table", action="append", metavar="番号|L行", help="編集する表")
    p.set_defaults(func=cmd_edit)
    return parser


def main(argv=None):
    _prepare_streams()
    parser = build_parser()
    args = parser.parse_args(argv)
    try:
        return args.func(args)
    except USER_ERRORS as error:
        if os.environ.get("MDTABLE_DEBUG") == "1":
            raise
        message = str(error)
        if isinstance(error, OSError) and not isinstance(error, (MarkdownError,)):
            message = "%s: %s" % (getattr(error, "filename", "") or "", error.strerror or error)
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
