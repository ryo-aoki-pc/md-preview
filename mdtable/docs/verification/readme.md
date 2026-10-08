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

## Excel 実機での確認（2026-10-09）

対象はコミット `1154f3c`（mdtable 0.1.0）。HackGen Console NF をフォントにした版で、下の「Excel 実機での確認の手順」を Excel で行った。

### Excel で確かめた環境

- Windows 11 Pro 10.0.26300、Office Home & Business 2021（クイック実行）の Excel 16.0.20430.20146、HackGen Console NF（Regular と Bold が `HKLM` に登録済み）
- mdtable は `scripts/setup-mdtable.py` で既定の場所に導入したもの
- Excel は COM（`Excel.Application`）で画面に出さずに動かし、セルへの入力・行の挿入・シート名の変更・結合の解除・保存をした。見た目は Excel から PDF に書き出して確かめた。`edit` だけは利用者の PowerShell から動かし、開いた Excel の窓を COM で操作した
- Claude のデスクトップアプリから確かめた。アプリの Bash から導入すると仮想環境がアプリ専用の場所（MSIX の仮想化）に作られ、利用者の PowerShell の `mdtable` が何も出さずに終了コード 1 で終わったので、PowerShell から導入し直した。アプリの中から `os.startfile` しても Excel は起動しなかった

### 結果

| 確かめたこと | 結果 |
| --- | --- |
| 開く | 保護ビューにならない。開いた直後は保存済みの状態（変えずに閉じても保存を聞かない） |
| フォント | 表のシートと目次のセルが HackGen Console NF。PDF でも `%LOCALAPPDATA%\lazygit` の `\` が `\` で表示される |
| 目次のリンク | 3 つのリンクとも、そのシートの表の範囲が選ばれる |
| 隠しシートと名前の定義 | `_mdtable` は veryHidden のまま保存される。名前の定義は、シート名の変更（`mdtable_2`）と行の挿入（`mdtable_1` が `A1:C5` に広がる）に追従する |
| 行の高さ | 結合の無い行は Excel が合わせる（1 行 13.3pt、2 行 26.6pt。BIZ UDゴシックでは 13.5pt と 25.5pt）。結合を含む行は見積もり（1 行 15pt）のままで、少し高いが文字は切れない |
| 文字列のまま入るか | 表の下の行の `1/2`・`- 項目`・`=1+1`、結合を解いたセルの `1/2` は文字列のまま。挿入した行は上の行の書式（文字列・本文のフォント）になる |
| Excel が保存した形 | 共有文字列、セル内の改行は CRLF、日本語を含む文字列に `phoneticPr`。何も変えずに保存したブックを取り込むと、全表が変更なし |
| 取り込み | 入力した文字のまま書き戻した（`- 項目` はセルの書き方どおり箇条書き）。名前を変えた `表2` も対応が取れ、結合を解いた表は作り直して変えた行だけが変わった。もう一度取り込むと全表が「適用済み」 |
| `edit` | Excel が開き（保護ビューにならない）、セルを直して保存して閉じてから Enter を押すと、変えた行だけを書き戻した。一時フォルダーは消え、Excel も終わった |

### 見つけて直したこと

- 結合で隠れるセルが標準の書式と ＭＳ Ｐゴシック になっていた（openpyxl は結合すると隠れるセルの書式を捨てる）。結合を解いて入力すると `1/2` が日付になるところだった。結合してから書式を付けるようにした
- 表の下の行に入力したセルに列の配置が付かず、パイプ表で「配置が混在」の警告が出た。列のスタイルにも列の配置を付けた
- 取り込んだブックをもう一度取り込むと、列の配置をそろえた表が衝突になった。書き戻した結果が今の行と同じなら「適用済み」にした（セルの文字を収束する形に直した表も同じ）
- 目次の A1 の「Markdown」が切れていた。A 列を広げた

### 直した後の自動テスト

- Windows（Python 3.14.8、comrak と手元の文書を含めて）: 119 件すべて成功
- WSL の AlmaLinux 10（Python 3.12.13）と podman の `python:3.9-slim`（Python 3.9.25）: 119 件が成功（comrak と手元の文書の 3 件と、Windows のフォントの登録の 1 件は飛ばす）

### Excel での確認で未検証のこと

- キー入力に固有の動き（オートコレクト・IME・[挿入オプション]・貼り付け）。セルへの入力は COM で値を入れた
- Excel の画面での表示（行の高さ・列幅などは COM で読んだ値と PDF で確かめた）
- Excel for the web・LibreOffice・Google スプレッドシート・Excel for Mac、macOS、Raspberry Pi

## 本文と複数の表がある文書の GitLab での描画（2026-10-09）

対象はコミット `1154f3c`（mdtable 0.1.0）。本文と表がいくつもある文書で、一部の表を Excel で編集して取り込んでも、ほかの部分の GitLab での表示が変わらないことを確かめた。

### 確かめ方

- 文書を書き出し、上の節と同じく Excel（COM）で表を編集して保存し、取り込んだ。もう一度取り込んで「適用済み」になることも見た
- 編集の前と後の文書を、gitlab.com（19.5.0-pre）の Markdown API で描画した。描画した HTML の一番外側の表をそれぞれ印に置き換え、`data-sourcepos` と脚注の番号（描画ごとに変わる）を除いて、表の外と表ごとに前後を比べた
- 見た目は、描画した HTML に GitLab の `.md` の規則（段落とセルの余白）を当てて画像にした

### 描画の比較の結果

| 文書 | 編集 | 結果 |
| --- | --- | --- |
| 見本 [`multi-tables.md`](multi-tables.md)（見出し・段落・番号付きリスト・箇条書き・コード・アラート・説明リスト・脚注と 7 つの表） | 表 1〜5（行の追加とセルの箇条書き、番号付きリストの中の表のセル内の改行、直後に空行の無いリストが続く表へのコードブロック、HTML の表へのコードブロックと説明リスト、アラートの中の表の太字） | 表の外の描画は同じ。表の数は 7 のままで、変わったのは表 1〜5 だけ。コードブロックで HTML の表にした表の直後のリストは、空行を足してリストのまま |
| `wezterm/README.md`（966 行・26 表） | 5 表（セル内の改行と行の挿入、箇条書きの中の表のセルへの箇条書き、結合で HTML の表に、コードブロックで HTML の表に、文書の最後の表） | 編集していない 21 表は変更なし。Markdown の差分は 5 表の中だけ。表の外の描画は同じで、変わったのは 5 表だけ |

- 編集した表は、セルの箇条書き・改行・説明リスト・コードブロック（`sh` と `html` のシンタックスハイライト）・結合（`rowspan`）・列の配置が描画された
- HTML の表で Markdown として書いたセル（コードスパンやリンクのあるセル）は `<p>` に入る。GitLab の CSS では段落の上の余白は 0 なのでほかのセルと上がそろい、下に 16px の余白が付く分だけ、その行が少し高くなる
- 見本の取り込みは、自動テスト（`tests/test_workbook.py` の `test_many_tables_with_body`）でも確かめる。このテストを足した 120 件が、Windows（Python 3.14.8、comrak と手元の文書を含む）、WSL の AlmaLinux 10（Python 3.12.13）、Python 3.9.25 で成功した（Windows 以外は 4 件を飛ばす）

## Excel 実機での確認の手順

Excel のある PC で行い、結果をこの記録に新しい節として足す。Excel が保存したブックの形（共有文字列など）が、テストの `excel_shape`（Excel の保存の形の模擬）と違えば、模擬とテストを直す。ブックそのものは作成者名などが入るので、リポジトリに入れない。

1. 確認用の文書の写しを書き出す。

   ```powershell
   Copy-Item docs\verification\excel-check.md $env:TEMP\excel-check.md
   mdtable export $env:TEMP\excel-check.md
   Start-Process $env:TEMP\excel-check.tables.xlsx
   ```

   - 開けること、保護ビューなら [編集を有効にする] で編集できること
   - フォントが HackGen Console NF（入っていれば）で、`表1` の `%LOCALAPPDATA%\lazygit` の `\` が `¥` でなく `\` に見えること
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

   - 入力したとおりの文字（`1/2`・`=1+1`）が書き戻されること。`- 項目` はセルの書き方どおり箇条書き（`<ul><li>項目</li></ul>`）になる
   - 名前を変えた `表2` も対応が取れていること
   - 閉じるときに Excel が保存を聞いたか
