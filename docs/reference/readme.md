# md-preview の設計

`mdpreview` の設計の判断と理由。導入は [導入と更新](../install.md)、操作は [使い方](../usage.md)、開発で守ることは [AGENTS.md](../../AGENTS.md) にある。

## 描画の流れ

描画は、Markdown を HTML にする段、消毒とフィルターの段、ブラウザに表示する段に分ける。プレビューと HTML への書き出しは、同じ [internal/pipeline](../../internal/pipeline/pipeline.go) の `Renderer.Render` を通す。表示先によって消毒や構文の扱いが変わることを避けるため。

1. CR と front matter を処理し、`::include{file=…}` を展開する
1. GitLab の gem `gitlab-glfm-markdown` のコードを WebAssembly にした [描画エンジン](../../engine/engine.go) で HTML にする
1. HTML を木として読み、コードの言語・JSON の表など、GitLab で消毒より前に置かれるフィルターを通す
1. [消毒の規則](../../internal/sanitize/sanitize.go)で、許可していない要素・属性・リンクを除く
1. 数式・図・目次・脚注などを処理し、最後にコードを配色する
1. HTML と、見出し・取り込みの記録・GitLab との違いの知らせを返す

HTML の木は `golang.org/x/net/html` で扱い、[internal/htmltree](../../internal/htmltree/tree.go) が取り消しと深さを検査する。入れ子は 512 段までとし、超えると `入れ子が深すぎるため、描画できません。` を返す。消毒前後の処理は [internal/filters](../../internal/filters/)、最後の配色は [internal/highlight](../../internal/highlight/highlight.go) が受け持つ。Chroma の字句を GitLab と同じ短いクラスに対応づけ、包みと行の span も GitLab と同じ形にする。

エンジンの出力だけでは消毒が済んでいない。HTML を文字列の置き換えで直さず、木に対して許可リストを適用する。フィルターの順序も GitLab に合わせ、消毒の後ろで足す属性をそれぞれ検査する。コードの配色が作る要素と属性は、その段で値を制限する。

描画の関数は、include の読み込みとメディアの存在確認を、呼ぶ側から渡す関数に任せる。Markdown の処理とファイルを読む権限を分けるため。[session.FileContext](../../internal/session/files.go) は、同じパス検査を通したルート内のファイルだけを、この関数へ渡す。

## GitLab に合わせる範囲と確かめ方

構文解析と整形は、似た解析器で作り直す代わりに GitLab の gem のコードを使う。`glfm.rs`・`formatter.rs` の写しは変更せず、WebAssembly から呼ぶ入口を付ける。`.wasm` は実行ファイルに埋め込み、`engine` パッケージが wazero で動かす。gem に渡すオプションの元になる GitLab のソースも保存し、試験で値を比べる。固定した版とハッシュは [engine.lock.json](../../engine/engine.lock.json) にある。

その後ろの Ruby のフィルターは Go で実装する。GitLab の利用者・プロジェクト・権限や外部の図サービスが必要な処理は、[internal/pipeline](../../internal/pipeline/pipeline.go) に省く理由を記す。対応する処理が無い場所を、見た目だけで解決したことにはしない。

PlantUML は外部の図サービスを使わず、PATH の絶対パスの項目にある手元の `plantuml` を使う。[フィルター](../../internal/filters/pre.go)は GitLab と同じく消毒の前にコードブロックを img に変え、原文と sha256 の ID を集める。コマンドが無い場合と `--no-plantuml`、原文が 256 KiB を超える図は、原文のまま残す。

[描画する部品](../../internal/plantuml/plantuml.go)は原文を標準入力にだけ渡し、`SANDBOX` と Java の headless を指定して一時フォルダーで起こす。`SANDBOX` は、環境変数 `PLANTUML_SECURITY_PROFILE` と、`JAVA_TOOL_OPTIONS` の最後に足す同じ名前の Java のプロパティの両方で指定する（PlantUML はプロパティを先に読むので、利用者の `JAVA_TOOL_OPTIONS` に別の値があっても `SANDBOX` になる）。原文は、PlantUML のサーバー（GitLab が図を送る先）と同じく、先頭が `@start` でなければ `@startuml` と `@enduml` で包む。構文に誤りのある図は、PlantUML が誤りを描いた SVG を返すので、GitLab と同じくその図を見せる。SVG でない出力（原文に `@@@format png` の行があるときなど）は失敗として扱う。描画は直列で、同じ ID の待ちは共有し、成功した SVG だけをメモリーに覚える。時間切れ・中止・終了では子のプロセスを木ごと止める。プレビューは token・Host・Sec-Fetch-Site を検査した経路から SVG を sandbox 付きで返し、ページでは img として表示する。SVG を DOM に入れず、失敗時は原文のコードブロックへ戻す。

[見本との比較](../../internal/pipeline/fixtures_test.go)は、合成の Markdown を GitLab が描いた HTML と、手元の最終 HTML を木にして比べる。属性の順、遅延画像、脚注の乱数、絵文字の囲み、コードの配色の字句は、試験で列挙した差だけを正規化する。それ以外の既知の差は、[known_differences.json](../../internal/pipeline/testdata/known_differences.json) に、文書・木の場所・理由・両側の値を固定する。参照のリンクを一律に文字へ戻すような、違いを隠す正規化はしない。

見本を作る [開発用スクリプト](../../tools/make-fixtures.mjs)と、保存済みの見本を読む試験を分けている。試験のたびに GitLab に接続せず、同じ入力と出力で比べるため。合わせた範囲と GitLab との差は [README](../../README.md#消毒とフィルター) にある。

## サーバーとファイルを読む範囲

プレビューは [HTTP サーバー](../../internal/server/server.go)を手元に起こす。127.0.0.1 だけで待ち受け、URL に毎回 16 バイトの乱数を 32 桁の 16 進で入れる。ループバックへの待ち受けだけに頼らず、`Host` と `Sec-Fetch-Site` も検査し、CORS の許可を返さない。別のサイトから手元の文書へアクセスさせないための境界にする。

サーバーは接続を自分で受けて `net/http.ReadRequest` で読む。パスの自動整理・接続の再利用・Expect の自動応答を通さず、token は定数時間で比べる。`Host` が複数ある要求と、行頭の空白で折り返したヘッダーは 400 で拒む。

ファイルを開く入口は [internal/resolve](../../internal/resolve/resolve.go) の `Resolve` に集める。URL のパスを戻し、`..`・バックスラッシュ・`.git`、Windows のドライブやデバイス名として扱われる書き方を拒む。さらに実パスを求めて、ルートの外へ出るリンクも拒む。文字列の上でルートの下に見えるだけでは、配信を認めない。

数式と図のライブラリも、同じサーバーから配る。配信対象は [page/lib/lock.json](../../page/lib/lock.json) にあるファイルに限り、`node_modules` をそのまま公開しない。ページの CSP は `script-src 'self'` とし、文書が任意のスクリプトを読み込む入口を作らない。

## 文書の状態と取り込みの見張り

描画は [internal/session](../../internal/session/session.go) が管理し、文書の更新をまとめる。プレビューの描画は 15 秒、HTML への書き出しは 60 秒の `context` の期限で止める。描画の各段と WebAssembly の実行に取り消しを伝え、時間切れの後も描画を受け付ける。

編集中の文書と、相対リンクや一覧から開いたファイル文書は別の状態を持つ。描画の通知は文書ごとに送り、エディタのスクロールは編集中の文書のタブだけへ送る。別の文書を読んでいるタブを、編集中の文書の位置へ動かさないため。

- ファイル文書の監視は 32 件まで。最後のタブが離れて 5 秒後に監視と結果を捨て、読まれていない文書を持ち続けない
- include の展開は [internal/include](../../internal/include/include.go) で行う。32 回まで・再帰しない・展開後 16 MiB までに制限し、取り込み先からさらに読み込みを広げない
- 取り込み先の監視も、ルートの内側かを検査して文書ごとに 32 個までにする。無いファイルは安全な親の下で出現を待ち、作成されたときにも描き直す
- 編集中の文書を描き直すときは、最後に受けた本文を使う。include が変わったときに、未保存の内容をディスク上の古い本文に戻さない

見張りのパスの検査は [internal/watch の IncludePath](../../internal/watch/watch.go)、置き換え保存や削除・作成への追従は [watch.go](../../internal/watch/watch.go) に分けている。100 ミリ秒ごとに、更新時刻・大きさ・ファイルの同一性を確かめる。書かれてから 2 秒の内に見た 32 MiB までのファイルは、中身のハッシュを覚え、更新時刻の刻みが過ぎてから 1 回だけ確かめ直す。同じ更新時刻・同じ大きさでの書き換えを拾うため。

文書・include・配信するファイルの読み取りには [internal/fileopen](../../internal/fileopen/open_windows.go) を使う。Windows では、読んでいる間も名前の変更と削除を許して開く。置き換え保存を読み取り中のハンドルで妨げないため。

## ブラウザの表示

[ページ](../../page/preview.js)は、消毒済みの本文と、見出し・知らせ・ファイル名を別に受け取る。本文以外は `textContent` で入れる。原文を抜き出した知らせには HTML が混ざることがあるため、見出しや通知の表示を通じて HTML として解釈させない。

GitLab との差を知らせる [internal/notices](../../internal/notices/notices.go) は、完成した木を変えない。差の説明を加えるために、比較する本文の構造を変えないため。include の展開後の位置も原文の行へ対応させ、知らせから本文へ移る位置に使う。

配色・本文幅と、見出し欄・知らせの欄の開閉は `sessionStorage` に保存する。印刷用の CSS では操作欄・見出し欄・知らせを隠し、本文を印刷する。[preview.css](../../page/preview.css) は、プレビューの操作と本文のスタイルを分けて持つ。

## Neovim との取り決め

Neovim の橋渡しは [LazyVimStarter](https://github.com/ryo-aoki-pc/LazyVimStarter) が持ち、アプリは [internal/stdio](../../internal/stdio/stdio.go) で受ける。ファイルの保存を待たずに本文とカーソル位置を受け取るため、`mdpreview --stdio` の標準入力・標準出力に UTF-8 の JSON を 1 行ずつ流す。標準出力には、人が読む案内を混ぜない。

- 取り決めの版は 1。最初の `hello` で版と表示の指定を受け、`ready` で URL を返す。ブラウザは Neovim 側で開く
- `doc` と `scroll` で本文と位置を受ける。休止中の入力は捨て、再開後に受けた本文で描画を戻す。休止しても URL は保つ
- `clients` でタブ数、`jump` で文書のパスと行を返す。ダブルクリックによる移動は、編集中の文書を見ているときだけ有効にする
- 入力の 1 行は 96 MB まで。誤りのある行は捨て、標準入力が閉じたら描画を取り消し、サーバーと描画エンジンも閉じる

ダブルクリックの要求にも、token・送信元・JSON の型と本文の大きさを検査する。ブラウザが渡した任意のパスを、そのままエディタで開く取り決めにはしない。細かな入力と終了の規則は [AGENTS.md](../../AGENTS.md#neovim-との取り決め) にある。

## 公式スタイル

公式の CSS・フォントは、アプリに同梱して再配布せず、利用者が `mdpreview install-style` を実行した端末で取得する。取得の入口を [internal/assets の Install](../../internal/assets/install.go) に分け、プレビューを起こす操作や導入状態の確認に、取得を伴わせない。

[assets.lock.json](../../assets/assets.lock.json) に、元の版・取得元・大きさ・ハッシュ・ライセンスを固定する。保存先はアプリと別の `md-preview-assets` とし、版と lock のハッシュから決めたフォルダーに置く。実行ファイルを更新しても、取得した資産を巻き込まないため。

[internal/assets の Load](../../internal/assets/loaded.go) は通信せず、lock・導入情報・全ファイルの大きさと sha256 を比べる。パスを検査した上で読み、検査したバイト列だけをサーバーへ渡す。導入情報のファイル名をそのまま配信の許可に使わない。

未導入なら同梱のスタイルを使う。導入後は次に起こしたプレビューから公式の CSS と GitLab Sans を使い、コードの配色はライト・ダーク共通の Dracula にする。公式資産を使えない場合も同梱のスタイルで表示を続ける。

## HTML への書き出し

[internal/export](../../internal/export/export.go) は、プレビューと同じ描画の流れとパス検査を使って、消毒済みの本文を単一の HTML にする。サーバーを起こすための URL や操作欄を含めず、`file://` から読む用途にする。

- 入力は UTF-8 のファイル 1 つ、16 MiB まで。描画は `context` の期限で 60 秒で打ち切り、大きさと処理時間に上限を置く
- 同梱スタイルの本文と印刷用の部分を使い、公式資産は含めない。HTML を渡すために、公式資産の再配布を必要としない形にする
- 文書に数式があれば KaTeX と数式のフォント、mermaid の図があれば mermaid を埋める。必要なときだけ含め、どちらも無ければスクリプトを入れない
- PlantUML は手元で描いた SVG を img の data URL に埋め、包む a のリンク先を外す。描けない図は文字のコードブロックに戻して件数を知らせる。PlantUML のためのスクリプトやサーバーの URL は入れない
- 埋め込む各スクリプトのバイト列から CSP の sha256 を作る。本文が加えた別のスクリプトを動かす許可にはしない
- ルート内の相対 URL を、書き出し先から見た相対パスに直す。メディアをコピーしたり、リンク先の Markdown をまとめて HTML にしたりはしない
- 書き出し先がルートの外のときは、相対 URL を直さない。書き出し先から見た形にすると、ルートより上のフォルダーの名前（利用者名など）が HTML に入り、渡した相手に見えるため
- 入力の Markdown と同じ書き出し先は拒む。引数の書き間違いで、原稿を HTML で上書きしないため
- 出力は隣の一時ファイルへ書き、完成後に入れ替える。作りかけの HTML を出力先へ残すことを避ける

知らせの件数は [CLI](../../internal/cli/cli.go) が標準エラーへ出す。`-o` を省いたときの標準出力には HTML だけを出し、案内が文書に混ざらないようにする。配色の既定は OS に従う `system`、本文幅の既定は全幅の `fluid`。`system` の暗い配色は画面だけに使い、印刷は明るい配色にする。

## 導入

利用する端末には実行ファイルを 1 つ置く。描画エンジン・ページ・数式と図のライブラリ・ライセンスの原文を埋め込み、Go・Node.js・Rust やコンテナを利用時の前提にしない。置き場所は `~/.local/bin/mdpreview`（Windows は `%USERPROFILE%\.local\bin\mdpreview.exe`）。取得したファイルを sha256 で確かめ、導入も更新も同じ手順で置く。

配布は [release.yml](../../.github/workflows/release.yml) が `v` + 数字 3 つのタグで行う。[scripts/build-release.sh](../../scripts/build-release.sh) が `windows/amd64`・`linux/amd64`・`linux/arm64` の実行ファイルを作り、検査を通して GitHub Releases に `SHA256SUMS`・`THIRD-PARTY-LICENSES.txt` と一緒に置く。版は `internal/version/version.go` と [licenses/manifest.json](../../licenses/manifest.json) に同じ値を記す。

同梱しているもののライセンスは [licenses/](../../licenses/) の一覧と原文で管理し、`mdpreview --licenses` で原文を出す。Go 本体とモジュール・描画エンジン・KaTeX・mermaid の版と原文の対応を試験で確かめる。公式スタイルはこの実行ファイルに入れず、利用者が明示して取得する。
