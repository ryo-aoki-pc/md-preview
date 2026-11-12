# AGENTS.md

md-tools は Markdown を扱う 3 つの道具をまとめたリポジトリ。md-preview（`mdpreview`）はルート（`cmd/mdpreview/`・`internal/`・`engine/`・`page/` など）、md-table-excel（`mdtable`）は `mdtable/`、md-table-sticky（ブックマークレット）は `sticky/` に置く。

`mdtable/`・`sticky/` を触るときは、その中の `AGENTS.md` も読む。

このリポジトリで作業するコーディングエージェント（Claude Code・Codex・Grok Build・Antigravity CLI）への指示。Claude Code は CLAUDE.md の `@AGENTS.md` で、Codex・Grok Build・Antigravity CLI はこのファイルを直接読む。

Markdown を GitLab の描画でプレビューするアプリ `mdpreview`。本文を GitLab にも CDN にも送らず、手元で HTML にする。概要は [README.md](README.md)。Neovim から使う橋渡し（`<leader>cp`）は [LazyVimStarter](https://github.com/ryo-aoki-pc/LazyVimStarter) にある。

## 導入手順の正本

AlmaLinux 10・Windows 11 の導入・更新の手順は [docs/install.md](docs/install.md)、使い方の手順の正本は [docs/usage.md](docs/usage.md) にある。設計は [docs/reference/readme.md](docs/reference/readme.md) にある。配布は `.github/workflows/release.yml` と `scripts/build-release.sh` で行い、OS・CPU ごとの単体の実行ファイルを GitHub Releases に置く。実行ファイルの版は `internal/version/version.go` の 1 か所で決める。版を上げるときは `internal/version/version.go`、`licenses/manifest.json` の md-preview、`mdtable/licenses/manifest.json` の md-table-excel、`package.json`・`package-lock.json` の版を同じ値にする。`v<版>` のタグを push すると、`mdpreview` と `mdtable` の配布物が 1 つの Release に載る。導入の操作を変えたら、README と `docs/install.md` の案内を合わせる。項の見出しを変えたら、そこへリンクしている README・ほかの手順書・LazyVimStarter などのリンクも直す。

## 守ること

- `mdtable/` の Go のコードがルートから import してよいのは `internal/version` だけ。`mdpreview` に excelize などの `mdtable` だけの依存を入れない。ルートの試験が、両コマンドの依存を検査する
- 描画と表示の経路から、外へ通信しない。本文を GitLab の API に送る処理、CDN からライブラリを読む処理を足さない
- PlantUML の原文は手元のコマンドの標準入力だけに渡す。`PLANTUML_SECURITY_PROFILE=SANDBOX` を必ず指定し、PATH の絶対パスの項目だけから探す。SVG は img と sandbox 付きの応答だけで扱い、DOM に入れない。描けなかった場合は原文のコードブロックへ戻す
- 公式スタイルの取得は、利用者が明示した `mdpreview install-style` だけで行う。起動と `--check` は通信しない。配信する資産はロック・導入情報・全ファイルの大きさと sha256 を確かめ、実パスを `internal/resolve` の `Resolve` で保存先の内側に限り、検証したバイト列だけを許可リストで配る。失敗時は同梱スタイルへ戻す
- `mdpreview render` は期限付きの `context` で60秒以内に描画し、消毒済みの `pipeline.Result` の `HTML` だけを本文に使う。公式資産は再配布せず、同梱スタイルと文書に必要な KaTeX・mermaid だけを埋める。スクリプトごとの sha256 を CSP に固定し、数式も図も無ければスクリプトを入れない。サーバーの token や端末の絶対パスを生成しない。出力は完成後に隣の一時ファイルから入れ替える
- `engine` パッケージの出力の HTML は消毒していない。消毒を通す前の HTML を、ページにもファイルにも出さない
- `engine/src/glfm.rs`・`engine/src/formatter.rs`・`engine/licenses/gitlab-glfm-markdown.txt` は gem `gitlab-glfm-markdown` の写し、`engine/gitlab/glfm_markdown.rb` は GitLab のソースの写しで、書き換えない。GitLab の描画に合わせる直しは、gem の版を上げて取り込む
- フィルターの順序は GitLab の `GfmPipeline` に合わせる。消毒より前のフィルターを消毒の後ろへ動かさない。配色（SyntaxHighlight）は最後に置き、その後ろに消毒を掛けない。消毒の後ろのフィルターが足す属性と `style` は、検証した値だけにする
- 消毒は `internal/sanitize` の規則で行う。正規表現で HTML の文字列を直接書き換えて消毒しない
- `test/fixtures/*.gitlab.html` は GitLab が返した HTML そのままで、手で書き換えない。見本の入力（`test/fixtures/*.md`）は合成の文章だけにし、実在の利用者・プロジェクトに解決される参照（`@名前` など）を入れない（GitLab が返す HTML に、その人の名前が入る）
- サーバーは 127.0.0.1 だけで待ち受け、URL に推測できない token を入れる。`Host` と `Sec-Fetch-Site` を確かめ、CORS のヘッダーは付けない。ファイルは `internal/resolve` の `Resolve` でルートの内側の実ファイルに限る（`..`・`\`・Windows の `:`・末尾の点と空白・デバイス名・`.git`・リンクでの脱出を拒む）。これらを緩めない
- Markdown への書き込みは `--edit` を付けたとき（ファイルへ）か、Neovim の橋渡しが `hello` で `edit: 1` を知らせたとき（バッファへ。ファイルには書かない）だけ行う。書く先は要求の本文や URL から取らず、サーバーが描画している文書だけにし、`?doc=` は今までどおり `Resolve` で検査する。`POST _/edit` は token・Host・Sec-Fetch-Site・要求の Host と同じ Origin・JSON の型・本文の上限を確かめる。描画のもとの本文と今のファイルが違えば書かず、バッファへの反映は描画のもとの `buf`・`rev` を Lua が検査する。これらを緩めない
- 独自の形式で受け取る写しは、別のサイトも書けるので信用しない。ページで形と大きさ、`internal/edit` で型と上限、`tableedit` で原文と結合を検査する。写しに端末の絶対パスとサーバーの token を入れない（文書の照合は起動ごとの乱数を鍵にした HMAC を使う）。`text/html` に原文と属性を入れない。表をまたぐ移動も 1 個の op で 1 手にする
- 取り込んだファイルの監視も `internal/resolve` の `Resolve` でルート内を確かめ、文書ごとに32個までにする。失敗した include は、安全な親の下で出現を待つ。編集中の文書の再描画には最後に受けた本文を使う
- `page/lib/` は KaTeX・mermaid の原文の写し。手で直さず、`tools/update-page-libs.mjs` で `lock.json`・ライセンスと一緒に更新する。CSS に残る woff / ttf の参照は変えず、配るフォントは woff2 だけにする
- ページの CSP は `script-src 'self'`。ページに入れるスクリプトとライブラリは、同じサーバーから配る。配るライブラリは `page/lib/lock.json` にあるファイルと、埋め込んだ `sticky/src/sticky-table.js` だけにし、`node_modules` をそのまま見せない
- ページでは、サーバーから届いた文字（見出し・知らせ・ファイル名・編集の UI の知らせやセルの文字）を `textContent` で入れる。入力欄には `value` で入れる。HTML として入れてよいのは、`pipeline.Result` の `HTML` だけ
- `engine/glfm_markdown.wasm` を手で差し替えない。`engine/build.sh --update` で作り、`engine/engine.lock.json` と一緒にコミットする

## 構成

- `go.mod`・`go.sum` — `mdpreview` と `mdtable` で共通の Go のモジュールと依存の固定値。`go` の行は修正版まで書き、配布のビルドに使う Go の版を固定する
- `engine/engine.go` — 埋め込んだ `.wasm` を wazero で動かす Go の描画エンジン。出力は消毒していない。取り消し・異常終了後とメモリー上限超過時に実体を作り直す
- `engine/testdata/golden.json` — Node.js 版が作った描画結果の見本。出力とバイト単位で比べる
- `page/embed.go` — ページと `page/lib/` の資産の埋め込み。Go のソースは含めない
- `cmd/mdpreview/` — コマンド。描画エンジンを起動ごとに1回だけ用意し（`--help`・`--version`・`--licenses`・`install-style` では用意しない）、PATH の `plantuml` と公式スタイルを、プレビュー・Neovim の JSON 行・書き出しにつなぐ
- `mdtable/` — Markdown の表を Excel で編集する `mdtable`。決まりは `mdtable/AGENTS.md`
- `sticky/` — GitLab の表を固定するブックマークレット md-table-sticky。`mdpreview` は `sticky/src/sticky-table.js` を埋め込んでページに配る。決まりは `sticky/AGENTS.md`
- `internal/version/` — `mdpreview` と `mdtable` に共通の既定の版と、ビルド時に差し替える変数
- `licenses.go`・`licenses/` — `mdpreview` に入るもののライセンスの原文の同梱と、`mdpreview --licenses` の出力。`licenses/manifest.json` は出す順の一覧（名前・版・種類・原文の場所）、`licenses/go/` は Go 本体と Go のモジュールの原文の写し（書き換えない）。`mdtable` の一覧と原文は `mdtable/licenses/` に置く。描画エンジンの crate の原文は `engine/licenses/`（`engine/licenses.go` が埋め込む）、KaTeX・mermaid の原文は `page/lib/` を、元の場所のまま使う
- `scripts/build-release.sh` — 両コマンドの配布用のビルド。`mdpreview` は `windows/amd64`・`linux/amd64`・`linux/arm64`、`mdtable` は `windows/amd64` 向けの実行ファイルを、同じ設定（CGO 無し・`-trimpath`・版の埋め込み）で作る。各コマンドのライセンス全文と、6 ファイルの `SHA256SUMS` も作る。GitHub Actions の検査と配布の両方が呼ぶ
- `.github/workflows/test.yml`・`test-mdtable.yml` — `mdpreview` 側と `mdtable` 側の検査（ubuntu・windows）。両コマンドの配布物のビルドと sha256 の検査は `test.yml` が行う。`test.yml` の「sticky の検証」は Node.js 22 で格子・組み立てとブックマークレットの生成物を検査する
- `.github/workflows/release.yml` — `v` + 数字3つのタグで、両コマンドを 1 つの GitHub Release に配布する。検査とビルドを通し、実行ファイル4つ・`mdpreview-THIRD-PARTY-LICENSES.txt`・`mdtable-THIRD-PARTY-LICENSES.txt`・`SHA256SUMS` を載せる
- `internal/plantuml/` — 絶対パスの PATH の探索、原文を標準入力だけに渡す手元の描画、共有・直列の待ち行列・成功のキャッシュ。取り消し・時間切れ・終了では子孫も止める
- `assets/embed.go` — 公式資産そのものを含めず、`assets/assets.lock.json` の元のバイト列だけを埋め込む
- `internal/assets/` — 固定した公式資産の取得・CSS の相対化・一時領域からの入れ替えと、通信しない導入状態の検査。検査済みのバイト列の写しだけを許可リストで渡す
- `internal/resolve/`・`internal/documents/` — URL と実パスの閉じ込め、Markdown の一覧。Windows のジャンクションは OS のハンドルから最終パスを取得する
- `internal/watch/` — 標準ライブラリの定期確認による変更・削除・再作成の監視と、存在しない include の親の検査。書かれた直後に見たファイルは中身のハッシュを覚え、更新時刻の刻みが過ぎてから1回だけ確かめ直す（同じ大きさ・同じ更新時刻の書き換えを拾う）
- `internal/fileopen/` — 文書・include・リポジトリのファイルの読み取り。Windows では、読んでいる間も名前の変更と削除を許して開く（標準の `os.Open` は許さない）
- `internal/session/` — 文書ごとの最新描画、描画の番号と原文・行対応の保持、状態を変えない試しの描画とファイルの即時読み直し、取り消しと時間制限、ファイル文書・include・図の保持と破棄。描画器はインターフェースで受け取り、試験では偽物を使う
- `internal/edit/` — 描画と原文の表の対応づけ、表・範囲・チェックボックスの編集、写しの検査と文書の照合値、ファイルの再検査と書き戻し、文書ごとの差分履歴。Neovim のバッファではファイルに触れず、変更を 1 個の行の置き換えへまとめ、編集元の応答を待つ。試しの描画は最終 HTML の比較にだけ使う
- `internal/pagefiles/` — 埋め込みページの設定と、固定情報の大きさ・sha256 を照合するライブラリの読み取り
- `internal/server/` — 127.0.0.1 の HTTP と SSE。要求は `net/http.ReadRequest` で解析し、パス整理・接続再利用・Expect の自動応答を使わない
- `internal/stdio/` — Neovim との版1の取り決め。任意の編集対応の通知、バッファの行の置き換え要求と結果の照合、時間切れ・休止・終了による応答待ちの解除も扱う
- `internal/cli/` — コマンドの引数、ブラウザ起動と終了処理、`render`・`install-style` の入口。描画エンジンの用意・PlantUML・公式スタイルは外から渡す関数で接続する
- `internal/export/` — 単一 HTML の書き出し。入力の検査、ルート内の相対 URL の書き換え、本文と印刷用スタイル、文書に必要な KaTeX・mermaid と woff2 の埋め込み、スクリプトごとの CSP のハッシュ、PlantUML の図の埋め込みと原文への復帰、一時ファイルからの入れ替え。`extras.js` は書き出した HTML の中で数式と図を描くスクリプト
- `internal/pipeline/` — 描画の流れ（消毒・フィルター・配色・取り込み・知らせ）と、展開・front matter・CR を考慮した原文の行対応。`api.go` の型と入口（`Renderer.Render`）をほかのパッケージが使う。処理と状態は文書ごとに持ち、取り消しと深さの上限を確認する
- `internal/htmltree/`・`internal/sanitize/`・`internal/filters/` — HTML の木の操作と GitLab の許可リスト、消毒前後のフィルター
- `internal/highlight/` — Chroma の字句と Rouge 系の短いクラス、行ごとの包み。使う言語だけ遅延して準備する
- `internal/include/`・`internal/paths/`・`internal/notices/` — include の展開と原文・展開後の双方向の行対応、相対パス、完成した木を変えない知らせの収集
- `internal/plantumlsource/` — 図の原文の SHA-256 と標準入力の開始・終了の補完
- `internal/pagetest/` — ブラウザの試験（`test/page.test.mjs`）だけが使う、試験用のサーバーのコマンド。`internal/server` を、標準入力の JSON の行で動かす（偽の文書の状態・本物の `internal/session` と描画・偽の図・合成の公式資産を切り替える）。製品の実行ファイルには入らない
- `internal/pipeline/fixtures_test.go`・`internal/pipeline/testdata/known_differences.json` — GitLab の本物の出力（`test/fixtures/*.gitlab.html`）との比べ。許した5つの正規化のほかは、要素・属性・文字を厳密に比べる。GitLab のデータや設定が要って直せない差だけを、場所・理由・両側の値を固定して表に置く
- `internal/pipeline/testdata/golden/pipeline.jsonl.gz` — Node.js 版（`80784dd`）の `renderDocument` の入力と出力 2,715 件（Node.js 版の試験が通した入力と、公開の文書）。出力を、HTML は木で、ほかは JSON で比べる見本。手で書き換えない

- `engine/` — 描画エンジンの Rust の crate。`src/glfm.rs`・`src/formatter.rs` は gem の写し、`src/lib.rs` は WebAssembly から呼ぶ入口（Ruby の橋渡しの代わり）。`gitlab/glfm_markdown.rb` は GitLab がこの gem に渡すオプションのソースの写し（`go test ./engine` が `engine/engine.go` の `GitLabOptions()` と比べる）、`glfm_markdown.wasm` はビルドした成果物、`engine.lock.json` は GitLab・gem・comrak・コンテナのイメージ・`.wasm` の固定値、`licenses/` は `.wasm` に入る crate のライセンスの原文、`build.sh` は作り直す手順
- `page/` — ブラウザの表示。文書の移動・一覧・見出し欄・表の操作・GitLab との違いの知らせ・印刷の表示を扱い、KaTeX・mermaid・md-table-sticky は同じサーバーから読む。`preview.js` は本文の写しと画面の対応で差分更新し、変わらない要素と表の操作状態を残す。md-table-sticky の内部への依存を表の固定の節へ集め、食い違いでは全置換へ戻す。`edit-grid.js` は DOM に頼らない範囲・TSV・HTML・写しの計算を `window.gpEditGrid` にまとめ、Node で試す。`edit.js` は `window.gpPreview`・`gp:render`・`gp:stop` を使い、層を重ねて表とブロックの原文を編集し、編集できるタスクのチェックボックスを編集モード中だけ有効にする
- `page/lib/` — ブラウザに配る KaTeX・mermaid と原文のライセンス。`lock.json` に版、npm の integrity、各ファイルの sha256・大きさを固定する
- `tools/update-page-libs.mjs` — 開発用。`node_modules` の KaTeX・mermaid の原文を写し直し、`--check` で固定情報・原文との一致を検査する
- `docs/install.md` — AlmaLinux 10・Windows 11 の導入・更新の手順
- `docs/images/` — README に載せるプレビューの画面（ライトの配色は `preview-bundled.png`・`preview-official.png`、ダークの配色は `preview-bundled-dark.png`・`preview-official-dark.png`）、比べるための gitlab.com の画面（`gitlab.png`・`gitlab-dark.png`）と、そのもとの合成の Markdown（`sample.md`）。プレビューの画面は、`mdpreview docs/images/sample.md`（同梱のスタイルは `--no-official-style` を付ける。公式スタイルは、先に `mdpreview install-style` で取得しておく）を、幅 640 px・倍率 2 のブラウザで開き、数式と図が描けてから、操作欄の上端から本文の終わりまでを撮ったもの（6 枚は、いちばん長いものの高さに揃える。並べたときに上下がずれないように）。ライトとダークは、ページの「配色」を「システム」のままにして、ブラウザの配色の設定を切り替えて撮る。表示を変えたら、プレビューの 4 枚を撮り直す。gitlab.com の画面は、公開のプロジェクトに `sample.md` を置き、そのファイルの画面をログインせずに開いて、ファイルの枠（名前の帯と本文）の幅が 640 px になるように画面の幅を合わせ、倍率 2 で、数式と図が描けてから、枠の上端から、プレビューの画面と同じ高さまでを撮ったもの。置くときのコミットには GitLab の非公開用のアドレスを使い、撮った後にプロジェクトを消す。ファイルの画面を続けて開くと、ログインなしの通信の回数の制限（429）に当たるので、間を空ける
- `tools/make-fixtures.mjs` — 開発用。見本の入力を gitlab.com の Markdown API に送り、返った HTML を保存する。アプリからは呼ばれない
- `test/` — ブラウザの試験と、その補助。`test/fixtures/` は、合成の Markdown（`<名前>.md`）、GitLab の本物の出力（`<名前>.gitlab.html`）、取得した条件（`fixtures.json`）

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
5. `engine/engine.go` の `GitLabOptions()` を、写した `glfm_markdown.rb` の `OPTIONS` に合わせる（`go test ./engine` が比べる）

## KaTeX・mermaid の版を上げる

実行時は `page/lib/` の写しを使い、KaTeX・mermaid の `node_modules` は要らない。更新のときだけ開発依存を導入する。

1. `npm install --save-dev --save-exact katex@<版> mermaid@<版>` で版を固定し、`package.json` と `package-lock.json` を合わせる
2. `node tools/update-page-libs.mjs` で JS・CSS・woff2 と原文のライセンスを写し、`page/lib/lock.json` を書き直す。CSS は原文のままにする。今のブラウザは先頭の woff2 を選ぶため、woff / ttf は置かない
3. `node tools/update-page-libs.mjs --check`・`go test ./...`・`node --test test/page.test.mjs` を実行する。Playwright の試験では、本物の数式・図と外部への要求がないことも確かめる
4. 写し・ライセンス・`lock.json`・npm の固定情報を一緒にレビューし、同じ変更に含める。`page/lib/**` は `.gitattributes` の `-text -whitespace` で原文のバイトを保つ

`--check` は開発依存がない導入でも sha256 と大きさを検査する。開発依存がある場合は、その版と原文にも比べる。

## 見本を作り直す

GitLab の描画が変わったか確かめるとき、見本の入力を足したときに行う。試験（`go test ./...` と `node --test test/page.test.mjs`）は GitLab に繋がない。

```sh
GITLAB_TOKEN=<read_api のトークン> node tools/make-fixtures.mjs          # 全部
GITLAB_TOKEN=<read_api のトークン> node tools/make-fixtures.mjs <名前>   # 1 つだけ
```

- 送るのは `test/fixtures/*.md` だけ。トークンは環境変数から読み、表示も保存もしない
- 作り直した後、`<名前>.gitlab.html` に実在の利用者の名前などが入っていないか確かめる（`class="gfm`・`data-user` を探す）
- `go test ./...` で比べ、`node --test test/page.test.mjs` でブラウザの表示も確かめる。合わない所は実装を直す。GitLab のデータや設定が要って直せない所だけを、`internal/pipeline/testdata/known_differences.json` の表に、場所と理由を書いて入れる

## テスト

Go 1.27.3 で、次のコマンドで確かめる。Go のソースを直したら `gofmt -w <ファイル>` で整形し、`gofmt -l .` の出力が空であることを確認する。

描画エンジンが出す HTML の属性の並びは、呼び出しの履歴で変わる。並びに頼る処理・試験を書かない。
描画の見本は、HTML を木で、見出し・取り込み・知らせ・図を JSON で比較する。配色の字句の span だけを除き、行の span と包み・文字は比較する。`<plaintext>` の後ろにエンジンの属性順が文字として出る2入力だけは、入力の SHA-256 で限定し、文字内の開始タグの属性順をそろえる（不要になった例外は試験で知らせる）。見本そのものは変えない。

```sh
go vet ./...
go test ./...
go test -race ./...
gofmt -l .
```

`./...` は `mdtable/` のパッケージも含む。`mdtable/` の `-race` は時間がかかるので、まとめて動かすときは `-timeout 20m` を付ける。GitHub Actions は、`mdtable/` を除いた検査（`test.yml`）と、`mdtable/` だけの検査（`test-mdtable.yml`。`mdtable/**`・`internal/version/**`・`go.mod`・`go.sum`・`LICENSE`・`.gitattributes` が変わったときだけ）に分けて動かす。`mdtable/` だけを確かめるコマンドは `mdtable/AGENTS.md` にある。

ルートを受け取る関数（`resolve.Resolve`・`documents.ListMarkdown`・`session.FileContext`・`watch.IncludePath` など）は、ルートが実パスであることを前提にする。試験で一時フォルダーをルートにするときは、`resolve.RealPath(t.TempDir())` で実パスにしてから渡す（Windows の一時フォルダーのパスには、8.3 形式の短い名前が入ることがある）。

`sticky/src/sticky-table.js` を変えたら、`sticky/` で `npm run build`（`dist/` を作り直す）と `npm test`、ルートで `go test ./...` と `node --test test/page.test.mjs` を通す。

依存を足す・上げるとき、Go の版を上げるときは、各実行ファイルの `licenses/manifest.json` と `licenses/go/` の原文の写しも合わせる。試験は `go list -deps` で `mdpreview` の windows/amd64・linux/amd64・linux/arm64 の依存の和と、`mdtable` の windows/amd64 の依存を求め、それぞれの一覧と比べる。両一覧の和はルートの `go.mod` の直接・間接の require と比べ、原文とほかの部品の版は Go のモジュールのキャッシュ・`engine/Cargo.lock`・`page/lib/lock.json` と比べる。版を上げるときは、`internal/version/version.go`、`licenses/manifest.json` の md-preview、`mdtable/licenses/manifest.json` の md-table-excel、`package.json`・`package-lock.json` の版を同じ値にする（配布の workflow は、タグと `version.go` の版が違うと止まる）。

`GOPROXY=off` で試験する前に、通信できる環境で一度 `go mod download` を実行し、固定した依存をキャッシュへ用意しておく。

`-race` には C のコンパイラーが要る。Go の試験は描画の見本、GitLab のオプション、異常終了・取り消しからの復帰、メモリー上限、同時呼び出し、埋め込んだ資産の大きさと sha256、コマンドの出力を確かめる。

`internal/plantuml/` の試験は `go test` 自身の実行ファイルを偽のコマンドとして使い、本物の PlantUML・Java を起こさない。`internal/assets/` の試験は一時の保存先と 127.0.0.1 の偽のレジストリを使う。本物の保存先には触れず、公開レジストリへの試験用の要求も手元へ置き換える。`GOFLAGS=-mod=readonly`・`GOPROXY=off` と、作業ツリー内の `GOCACHE` で実行する。

ブラウザの試験と、残す JavaScript の構文は次のコマンドで確かめる。開発用に Node.js 22 以上が要る。

```sh
node --test test/page-libs.test.mjs
node --test test/edit-grid.test.mjs
node --test test/page.test.mjs
for f in page/*.js test/*.mjs tools/*.mjs; do node --check "$f"; done
git diff --check
```

- Go の試験は消毒・フィルター・配色・include・知らせ・悪意のある入力、パスの境界、HTTP / SSE、文書の保持と監視、JSON 行の入出力、PlantUML・公式資産、HTML への書き出し・CLI を確かめる。GitLab の本物の出力との比較は `internal/pipeline/fixtures_test.go` にある。既知の差の表は件数・見本の名前・種類・場所・理由・固定した値も検査する
- `test/page-libs.test.mjs` — 合成の npm パッケージで、原文のバイト・版と integrity の固定・改変と一覧にないファイルの拒否を確かめる。`lock.json` に md-table-sticky があれば拒む。公開レジストリへ接続しない
- `test/page.test.mjs` — サーバー（`internal/pagetest` を試験の最初に `go build` して子プロセスで起こす）・本物の KaTeX・mermaid によるブラウザ試験。書き出しの試験は `mdpreview render` を使う。Go が無い場合も理由を付けて skip する。文書の移動・戻ると進む・一覧・見出し欄・違いの知らせの安全な表示と行への移動・include の再描画・印刷と、外部要求の試みがないことを確認する。見本の行の変更と表のセル・行の更新後の木を新しく開いたページと比較し、要素・details・数式・図の保持、全置換への復帰、固定・検索・全幅表示中の表の記録と内側の span、`gpPreview` の表の順・スクロール保持・編集時のジャンプ停止も確かめる。書き出した単一 HTML を `file://` で開き、数式・図・配色、CSP 違反・外部要求が無いことも確かめる。合成の公式資産と偽の図で、公式スタイルの読み込みと失敗時の復帰、PlantUML の表示・失敗・使い回しを確かめる。Playwright（`PLAYWRIGHT_MODULE` または `playwright`）と Edge / Chrome / Chromium がない場合は理由を付けて skip する。ブラウザの場所は `EDGE_BIN`・`CHROME_BIN` で指定できる
- `test/edit-grid.test.mjs` は範囲・TSV・HTML・写しの計算を DOM なしで確かめる。`internal/edit/testdata/page-fragments.json` は `go test ./internal/edit -update` で作り直し、`tableedit.Copy` と JavaScript の写しを比べる
- `test/page.test.mjs` の編集では、表の範囲の選択・メニュー・クリップボード・移動・キー・貼り付け・行列・HTML 原文・取り消し、段落・見出し・リスト・引用・コードの原文、タスクの切り替え、CRLF・合成した IME・競合を実ファイルの内容まで比べる。固定・検索・全幅表示の記録とチェック切り替え後の要素の保持、編集の知らせを HTML として扱わないこと、Neovim へ送る段落・タスクの行の置き換えも確かめる。実際の IME の操作は未検証
- `test/helpers.mjs` — 作業ツリー内の一時ファイル、試験用サーバーと書き出しコマンドのビルド・起動・終了、JSON 行の応答待ちの補助。`startGoPageServer` の `edit: 'file'` と `live: true` で実ファイルの編集を試せる
- `test/assets-fixture.mjs` — ブラウザの試験で使う合成の公式資産と固定情報の作成
- 描画に関わる変更は、試験に例を足す。GitLab の出力と比べて確かめたことだけを「同じ」と書く

## 文書の移動と一覧

- `/<token>/` は編集中の文書、`?doc=<ルートからの相対パス>` はファイルの文書。`_/events?doc=...` はその文書の描画だけを送り、スクロールは編集中の文書のタブだけへ送る。ファイルのパスは `internal/resolve` の `Resolve` で検査する
- Markdown の拡張子は `md`・`markdown`・`mdown`・`mkd`・`mkdn`。同じファイルを編集している場合は未保存の本文を使い、別ルートへ移ったファイル文書のタブには `reload` を送る
- 見張るファイル文書は32件まで。最後のタブが離れて5秒後に監視と結果を捨てる。上限に達した場合は、タブのいない古い文書から捨てる
- `_/list` はルート内の Markdown の一覧を JSON で返す。`?list` で一覧の画面を出す。深さ20・5000件までで、隠しファイル・`node_modules`・フォルダーのリンクは除く。無名の文書だけでルートがないときは空
- フォルダーを引数にした場合はその場所をルートとし、最初に一覧を出す。`--root` とは併用しない
- 見出し欄の開閉は配色・本文幅と同じ sessionStorage に保存する。文字は textContent で入れ、印刷時は見出し欄と操作欄を隠す

## Neovim との取り決め

- 取り決めの版は1。`mdpreview --stdio [--port <番号>]` の標準入力と標準出力を、UTF-8 の JSON オブジェクト1行ずつ（区切りは `\n`）でつなぐ。標準出力に人向けの文は書かない。ブラウザの起動は Lua が行う
- Lua からは、最初に `hello`（`protocol: 1` と省略できる `appearance`・`edit: 1`）、続いて `doc`・`scroll`・`pause`・`resume` を送る。`edit` は数値の 1 のときだけ有効にし、ほかの値は省略と同じ扱いにする。休止中の本文とスクロールは捨て、再開後の本文で描画を戻す。休止してもプロセス・サーバー・URL は保つ
- アプリからは `ready`（版・アプリの version・URL）を1回、`clients`（タブ数。最初は0）、`jump`（今の文書の path と行）、`error`（日本語1行）を返す。`hello` に `edit: 1` があったときだけ、`ready` の最後に `edit: 1` を加える。省略する古い相手への出力は変えず、編集の経路と設定を有効にせず、`edit` も送らない。hello 前の行と対応しない版は `fatal: true` と終了コード2にする
- `doc` は `path`（絶対パスまたは `null`）・`name`・`text`・`rev` に、省略できる `buf` を加えられる。`buf` は正の安全な整数（1〜9007199254740991）だけを受け付ける。`text` はバッファの行を `\n` でつなぎ、末尾に `\n` を 1 個付ける。編集できるのは `buf` のある編集中の文書だけで、設定は `{scope: "current", undo: false}`。ファイルへ書かず、保存は利用者の `:w`、取り消しは Neovim の `u` で行う
- 編集に対応する相手へは、アプリが `edit`（要求ごとに増える正の `id`・`jump` と同じ `path`、無名なら `null`・`buf`・描画のもとの `doc` の `rev`・`edits`）を送る。`edits` は今は必ず 1 個の `{start, end, lines}` で、行は 0 始まり、`end` は含まず、`lines` は改行を含まない文字列の配列（削除は空配列）。CR はそのまま残し、1 行は区切りを含めて 8 MiB までにする
- Lua は `buf`・`path`・`rev` を確かめてバッファの行を置き換え、`edit_result`（同じ `id`・真偽の `ok`・省略できる日本語1行の `message`）を返し、反映後の新しい `doc` を送る。`id` は正の安全な整数、`ok` は真偽を必須とし、誤った行は `error` で知らせる。知らない番号・済んだ番号の結果は黙って捨てる。`message` は文字列だけを使い、改行以降を捨て、200文字までにする。拒否は HTTP 409 の `rejected` とし、省略時は既定の知らせを使う
- 編集の応答待ちは既定で 5 秒。時間切れは HTTP 504 の `timeout` として返し、Neovim に反映されたかを決めず、次の描画を待つ。休止中の編集と休止時の応答待ちは 409 の `paused`、終了時は 503 の `closed` で終え、サーバーを閉じる前に待ちを解除する。`resume` は新しい編集を受けられる状態へ戻す。出力の待ち行列へ積めなければ 503 の `busy`、編集の行が大きすぎれば 413 の `large` にする
- 入力の1行は96 MBまで。誤りのある行は捨てて続ける。標準入力が閉じたらタブに `stop`（`reason: 'exit'`）を送り、描画を取り消し、サーバーと描画エンジンを閉じて終了コード0で終わる
- `CONFIG.jump` が真で、編集中の文書を見ている場合だけ、本文のダブルクリックから `POST _/jump` に行番号を送る。サーバーは token・Host・Sec-Fetch-Site・Origin・JSON の型と1 KBの本文上限を確かめる。単体のファイル表示では無効

## 書き方の規則

- コードのコメントと利用者に出すメッセージは日本語。まわりのコードの書き方に合わせる
- ソースは LF。文書（README・docs）は日本語。検証していないことを「動く」と書かない
- 検証記録は、Forgejo の非公開リポジトリ `setup-notes-verification` の `repos/md-tools/docs/verification/` に置く。このリポジトリからは URL で指さず、「`<検証記録のパス>` の「<見出し>」」と文字で書く
- 公開のリポジトリなので、ホスト名・アドレス・機材など、環境が分かる値を書かない
