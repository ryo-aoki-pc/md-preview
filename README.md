# md-preview

Markdown を GitLab の描画でプレビューするアプリ（`mdpreview`）。本文を GitLab に送らず、手元で HTML にする。

導入のスクリプトと手順書は、これから足す。今は、clone の中で `npm ci --ignore-scripts` を実行してから、`node cli.mjs` で動かす。

## 使い方

```sh
node cli.mjs README.md          # ファイルを開く。ブラウザに表示し、ファイルが変わると描き直す
node cli.mjs docs               # フォルダーを開く。中の Markdown の一覧を出す
node cli.mjs --no-open README.md   # ブラウザを開かず、URL だけを出す
```

- 127.0.0.1 だけで待ち受け、URL に推測できない値を入れる。出てきた URL（`プレビュー: http://127.0.0.1:…`）をブラウザで開く
- 本文の相対リンクが別の Markdown を指すとき、同じタブの中でその文書へ移る（ブラウザの戻る・進むで戻れる）。操作欄の「一覧」で、フォルダーの Markdown の一覧を出す
- 操作欄の「見出し」で、見出しの一覧を左に出し入れする。配色（システム・ライト・ダーク）と本文幅（全幅・固定幅）も操作欄で変える
- 数式（KaTeX）と図（mermaid）は、同じサーバーから配るライブラリでブラウザが描く。CDN は使わない
- 画像・動画・リンクの相対パスは、手元のファイルを指す。読めるのは、開いたファイルのリポジトリ（`.git` のあるフォルダー。無ければファイルのあるフォルダー）の内側だけで、`.git` の中は返さない。`--root <フォルダー>` で変えられる
- 本文が外部の URL の画像・動画を指すときは、ブラウザがその URL から読む（GitLab と同じ）。それ以外に、表示のために外へ通信することはない
- `--port <番号>` で待ち受けるポートを、`--browser <実行ファイル>` で開くブラウザを指定する。Ctrl+C で終わる

Neovim からは、[LazyVimStarter](https://github.com/ryo-aoki-pc/LazyVimStarter) の `<leader>cp` が `mdpreview --stdio` を起こして使う（未保存の内容の表示、カーソルに合わせたスクロール、プレビューのダブルクリックでエディタの行へ移る）。取り決めは [AGENTS.md](AGENTS.md) の「Neovim との取り決め」にある。

## 描画エンジン

GitLab は、Markdown を gem `gitlab-glfm-markdown`（Rust。Comrak に GitLab の整形を重ねたもの）で HTML にしている。このリポジトリは、その gem の `glfm.rs`・`formatter.rs` を書き換えずに WebAssembly にビルドして使う。構文解析と整形の段は、GitLab と同じコードが動く。

- 合わせている版: GitLab 19.5.1（gem 0.0.45、Comrak 0.56.0）。固定値は [engine/engine.lock.json](engine/engine.lock.json)
- gem に渡すオプションは、GitLab の `OPTIONS` と同じ値にしてある。GitLab が機能フラグで決める `github_pre_lang` だけは、`<pre lang="…">` の形に固定している
- `engine/glfm_markdown.wasm` は外の機能を何も取り込まない。Node.js 22 以上があれば動き、Rust は要らない
- gem とその依存のライセンスの原文は [engine/licenses/](engine/licenses/) にある

```js
import { renderMarkdown } from './lib/engine.mjs';

renderMarkdown('- [~] 対象外\n');
// <ul class="task-list" …><li class="inapplicable task-list-item" …>…
```

`renderMarkdown` の HTML は消毒していない（GitLab も、この後ろの段で消毒する）。

## 消毒とフィルター

GitLab は、エンジンの HTML に Ruby のフィルター（`Banzai::Pipeline::GfmPipeline`）を順に掛けて、最終の HTML にする。`lib/pipeline.mjs` の `renderDocument` は、その順序のまま JavaScript に写したもの。

```js
import { renderDocument } from './lib/pipeline.mjs';

const { html, headings, notices } = renderDocument('# 題\n\n- [x] 済み\n');
```

- 消毒は GitLab と同じ許可リスト（html-pipeline の許可リストに GitLab の差分を重ねたもの）で行う。`<script>`、`on…` の属性、`javascript:` のリンクなどは出力に残らない
- 写したフィルター: コードの言語、JSON の表、画像の幅と高さ、数式と mermaid の目印、色のチップ、動画と音声、目次、脚注、差分の記法、画像のリンク、外部リンク、タスク、文字の向き、コードの配色
- `::include{file=…}` は、読み込みの関数を渡したときだけ、ルートの内側のファイルで展開する（32 個まで・再帰しない。GitLab のリポジトリの表示と同じ決まり）。URL は取り込まず、その URL へのリンクにする
- 写していないもの（原文のまま出す）: `#123`・`@名前` などの参照、PlantUML、Kroki、提案（suggestion）、独自の絵文字
- `notices` に、GitLab では別の見え方になる所（参照になりうる記法、PlantUML などの図、取り込めなかった include、文字のまま残った絵文字の短縮記法、手元に無い画像）を、原文の行と一緒に返す
- GitLab と違う所: 絵文字の短縮記法（`:smile:`）は `<gl-emoji>` で包まず文字にする。脚注の ID に乱数を付けない。外部の画像は GitLab の中継を通さず、元の URL から読む。コードの配色は highlight.js の字句を Rouge のクラス名に対応づけたもので、色分けは GitLab より粗い

合成の Markdown 17 個を gitlab.com の API に送って保存した HTML（`test/fixtures/`）と、試験で比べている。上の違いと、GitLab のデータや設定が要る 11 か所（`test/fixtures.test.mjs` の「既知の差」）を除いて、要素・属性・文字が一致する。

## テスト

```sh
npm ci --ignore-scripts
npm test
```

ページの試験（`test/page.test.mjs`）は、Playwright と Edge・Chrome・Chromium のどれかが要る。Playwright を通常の場所から読めないときは、`PLAYWRIGHT_MODULE` に導入先を指定する。無ければ、その試験だけ skip になる（合格には数えない）。

作り直す手順と決まりは [AGENTS.md](AGENTS.md) にある。
