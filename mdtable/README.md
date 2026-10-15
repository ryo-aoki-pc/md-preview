# md-table-excel

GLFM（GitLab Flavored Markdown）の文書にある表を Excel で編集して、元の文書に書き戻すツール（`mdtable` コマンド）です。Markdown の表（パイプ表）と HTML の表のどちらも扱い、セルの中は Excel 上でも Markdown の記法で書けます。

## 導入

- 対象: AlmaLinux 10 と Windows 11 の自分のユーザー。Python 3.9 以上と git が要る
- `~/md-table-excel`（Windows は `%USERPROFILE%\md-table-excel`）に clone し、`scripts/setup-mdtable.py` で、リポジトリの外の専用の仮想環境に入れる。起動コマンドは `~/.local/bin` に置く
- 手順は setup-notes にある。更新も同じ項で行う

| | AlmaLinux 10 | Windows 11 |
| --- | --- | --- |
| 導入・更新 | [mdtable を導入・更新する](https://github.com/ryo-aoki-pc/setup-notes/blob/main/docs/almalinux-setup.md#mdtable-を導入更新する) | [mdtable を導入・更新する](https://github.com/ryo-aoki-pc/setup-notes/blob/main/docs/windows-setup.md#mdtable-を導入更新する) |
| 使い方 | [mdtable で Markdown の表を Excel と往復する](https://github.com/ryo-aoki-pc/setup-notes/blob/main/docs/almalinux-setup.md#mdtable-で-markdown-の表を-excel-と往復する) | [mdtable で Markdown の表を Excel と往復する](https://github.com/ryo-aoki-pc/setup-notes/blob/main/docs/windows-setup.md#mdtable-で-markdown-の表を-excel-と往復する) |
| Excel で編集するときの注意 | [mdtable のブックを Excel で編集するときの注意](https://github.com/ryo-aoki-pc/setup-notes/blob/main/docs/almalinux-setup.md#mdtable-のブックを-excel-で編集するときの注意) | [mdtable のブックを Excel で編集するときの注意](https://github.com/ryo-aoki-pc/setup-notes/blob/main/docs/windows-setup.md#mdtable-のブックを-excel-で編集するときの注意) |
| 戻す | [ロールバック](docs/extra/readme.md#ロールバック) | [Windows 11 のロールバック](docs/extra/readme.md#windows-11-のロールバック) |

- macOS（未検証）: clone したフォルダーで `python3 scripts/setup-mdtable.py` を実行する。起動コマンドは `~/.local/bin/mdtable`

## できること

- 文書の表を 1 つのブックに書き出し（表ごとに 1 シートと目次）、Excel で編集したら書き戻す。変えていない表・行・セルは 1 文字も変えない
- HTML の表のセル結合（`rowspan` / `colspan`）は、Excel の結合セルになる
- セルの中に、箇条書き・番号付きリスト・説明リスト・コードブロック（言語名を書けば GitLab でシンタックスハイライトされる）を Markdown の記法で書ける
- 説明リスト・番号付きリスト・箇条書き・引用（アラート）・脚注の中で字下げされた表も扱える
- Markdown の表でセルを結合したり、セルにコードブロックを書いたりすると、HTML の表にして書き戻す

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

## 対象外になる表

次の表は書き出さず、目次と `list` に理由を出します。GitLab でも崩れて表示されるものが多く、直してから使います。

- 字下げのしすぎで `</td>` がコードブロックになっている、`<pre>` の中に空行がある、表の構造（`<tr>` など）の間に文字がある HTML の表
- セルが重なる（`rowspan` / `colspan`）、`<tfoot>` が `<tbody>` の前にある HTML の表
- 見出し行が引用やリストの続きの行（遅延行）になっている Markdown の表
- セルに制御文字がある、1 つのセルが Excel の上限（32,767 文字）を超える表

## 文書

- [設計と変換の規則](docs/reference/readme.md)
- [ロールバック](docs/extra/readme.md)
- [検証記録](docs/verification/readme.md): 手元の文書と comrak での確認、Excel の実機での確認とその手順
- テストのコマンドと、変更するときの決まりは [AGENTS.md](AGENTS.md)
