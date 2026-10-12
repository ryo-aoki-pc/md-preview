# md-table-excel

導入・利用・更新は [AlmaLinux 10 の手順](https://github.com/ryo-aoki-pc/setup-notes/blob/main/docs/almalinux-setup.md) / [Windows 11 の手順](https://github.com/ryo-aoki-pc/setup-notes/blob/main/docs/windows-setup.md) に集約している。設定仕様・開発者向け操作・過去の検証・ロールバックはこのリポジトリで管理する。

移動先は[集約対応表](https://github.com/ryo-aoki-pc/setup-notes/blob/main/docs/dev/consolidation-map.md)で確認する。


GLFM（GitLab Flavored Markdown）の文書にある表を Excel で編集して、元の文書に書き戻すツール（`mdtable` コマンド）です。Markdown の表（パイプ表）と HTML の表のどちらも扱い、セルの中は Excel 上でも Markdown の記法で書けます。

[設計と変換の規則](docs/reference/readme.md) / [検証記録](docs/verification/readme.md)

## できること

- 文書の表を 1 つのブックに書き出し（表ごとに 1 シートと目次）、Excel で編集したら書き戻す。変えていない表・行・セルは 1 文字も変えない
- HTML の表のセル結合（`rowspan` / `colspan`）は、Excel の結合セルになる
- セルの中に、箇条書き・番号付きリスト・説明リスト・コードブロック（言語名を書けば GitLab でシンタックスハイライトされる）を Markdown の記法で書ける
- 説明リスト・番号付きリスト・箇条書き・引用（アラート）・脚注の中で字下げされた表も扱える
- Markdown の表でセルを結合したり、セルにコードブロックを書いたりすると、HTML の表にして書き戻す

## 導入

[AlmaLinux 10](https://github.com/ryo-aoki-pc/setup-notes/blob/main/docs/almalinux-setup.md#mdtable-を導入更新する任意) / [Windows 11](https://github.com/ryo-aoki-pc/setup-notes/blob/main/docs/windows-setup.md#mdtable-を導入更新する任意)

macOS はリポジトリのフォルダーで `python3 scripts/setup-mdtable.py` を実行する。

## 使い方

[AlmaLinux 10](https://github.com/ryo-aoki-pc/setup-notes/blob/main/docs/almalinux-setup.md#mdtable-で-markdown-の表を-excel-と往復する) / [Windows 11](https://github.com/ryo-aoki-pc/setup-notes/blob/main/docs/windows-setup.md#mdtable-で-markdown-の表を-excel-と往復する)

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


[AlmaLinux 10](https://github.com/ryo-aoki-pc/setup-notes/blob/main/docs/almalinux-setup.md#mdtable-のブックを-excel-で編集するときの注意) / [Windows 11](https://github.com/ryo-aoki-pc/setup-notes/blob/main/docs/windows-setup.md#mdtable-のブックを-excel-で編集するときの注意)

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
