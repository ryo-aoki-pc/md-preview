# 検証記録

[README](../../README.md) / [設計と変換の規則](../reference/readme.md)

## 最初の版の検証（2026-10-09）

対象はコミット `8a0ef42`（mdtable 0.1.0）。

### 環境

- Windows 11 Pro 10.0.26300、scoop の Python 3.14.8、openpyxl 3.1.5、Node.js 26.10.0
- comrak 0.48.0-rc.0 と markdownlint-cli2 0.23.3（markdownlint 0.41.1）は、Neovim の `glfm-format`（`%LOCALAPPDATA%\nvim-data\glfm-format`）に入っていたもの
- GitLab の描画は gitlab.com（19.5.0-pre）の Markdown API（`POST /api/v4/markdown`、`gfm: true`）。トークンは環境変数 `GITLAB_TOKEN` から読み、送ったのは下の見本の文書だけ
- WSL の AlmaLinux 10（Python 3.12.13、openpyxl 3.1.5）と、podman の `python:3.9-slim`（Python 3.9.25、openpyxl 3.1.5）

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

## ロールバックの検証（2026-10-15）

[ロールバック](../extra/readme.md)の手順を、文書のコードブロックをそのまま取り出して流し、入れたものが消えるかを確かめた。対象は、この記録を足したコミットの `docs/extra/readme.md` と、mdtable 0.1.0 の `scripts/setup-mdtable.py`。

### ロールバックを確かめた環境

- WSL の AlmaLinux 10（Python 3.12.13）。使い捨ての `HOME`（`/tmp` の下）で、`XDG_DATA_HOME` を外し、`PATH` は `~/.local/bin` と OS の既定だけにした
- Windows 11 Pro 10.0.26300 の Windows PowerShell 5.1（scoop の Python 3.14.8）。使い捨ての `USERPROFILE`・`LOCALAPPDATA` を設定したプロセスで流した

### ロールバックの結果

- どちらも、clone → `setup-mdtable.py` → `--check`（`導入済み: mdtable 0.1.0`）の後に、ロールバックの手順で、起動コマンド（AlmaLinux 10 はシンボリックリンクの `~/.local/bin/mdtable`、Windows は写しの `mdtable.exe`）・仮想環境（`~/.local/share/md-table-excel`・`%LOCALAPPDATA%\md-table-excel`）・clone が消えた
- AlmaLinux 10 では、`command -v` が何も出さず、`ls -d` が 3 つとも `No such file or directory` を出した。Windows では、`Test-Path` が手順 2 で `False` を 2 つ、手順 4 で `False` を 1 つ出した
- 残ったのは、空の `~/.local/bin`・`~/.local/share` と、pip のキャッシュ（AlmaLinux 10 の `~/.cache/pip`）
- レビューの指摘で、仮想環境の場所の決め方（`XDG_DATA_HOME` が絶対パスのときだけ使う）と、clone の確かめ（どのブランチにも push していないコミットと stash も見る）・確認の表示を直した後も、同じ方法で流し直し、同じ結果だった
- Windows の手順 2 の `Get-Command` は、試したプロセスの PATH に、この PC に入っている本物の `~/.local/bin` も入っていたので、そちらの `mdtable.exe` を出した。使い捨ての場所の起動コマンドが消えたことは、`Test-Path` で確かめた

### ロールバックで未検証のこと

- macOS
- mdtable や、それが開いた Excel のブックが動いている間に消すとき（Windows でファイルが使用中になる場合）
- 使い捨てではない、本物のホームでの実行

## comrak の場所を md-format の導入先にした確認（2026-10-15）

Neovim の Markdown の整形器が、LazyVimStarter の `glfm-format`（`%LOCALAPPDATA%\nvim-data\glfm-format`）から、
別のリポジトリの [md-format](https://github.com/ryo-aoki-pc/md-format) に移った。comrak の場所の案内を md-format の導入先にしたので、
そこにある comrak で突き合わせた。

### 環境と対象

- 対象は mdtable のコミット `a7e0164`（この節を足す前の `main`）。2026-10-16 に、`main` の `b60ac1e` に載せ直した版でやり直し、
  結果は下と同じだった（そのときの Python は scoop の Python 3.14.8 と openpyxl 3.1.5）
- Windows 11 Pro 10.0.26300、mdtable の導入先の仮想環境の Python 3.14.8（openpyxl 入り）、Node.js 26.10.0
- comrak 0.48.0-rc.0（md-format の導入先。確かめたときは、md-format の作業中のブランチを一時のフォルダーに導入したもの）
- 手元の文書は、docs のサブモジュール 9 つの `*.md` 224 本と、setup-notes の `almalinux-setup.md` の 15,092 行の版（コミット `89f9712`）

### 結果

- `MDTABLE_COMRAK_DIR` だけを設定した自動テストは、120 件のうち 118 件が成功し、2 件を飛ばした（手元の文書の確認）
- `MDTABLE_CORPUS` も設定すると、120 件のうち 1 件が失敗した。`test_oracles.ComrakTest.test_corpus` で、setup-notes-verification の
  `docs/verification/shell-terminal.md` の区切り線の行の範囲が comrak と違った（mdtable は 2823 行、comrak は 2823〜2824 行）
- 同じ失敗は、comrak を前の場所（`nvim-data\glfm-format`、同じ 0.48.0-rc.0）にしても出た。comrak の場所を変えたことではなく、
  その文書に対する mdtable のブロックの解析の食い違い（この確認では直していない）

### この確認で未検証のこと

- 上の `shell-terminal.md` の食い違いの原因と、表の書き戻しへの影響
- AlmaLinux 10 での突き合わせ

## comrak との突き合わせの区切り線の食い違いを直した確認（2026-10-16）

上の節で未検証に残した、`shell-terminal.md` の `test_corpus` の失敗の原因を調べ、試験の比べ方を直した。

### 原因

その文書は「`---`（区切り線）、空行、文書の終わり」で終わる。上の節の行の番号は 0 から数えたもので、1 から数えると区切り線は 2824 行。
comrak 0.48.0-rc.0 の `ThematicBreak` の行を、最小の文書で確かめた（1 から数える）:

| 文書 | comrak の区切り線 |
|---|---|
| `a⏎⏎---⏎` | 3〜3 |
| `a⏎⏎---⏎⏎`・`a⏎⏎***⏎⏎`・`a⏎⏎___⏎⏎` | 3〜4 |
| `a⏎⏎---⏎⏎⏎` | 3〜5 |
| `> ---⏎>⏎` | 1〜2 |
| `- ---⏎⏎`（これも区切り線） | 1〜2 |
| `a⏎⏎---⏎⏎b⏎⏎---⏎⏎` | 1 つめは 3〜3、文書の終わりの 2 つめは 7〜8 |
| `a⏎⏎---⏎⏎<!-- c -->⏎⏎`・`a⏎⏎---⏎⏎[x]: /u⏎⏎` | 3〜3 |
| `- a⏎⏎  ---⏎⏎`（リスト項目の中） | 3〜3 |

- 確かめた範囲では、comrak は、文書の直下か引用の中の最後のブロックが区切り線で、後ろに空行（引用の中では `>` だけの行）しか無いときに、
  区切り線の終わりを文書の終わりの空行まで延ばした。リスト項目の中の区切り線と、見出し・コード・HTML のブロック・表・段落は、同じ形でも延ばさなかった
- CommonMark の区切り線は、いつも 1 行。mdtable は、上のどの形でも 1 行と読んだ。食い違いは comrak の位置の付け方の癖と判断した。
  描画に関わらないのは、位置（sourcepos）が描画の HTML に出ないという仕様からの判断で、描画の API では見ていない
- 表の書き戻しへの影響は無い。この直しは mdtable の解析を変えず、表の見分けは区切り線の終わりの行を使わない。同じ文書の無編集の往復（`CorpusRoundTripTest`）も、
  直す前から変更なしだった

### 直し

- 試験の comrak の側の署名で、区切り線の終わりの行を始まりの行にそろえた（下書きは Codex）
- 試験の例に、文書の終わりの区切り線（延びが 1 行・2 行、`***`、引用の中、`- ---`、2 つめだけが延びる文書）と、延ばさない形（リスト項目の中、setext の見出しの下線）を足した。
  延びる形の例は、直しを戻すと落ちた

### 結果

- Windows 11 Pro 10.0.26300（scoop の Python 3.14.8・openpyxl 3.1.5・Node.js 26.10.0。comrak は上の節と同じ）で、`MDTABLE_COMRAK_DIR` と `MDTABLE_CORPUS` を
  設定して 120 件すべてが成功した
- AlmaLinux 10.2（abiko-pi、aarch64。Python 3.14.8 の使い捨ての仮想環境に openpyxl 3.1.5、Node.js 22.23.2、md-format の作業中のブランチを一時のフォルダーに
  導入した comrak 0.48.0-rc.0、同じ文書の写し）でも、両方を設定して 120 件のうち 119 件が成功し、1 件（Windows のフォントの登録を見る試験）を飛ばした

### この確認で未検証のこと

- comrak のほかの版と、GitHub・GitLab の解析器が、区切り線の位置をどう付けるか
- 脚注の定義・説明リストの中の、文書の終わりの区切り線

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
