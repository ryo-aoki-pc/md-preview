"""Python 0.5.2 のテスト入力と結果を保存する。通常の Go テストには Python は不要。

リポジトリ直下で python -B testdata/regenerate.py を実行する。
Python 版、tests/、scripts/ と openpyxl が残っている間だけ再生成できる。
単体テストを実行して実際の引数と結果を記録するので、元の入力を二重管理しない。
保存するブックは openpyxl 製で、パスと日時を固定し、個人の情報は含めない。
"""

import contextlib
import datetime
import functools
import gzip
import hashlib
import importlib.util
import inspect
import io
import json
import os
import pathlib
import re
import shutil
import subprocess
import sys
import tempfile
import unittest
import zipfile

ROOT = pathlib.Path(__file__).resolve().parents[1]
OUT = ROOT / "testdata"
sys.path[:0] = [str(ROOT), str(ROOT / "tests")]
os.environ["MDTABLE_FONT"] = "BIZ UDゴシック"
os.environ["NO_COLOR"] = "1"
from mdtable import blocks, cell_from_html, cell_syntax, changes, document, model, sync, workbook
import helpers
import openpyxl

CURRENT = None
RECORDS = {}
SOURCES = {}
BOOKS = {}
BUSY = False


def write_text(path, value, newline="\n"):
    with open(path, "w", encoding="utf-8", newline=newline) as handle:
        handle.write(value)


def dump(path, value):
    mark_versions(value)
    if path.name == "goldens.json.gz":
        strings, indices = [], {}
        def intern(v):
            if isinstance(v, str) and len(v) > 96:
                if v not in indices:
                    indices[v] = len(strings)
                    strings.append(v)
                return {"$text": indices[v]}
            if isinstance(v, list):
                return [intern(x) for x in v]
            if isinstance(v, dict):
                return {k: intern(x) for k, x in v.items()}
            return v
        value = dict(value=intern(value), strings=strings)
    data = json.dumps(value, ensure_ascii=False, separators=(",", ":")).encode("utf-8")
    path.parent.mkdir(parents=True, exist_ok=True)
    with open(path, "wb") as handle:
        with gzip.GzipFile(filename="", mode="wb", fileobj=handle, mtime=0) as zipped:
            zipped.write(data)


def mark_versions(value):
    """ツールが書いた版の欄だけを印にし、文書やセルの文字はそのまま残す。"""
    if isinstance(value, list):
        for item in value:
            mark_versions(item)
    elif isinstance(value, dict):
        if value.get("title") == "_mdtable":
            cell = value.get("cells", {}).get("C1")
            if isinstance(cell, dict) and isinstance(cell.get("value"), str):
                cell["value"] = "<VERSION>"
        args = value.get("args")
        if value.get("op") == "command":
            args = value.get("input", {}).get("args")
        if args == ["--version"] and isinstance(value.get("want"), dict):
            value["want"]["out"] = "mdtable <VERSION>\n"
        for item in value.values():
            mark_versions(item)


def norm(text, directory):
    if not isinstance(text, str):
        return text
    for path in sorted({str(directory), str(directory).replace("\\", "/"),
                        json.dumps(str(directory))[1:-1], json.dumps(str(directory), ensure_ascii=False)[1:-1]}, key=len, reverse=True):
        text = text.replace(path, "<DIR>")
    return text.replace("<DIR>\\\\", "<DIR>/").replace("<DIR>\\", "<DIR>/")


def normalized(value, directory):
    if isinstance(value, str):
        return norm(value, directory)
    if isinstance(value, dict):
        return {k: normalized(v, directory) for k, v in value.items()}
    if isinstance(value, (tuple, list)):
        return [normalized(v, directory) for v in value]
    return value


def mvalue(m):
    if m is None:
        return None
    return dict(kind=m.kind or "", rows=m.rows, cols=m.cols,
                cells=[list(c.key()) for c in m.cells], holes=sorted(m.holes))


def regions_value(regions):
    result = []
    for r in regions:
        item = {k: getattr(r, k) for k in ("number", "kind", "start", "end", "prefixes", "bodies",
                "cont_prefix", "head", "tail", "location", "heading", "heading_line", "warnings")}
        item["excluded"] = r.excluded or ""
        parsed = r.parsed if hasattr(r.parsed, "model") else None
        item["model"] = mvalue(parsed.model) if parsed else None
        item["link_definitions"] = [c.link_definitions for c in parsed.cells] if r.kind == "html" and parsed else []
        result.append(item)
    return result


def block_value(doc):
    nodes = []
    for n in doc.root.walk():
        item = [n.kind, n.start_line, n.end_line,
                [a.kind for a in n.ancestors() if a.kind != blocks.DOCUMENT]]
        nodes.append(item)
    return nodes


def book_bytes(path):
    """ZIP の時刻・文書プロパティ・管理シートのパスを固定する。"""
    directory = pathlib.Path(path).resolve().parent
    markdown_directory = directory
    exported = None
    book = openpyxl.load_workbook(path, read_only=True)
    try:
        if "_mdtable" in book:
            markdown = book["_mdtable"]["B2"].value
            exported = book["_mdtable"]["B3"].value
            if isinstance(markdown, str) and pathlib.Path(markdown).is_absolute():
                markdown_directory = pathlib.Path(markdown).parent
    finally:
        book.close()
    target = io.BytesIO()
    with zipfile.ZipFile(path) as source, zipfile.ZipFile(target, "w", zipfile.ZIP_DEFLATED) as dest:
        for name in source.namelist():
            data = source.read(name)
            if name.endswith(".xml"):
                text = data.decode("utf-8")
                text = norm(norm(text, markdown_directory), directory).replace("<DIR>", "&lt;DIR&gt;")
                if isinstance(exported, str) and exported:
                    text = text.replace(exported, "2000-01-01T00:00:00")
                if name == "docProps/core.xml":
                    text = re.sub(r"(<(?:dcterms:created|dcterms:modified)[^>]*>).*?(</)",
                                  r"\g<1>2000-01-01T00:00:00Z\2", text)
                data = text.encode("utf-8")
            info = zipfile.ZipInfo(name, (2000, 1, 1, 0, 0, 0))
            info.compress_type = zipfile.ZIP_DEFLATED
            dest.writestr(info, data)
    data = target.getvalue()
    key = hashlib.sha256(data).hexdigest()[:20]
    BOOKS[key] = data
    return key


def report_value(report):
    return {key: getattr(report, key) for key in ("messages", "warnings", "errors", "diff", "changed", "output")}


def data_value(data):
    result = {k: getattr(data, k) for k in ("markdown_path", "markdown_relpath", "warnings")}
    result["source_hashes"] = sorted(data.source_hashes)
    result["tables"] = []
    for t in data.tables:
        item = {k: getattr(t, k) for k in ("number", "kind", "start", "end", "source_hash", "fingerprint",
                "sheet_title", "occurrence", "same", "missing", "errors", "warnings")}
        item["model"] = mvalue(t.model)
        result["tables"].append(item)
    return result


def load_script(name):
    spec = importlib.util.spec_from_file_location(name.replace("-", "_"), ROOT / "scripts" / (name + ".py"))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


COMPARE = load_script("compare-go")
WORKBOOK = load_script("compare-go-workbook")


def inspect_book(path):
    directory = pathlib.Path(path).resolve().parent
    return normalized(WORKBOOK.inspect_book(path, directory), directory)


def record(package, op, inputs, output=None, error=None):
    # モックで出力を壊す 2 件は Go でも Verify を直接呼ぶ独立したテストにする。
    if (op == "convert" and CURRENT == "test_convert.ConversionVerifyTest.test_changed_cell_text_prevents_file_write"
            or op == "import" and CURRENT == "test_workbook.ExportImportTest.test_html_verification_rejects_lost_structure"):
        return
    RECORDS.setdefault(package, []).append(dict(test=CURRENT, op=op, input=inputs, want=output, error=error))


def replace_function(original, wrapped):
    # from ... import ... で読み込まれた別名も同じ計測口にそろえる。
    for name, module in list(sys.modules.items()):
        if name.startswith(("mdtable", "test_")) or name == "helpers":
            for attr, value in list(vars(module).items()):
                if value is original:
                    setattr(module, attr, wrapped)


def traced(original, package, op, encode_input, encode_output=lambda v, a, k: v):
    @functools.wraps(original)
    def wrapped(*args, **kwargs):
        global BUSY
        caller = inspect.currentframe().f_back.f_globals.get("__name__", "")
        capture = CURRENT and not BUSY and (caller.startswith("test_") or caller == "helpers")
        if not capture:
            return original(*args, **kwargs)
        BUSY = True
        try:
            inputs = encode_input(args, kwargs)
            try:
                value = original(*args, **kwargs)
            except Exception as error:
                message = str(error)
                if op in ("read", "import", "export", "convert"):
                    message = norm(message, pathlib.Path(args[0]).parent)
                record(package, op, inputs, error=[message, getattr(error, "row", 0) or 0, getattr(error, "col", 0) or 0])
                raise
            record(package, op, inputs, encode_output(value, args, kwargs))
            return value
        finally:
            BUSY = False
    replace_function(original, wrapped)


def file_input(args, kwargs):
    path = pathlib.Path(args[0])
    bound = dict(kwargs)
    return normalized(dict(path=path.name, exists=path.exists(), before=path.read_bytes().hex() if path.exists() else "", args=list(args[1:]), options=bound, font=os.environ.get("MDTABLE_FONT")), path.parent)


def file_output(value, args, kwargs):
    path = pathlib.Path(args[0])
    result = dict(report=report_value(value), after=path.read_bytes().hex())
    if value.output and str(value.output).endswith(".xlsx") and pathlib.Path(value.output).exists():
        result["properties"] = inspect_book(value.output)
    return normalized(result, path.parent)


def import_input(args, kwargs):
    bound = inspect.signature(ORIGINAL_IMPORT).bind(*args, **kwargs)
    bound.apply_defaults()
    p = bound.arguments
    path = pathlib.Path(p["md_path"]) if p["md_path"] else None
    if path is None:
        data = ORIGINAL_READ(args[0])
        path = pathlib.Path(data.markdown_path)
    return dict(book=book_bytes(args[0]), md=path.name, exists=path.exists(),
                before=path.read_bytes().hex() if path.exists() else "", options={k: v for k, v in p.items() if k not in ("xlsx_path", "md_path")})


def import_output(value, args, kwargs):
    bound = inspect.signature(ORIGINAL_IMPORT).bind(*args, **kwargs)
    path = bound.arguments.get("md_path")
    if path is None:
        path = ORIGINAL_READ(args[0]).markdown_path
    path = pathlib.Path(path)
    return normalized(dict(report=report_value(value), after=path.read_bytes().hex() if path.exists() else ""), path.parent)


ORIGINAL_IMPORT = sync.import_workbook
ORIGINAL_READ = workbook.read


def generate_units():
    global CURRENT
    suite = unittest.defaultTestLoader.discover(str(ROOT / "tests"))
    modules = [m for n, m in sys.modules.items() if n.startswith("test_")]
    # find_tables の返す領域と元文書を結び付け、選択と書き戻しの入力を残す。
    original_find = document.find_tables
    def find(md):
        result = original_find(md)
        for r in result:
            SOURCES[id(r)] = md.text()
        return result
    replace_function(original_find, find)
    traced(helpers.regions, "table", "regions", lambda a, k: a[0], lambda v, a, k: regions_value(v))
    traced(find, "table", "find", lambda a, k: a[0].text(), lambda v, a, k: regions_value(v))
    traced(document.select_tables, "table", "select", lambda a, k: [SOURCES[id(a[0][0])], a[1]], lambda v, a, k: [r.number for r in v])
    from mdtable import pipe_table
    for fn, package, op in [(cell_syntax.normalize_list_spacing, "cellsyntax", "spacing"),
            (cell_syntax.to_pipe_cell, "cellsyntax", "pipe"), (cell_syntax.to_html_cell, "cellsyntax", "html"),
            (cell_syntax.chunk_to_excel, "cellsyntax", "chunk"), (pipe_table.cell_to_excel, "table", "cell"),
            (blocks.split_lines, "blocks", "lines"), (blocks.unescape_pipes, "blocks", "unescape"),
            (model.normalize_text, "model", "normalize"), (workbook.check_cell_text, "workbook", "check"),
            (workbook.escape_for_excel, "workbook", "escape")]:
        traced(fn, package, op, lambda a, k: a[0])
    for module in modules:
        if hasattr(module, "from_html"):
            traced(module.from_html, "cellfromhtml", "html", lambda a, k: a[0])
    traced(blocks.parse_text, "blocks", "parse", lambda a, k: [a[0], k.get("front_matter", True), k.get("keep_definitions", False)], lambda v, a, k: block_value(v))
    traced(model.sheet_form, "model", "sheet", lambda a, k: mvalue(a[0]), lambda v, a, k: mvalue(v))
    traced(changes.compare_models, "changes", "compare", lambda a, k: [mvalue(x) for x in a],
           lambda v, a, k: dict(summary=v.summary(), details=v.details(), full=v.details(28),
                               cells=[[c.row, c.col, c.old_text, c.new_text, c.kind] for c in v.cells]))
    traced(workbook.font_entry_matches, "workbook", "font_entry", lambda a, k: list(a))
    traced(workbook.sheet_name_for, "workbook", "sheet_name", lambda a, k: [a[0], a[1], sorted(a[2])])
    traced(sync.convert, "sync", "convert", file_input, file_output)
    traced(sync.export, "sync", "export", file_input, file_output)
    traced(sync.import_workbook, "sync", "import", import_input, import_output)
    traced(sync._render, "sync", "render", lambda a, k: [SOURCES[id(a[0])], a[0].number, mvalue(a[1]), k.get("keep_kind", False)],
           lambda v, a, k: [v[0], v[1], a[2]])
    traced(sync.verify, "sync", "verify", lambda a, k: [a[0].text(), a[1],
        [dict(number=r.region.number, lines=r.lines, model=mvalue(r.model), target=r.target_kind or "") for r in a[2]]])
    traced(workbook.read, "workbook", "read", lambda a, k: dict(book=book_bytes(a[0]), options=k),
           lambda v, a, k: normalized(data_value(v), pathlib.Path(a[0]).parent))
    original_cli = helpers.run_cli
    def cli_call(*args, **kwargs):
        if not CURRENT or CURRENT == "test_cli.CliTest.test_edit_retries_unreadable_workbook_once":
            return original_cli(*args, **kwargs)
        directory = pathlib.Path(args[1]).parent if len(args) > 1 else ROOT
        files = {}
        for p in directory.iterdir():
            if p.is_file() and p.suffix in (".md", ".xlsx"):
                files[p.name] = dict(book=book_bytes(p)) if p.suffix == ".xlsx" else dict(text=p.read_bytes().hex())
        value = original_cli(*args, **kwargs)
        after = {p.name: p.read_bytes().hex() for p in directory.glob("*.md")}
        # argparse の使い方の文は実装ごとに異なるので、終了コードだけを検査する。
        result = dict(code=value[0], out=value[1].replace("\r\n", "\n"), err=value[2].replace("\r\n", "\n"), after=after)
        record("cli", "command", normalized(dict(args=list(args), files=files), directory), normalized(result, directory))
        return value
    replace_function(original_cli, cli_call)
    for module in modules:
        if hasattr(module, "rewrite"):
            original = module.rewrite
            def setup_rewrite(original):
                @functools.wraps(original)
                def rewrite(text, number, edit):
                    global BUSY
                    if not CURRENT or BUSY:
                        return original(text, number, edit)
                    BUSY = True
                    edited = []
                    def capture(m):
                        edit(m)
                        edited.append(mvalue(m))
                    try:
                        value = original(text, number, capture)
                        record("sync", "rewrite", [text, number, edited[0]], value)
                        return value
                    finally:
                        BUSY = False
                return rewrite
            replace_function(original, setup_rewrite(original))
    class Result(unittest.TextTestResult):
        def startTest(self, test):
            global CURRENT
            CURRENT = test.id()
            super().startTest(test)
        def stopTest(self, test):
            global CURRENT
            CURRENT = None
            super().stopTest(test)
    result = unittest.TextTestRunner(resultclass=Result, verbosity=1).run(suite)
    if not result.wasSuccessful():
        raise RuntimeError("元の Python テストが失敗したので期待値を保存しない")
    for package, records in RECORDS.items():
        dump(OUT / "unit" / (package + ".json.gz"), records)
    names = sorted({test.id() for module in modules for test in flatten(unittest.defaultTestLoader.loadTestsFromModule(module))})
    covered = {r["test"] for records in RECORDS.values() for r in records}
    write_text(OUT / "coverage.json", json.dumps(dict(total=len(names), recorded=sorted(covered),
        native_or_python_specific=sorted(set(names) - covered)), ensure_ascii=False, indent=2) + "\n")


def flatten(suite):
    for test in suite:
        if isinstance(test, unittest.TestSuite):
            yield from flatten(test)
        else:
            yield test


def run_cli(args, directory):
    from mdtable import cli
    stdout, stderr = io.StringIO(), io.StringIO()
    with contextlib.redirect_stdout(stdout), contextlib.redirect_stderr(stderr):
        code = cli.main(args)
    return dict(code=code, out=norm(stdout.getvalue(), directory), err=norm(stderr.getvalue(), directory))


def generate_goldens():
    global CURRENT
    CURRENT = None
    sources = OUT / "documents"
    sources.mkdir(exist_ok=True)
    for path in sorted((ROOT / "docs" / "verification").glob("*.md")):
        shutil.copyfile(path, sources / path.name)
    import subprocess
    subprocess.run([sys.executable, "-B", str(ROOT / "scripts" / "fuzz-docs.py"), str(sources / "fuzz"), "150", "1"], check=True)
    documents, imports, exports = [], [], []
    with tempfile.TemporaryDirectory() as temporary:
        directory = pathlib.Path(temporary)
        old = os.getcwd()
        os.chdir(directory)
        try:
            for path in sorted(sources.rglob("*.md")):
                md = directory / "document.md"
                before = path.read_bytes()
                md.write_bytes(before)
                listing = run_cli(["list", "document.md", "--json"], directory)
                listing = json.loads(listing["out"]) if listing["code"] == 0 else None
                cases = []
                for command in COMPARE.cases_for(listing, False):
                    md.write_bytes(before)
                    command = command[:1] + ["document.md"] + command[1:]
                    result = run_cli(command, directory)
                    result["after"] = md.read_bytes().hex()
                    cases.append(dict(args=command, want=result))
                documents.append(dict(source=path.relative_to(OUT).as_posix(), cases=cases))
            # 書き出しの性質は 3 つの検証文書で確かめる。
            for path in sorted(sources.glob("*.md")):
                md.write_bytes(path.read_bytes())
                result = run_cli(["export", "document.md", "-o", "book.xlsx", "--force"], directory)
                if result["code"] == 0:
                    exports.append(dict(source=path.relative_to(OUT).as_posix(), want=inspect_book("book.xlsx")))
            # 小さな 2 表の見本で各編集を独立して保存する。
            source = "# 見本\n\n| 名前 | 値 |\n| :--- | ---: |\n| a | b |\n| c | d |\n\n<table>\n<tr><th>項目</th><th>内容</th></tr>\n<tr><td>x</td><td>y</td></tr>\n</table>\n"
            write_text(md, source, newline="")
            run_cli(["export", "document.md", "-o", "book.xlsx", "--force"], directory)
            original = pathlib.Path("book.xlsx").read_bytes()
            for kind in ["unchanged"] + WORKBOOK.KINDS:
                pathlib.Path("book.xlsx").write_bytes(original)
                wb = openpyxl.load_workbook("book.xlsx", rich_text=True)
                if kind != "unchanged":
                    WORKBOOK.mutations(wb, kind)
                wb.save("book.xlsx")
                wb.close()
                key = book_bytes("book.xlsx")
                for options in ([], ["--force"], ["--strict-text"], ["--keep-kind"], ["--table", "1"], ["--table", "999"]):
                    for dry in (False, True):
                        write_text(md, source, newline="")
                        args = ["import", "book.xlsx", "--md", "document.md"] + options + (["--dry-run"] if dry else [])
                        if kind == "escape":
                            # _x005F_ の意図した違いだけ、正しい文字に戻した Python の期待値を残す。
                            original_cell_text = workbook._cell_text
                            def corrected(*a, **k):
                                value = original_cell_text(*a, **k)
                                return "_x005F_ / x_x0041_x0042_ / _xD800_" if value == "_ / x_x0041_x0042_ / _xD800_" else value
                            workbook._cell_text = corrected
                        try:
                            result = run_cli(args, directory)
                        finally:
                            if kind == "escape":
                                workbook._cell_text = original_cell_text
                        result["after"] = md.read_bytes().hex()
                        imports.append(dict(kind=kind, book=key, before=source.encode().hex(), args=args, want=result))
        finally:
            os.chdir(old)
    dump(OUT / "goldens.json.gz", dict(documents=documents, imports=imports, exports=exports))
    shutil.copyfile(ROOT / "tests" / "comrak_ast.mjs", OUT / "comrak_ast.mjs")
    import test_oracles
    write_text(OUT / "oracle_samples.json", json.dumps(test_oracles.SAMPLES, ensure_ascii=False, indent=2) + "\n")


if __name__ == "__main__":
    if (OUT / "workbooks.zip").exists():
        with zipfile.ZipFile(OUT / "workbooks.zip") as archive:
            for name in archive.namelist():
                BOOKS[name[:-5]] = archive.read(name)
    if "--goldens-only" not in sys.argv:
        generate_units()
    if "--units-only" not in sys.argv:
        generate_goldens()
    used = set()
    def references(v):
        if isinstance(v, list):
            for x in v:
                references(x)
        elif isinstance(v, dict):
            if "book" in v:
                used.add(v["book"])
            for x in v.values():
                references(x)
    for path in list((OUT / "unit").glob("*.json.gz")) + [OUT / "goldens.json.gz"]:
        references(json.loads(gzip.decompress(path.read_bytes())))
    with zipfile.ZipFile(OUT / "workbooks.zip", "w", zipfile.ZIP_DEFLATED) as archive:
        for key, data in sorted(BOOKS.items()):
            if key in used:
                archive.writestr(key + ".xlsx", data)
    subprocess.run([sys.executable, "-B", str(OUT / "generate_go_tests.py")], cwd=ROOT, check=True)
    print(json.dumps(dict(unit_cases={k: len(v) for k, v in RECORDS.items()}, books=len(BOOKS)), ensure_ascii=False))
