# md-table-sticky の登録と更新（Windows 11 の Edge）

- GitLab の Markdown の表で、先頭行と先頭列を固定してスクロールできるようにするブックマークレット（[md-table-sticky](../README.md)）
- 使い方は [md-table-sticky の使い方](usage.md) にある
- どの項も単独で通せる。項のリードの前提を先に満たす

<details id="この文書の補足と注意点">
  <summary>この文書の補足と注意点</summary>

**参照**

- 検証記録（setup-notes-verification の `repos/md-table-sticky/docs/verification/readme.md`）
- 検証記録（setup-notes-verification の `docs/verification/windows-setup.md`）

</details>

## md-table-sticky のブックマークレットを登録する

- この PC の Edge（Microsoft Edge）に「GitLab の表を固定」をお気に入りとして登録する
- 前提: 無し
- 実行場所: この PC のデスクトップの Edge（Microsoft Edge）
- 新しい版が出たときも、この項で作り直す

1. Edge を開き、ブックマークレットの全文をコピーする。

   - [dist/bookmarklet.txt の Raw](https://raw.githubusercontent.com/ryo-aoki-pc/md-table-sticky/main/dist/bookmarklet.txt) を開き、Ctrl+A → Ctrl+C で全部をコピーする
   - 先頭が `javascript:` の文字列をコピーできればよい
   - ブックマークレットはアドレスバーには貼らない

1. お気に入りの管理を開き、左の「お気に入りバー」を選ぶ。

   - アドレスバーに `edge://favorites` と入れて Enter を押す
   - 「お気に入り」のページが開けばよい
   - 前の版の「GitLab の表を固定」が右の一覧にあれば、その行の右の「×」を押して消す

1. お気に入りを追加する。

   - 上の「お気に入りの追加」を押す
   - 「お気に入りの追加」の「名前」に「GitLab の表を固定」と入れ、「URL」にこの項の手順 1 でコピーした全文を貼って、「保存」を押す
   - 右の一覧に「GitLab の表を固定」が出て、その URL が `javascript:` で始まればよい
   - お気に入りバーが見えなければ、Ctrl+Shift+B で表示する

---
