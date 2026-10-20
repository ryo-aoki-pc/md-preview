# md-table-sticky

GitLab の Markdown の表で、先頭行・先頭列を固定してスクロールできるようにするブックマークレット。結合セルを見て固定する範囲を決める。見る人はソフトウェアや拡張機能を入れず、ブックマークを 1 つ足す。

対象は GitLab 19.5 以降（gitlab.com と self-managed）、Chrome・Edge。GitLab が描いた後の表に当てる。表の Markdown 自体は変えない。

## 登録のしかた

次のうち 1 つを使う。ブックマークの名前は「GitLab の表を固定」にする。

1. [dist/bookmarklet.txt](dist/bookmarklet.txt) を GitHub の **Raw** で開き、全部を写す。ブラウザーのブックマークの管理（Edge は「お気に入り」）で新しいブックマークを作り、URL の欄に貼る。アドレスバーには貼らない。`javascript:` が消されるため。
2. [dist/bookmarklet.html](dist/bookmarklet.html) を手元に落として開く。「GitLab の表を固定」のリンクをブックマークバーへドラッグする。ページの「URL を写す」ボタンも使える。

## 使い方

表のある GitLab のページで、登録したブックマークを押す。もう一度押すと元に戻る。ページを読み込み直したら押し直す。

押した後は、ファイルを移る・コメントを開くなどで新しく出た表も処理する。表ごとに縦と横へスクロールする。`<details>` の中の表も、開いたときの大きさに合わせて測り直す。

固定の決まりは次のとおり。

- 見出しの行（`thead` と、先頭に続く `th` だけの行）を上に固定する。無ければ 1 行目を使う。
- 行の見出しの列（本文の先頭から `th` でそろう列）を左に固定する。無ければ 1 列目を使う。
- 見出しの結合セルが境目をまたぐときは、固定する範囲を広げる。ただし全列になるときは広げず、そのセルの中身だけを左に残す。本文の区切りの行など、境目をまたぐセルも同じ扱いにする。
- 縦に長い左の見出しや、横に長い上の見出しは、中身も見えている範囲に残す。文字の揃えは変えない。
- 固定する部分が表示の範囲の半分を超えると、その向きは固定しない。全行・全列になるときも、その向きは固定しない。
- gitlab.com は本文の 1 行目の `th` も上に固定する。この道具は、固定する範囲の外にあるものの固定を外す。GitLab 自身の表の 1 行目の固定は残す。
- セルが重なった表、入れ子の内側の表、コード用の表、GitLab の JSON の表は対象にしない。

固定する表は、元の罫線を読んでセルごとに付け直す。罫線を重ねる方式から分ける方式へ替え、固定したセルの間から本文が透ける帯と、罫線の二重描画を防ぐ。ページの子の増減があったときは、新しい表と、内容や並びの変わった表だけを測り直す。時刻の表示など、表の外の更新では測り直さない。

GitLab 自身は表の 1 行目を固定する。この道具は、先頭列、2 行目以降の見出し、GitLab が包まない表（19.5.0 の `thead` の無い表）の固定を足す。この道具が行の固定をしないときも、GitLab 自身の先頭行の固定は残る。

## GitLab 19.5.0 の注意

GitLab 19.5.0 は、`thead` のある表を描き直すとき、`thead` の 2 行目以降、見出しのセルの `colspan` と `rowspan`、2 つ目以降の `tbody` を表示しない。GitLab のソースで確かめた。gitlab.com では 2026-10-19 の変更で直っている。

この道具では、表示されなくなったセルを戻せない。19.5.0 で結合した見出しを見せるには、`<thead>` を使わず、`<tbody>` の先頭に `<th>` だけの行を置く。19.5.0 は `thead` の無い表を描き直さない。

## ブックマークレットが止められているとき

会社の PC のポリシーでブックマークレットが止められているときは、ソースを開発者ツールのコンソールで動かす方法もある。

1. GitHub で [src/sticky-table.js](src/sticky-table.js) を **Raw** で開き、全部を写す。
2. 表のあるページで開発者ツール（F12）のコンソールを開き、貼って Enter を押す。Chrome・Edge で最初に貼るときに `allow pasting` と打つように言われたら、自分で打ってから貼り直す。
3. もう一度同じソースを貼って Enter を押すと、固定を外す。

## 開発

Node.js 22 以上を使う。依存のパッケージは無い。

```sh
npm test
npm run build
node tools/build-bookmarklet.mjs --check
```

`src/sticky-table.js` を変えたら、`npm run build` で `dist/` を作り直す。生成物もリポジトリに含める。ブラウザーの試験は Edge か Chromium を探す。`MTS_BROWSER` に実行ファイルのパスを指定できる。見つからなければ、その試験は理由を出して飛ばす。

確かめ用の Markdown は `test/fixtures/sample.md`。次で作り直せる。

```sh
node test/fixtures/gen-sample.mjs test/fixtures/sample.md
```

HTML を gitlab.com の Markdown API で描き直すときは、環境変数 `GITLAB_TOKEN` を設定して次を実行する。

```sh
node tools/render-gitlab.mjs
```

`PRIVATE-TOKEN` ヘッダーで送り、`test/fixtures/sample.gitlab.html` を作り直す。`data-sourcepos` は取り除く。トークンはコマンドの引数やファイルに書かない。ブックマークレット自体は通信せず、外部のスクリプトも読み込まない。

設計は [docs/reference/design.md](docs/reference/design.md)。実機での検証記録は [非公開側の readme.md](https://github.com/ryo-aoki-pc/setup-notes-verification/blob/main/repos/md-table-sticky/docs/verification/readme.md) に置く。
