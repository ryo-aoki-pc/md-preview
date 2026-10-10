# AGENTS.md

このリポジトリで作業するコーディングエージェント（Claude Code・Codex・Grok Build）への指示。Claude Code は CLAUDE.md の `@AGENTS.md` で、Codex と Grok Build はこのファイルを直接読む。

利用者への回答・質問・報告は、常に日本語で書く（コードのコメントなどの言語は、このファイルのほかの決まりに従う）。

GLFM の文書にある表（パイプ表・HTML の表）を Excel で編集して書き戻す `mdtable` コマンド。使い方は [README.md](README.md)、設計の理由と変換の規則は [docs/reference/readme.md](docs/reference/readme.md)、検証記録は [docs/verification/readme.md](docs/verification/readme.md)。

## 構成

- `mdtable/blocks.py` — comrak（GitLab の解析器）と同じ規則のブロックの解析。行ごとのコンテナの接頭辞の位置も持つ
- `mdtable/inline.py` — コードスパン・生の HTML・数式などのひとかたまりの走査、行頭のエスケープ、強調が成り立つかの判定
- `mdtable/document.py` — 文書の読み書き（改行コード・BOM を保つ）、表の検出と番号、置き換え
- `mdtable/pipe_table.py` — パイプ表の解析・セルの変換・書き戻し（行とセルの再利用）
- `mdtable/html_table.py`・`htmltok.py`・`cell_from_html.py` — HTML の表の範囲・字句解析・格子・セルの変換・差し替えと作り直し
- `mdtable/cell_syntax.py` — Excel のセルの Markdown をパイプ表のセル・HTML の表のセルにする（とその逆）
- `mdtable/workbook.py` — ブックの書き出しと読み込み。`mdtable/sync.py` — 書き出し・取り込みの手順と、書き戻した結果の確かめ
- `mdtable/cli.py` — コマンド（list / export / import / edit）
- `scripts/setup-mdtable.py` — リポジトリ外の仮想環境への導入
- `tests/` — unittest。`test_oracles.py` は環境変数を設定したときだけ、手元の文書と comrak で確かめる

## テスト

```sh
python3 -B -m unittest discover -s tests -v
```

- Windows では `python`。openpyxl のある Python で、リポジトリの直下で動かす
- comrak と突き合わせるときは `MDTABLE_COMRAK_DIR` に comrak の npm パッケージのあるフォルダー（Neovim の `glfm-format` など）を、手元の文書で無編集の往復を確かめるときは `MDTABLE_CORPUS` に文書のフォルダーを設定する

## 書き方の規則

- コードのコメントと利用者に出すメッセージは日本語。型ヒントは付けない。Python 3.9 で動く書き方にする（`match`・`X | Y`・`zip(strict=)` などは使わない）
- エラーは日本語 1 行で出し、Traceback は `MDTABLE_DEBUG=1` のときだけ出す
- 表の検出やブロックの規則を変えたら、`test_oracles.py` で comrak と突き合わせる。GitLab の描画に関わる変更は、GitLab の Markdown API（`/api/v4/markdown`）でも確かめ、検証記録に残す
- 変えていない表・行・セルを 1 文字も変えないことを崩さない（`tests/test_tables.py`・`test_workbook.py` の往復のテスト）
- ソースは LF。Windows の Python でファイルを書き換えるときは `newline=""` か `newline="\n"` を付ける
- 文書（README・docs）は日本語。検証していないことを「動く」と書かない。検証記録の過去の節は書き換えず、新しい節を足す

## 共同作業の規則

このリポジトリでは、Claude Code・Codex・Grok Build が同じ規則で作業する。分担と `main` への取り込みは人が決める。

- 起動された worktree（作業ディレクトリ）の中だけでファイルを変える。ほかの worktree のファイルは変えない
- 今のブランチにだけコミットする。`main` にはコミットも push もしない
- 頼まれた範囲のファイルだけを変える。範囲の外を変えるときは、変える前に理由を書いて確かめる
- 終わったら、テストとリンターを通してから、目的ごとにコミットする。通らなければコミットせずに、結果を報告する
- コミットしたら、今のブランチを push し、`main` への Pull Request を作る（既にあれば足す）。`main` への取り込み（マージ）とブランチの削除は人が行う。今のブランチに `main` を取り込むのは、頼まれたときと、Pull Request が競合したときだけ
- 秘密情報（`.env`・鍵・トークン・パスワード）を読まない・書かない・出力しない
- レビューを頼まれたら、ファイルを変えずに、指摘を「重大度・場所（ファイル:行）・理由・直し方」で挙げる
- ほかの担当の変更は、`git diff main...agent/codex` のように git で読む（ほかの worktree へ移らない）
