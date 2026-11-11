"""Python 版と Go 版の mdtable の出力を突き合わせる。

文書ごとに、Python 版と Go 版へ同じ文書の写しを別々のフォルダーに置いて同じコマンドを打ち、
終了コード・標準出力・標準エラー・書き換えた後の文書のバイト列を比べる。
フォルダーの場所の違いは <DIR> に置き換えてから比べる。

使い方:
  python scripts/compare-go.py --go <Go 版の mdtable> --py-root <Python 版のリポジトリ> --out <結果のフォルダー>
         [--jobs 8] [--quick] [--py-cache <フォルダー>] <文書か文書のフォルダー> ...

  --quick     表ごとの書き換え（--dry-run なし）と、見出し・行番号での選び方を省く
  --py-cache  Python 版の結果をここに覚え、次から使う（Python 版を変えたら消す）

違いがあれば <結果のフォルダー>/mismatch/ に 1 件ずつ書き、終了コード 1 で終わる。
"""

import argparse
import concurrent.futures
import hashlib
import json
import os
import shutil
import subprocess
import sys

TIMEOUT = 300


def collect(paths):
    docs = []
    for path in paths:
        if os.path.isdir(path):
            for root, dirs, files in os.walk(path):
                dirs.sort()
                for name in sorted(files):
                    if name.lower().endswith((".md", ".markdown")):
                        docs.append(os.path.join(root, name))
        else:
            docs.append(path)
    return docs


class Runner(object):
    def __init__(self, go, py_root, cache):
        self.go = os.path.abspath(go)
        self.py_root = os.path.abspath(py_root)
        self.cache = cache

    def command(self, impl, args):
        if impl == "go":
            return [self.go] + args
        return [sys.executable, "-B", "-m", "mdtable"] + args

    def run(self, impl, args, cwd, name, original):
        """(終了コード, 標準出力, 標準エラー, 書いた後の文書) を返す。文書は元に戻す。"""
        key = None
        if impl == "py" and self.cache:
            digest = hashlib.sha256()
            digest.update(original)
            digest.update(json.dumps([name, args]).encode("utf-8"))
            key = os.path.join(self.cache, digest.hexdigest() + ".json")
            if os.path.exists(key):
                with open(key, "r", encoding="utf-8") as handle:
                    data = json.load(handle)
                return (data[0], bytes.fromhex(data[1]), bytes.fromhex(data[2]),
                        bytes.fromhex(data[3]) if data[3] is not None else None)
        env = dict(os.environ)
        env.pop("NO_COLOR", None)
        env.pop("MDTABLE_DEBUG", None)
        if impl == "py":
            env["PYTHONPATH"] = self.py_root
        try:
            proc = subprocess.run(self.command(impl, args), cwd=cwd, env=env, stdin=subprocess.DEVNULL,
                                  stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=TIMEOUT)
            code, out, err = proc.returncode, proc.stdout, proc.stderr
        except subprocess.TimeoutExpired:
            code, out, err = "timeout", b"", b""
        path = os.path.join(cwd, name)
        with open(path, "rb") as handle:
            after = handle.read()
        if after == original:
            after = None
        else:
            with open(path, "wb") as handle:
                handle.write(original)
        result = (code, normalize(out, cwd), normalize(err, cwd), after)
        if key is not None and code != "timeout":
            with open(key, "w", encoding="utf-8") as handle:
                json.dump([result[0], result[1].hex(), result[2].hex(),
                           result[3].hex() if result[3] is not None else None], handle)
        return result


def normalize(data, cwd):
    variants = [cwd, cwd.replace("\\", "/"), json.dumps(cwd)[1:-1]]
    for variant in sorted(set(variants), key=len, reverse=True):
        data = data.replace(variant.encode("utf-8"), b"<DIR>")
    return data


def cases_for(listing, quick):
    """list --json の結果から、打つコマンドの並びを作る。"""
    cases = [["list"], ["list", "--json"]]
    tables = listing.get("tables", []) if listing else []
    numbers = [str(t["number"]) for t in tables]
    for table in tables:
        other = "pipe" if table["kind"] == "html" else "html"
        cases.append(["convert", "--table", str(table["number"]), "--to", other, "--dry-run"])
        if not quick:
            cases.append(["convert", "--table", str(table["number"]), "--to", other])
    if tables:
        first = tables[0]
        cases.append(["convert", "--table", str(first["number"]), "--to", first["kind"], "--dry-run"])
        for target in ("html", "pipe"):
            args = ["convert"]
            for number in numbers:
                args += ["--table", number]
            cases.append(args + ["--to", target, "--dry-run"])
        if not quick:
            cases.append(["convert", "--table", "L%d" % first["end_line"], "--to", "html", "--dry-run"])
            headed = [t for t in tables if t.get("heading")]
            if headed:
                cases.append(["convert", "--table", headed[0]["heading"], "--to", "pipe", "--dry-run"])
    cases.append(["convert", "--table", str(len(tables) + 1), "--to", "html", "--dry-run"])
    return cases


def compare_doc(runner, index, doc, out_dir, quick):
    with open(doc, "rb") as handle:
        original = handle.read()
    name = os.path.basename(doc)
    base = os.path.join(out_dir, "work", "%04d" % index)
    dirs = {}
    for impl in ("py", "go"):
        dirs[impl] = os.path.abspath(os.path.join(base, impl))
        os.makedirs(dirs[impl], exist_ok=True)
        with open(os.path.join(dirs[impl], name), "wb") as handle:
            handle.write(original)
    results = []
    py_list = runner.run("py", ["list", "--json", name], dirs["py"], name, original)
    listing = None
    if py_list[0] == 0:
        try:
            listing = json.loads(py_list[1].decode("utf-8"))
        except ValueError:
            listing = None
    for args in cases_for(listing, quick):
        full = args[:1] + [name] + args[1:] if args[0] in ("list", "convert") else args
        py = runner.run("py", full, dirs["py"], name, original)
        go = runner.run("go", full, dirs["go"], name, original)
        results.append((full, py, go))
    shutil.rmtree(base, ignore_errors=True)
    return doc, results


def describe(result):
    code, out, err, after = result
    lines = ["exit: %s" % code, "--- stdout ---", out.decode("utf-8", "replace"),
             "--- stderr ---", err.decode("utf-8", "replace")]
    if after is not None:
        lines += ["--- 書いた後の文書（sha256 %s、%d バイト）---" % (hashlib.sha256(after).hexdigest(), len(after))]
    return "\n".join(lines)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--go", required=True)
    parser.add_argument("--py-root", required=True)
    parser.add_argument("--out", required=True)
    parser.add_argument("--jobs", type=int, default=8)
    parser.add_argument("--quick", action="store_true")
    parser.add_argument("--py-cache")
    parser.add_argument("paths", nargs="+")
    args = parser.parse_args()
    if args.py_cache:
        os.makedirs(args.py_cache, exist_ok=True)
    out_dir = os.path.abspath(args.out)
    mismatch_dir = os.path.join(out_dir, "mismatch")
    shutil.rmtree(mismatch_dir, ignore_errors=True)
    os.makedirs(mismatch_dir, exist_ok=True)
    runner = Runner(args.go, args.py_root, args.py_cache)
    docs = collect(args.paths)
    total = 0
    mismatches = []
    by_command = {}
    with concurrent.futures.ThreadPoolExecutor(max_workers=args.jobs) as pool:
        futures = [pool.submit(compare_doc, runner, i, doc, out_dir, args.quick) for i, doc in enumerate(docs)]
        for future in concurrent.futures.as_completed(futures):
            doc, results = future.result()
            for full, py, go in results:
                total += 1
                command = full[0] + (" --json" if "--json" in full else "") + (" --dry-run" if "--dry-run" in full else "")
                counts = by_command.setdefault(command, [0, 0])
                counts[0] += 1
                if py == go:
                    continue
                counts[1] += 1
                parts = []
                for label, i in (("終了コード", 0), ("標準出力", 1), ("標準エラー", 2), ("書いた後の文書", 3)):
                    if py[i] != go[i]:
                        parts.append(label)
                mismatches.append((doc, full, parts))
                file_name = "%04d.txt" % len(mismatches)
                with open(os.path.join(mismatch_dir, file_name), "w", encoding="utf-8", newline="\n") as handle:
                    handle.write("文書: %s\nコマンド: mdtable %s\n違い: %s\n\n=== Python ===\n%s\n\n=== Go ===\n%s\n"
                                 % (doc, " ".join(full), "・".join(parts), describe(py), describe(go)))
                    if py[3] is not None and go[3] is not None and py[3] != go[3]:
                        import difflib
                        handle.write("\n=== 書いた後の文書の差（Python → Go）===\n")
                        handle.writelines(difflib.unified_diff(
                            py[3].decode("utf-8", "replace").splitlines(True),
                            go[3].decode("utf-8", "replace").splitlines(True), "python", "go"))
    sys.stdout.reconfigure(encoding="utf-8")
    print("文書 %d 個、コマンド %d 回、違い %d 回" % (len(docs), total, len(mismatches)))
    for command, (count, bad) in sorted(by_command.items()):
        print("  %-22s %6d 回中 %d 回違う" % (command, count, bad))
    for doc, full, parts in mismatches[:20]:
        print("  違い: %s: mdtable %s（%s）" % (doc, " ".join(full), "・".join(parts)))
    if mismatches:
        print("詳しくは %s" % mismatch_dir)
    return 1 if mismatches else 0


if __name__ == "__main__":
    sys.exit(main())
