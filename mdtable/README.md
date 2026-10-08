# md-table-excel

GLFM（GitLab Flavored Markdown）の文書にある表を Excel で編集して、元の文書に書き戻すツール（`mdtable` コマンド）です。Markdown の表（パイプ表）と HTML の表のどちらも扱い、セルの中は Excel 上でも Markdown の記法で書けます。

[設計と変換の規則](docs/reference/readme.md) / [検証記録](docs/verification/readme.md)

## できること

- 文書の表を 1 つのブックに書き出し（表ごとに 1 シートと目次）、Excel で編集したら書き戻す。変えていない表・行・セルは 1 文字も変えない
- HTML の表のセル結合（`rowspan` / `colspan`）は、Excel の結合セルになる
- セルの中に、箇条書き・番号付きリスト・説明リスト・コードブロック（言語名を書けば GitLab でシンタックスハイライトされる）を Markdown の記法で書ける
- 説明リスト・番号付きリスト・箇条書き・引用（アラート）・脚注の中で字下げされた表も扱える
- Markdown の表でセルを結合したり、セルにコードブロックを書いたりすると、HTML の表にして書き戻す

## 導入

Python 3.9 以降が要ります。Windows は `scoop install python` で入ります。このリポジトリのフォルダーで実行します。

Windows（PowerShell）:

```powershell
python scripts/setup-mdtable.py
```

Linux / macOS:

```sh
python3 scripts/setup-mdtable.py
```

- リポジトリの外に専用の仮想環境を作り、mdtable と [openpyxl](pyproject.toml) を入れる。置き場所は Windows が `%LOCALAPPDATA%\md-table-excel`、Linux が `~/.local/share/md-table-excel`（`--install-dir` で変えられる）
- 起動用のコマンド `mdtable` を、Windows は `%USERPROFILE%\.local\bin`、Linux / macOS は `~/.local/bin` に置く（`--bin-dir` で変えられる）。そこが `PATH` に無ければ知らせる
- `--check` は導入の状態を確かめるだけで、何も変えない。リポジトリを更新したら、導入し直す
- Linux で仮想環境を作るときに `ensurepip` のエラーが出たら、その OS の Python の venv 用のパッケージを入れてから再実行する（AlmaLinux は `sudo dnf install python3-pip`、Ubuntu は `sudo apt install python3-venv`）

## 使い方

Windows では、`edit` で Excel を開いて編集し、閉じたら書き戻せます。

```powershell
mdtable list README.md
mdtable edit README.md
```

- `list` は文書の表の番号・行・種類・大きさ・場所（リストの中など）・直前の見出しを出す
- `edit` は一時フォルダーにブックを書き出して Excel で開く。編集して保存し、Excel を閉じてから Enter を押すと書き戻す。`--table 3`（3 番目の表）や `--table L120`（120 行目を含む表）で表を選べる
- 書き戻せないとき（衝突など）や Ctrl+C で止めたときはブックを残し、取り込み直すコマンドを表示する

ブックをファイルとして残すときは、`export` と `import` を使います。

```powershell
mdtable export README.md
mdtable import README.tables.xlsx --dry-run
mdtable import README.tables.xlsx
```

- `export` は Markdown と同じフォルダーに `README.tables.xlsx` を書く（`-o` で変えられる）。既にあれば `--force` を付けない限り上書きしない
- `import` はブックに記録した Markdown に書き戻す（`--md` で変えられる）。`--dry-run` は差分を出すだけで書き戻さない
- 書き出した後に Markdown の表を変えていたら、衝突として何も書き戻さない（`--force` でブックの内容で上書きする）。一度書き戻したブックをもう一度取り込むと「適用済み」になる
- 取り込みが終わったら、ブックは消してよい

## セルの書き方

| 書きたいもの | Excel のセルの書き方 |
| --- | --- |
| 改行 | セルの中で改行する（Alt+Enter）。空行は段落の区切り |
| 箇条書き | `- 項目`。入れ子は、親の文字の位置まで空白で字下げする |
| 番号付きリスト | `1. 項目`（3 から始めるなら `3. 項目`） |
| タスク | `- [ ] 未着手`・`- [x] 完了` |
| 説明リスト | `用語` の次の行に `: 説明`。組と組の間は空行 |
| コードブロック | ` ``` ` で囲む。言語名を ` ```sh ` のように書くとハイライトされる |
| 太字・コード・リンク | `**太字**`・`` `コード` ``・`[文字](https://example.com)` |
| `\|` | そのまま書く（書き戻しでエスケープする） |
| 行頭の `-` や `1.` を文字として | `\-`・`1\.` のようにエスケープする（元の文書の文字はこの形で出る） |

- リストは空行で終わる。リストの後に段落を続けるときは、間に空行を入れる
- `x` の次の行に `2. y` と書くと、1 つの段落になる（番号付きリストは 1 から始めるか、前に空行を入れる）
- Markdown の表では 1 行目が見出し。列の配置（左・中央・右）は、列の中で多いセルの配置になる
- HTML の表では「太字かつ塗りつぶし」のセルが見出し（`<th>`）。セルの結合が `rowspan` / `colspan` になる。セルの配置は `align` になる

## 書き戻しの規則（要点）

- Markdown の表は、元の書き方（区切り行の `|---|` と `| --- |`、行頭・行末の `|`）を保つ。セルの改行は `<br>`、リストは `<ul><li>` などになる
- HTML の表で書式のあるセルは、`<td>` の後と `</td>` の前に空行を入れて Markdown のまま書く（GitLab の文書が勧める書き方）。書式の無いセルは `<td>文字</td>` の 1 行
- 表の形（行・列・結合）を変えなければ、変えたセルだけを書き換える。形を変えたら表を作り直すが、変えていない行とセルは元の文字のまま
- リストや引用の中の表は、元の字下げ（空白や `>`）を付けて書き戻す。書き戻した後に読み直し、表の場所や前後の解釈が変わるときは書き戻さない

詳しくは[設計と変換の規則](docs/reference/readme.md)にあります。

## Excel で編集するときの注意

- セルは文字列の書式（`@`）になっている。書式をクリアしたセルや、別のブック・Web から「すべて貼り付け」したセルでは、Excel が入力を数値や日付に変えることがある（`1/2` → 日付、`3.10` → 3.1 など）。日付になったセルがあると取り込みは止まる
- 貼り付けは「値のみ」か、セルの編集中（F2・数式バー）に行う
- オートコレクト（`(c)` → `©` など）は入力を変える。コードを書くなら Excel の [ファイル] → [オプション] で切る
- 見出し行の直下に行を挿入すると、見出しの書式が写る（HTML の表では `<th>` になる）。[挿入オプション] で「下と同じ書式を適用」を選ぶ
- [セルを結合して中央揃え] は中央揃えも付く。結合だけなら [セルを結合]
- 「テーブルとして書式設定」（Ctrl+T）は使わない
- セルの一部だけの書式（文字の色・斜体など）は書き戻さない。強調は `**` などで書く
- シート名の変更・並べ替えはよい。シートを消した表は変更なしとして扱う。足したシートは読まない
- 表から離れたセルに書いた値は無視する（警告を出す）
- フォントは、書き出す PC に HackGen Console NF が入っていればそれ、無ければ BIZ UDゴシック（`\` が `¥` に見える）。環境変数 `MDTABLE_FONT` にフォント名を入れると、そのフォントにする

## 対象外になる表

次の表は書き出さず、目次と `list` に理由を出します。GitLab でも崩れて表示されるものが多く、直してから使います。

- 字下げのしすぎで `</td>` がコードブロックになっている、`<pre>` の中に空行がある、表の構造（`<tr>` など）の間に文字がある HTML の表
- セルが重なる（`rowspan` / `colspan`）、`<tfoot>` が `<tbody>` の前にある HTML の表
- 見出し行が引用やリストの続きの行（遅延行）になっている Markdown の表
- セルに制御文字がある、1 つのセルが Excel の上限（32,767 文字）を超える表

## テスト

```sh
python3 -B -m unittest discover -s tests -v
```

Windows では `python -B -m unittest discover -s tests -v` です。openpyxl が要るので、導入した仮想環境の Python で動かします。手元の文書と comrak での確認は[検証記録](docs/verification/readme.md)にあります。
