# AGENTS.md

このリポジトリで作業するコーディングエージェント（Claude Code・Codex・Grok Build・Antigravity CLI）への指示。Claude Code は CLAUDE.md の `@AGENTS.md` で、Codex・Grok Build・Antigravity CLI はこのファイルを直接読む。

Markdown を GitLab の描画でプレビューするアプリ `mdpreview`。本文を GitLab にも CDN にも送らず、手元で HTML にする。概要は [README.md](README.md)。Neovim から使う橋渡し（`<leader>cp`）は [LazyVimStarter](https://github.com/ryo-aoki-pc/LazyVimStarter) にある。

## 守ること

- 描画と表示の経路から、外へ通信しない。本文を GitLab の API に送る処理、CDN からライブラリを読む処理を足さない
- `lib/engine.mjs` の HTML は消毒していない。消毒を通す前の HTML を、ページにもファイルにも出さない
- `engine/src/glfm.rs`・`engine/src/formatter.rs`・`engine/licenses/gitlab-glfm-markdown.txt` は gem `gitlab-glfm-markdown` の写し、`engine/gitlab/glfm_markdown.rb` は GitLab のソースの写しで、書き換えない。GitLab の描画に合わせる直しは、gem の版を上げて取り込む
- フィルターの順序は GitLab の `GfmPipeline` に合わせる。消毒より前のフィルターを消毒の後ろへ動かさない。配色（SyntaxHighlight）は最後に置き、その後ろに消毒を掛けない。消毒の後ろのフィルターが足す属性と `style` は、検証した値だけにする
- 消毒は `hast-util-sanitize` と `lib/sanitize.mjs` の規則で行う。正規表現で HTML の文字列を直接書き換えて消毒しない
- `test/fixtures/*.gitlab.html` は GitLab が返した HTML そのままで、手で書き換えない。見本の入力（`test/fixtures/*.md`）は合成の文章だけにし、実在の利用者・プロジェクトに解決される参照（`@名前` など）を入れない（GitLab が返す HTML に、その人の名前が入る）
- `engine/glfm_markdown.wasm` を手で差し替えない。`engine/build.sh --update` で作り、`engine/engine.lock.json` と一緒にコミットする

## 構成

- `engine/` — 描画エンジンの Rust の crate。`src/glfm.rs`・`src/formatter.rs` は gem の写し、`src/lib.rs` は WebAssembly から呼ぶ入口（Ruby の橋渡しの代わり）。`gitlab/glfm_markdown.rb` は GitLab がこの gem に渡すオプションのソースの写し（試験が `lib/engine.mjs` の値と比べる）、`glfm_markdown.wasm` はビルドした成果物、`engine.lock.json` は GitLab・gem・comrak・コンテナのイメージ・`.wasm` の固定値、`licenses/` は `.wasm` に入る crate のライセンスの原文、`build.sh` は作り直す手順
- `lib/engine.mjs` — `.wasm` を読み、Markdown を GitLab の整形器の HTML にする（`renderMarkdown`）。オプションは GitLab の `OPTIONS` と同じ値（`GITLAB_OPTIONS`）
- `lib/pipeline.mjs` — front matter と CR を処理し、GitLab の順で消毒・フィルターを通す同期の `renderDocument`
- `lib/sanitize.mjs` — html-pipeline と GitLab の許可リスト、木の変換、リンクの消毒
- `lib/filters/` — GitLab のフィルターごとの木の変換。最後に `syntax-highlight.mjs` でコードを配色する
- `lib/highlight.mjs` — highlight.js を遅延して読み、字句を Rouge のクラスと行ごとの木へ変換する
- `lib/tree.mjs` — フィルターで共有する hast の操作
- `tools/make-fixtures.mjs` — 開発用。見本の入力を gitlab.com の Markdown API に送り、返った HTML を保存する。アプリからは呼ばれない
- `test/` — `node --test` の試験。`test/fixtures/` は、合成の Markdown（`<名前>.md`）、GitLab の本物の出力（`<名前>.gitlab.html`）、取得した条件（`fixtures.json`）

## 描画エンジンを作り直す・上げる

使うホストに Rust もコンテナも要らない。作り直すのは、gem の版を上げるときと、`engine/src/lib.rs`・`engine/Cargo.toml` を変えたときだけ。podman（無ければ docker）と Node.js が要る。

```sh
engine/build.sh              # Cargo.lock のとおりにビルドし、作り直した .wasm・置いてある .wasm・engine.lock.json が同じことを確かめる
engine/build.sh --check-gem  # 写しと、Cargo.lock の重なる crate の版と checksum が、取得した gem・GitLab のソースと同じことを確かめる
engine/build.sh --update     # .wasm・licenses/・engine.lock.json の wasm の値を書き直す
```

GitLab の版を上げるときは、次の順に行う。

1. その版の `Gemfile.lock` で `gitlab-glfm-markdown` の版を調べ、rubygems.org のソース gem から `glfm.rs`・`formatter.rs`・`LICENSE`・`Cargo.lock` を写す。`lib/banzai/filter/markdown_engines/glfm_markdown.rb` を `engine/gitlab/glfm_markdown.rb` に写す
2. `engine/Cargo.toml` の版と comrak の版を合わせる
3. `engine/engine.lock.json` の `gitlab`（版・出典・オプションの URL と sha256）、`gem`（版・URL・sha256・`files` の sha256）、`comrak` を書き換える。`build.sh` は最初に写しを `engine.lock.json` の sha256 と比べるので、ここを合わせないと止まる
4. `engine/build.sh --check-gem`、`engine/build.sh --update` の順に実行する
5. `lib/engine.mjs` の `GITLAB_OPTIONS` を、写した `glfm_markdown.rb` の `OPTIONS` に合わせる（`npm test` が比べる）

## 見本を作り直す

GitLab の描画が変わったか確かめるとき、見本の入力を足したときに行う。試験（`npm test`）は GitLab に繋がない。

```sh
GITLAB_TOKEN=<read_api のトークン> node tools/make-fixtures.mjs          # 全部
GITLAB_TOKEN=<read_api のトークン> node tools/make-fixtures.mjs <名前>   # 1 つだけ
```

- 送るのは `test/fixtures/*.md` だけ。トークンは環境変数から読み、表示も保存もしない
- 作り直した後、`<名前>.gitlab.html` に実在の利用者の名前などが入っていないか確かめる（`class="gfm`・`data-user` を探す）
- `npm test` で比べる。合わない所は実装を直す。GitLab のデータや設定が要って直せない所だけを、`test/fixtures.test.mjs` の「既知の差」の表に、場所と理由を書いて入れる

## テスト

```sh
npm test
for f in lib/*.mjs lib/filters/*.mjs test/*.mjs; do node --check "$f"; done
git diff --check
```

- `test/sanitize.test.mjs` — 許可リスト・URL・危険な属性と要素・タスク入力の条件
- `test/filters.test.mjs` — 各フィルターの Ruby ソースの分岐
- `test/highlight.test.mjs` — 配色の言語・スコープ・行分け・エスケープ・遅延読み込み
- `test/fixtures.test.mjs` — GitLab の保存済みの実出力と木を厳密に比較する。意図した差だけを正規化し、設定が必要な差は場所と値を固定する
- `test/pipeline.test.mjs` — 全 Markdown 素材と代表構文、消毒の境界、文書間の独立性
- `test/xss.test.mjs` — 悪意のある入力を通し、出力を HTML として読み直した木に、許可していない要素・属性・URL が無いことを確かめる
- Node.js 22 以上
- 描画に関わる変更は、試験に例を足す。GitLab の出力と比べて確かめたことだけを「同じ」と書く

## 書き方の規則

- コードのコメントと利用者に出すメッセージは日本語。まわりのコードの書き方に合わせる
- ソースは LF。文書（README・docs）は日本語。検証していないことを「動く」と書かない
- 検証記録は、Forgejo の非公開リポジトリ `setup-notes-verification` の `repos/md-preview/docs/verification/` に置く。このリポジトリからは URL で指さず、「`<検証記録のパス>` の「<見出し>」」と文字で書く
- 公開のリポジトリなので、ホスト名・アドレス・機材など、環境が分かる値を書かない
