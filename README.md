# md-preview

Markdown を GitLab の描画でプレビューするアプリ（`mdpreview`）。本文を GitLab に送らず、手元で HTML にする。

今あるのは描画エンジンまで。プレビューのサーバーとページ、コマンドは、これから足す。

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

## テスト

```sh
npm test
```

作り直す手順と決まりは [AGENTS.md](AGENTS.md) にある。
