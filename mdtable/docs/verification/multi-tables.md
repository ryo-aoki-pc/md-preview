<!-- markdownlint-disable -->
<!-- 確かめるために、空行の無い表とリストの並びや HTML の表をわざと含むので、markdownlint の自動修正をかけない -->

# 複数の表と本文の確認

この文書は、本文の段落・見出し・リスト・コードと、複数の表が混ざった文書を mdtable で編集したときに、GitLab の表示が崩れないかを確かめるためのものです。

## 概要

本文の段落です。**強調**や `コード`、[リンク](https://example.com) を含みます。
2 行目は同じ段落の続きです。

| 項目 | 値 | 備考 |
|---|:---:|---|
| 名前 | md-table-excel | `mdtable` コマンド |
| 版 | 0.1.0 | 2026-10-09 |

表のすぐ後の段落です（表との間に空行があります）。

## 手順

1. 準備する。

   | 手順 | コマンド |
   | --- | --- |
   | 導入 | `python scripts/setup-mdtable.py` |
   | 確認 | `mdtable --version` |

2. 書き出す。

   ```powershell
   mdtable export README.md
   ```

3. 取り込む。

- 箇条書きの項目
  - 入れ子の項目

| 種類 | 説明 |
|---|---|
| パイプ表 | Markdown の表 |
| HTML の表 | セル結合ができる |
- 表のすぐ後のリスト（空行なし）

<table>
<thead>
<tr><th rowspan="2">区分</th><th colspan="2">内容</th></tr>
<tr><th>項目</th><th>説明</th></tr>
</thead>
<tbody>
<tr><td>導入</td><td>dnf</td><td>パッケージを入れる</td></tr>
<tr><td>設定</td><td>dotfiles</td><td>リンクを張る</td></tr>
</tbody>
</table>

HTML の表の後の段落です。

> [!NOTE]
> アラートの中の表です。
>
> | キー | 動作 |
> |---|---|
> | `e` | 編集 |
> | `q` | 終了 |

用語
: 説明リストの説明です。

  | 設定 | 既定 |
  |---|---|
  | フォント | HackGen Console NF |

## 末尾

最後の段落です。脚注も付けます[^1]。

| 最後 | 表 |
|---|---|
| a | b |

[^1]: 脚注の本文です。
