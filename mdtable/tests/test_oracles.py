"""手元の文書と comrak を使う確認（環境変数を設定したときだけ動く）。

- MDTABLE_CORPUS: Markdown のあるフォルダー。すべての表を書き出して無編集で取り込み、変わらないこと
- MDTABLE_COMRAK_DIR: comrak の npm パッケージを持つフォルダー（例: Neovim の glfm-format）。
  ブロックの構造（葉のブロックの種類・行・祖先、表の行）が comrak と同じこと
"""

import glob
import json
import os
import shutil
import subprocess
import tempfile
import unittest
from collections import Counter

from helpers import ROOT

from mdtable import blocks, sync

CORPUS = os.environ.get("MDTABLE_CORPUS")
COMRAK = os.environ.get("MDTABLE_COMRAK_DIR")

TYPES = {
    "Paragraph": "paragraph", "Heading": "heading", "CodeBlock": "code_block",
    "HtmlBlock": "html_block", "ThematicBreak": "thematic_break", "Table": "table",
    "BlockQuote": "block_quote", "Alert": "alert", "MultilineBlockQuote": "multiline_block_quote",
    "List": "list", "Item": "item", "TaskItem": "item", "DescriptionList": "description_list",
    "DescriptionItem": "description_item", "DescriptionTerm": "description_term",
    "DescriptionDetails": "description_details", "FootnoteDefinition": "footnote_definition",
}
LEAVES = {"paragraph", "heading", "html_block", "thematic_break", "table"}

SAMPLES = [
    "text\n| a | b |\n|---|---|\n| 1 | 2 |\n",
    "| a |\n|---|\n| 1 |\n- x\n",
    "| a |\n|---|\n| 1 |\n: x\n",
    "> p\n| a |\n> |---|\n",
    "1. 手順\n\n   | a | b |\n   |---|---|\n",
    "用語\n: 説明\n\n  | a |\n  |---|\n",
    "> [!NOTE]\n> 1. a\n>\n>    | x |\n>    |---|\n",
    "- | a | b |\n  |---|---|\n  | 1 | 2 |\n",
    "1. 手順\n\n   <table>\n   <tr><td>\n\n   - x\n\n   </td></tr>\n   </table>\n2. 次\n",
    "<table>\n<tr>\n<td>\n\n- a\n- b\n\n</td>\n</tr>\n</table>\n",
    "| a \\\\| b |\n|---|---|\n",
    "[^1]: 注\n\n    | x |\n    |---|\n",
]


def corpus_files():
    files = []
    for path in glob.glob(os.path.join(CORPUS, "**", "*.md"), recursive=True):
        parts = set(os.path.normpath(path).split(os.sep))
        if ".git" in parts or ".claude" in parts or "node_modules" in parts:
            continue
        files.append(path)
    return sorted(files)


def comrak_nodes(texts):
    result = subprocess.run(
        ["node", os.path.join(ROOT, "tests", "comrak_ast.mjs")],
        input=json.dumps(texts, ensure_ascii=False).encode("utf-8"),
        capture_output=True, env=dict(os.environ))
    if result.returncode != 0:
        raise RuntimeError(result.stderr.decode("utf-8", "replace"))
    return json.loads(result.stdout.decode("utf-8"))


def comrak_signature(nodes):
    signature = Counter()
    for node in nodes:
        kind = TYPES.get(node["type"])
        if node["type"] == "TableRow":
            signature[("row", node["start"] - 1)] += 1
        if kind not in LEAVES:
            continue
        chain = []
        parent = node["parent"]
        while parent is not None:
            if TYPES.get(nodes[parent]["type"]):
                chain.append(TYPES[nodes[parent]["type"]])
            parent = nodes[parent]["parent"]
        signature[(kind, node["start"] - 1, node["end"] - 1, tuple(chain))] += 1
    return signature


def own_signature(text):
    signature = Counter()
    doc = blocks.parse_text(text, front_matter=False)
    for node in doc.root.walk():
        if node.kind not in LEAVES:
            continue
        chain = tuple(a.kind for a in node.ancestors() if a.kind != blocks.DOCUMENT)
        signature[(node.kind, node.start_line, node.end_line, chain)] += 1
        if node.kind == blocks.TABLE:
            signature[("row", node.info["header"][0])] += 1
            for line_no, _offset in node.info["rows"]:
                signature[("row", line_no)] += 1
    return signature


@unittest.skipUnless(COMRAK and shutil.which("node"), "MDTABLE_COMRAK_DIR と node が要る")
class ComrakTest(unittest.TestCase):
    def check(self, texts, names):
        for text, name, nodes in zip(texts, names, comrak_nodes(texts)):
            with self.subTest(name=name):
                self.assertEqual(own_signature(text), comrak_signature(nodes))

    def test_samples(self):
        self.check(SAMPLES, [repr(s)[:60] for s in SAMPLES])

    @unittest.skipUnless(CORPUS, "MDTABLE_CORPUS が要る")
    def test_corpus(self):
        files = corpus_files()
        for index in range(0, len(files), 20):
            chunk = files[index:index + 20]
            texts = []
            for path in chunk:
                with open(path, "rb") as handle:
                    data = handle.read().decode("utf-8")
                texts.append(data[1:] if data.startswith("﻿") else data)
            self.check(texts, chunk)


@unittest.skipUnless(CORPUS, "MDTABLE_CORPUS が要る")
class CorpusRoundTripTest(unittest.TestCase):
    def test_no_edit_round_trip(self):
        work = tempfile.mkdtemp(prefix="mdtable-corpus-")
        try:
            for path in corpus_files():
                with self.subTest(path=path):
                    copy = os.path.join(work, "doc.md")
                    shutil.copyfile(path, copy)
                    xlsx = os.path.join(work, "doc.tables.xlsx")
                    try:
                        sync.export(copy, xlsx, force=True)
                    except sync.MdTableError:
                        continue
                    report = sync.import_workbook(xlsx, copy, dry_run=True)
                    self.assertEqual(report.errors, [])
                    self.assertFalse(report.changed, report.diff)
        finally:
            shutil.rmtree(work, ignore_errors=True)


if __name__ == "__main__":
    unittest.main()
