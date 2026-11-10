# md-preview

Markdown を GitLab の描画でプレビューするアプリ（`mdpreview`）。本文を GitLab に送らず、手元で HTML にする。

今あるのは、Markdown を HTML にする部分（描画エンジンと、その後ろの消毒・フィルター）まで。プレビューのサーバーとページ、コマンドは、これから足す。

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

const { html, headings } = renderDocument('# 題\n\n- [x] 済み\n');
```

- 消毒は GitLab と同じ許可リスト（html-pipeline の許可リストに GitLab の差分を重ねたもの）で行う。`<script>`、`on…` の属性、`javascript:` のリンクなどは出力に残らない
- 写したフィルター: コードの言語、JSON の表、画像の幅と高さ、数式と mermaid の目印、色のチップ、動画と音声、目次、脚注、差分の記法、画像のリンク、外部リンク、タスク、文字の向き、コードの配色
- 写していないもの（原文のまま出す）: `#123`・`@名前` などの参照、PlantUML、Kroki、提案（suggestion）、独自の絵文字、`::include`
- GitLab と違う所: 絵文字の短縮記法（`:smile:`）は `<gl-emoji>` で包まず文字にする。脚注の ID に乱数を付けない。外部の画像は GitLab の中継を通さず、元の URL から読む。コードの配色は highlight.js の字句を Rouge のクラス名に対応づけたもので、色分けは GitLab より粗い

合成の Markdown 17 個を gitlab.com の API に送って保存した HTML（`test/fixtures/`）と、試験で比べている。上の違いと、GitLab のデータや設定が要る 11 か所（`test/fixtures.test.mjs` の「既知の差」）を除いて、要素・属性・文字が一致する。

## テスト

```sh
npm test
```

作り直す手順と決まりは [AGENTS.md](AGENTS.md) にある。
