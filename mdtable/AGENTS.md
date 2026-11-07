# AGENTS.md

このリポジトリで作業するコーディングエージェント（Claude Code・Codex・Grok Build・Antigravity CLI）への指示。Claude Code は CLAUDE.md の `@AGENTS.md` で、Codex・Grok Build・Antigravity CLI はこのファイルを直接読む。

GLFM の文書にある表（パイプ表・HTML の表）を Excel で編集して書き戻す `mdtable` コマンド。セルの書き方と書き戻しの規則は [README.md](README.md)、設計の理由と変換の規則は [docs/reference/readme.md](docs/reference/readme.md)、検証記録は [非公開側の readme.md](https://github.com/ryo-aoki-pc/setup-notes-verification/blob/main/repos/md-table-excel/docs/verification/readme.md)。

## 導入手順の正本

導入・更新・使い方の手順は、setup-notes の [almalinux-setup.md](https://github.com/ryo-aoki-pc/setup-notes/blob/main/docs/almalinux-setup.md#mdtable) と [windows-setup.md](https://github.com/ryo-aoki-pc/setup-notes/blob/main/docs/windows-setup.md#mdtable) の「mdtable」とその子の項にある。このリポジトリには置かない。macOS の導入（README の 1 行）は、このリポジトリに置く。

## 構成

- `mdtable/blocks.py` — comrak（GitLab の解析器）と同じ規則のブロックの解析。行ごとのコンテナの接頭辞の位置も持つ
- `mdtable/inline.py` — コードスパン・生の HTML・数式などのひとかたまりの走査、行頭のエスケープ、強調が成り立つかの判定
- `mdtable/document.py` — 文書の読み書き（改行コード・BOM を保つ）、表の検出と番号、置き換え
- `mdtable/pipe_table.py` — パイプ表の解析・セルの変換・書き戻し（行とセルの再利用）
- `mdtable/html_table.py`・`htmltok.py`・`cell_from_html.py` — HTML の表の範囲・字句解析・格子・セルの変換・差し替えと作り直し
- `mdtable/cell_syntax.py` — Excel のセルの Markdown をパイプ表のセル・HTML の表のセルにする（とその逆）
- `mdtable/workbook.py` — ブックの書き出しと読み込み。`mdtable/sync.py` — 書き出し・取り込みの手順と、書き戻した結果の確かめ
- `mdtable/model.py` — 表のモデル（Excel と Markdown の間で受け渡す形）と、セルの文字の正規化。`mdtable/textwidth.py` — 文字の表示幅
- `mdtable/cli.py` — コマンド（list / export / import / edit）。`mdtable/__main__.py` は `python -m mdtable`、`mdtable/__init__.py` は版（`__version__`）
- `scripts/setup-mdtable.py` — リポジトリ外の仮想環境への導入。`pyproject.toml` — パッケージの定義（`readme = "README.md"` なので、README はパッケージの説明にもなる）
- `tests/` — unittest。`test_oracles.py` は環境変数を設定したときだけ、手元の文書と comrak で確かめる
- `docs/reference/readme.md` — 設計と変換の規則
- 非公開側の検証記録の `readme.md` — 検証記録と、Excel の実機での確認の手順
- `docs/verification/` — `multi-tables.md`（`tests/test_workbook.py` が読む）と `excel-check.md`（Excel の確認に使う）の見本の文書だけを置く

## テスト

```sh
python3 -B -m unittest discover -s tests -v
```

- Windows では `python`。openpyxl のある Python で、リポジトリの直下で動かす
- comrak と突き合わせるときは `MDTABLE_COMRAK_DIR` に `node_modules/comrak` を持つフォルダー（Markdown の整形器 [md-format](https://github.com/ryo-aoki-pc/md-format) を導入した導入先。`%LOCALAPPDATA%\md-format`・`${XDG_DATA_HOME:-~/.local/share}/md-format` など）を、手元の文書で無編集の往復を確かめるときは `MDTABLE_CORPUS` に文書のフォルダーを設定する

## 書き方の規則

- コードのコメントと利用者に出すメッセージは日本語。型ヒントは付けない。Python 3.9 で動く書き方にする（`match`・`X | Y`・`zip(strict=)` などは使わない）
- エラーは日本語 1 行で出し、Traceback は `MDTABLE_DEBUG=1` のときだけ出す
- 表の検出やブロックの規則を変えたら、`test_oracles.py` で comrak と突き合わせる。GitLab の描画に関わる変更は、GitLab の Markdown API（`/api/v4/markdown`）でも確かめ、検証記録に残す
- 変えていない表・行・セルを 1 文字も変えないことを崩さない（`tests/test_tables.py`・`test_workbook.py` の往復のテスト）
- ソースは LF。Windows の Python でファイルを書き換えるときは `newline=""` か `newline="\n"` を付ける
- 文書（README・docs）は日本語。検証していないことを「動く」と書かない

## 文書の置き場所

- 導入・利用・更新の手順は正本に置き、README の「導入」の表から正本の項へ直接リンクする（手順書の先頭やカテゴリの見出しへはリンクしない）。執筆規則は setup-notes の `docs/dev/writing-rules.md`
- `docs/extra/` は注意点。ロールバック（取り消し）の手順は書かない。`docs/reference/` は背景と規則。検証記録（環境・日付・対象コミット・実出力・未確認事項）は、非公開の `ryo-aoki-pc/setup-notes-verification` の `repos/md-table-excel/docs/verification/` に置く。テストのコマンドは上の「テスト」
- 検証記録を書く・直すときは、非公開の `setup-notes-verification` を clone し、そちらへ Pull Request を作る
- 文書を移す・分ける・まとめるときは、移した経緯と、旧ファイル名・旧見出しとの対応を文書に書かない（経緯は git の履歴と Pull Request の本文に残す）。リンクを今の場所へ直す。中身が新しい場所への案内だけになったファイルは消す
- 残す検証記録の実施日・対象版・環境・出力・成功と失敗・未実施の範囲・記録したときの手順番号は書き換えない。再検証せずに検証範囲を広げない。記録の手順番号が今の手順と違い、本文を読むのに要るときだけ「記録の手順 | 今の手順」の表で結ぶ
- 導入先（仮想環境・起動コマンドの場所）を変えたら、正本の「mdtable を導入・更新する」を合わせて直す
