# Excel での確認用の文書

[検証記録](readme.md)の「Excel 実機での確認の手順」で使う文書です。この文書の写しを書き出して、Excel で開きます。

## Markdown の表

| ツール | 導入元 | 備考 |
|---|:---:|---|
| lazygit | scoop | `scoop install lazygit`<br>設定は `%LOCALAPPDATA%\lazygit` |
| yazi | scoop | <ul><li>`yazi.toml`</li><li>`keymap.toml`</li></ul> |
| 版 | 3.4 | 2026-10-09 |

## HTML の表

<table>
<thead>
<tr><th rowspan="2">区分</th><th colspan="2">内容</th></tr>
<tr><th>項目</th><th>説明</th></tr>
</thead>
<tbody>
<tr>
<td rowspan="2">導入</td>
<td><code>dnf</code></td>
<td>

- パッケージを入れる
- **root** で実行する

</td>
</tr>
<tr>
<td>コード</td>
<td><pre><code class="language-sh">sudo dnf install -y epel-release</code></pre></td>
</tr>
</tbody>
</table>

## リストの中の表

1. 確認する。

   | 手順 | コマンド |
   | --- | --- |
   | 確認 | `mdtable --version` |
