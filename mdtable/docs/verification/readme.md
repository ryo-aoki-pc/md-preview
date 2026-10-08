# 検証記録

[README](../../README.md) / [設計と変換の規則](../reference/readme.md)

## 最初の版の検証（2026-10-09）

対象はコミット `8a0ef42`（mdtable 0.1.0）。

### 環境

- Windows 11 Pro 10.0.26300、scoop の Python 3.14.8、openpyxl 3.1.5、Node.js 26.10.0
- comrak 0.48.0-rc.0 と markdownlint-cli2 0.23.3（markdownlint 0.41.1）は、Neovim の `glfm-format`（`%LOCALAPPDATA%\nvim-data\glfm-format`）に入っていたもの
- GitLab の描画は gitlab.com（19.5.0-pre）の Markdown API（`POST /api/v4/markdown`、`gfm: true`）。トークンは環境変数 `GITLAB_TOKEN` から読み、送ったのは下の見本の文書だけ
- WSL の AlmaLinux 10（Python 3.12.13、openpyxl 3.1.5）と、podman の `python:3.9-slim`（Python 3.9.25、openpyxl 3.1.5）

### 自動テスト

```powershell
$env:MDTABLE_COMRAK_DIR = "$env:LOCALAPPDATA\nvim-data\glfm-format"
$env:MDTABLE_CORPUS = "$HOME\docs"
python -B -m unittest discover -s tests -v
```

- Windows: 112 件すべて成功（comrak と手元の文書の確認を含む）
- WSL の AlmaLinux 10・Python 3.9.25: 環境変数を付けずに 112 件が成功（comrak と手元の文書の確認の 3 件は飛ばす）

### 手元の文書（docs 配下の 217 本・608 表）

- 表の検出: 葉のブロック（段落・見出し・コード・HTML ブロック・区切り線・表）の種類・行・祖先と、表の行が、全 217 本で comrak の構文木と一致した。決めた規則はこの突き合わせで確かめた（例: `| a \\| b |` は comrak では区切られない）
- 書き出して無編集で取り込むと、全 608 表で変更なしになった
- 12,118 個のセルを、元の文字を使わずに Excel の文字から書き直しても、元のセルの文字と 1 文字も違わなかった。行頭の `2.`（後ろに空白）などは Excel では `2\.` と出し、パイプ表に書き戻すときに外す
- 見出しより多いセル（コードスパンの中の `|` で列が割れている 2 行）は警告を出した

### GitLab での描画

見本の文書を書き出し、openpyxl でセルを編集（Excel で入力したのと同じく文字列として）して書き戻し、GitLab の Markdown API で描画した HTML を確かめた。

| 確かめたこと | 結果 |
| --- | --- |
| パイプ表のセルの箇条書き・番号付きリスト・説明リスト | `<ul>`・`<ol>`・`<dl>` になる |
| パイプ表のセルの改行・`\|` | `<br>` になる。コードの中と外の `\|` はどちらも `\|` で表示される |
| HTML の表のセルの Markdown の部分 | 説明リスト（`<dl>`）・タスク（チェックボックス）・入れ子のリスト・番号付きリスト（`start="3"`）になる |
| セル結合と配置 | `rowspan`・`colspan`・`align` が残る |
| セルのコードブロック | ` ```python `・` ```yaml `・` ```sh ` がシンタックスハイライトされる（`language-python` などとトークンの `span`）。元の HTML の `<pre><code class="language-python">` と `<pre lang="yaml">` もハイライトされる |
| パイプ表からの HTML の表への変換 | 結合は `rowspan`、列の配置は `align` になる。直後のリストは空行を足してリストのまま |
| 字下げされた表 | 番号付きリスト・説明・アラートの中の表が、その中のまま描画される |

### markdownlint

書き戻した見本の文書に `markdownlint-cli2 --fix` を当てても、ツールが書いた行は変わらなかった（変わったのは、元の文書にあった `|---|` の区切り行と、見本に元から無かった空行だけ）。

### 導入スクリプト

`scripts/setup-mdtable.py` の `--install-dir` と `--bin-dir` に一時フォルダーの中を指定して実行した。

- 仮想環境を作って入れ、置いた `mdtable.exe --version` が `mdtable 0.1.0` を出した。リポジトリにビルドのファイルは残らなかった
- `--check` は、`--bin-dir` が `PATH` に無いことを問題として出した（終了コード 1）
- 既定の置き場所（`%LOCALAPPDATA%\md-table-excel` と `%USERPROFILE%\.local\bin`）への導入は、この検証ではしていない

### 未検証

- Excel 実機（この PC に Excel が無い）。下の手順で確かめる
- `edit` で実際に Excel を開く流れ（テストでは開く処理を置き換えて確かめた）
- LibreOffice・Google スプレッドシート・Excel for Mac、macOS、Raspberry Pi

## Excel 実機での確認の手順

Excel のある PC で行い、結果をこの記録に新しい節として足す。保存したブックは、後でテストの材料にする。

1. 確認用の文書の写しを書き出す。

   ```powershell
   Copy-Item docs\verification\excel-check.md $env:TEMP\excel-check.md
   mdtable export $env:TEMP\excel-check.md
   Start-Process $env:TEMP\excel-check.tables.xlsx
   ```

   - 開けること、保護ビューなら [編集を有効にする] で編集できること
   - 「目次」のシート名のリンクで各シートへ移れること
   - 複数行のセル（`表1` の 2 行目など）で、行の高さが自動で合っていること
   - 見出しセルが太字・灰色、HTML の表の結合（`表2` の A1:A2・B1:C1・A3:A4）が結合セルになっていること

2. 次の編集をして保存し、Excel を閉じる。

   - `表1` の 5 行目（表の下）に、A5 `1/2`・B5 `- 項目`・C5 `=1+1` と入力する（文字列のまま入ること）
   - `表1` の 2 行目と 3 行目の間に行を挿入する（見出しの書式が写るか、写らないかを見る）
   - `表2` のシート名を変える
   - `表3` の B2 に、Alt+Enter で 2 行目を足す

3. 取り込んで差分を見る。

   ```powershell
   mdtable import $env:TEMP\excel-check.tables.xlsx --dry-run
   ```

   - 入力したとおりの文字（`1/2`・`- 項目`・`=1+1`）が書き戻されること
   - 名前を変えた `表2` も対応が取れていること
   - 閉じるときに Excel が保存を聞いたか（変更しなくても聞く想定）
