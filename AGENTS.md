# AGENTS.md

このリポジトリで作業するコーディングエージェント（Claude Code・Codex・Grok Build・Antigravity CLI）への指示。Claude Code は CLAUDE.md の `@AGENTS.md` で、Codex・Grok Build・Antigravity CLI はこのファイルを直接読む。

Markdown を GitLab の描画でプレビューするアプリ `mdpreview`。本文を GitLab にも CDN にも送らず、手元で HTML にする。概要は [README.md](README.md)。Neovim から使う橋渡し（`<leader>cp`）は [LazyVimStarter](https://github.com/ryo-aoki-pc/LazyVimStarter) にある。

## 導入手順の正本

AlmaLinux 10・Windows 11 の導入・更新の手順は [docs/install.md](docs/install.md) にある。導入スクリプトは `scripts/setup.mjs` に置く。導入の操作を変えたら、README と `docs/install.md` の案内を合わせる。項の見出しを変えたら、そこへリンクしている README・ほかの手順書・LazyVimStarter などのリンクも直す。

## 守ること

- 描画と表示の経路から、外へ通信しない。本文を GitLab の API に送る処理、CDN からライブラリを読む処理を足さない
- `lib/engine.mjs` の HTML は消毒していない。消毒を通す前の HTML を、ページにもファイルにも出さない
- `engine/src/glfm.rs`・`engine/src/formatter.rs`・`engine/licenses/gitlab-glfm-markdown.txt` は gem `gitlab-glfm-markdown` の写し、`engine/gitlab/glfm_markdown.rb` は GitLab のソースの写しで、書き換えない。GitLab の描画に合わせる直しは、gem の版を上げて取り込む
- フィルターの順序は GitLab の `GfmPipeline` に合わせる。消毒より前のフィルターを消毒の後ろへ動かさない。配色（SyntaxHighlight）は最後に置き、その後ろに消毒を掛けない。消毒の後ろのフィルターが足す属性と `style` は、検証した値だけにする
- 消毒は `hast-util-sanitize` と `lib/sanitize.mjs` の規則で行う。正規表現で HTML の文字列を直接書き換えて消毒しない
- `test/fixtures/*.gitlab.html` は GitLab が返した HTML そのままで、手で書き換えない。見本の入力（`test/fixtures/*.md`）は合成の文章だけにし、実在の利用者・プロジェクトに解決される参照（`@名前` など）を入れない（GitLab が返す HTML に、その人の名前が入る）
- サーバーは 127.0.0.1 だけで待ち受け、URL に推測できない token を入れる。`Host` と `Sec-Fetch-Site` を確かめ、CORS のヘッダーは付けない。ファイルは `lib/resolve.mjs` の `resolve()` でルートの内側の実ファイルに限る（`..`・`\`・Windows の `:`・末尾の点と空白・デバイス名・`.git`・リンクでの脱出を拒む）。これらを緩めない
- 取り込んだファイルの監視も `resolve()` でルート内を確かめ、文書ごとに32個までにする。失敗した include は、安全な親の下で出現を待つ。編集中の文書の再描画には最後に受けた本文を使う
- `page/lib/` は KaTeX・mermaid の原文の写し。手で直さず、`tools/update-page-libs.mjs` で `lock.json`・ライセンスと一緒に更新する。CSS に残る woff / ttf の参照は変えず、配るフォントは woff2 だけにする
- ページの CSP は `script-src 'self'`。ページに入れるスクリプトとライブラリは、同じサーバーから配る。配るライブラリは `page/lib/lock.json` にあるファイルだけにし、`node_modules` をそのまま見せない
- ページでは、サーバーから届いた文字（見出し・知らせ・ファイル名）を `textContent` で入れる。HTML として入れてよいのは、`renderDocument` の `html` だけ
- `engine/glfm_markdown.wasm` を手で差し替えない。`engine/build.sh --update` で作り、`engine/engine.lock.json` と一緒にコミットする

## 構成

- `engine/` — 描画エンジンの Rust の crate。`src/glfm.rs`・`src/formatter.rs` は gem の写し、`src/lib.rs` は WebAssembly から呼ぶ入口（Ruby の橋渡しの代わり）。`gitlab/glfm_markdown.rb` は GitLab がこの gem に渡すオプションのソースの写し（試験が `lib/engine.mjs` の値と比べる）、`glfm_markdown.wasm` はビルドした成果物、`engine.lock.json` は GitLab・gem・comrak・コンテナのイメージ・`.wasm` の固定値、`licenses/` は `.wasm` に入る crate のライセンスの原文、`build.sh` は作り直す手順
- `lib/engine.mjs` — `.wasm` を読み、Markdown を GitLab の整形器の HTML にする（`renderMarkdown`）。オプションは GitLab の `OPTIONS` と同じ値（`GITLAB_OPTIONS`）
- `lib/pipeline.mjs` — front matter・CR・include を処理し、GitLab の順で消毒・フィルターを通す同期の `renderDocument`。HTML・見出し・取り込みの記録・表示の差の通知を返す
- `lib/include.mjs` — IncludeFilter の文字列の展開。`context.readInclude` だけで本文を受け取り、32 回の上限・16 MiB の展開後の大きさ・原文への行番号の対応を管理する
- `lib/notices.mjs` — 完成した木を変えずに、参照・図・提案・取り込めなかった include・絵文字・見つからないメディアを通知する。存在の確認は `context.exists` があるときだけ行う
- `lib/paths.mjs` — ファイルを開かずに、文書の基準とルート内の相対パスを解決する
- `lib/sanitize.mjs` — html-pipeline と GitLab の許可リスト、木の変換、リンクの消毒
- `lib/filters/` — GitLab のフィルターごとの木の変換。最後に `syntax-highlight.mjs` でコードを配色する
- `lib/highlight.mjs` — highlight.js を遅延して読み、字句を Rouge のクラスと行ごとの木へ変換する
- `lib/tree.mjs` — フィルターで共有する hast の操作
- `cli.mjs` — 引数の解釈、ブラウザの起動、ファイルの監視と終了処理。実行コマンドは `mdpreview`。ファイル・フォルダー表示と `--stdio` の入口を切り替える
- `lib/resolve.mjs`・`lib/server.mjs` — ルート内のパス検査、127.0.0.1 の HTTP / SSE サーバーとローカルのライブラリ配信
- `lib/session.mjs`・`lib/worker.mjs` — 文書とスクロールの状態、1つの worker による文書ごとの描画と集約、時間制限、休止時の取り消し。ファイル文書の購読・監視・破棄と、include の依存先の監視も管理する
- `lib/stdio.mjs` — Neovim 用の JSON 行の入出力、96 MB の行制限、休止と再開、タブ数とジャンプの通知
- `lib/documents.mjs` — Markdown の拡張子と一覧。隠しファイル・依存・フォルダーのリンクを除き、深さと件数を制限する
- `lib/include-watch.mjs` — include の相対名を実パスにし、まだ無いファイルは安全な親まで検査する
- `lib/watch.mjs` — フォルダーの監視と定期的な stat で、置き換え保存にも追従する。ファイル文書の表示側には削除専用の通知も渡せる。include では、まだ無い親フォルダーの出現にも追従する
- `lib/page.mjs`・`page/` — ページ設定の埋め込みとブラウザの表示。文書の移動・一覧・見出し欄・GitLab との違いの知らせ・印刷の表示も扱う。KaTeX・mermaid は同じサーバーから読む
- `page/lib/` — ブラウザに配る KaTeX・mermaid と原文のライセンス。`lock.json` に npm の版・integrity と各ファイルの sha256・大きさを固定する
- `tools/update-page-libs.mjs` — 開発用。`node_modules` の原文を写し直し、`--check` で固定情報・原文との一致を検査する
- `scripts/setup.mjs` — リポジトリ外への導入・更新と `--check`。実行時のファイルと本番の依存を一時フォルダーで確かめて入れ替え、`mdpreview` の起動役を置く。任意の公式スタイルを保存する隣の `md-preview-assets` は触らない
- `docs/install.md` — AlmaLinux 10・Windows 11 の導入・更新の手順
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

## KaTeX・mermaid の版を上げる

実行時は `page/lib/` の写しを使い、KaTeX・mermaid の `node_modules` は要らない。更新のときだけ開発依存を導入する。

1. `npm install --save-dev --save-exact katex@<版> mermaid@<版>` で版を固定し、`package.json` と `package-lock.json` を合わせる
2. `node tools/update-page-libs.mjs` で JS・CSS・woff2 と原文のライセンスを写し、`page/lib/lock.json` を書き直す。CSS は原文のままにする。今のブラウザは先頭の woff2 を選ぶため、woff / ttf は置かない
3. `node tools/update-page-libs.mjs --check` と `npm test` を実行する。Playwright の試験では、本物の数式・図と外部への要求がないことも確かめる
4. 写し・ライセンス・`lock.json`・npm の固定情報を一緒にレビューし、同じ変更に含める。`page/lib/**` は `.gitattributes` の `-text -whitespace` で原文のバイトを保つ

`--check` は開発依存がない導入でも sha256 と大きさを検査する。開発依存がある場合は、その版と原文にも比べる。

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
for f in cli.mjs lib/*.mjs lib/filters/*.mjs page/*.js scripts/*.mjs test/*.mjs tools/*.mjs; do node --check "$f"; done
git diff --check
```

- `test/include.test.mjs` — include の記法・上限・非再帰・パス・失敗・容量・消毒、Comrak のエスケープの例と、読み込みなしの API 出力の互換性
- `test/notices.test.mjs` — 通知の拾い分け・include を展開した後の原文の行・重複・抜き出し・件数の上限・HTML を変えないこと
- `test/sanitize.test.mjs` — 許可リスト・URL・危険な属性と要素・タスク入力の条件
- `test/filters.test.mjs` — 各フィルターの Ruby ソースの分岐
- `test/highlight.test.mjs` — 配色の言語・スコープ・行分け・エスケープ・遅延読み込み
- `test/fixtures.test.mjs` — GitLab の保存済みの実出力と木を厳密に比較する。意図した差だけを正規化し、設定が必要な差は場所と値を固定する
- `test/pipeline.test.mjs` — 全 Markdown 素材と代表構文、消毒の境界、文書間の独立性
- `test/xss.test.mjs` — 悪意のある入力を直接渡す場合と include から取り込む場合を通し、出力を HTML として読み直した木に、許可していない要素・属性・URL が無いことを確かめる
- `test/resolve.test.mjs`・`test/server.test.mjs` — Windows / POSIX のパス検査と実 HTTP / SSE、配信の許可リスト、ヘッダー、文書別 SSE と一覧の拒否条件・順序・上限の試験
- `test/page-libs.test.mjs` — 写しの sha256・大きさ・ライセンス・更新と改変検出、許可リスト外の拒否と、KaTeX・mermaid の `node_modules` がない導入での配信を確かめる
- `test/session.test.mjs`・`test/watch.test.mjs`・`test/cli.test.mjs` — 描画の集約と時間切れからの復帰、文書ごとの最新入力、32件の上限と5秒後の破棄、include の変更・削除・出現と32個の監視上限・入れ替え・解除、ファイル保存、CLI のファイル・フォルダーの起動と終了の試験
- `test/page.test.mjs` — 本物のサーバー・KaTeX・mermaid によるブラウザ試験。文書の移動・戻ると進む・一覧・見出し欄・違いの知らせの安全な表示と行への移動・include の再描画・印刷と、外部要求の試みがないことを確認する。Playwright（`PLAYWRIGHT_MODULE` または `playwright`）と Edge / Chrome / Chromium がない場合は理由を付けて skip する。ブラウザの場所は `EDGE_BIN`・`CHROME_BIN` で指定できる
- `test/stdio.test.mjs` — 実際の子プロセスで hello / ready、本文・スクロール、タブ数、休止・再開、ジャンプ、行の分割・上限、パイプと終了、別ルートへの切り替え時のファイル文書の reload を確かめる
- `test/setup.test.mjs` — 偽の npm と手元の依存を使う、ネットワーク不要の導入試験。一時の clone と導入先を作業ツリー内に作り、コピー・ハッシュ・依存・起動役・PATH・失敗時の復元・中断後の回収・引数を確かめる。Windows では PowerShell の標準入力も確かめ、シェルが無ければ理由を付けて skip する
- `test/helpers.mjs` — 作業ツリー内の一時ファイル、HTTP と SSE、非同期条件の試験用補助
- Node.js 22 以上
- 描画に関わる変更は、試験に例を足す。GitLab の出力と比べて確かめたことだけを「同じ」と書く

## 文書の移動と一覧

- `/<token>/` は編集中の文書、`?doc=<ルートからの相対パス>` はファイルの文書。`_/events?doc=...` はその文書の描画だけを送り、スクロールは編集中の文書のタブだけへ送る。ファイルのパスは `resolve()` で検査する
- Markdown の拡張子は `md`・`markdown`・`mdown`・`mkd`・`mkdn`。同じファイルを編集している場合は未保存の本文を使い、別ルートへ移ったファイル文書のタブには `reload` を送る
- 見張るファイル文書は32件まで。最後のタブが離れて5秒後に監視と結果を捨てる。上限に達した場合は、タブのいない古い文書から捨てる
- `_/list` はルート内の Markdown の一覧を JSON で返す。`?list` で一覧の画面を出す。深さ20・5000件までで、隠しファイル・`node_modules`・フォルダーのリンクは除く。無名の文書だけでルートがないときは空
- フォルダーを引数にした場合はその場所をルートとし、最初に一覧を出す。`--root` とは併用しない
- 見出し欄の開閉は配色・本文幅と同じ sessionStorage に保存する。文字は textContent で入れ、印刷時は見出し欄と操作欄を隠す

## Neovim との取り決め

- 取り決めの版は1。`mdpreview --stdio [--port <番号>]` の標準入力と標準出力を、UTF-8 の JSON オブジェクト1行ずつ（区切りは `\n`）でつなぐ。標準出力に人向けの文は書かない。ブラウザの起動は Lua が行う
- Lua からは、最初に `hello`（`protocol: 1` と省略できる `appearance`）、続いて `doc`・`scroll`・`pause`・`resume` を送る。休止中の本文とスクロールは捨て、再開後の本文で描画を戻す。休止してもプロセス・サーバー・URL は保つ
- アプリからは `ready`（版・アプリの version・URL）を1回、`clients`（タブ数。最初は0）、`jump`（今の文書の path と行）、`error`（日本語1行）を返す。hello 前の行と対応しない版は `fatal: true` と終了コード2にする
- 入力の1行は96 MBまで。誤りのある行は捨てて続ける。標準入力が閉じたらタブに `stop`（`reason: 'exit'`）を送り、サーバーと worker を閉じて終了コード0で終わる
- `CONFIG.jump` が真で、編集中の文書を見ている場合だけ、本文のダブルクリックから `POST _/jump` に行番号を送る。サーバーは token・Host・Sec-Fetch-Site・Origin・JSON の型と1 KBの本文上限を確かめる。単体のファイル表示では無効

## 書き方の規則

- コードのコメントと利用者に出すメッセージは日本語。まわりのコードの書き方に合わせる
- ソースは LF。文書（README・docs）は日本語。検証していないことを「動く」と書かない
- 検証記録は、Forgejo の非公開リポジトリ `setup-notes-verification` の `repos/md-preview/docs/verification/` に置く。このリポジトリからは URL で指さず、「`<検証記録のパス>` の「<見出し>」」と文字で書く
- 公開のリポジトリなので、ホスト名・アドレス・機材など、環境が分かる値を書かない
