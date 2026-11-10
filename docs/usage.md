# md-preview の使い方（AlmaLinux 10・Windows 11）

- Markdown を GitLab の描画でプレビューする `mdpreview` コマンド（[md-preview](../README.md)）。本文は GitLab に送らず、手元で描く
- 導入と更新は [md-preview の導入と更新](install.md) にある
- どの項も単独で通せる。項のリードの前提を先に満たす
- bash のブロックはブラケットペーストの効く端末に、PowerShell のブロックは PowerShell 7 に貼り、貼った後に Enter を押す。貼った後は、反転表示の「確認」から後ろの出力を箇条書きで確かめる
- PowerShell 7 の導入と実行ポリシーは、setup-notes の [アプリを入れる](https://github.com/ryo-aoki-pc/setup-notes/blob/main/docs/windows-setup.md#アプリを入れる)の手順 6〜9 に従う
- PowerShell の貼り方は [Windows Terminal で複数行を 1 つの入力として貼る](https://github.com/ryo-aoki-pc/setup-notes/blob/main/docs/shell-terminal.md#windows-terminal-で複数行を-1-つの入力として貼る)に従う

<details id="この文書の補足と注意点">
  <summary>この文書の補足と注意点</summary>

**参照**

- 設計は [md-preview の設計](reference/readme.md) にある
- Neovim からは [LazyVimStarter](https://github.com/ryo-aoki-pc/LazyVimStarter) の `<leader>cp` で使う。未保存の内容の表示、カーソルに合わせたスクロール、本文のダブルクリックでエディタの行へ移る操作を扱う
- Neovim の導入は、setup-notes の [LazyVim の GitLab プレビューを導入する](https://github.com/ryo-aoki-pc/setup-notes/blob/main/docs/neovim.md#lazyvim-の-gitlab-プレビューを導入する)・[Windows 11 で LazyVim の GitLab プレビューを導入する](https://github.com/ryo-aoki-pc/setup-notes/blob/main/docs/neovim.md#windows-11-で-lazyvim-の-gitlab-プレビューを導入する)に従う
- 外へ通信する場面は [README](../README.md#外へ通信する場面) にある
- PlantUML のコードブロックは、PATH の絶対パスの項目に `plantuml` があれば手元で SVG にして表示する。コマンドが無い場合、`--no-plantuml` を付けた場合、原文が 256 KiB を超える場合は、コードブロックのまま表示し、「GitLab との違い」の「図」で知らせる
- `--no-plantuml` はプレビュー・`--stdio`・`render` で使える。描画に失敗したときは原文へ戻す。`render` では成功した SVG を HTML の画像に埋め、失敗した図の件数を標準エラーへ知らせる。原文は外へ送らず、図の中から手元のファイルや URL を読む処理も無効にする

</details>

## AlmaLinux 10 で使う

- GNOME にログインした自分のユーザーで行う。前提の項は、各項のリードのリンクから開く

### mdpreview で Markdown をプレビューする

- 試験用の小さな Markdown を作り、ブラウザで表示を確かめる
- 前提: [md-preview を導入・更新する](install.md#md-preview-を導入更新する)を通し、`mdpreview` が PATH にある
- 実行場所: GNOME にログインした自分のユーザーの端末（bash）。`sudo` は使わない

1. 試験用の Markdown を作り、プレビューを起こす。

   ```bash
   (
     # 試験用の Markdown を、専用のフォルダーに作って開く
     preview_dir=$(mktemp -d) || exit
     trap 'rm -rf -- "$preview_dir"' EXIT
     printf '# プレビューの確認\n\n本文です。\n' > "$preview_dir/preview.md" || exit
     printf '\n\033[7m 確認 \033[0m\n'
     mdpreview --no-open "$preview_dir/preview.md"
   )
   ```

   - `プレビュー: http://127.0.0.1:<ポート>/<32 桁の 16 進>/` の形の URL が出ればよい
   - エラーが出たら、中断して [導入の手順](install.md#md-preview-を導入更新する)で導入状態を確かめる

1. ブラウザで、この項の手順 1 に出た URL を開く。

   - 本文に「プレビューの確認」と「本文です。」が出ればよい
   - 操作欄の左に `preview.md` と出ればよい

1. ブラウザの操作欄で、表示を切り替える。

   - 「一覧」でルート内の Markdown の一覧を開き、ファイル名を押して文書を開く
   - 「見出し」で、左の見出し欄を出し入れする。見出しを押すと、本文のその見出しへ移る
   - 「配色」でシステム・ライト・ダーク、「本文幅」で全幅・固定幅を選ぶ
   - 「GitLab との違い <件数>」があれば押す。種類ごとの見出しと「<行番号> 行: <抜き出し>」を読み、行を押して本文の該当する行へ移る
   - 別の Markdown を指す相対リンクがある文書では、そのリンクを押して同じタブで移る。ブラウザの戻る・進むで行き来する
   - 印刷するときは、ブラウザの印刷を開いて本文を確かめる

1. プレビューを起こした端末で、Ctrl+C を押して止める。

   - コマンドの入力に戻ればよい

<details id="mdpreview-で-markdown-をプレビューする-の補足と注意点">
  <summary>mdpreview で Markdown をプレビューする の補足と注意点</summary>

**[mdpreview で Markdown をプレビューする](#mdpreview-で-markdown-をプレビューする)の手順 1**

- `--no-open` を外すと、既定のブラウザを開く。`--browser <実行ファイル>` で別のブラウザを指定する
- 試験用の Markdown を専用のフォルダーに作るのは、そのフォルダーがルートになるため（「一覧」に出るのは、その中の Markdown だけ）。止めると、フォルダーごと消す
- 自分の文書を開くときは、試験用ファイルの代わりに Markdown のパスを渡す。保存すると描き直す。未保存の内容を見るときは Neovim から使う
- フォルダーを開く形は `mdpreview docs`。指定したフォルダーをルートにし、最初に一覧を出す。`--root` とは併用しない
- ファイルを渡したときのルートは、上へたどって最初に `.git` があるフォルダー。無ければ、そのファイルのあるフォルダー。`--root <フォルダー>` で変えられる
- 画像・動画・音声・別の文書・include で読めるのはルート内のファイルだけ。`.git` の中は読まない
- `--port <番号>` で待ち受けるポートを指定する。省略すると空いているポートを選ぶ
- `::include{file=…}` で取り込むファイルの変更・削除・作成にも合わせて描き直す
- 公式スタイルは任意。[md-preview の公式スタイルを取得する](install.md#md-preview-の公式スタイルを取得する)に従う

**[mdpreview で Markdown をプレビューする](#mdpreview-で-markdown-をプレビューする)の手順 3**

- 「GitLab との違い」は、知らせが 1 件以上あるときだけ出る。この項の試験用文書には、知らせの対象になる記法を入れていない
- 数式は KaTeX、mermaid のコードブロックは mermaid で描く。どちらも同じサーバーから配る同梱のライブラリを使う
- 印刷用の表示では、操作欄・見出し欄・知らせを隠す

</details>

---

### mdpreview で Markdown を HTML に書き出す

- 試験用の Markdown を HTML に書き出し、ブラウザで開く
- 前提: [md-preview を導入・更新する](install.md#md-preview-を導入更新する)を通し、`mdpreview` が PATH にある
- 実行場所: GNOME にログインした自分のユーザーの端末（bash）。`sudo` は使わない

1. 試験用の Markdown を作り、HTML に書き出す。

   ```bash
   # 試験用の Markdown を HTML に書き出す
   preview_dir=$(mktemp -d) &&
     printf '# 書き出しの確認\n\n本文です。\n' > "$preview_dir/preview.md" &&
     mdpreview render -o "$preview_dir/preview.html" "$preview_dir/preview.md"
   ```

   - エラーなく終われば、この項の手順 2 へ進む
   - エラーが出たら、中断して入力ファイルと書き出し先を確かめる

1. 書き出したファイルを確かめる。

   ```bash
   printf '\n\033[7m 確認 \033[0m\n'
   ls -l -- "$preview_dir/preview.html"
   ```

   - `preview.html` があり、大きさが 0 より大きければよい
   - 表示されたファイルのパスを、この項の手順 3 で使う

1. ブラウザで、書き出した HTML を開く。

   - Ctrl+O を押し、この項の手順 2 で確かめたファイルを開く
   - 本文に「書き出しの確認」と「本文です。」が出ればよい

1. 試験用のフォルダーを消す。

   ```bash
   # この項の手順 1 で作った試験用のフォルダーを消す
   [ -n "${preview_dir:-}" ] && rm -r -- "$preview_dir"
   unset preview_dir
   ```

   - 何も出なければよい
   - この項の手順 1 と同じ端末で行う（別の端末では、消すフォルダーが分からない）

<details id="mdpreview-で-markdown-を-html-に書き出す-の補足と注意点">
  <summary>mdpreview で Markdown を HTML に書き出す の補足と注意点</summary>

**[mdpreview で Markdown を HTML に書き出す](#mdpreview-で-markdown-を-html-に書き出す)の手順 1**

- 自分の文書を使う形は `mdpreview render -o preview.html README.md`。`-o` を省くと標準出力に HTML を書く。書き出し先のフォルダーは先に用意する
- 知らせがあれば、標準エラーに種類ごとの件数を出す。例は `mdpreview: 参照: 2 件`、`mdpreview: 見つからないメディア: 1 件`。ほかの種類の名前は「図」「提案」「取り込み」「絵文字」と、件数が多くて省いた分の「省略した知らせ」
- `--theme system|light|dark` で配色、`--width fluid|fixed` で本文幅を選ぶ。既定は `system`（OS に従う）と `fluid`（全幅）
- `system` の暗い配色は画面だけに使い、印刷は明るい配色にする
- `--root <フォルダー>` でルートを指定できる。省略したときのルートの決め方はプレビューと同じ
- ルート内の相対パスの画像・動画・音声・リンクは、書き出し先から見た相対パスに直す。メディアのファイル自体は HTML に埋め込まない
- 書き出し先がルートの外のときは、相対パスを文書のフォルダーから見た形のままにし、そのことを標準エラーに知らせる（書き出し先から見た形にすると、ルートより上のフォルダーの名前が HTML に入るため）。リンクと画像を合わせるには、ルートの中へ書き出す
- 入力の Markdown と同じファイルは、書き出し先にできない
- 別の Markdown へのリンクもファイルへのリンクとして残る。リンク先の HTML をまとめて作る機能はない
- 公式スタイルを導入していても、書き出しには同梱のスタイルを使う。必要な KaTeX・mermaid と数式のフォントを埋め込む。数式も図も無ければスクリプトを入れない
- mermaid の図があると、ライブラリを埋め込むため HTML が大きくなる。数式と図を含む文書を書き出した例では約 6.2 MB

**[mdpreview で Markdown を HTML に書き出す](#mdpreview-で-markdown-を-html-に書き出す)の手順 3**

- サーバーを起こさず、`file://` から開く。HTML とメディアを別の場所へ移すときは、相対パスの関係を保つ

</details>

---

## Windows 11 で使う

- この PC のデスクトップで、自分のユーザーが行う。前提の項は、各項のリードのリンクから開く

### Windows 11 の mdpreview で Markdown をプレビューする

- 試験用の小さな Markdown を作り、ブラウザで表示を確かめる
- 前提: [Windows 11 で md-preview を導入・更新する](install.md#windows-11-で-md-preview-を導入更新する)を通し、`mdpreview` が PATH にある
- 実行場所: この PC のデスクトップの管理者ではない PowerShell 7。この項の手順 1 で開く

1. 管理者ではない PowerShell 7 を開く。

   - スタートメニューで「PowerShell」を探し、クリックして開く（「Windows PowerShell」ではない。「管理者として実行」にはしない）
   - 「PowerShell」が無ければ、[アプリを入れる](https://github.com/ryo-aoki-pc/setup-notes/blob/main/docs/windows-setup.md#アプリを入れる)の手順 6〜9 を通す

1. 試験用の Markdown を作り、プレビューを起こす。

   ```powershell
   & {
     # 試験用の Markdown を、専用のフォルダーに作って開く
     $previewDirectory = Join-Path ([IO.Path]::GetTempPath()) ("md-preview-" + [guid]::NewGuid())
     try {
       New-Item -ItemType Directory -Path $previewDirectory -ErrorAction Stop | Out-Null
       Set-Content -LiteralPath "$previewDirectory\preview.md" -Value "# プレビューの確認`n`n本文です。" -Encoding utf8 -ErrorAction Stop
       "`n$([char]27)[7m 確認 $([char]27)[0m"
       mdpreview --no-open "$previewDirectory\preview.md"
     } finally {
       Remove-Item -LiteralPath $previewDirectory -Recurse -Force -ErrorAction SilentlyContinue
     }
   }
   ```

   - `プレビュー: http://127.0.0.1:<ポート>/<32 桁の 16 進>/` の形の URL が出ればよい
   - エラーが出たら、中断して [導入の手順](install.md#windows-11-で-md-preview-を導入更新する)で導入状態を確かめる

1. ブラウザで、この項の手順 2 に出た URL を開く。

   - 本文に「プレビューの確認」と「本文です。」が出ればよい
   - 操作欄の左に `preview.md` と出ればよい

1. ブラウザの操作欄で、表示を切り替える。

   - 「一覧」でルート内の Markdown の一覧を開き、ファイル名を押して文書を開く
   - 「見出し」で、左の見出し欄を出し入れする。見出しを押すと、本文のその見出しへ移る
   - 「配色」でシステム・ライト・ダーク、「本文幅」で全幅・固定幅を選ぶ
   - 「GitLab との違い <件数>」があれば押す。種類ごとの見出しと「<行番号> 行: <抜き出し>」を読み、行を押して本文の該当する行へ移る
   - 別の Markdown を指す相対リンクがある文書では、そのリンクを押して同じタブで移る。ブラウザの戻る・進むで行き来する
   - 印刷するときは、ブラウザの印刷を開いて本文を確かめる

1. プレビューを起こした PowerShell 7 で、Ctrl+C を押して止める。

   - コマンドの入力に戻ればよい

<details id="windows-11-の-mdpreview-で-markdown-をプレビューする-の補足と注意点">
  <summary>Windows 11 の mdpreview で Markdown をプレビューする の補足と注意点</summary>

**[Windows 11 の mdpreview で Markdown をプレビューする](#windows-11-の-mdpreview-で-markdown-をプレビューする)の手順 2**

- `--no-open` を外すと、既定のブラウザを開く。`--browser <実行ファイル>` で別のブラウザを指定する
- 試験用の Markdown を専用のフォルダーに作るのは、そのフォルダーがルートになるため（「一覧」に出るのは、その中の Markdown だけ）。止めると、フォルダーごと消す
- 自分の文書を開くときは、試験用ファイルの代わりに Markdown のパスを渡す。保存すると描き直す。未保存の内容を見るときは Neovim から使う
- フォルダーを開く形は `mdpreview docs`。指定したフォルダーをルートにし、最初に一覧を出す。`--root` とは併用しない
- ファイルを渡したときのルートは、上へたどって最初に `.git` があるフォルダー。無ければ、そのファイルのあるフォルダー。`--root <フォルダー>` で変えられる
- 画像・動画・音声・別の文書・include で読めるのはルート内のファイルだけ。`.git` の中は読まない
- `--port <番号>` で待ち受けるポートを指定する。省略すると空いているポートを選ぶ
- `::include{file=…}` で取り込むファイルの変更・削除・作成にも合わせて描き直す
- 公式スタイルは任意。[Windows 11 で md-preview の公式スタイルを取得する](install.md#windows-11-で-md-preview-の公式スタイルを取得する)に従う

**[Windows 11 の mdpreview で Markdown をプレビューする](#windows-11-の-mdpreview-で-markdown-をプレビューする)の手順 4**

- 「GitLab との違い」は、知らせが 1 件以上あるときだけ出る。この項の試験用文書には、知らせの対象になる記法を入れていない
- 数式は KaTeX、mermaid のコードブロックは mermaid で描く。どちらも同じサーバーから配る同梱のライブラリを使う
- 印刷用の表示では、操作欄・見出し欄・知らせを隠す

</details>

---

### Windows 11 の mdpreview で Markdown を HTML に書き出す

- 試験用の Markdown を HTML に書き出し、ブラウザで開く
- 前提: [Windows 11 で md-preview を導入・更新する](install.md#windows-11-で-md-preview-を導入更新する)を通し、`mdpreview` が PATH にある
- 実行場所: この PC のデスクトップの管理者ではない PowerShell 7。この項の手順 1 で開く
- PowerShell のリダイレクトで HTML を保存せず、`-o` で書き出し先を指定する

1. 管理者ではない PowerShell 7 を開く。

   - スタートメニューで「PowerShell」を探し、クリックして開く（「Windows PowerShell」ではない。「管理者として実行」にはしない）
   - 「PowerShell」が無ければ、[アプリを入れる](https://github.com/ryo-aoki-pc/setup-notes/blob/main/docs/windows-setup.md#アプリを入れる)の手順 6〜9 を通す

1. 試験用の Markdown を作り、HTML に書き出す。

   ```powershell
   # 試験用の Markdown を HTML に書き出す
   $previewDirectory = Join-Path ([IO.Path]::GetTempPath()) ("md-preview-" + [guid]::NewGuid())
   & {
     New-Item -ItemType Directory -Path $previewDirectory -ErrorAction Stop | Out-Null
     Set-Content -LiteralPath "$previewDirectory\preview.md" -Value "# 書き出しの確認`n`n本文です。" -Encoding utf8 -ErrorAction Stop
     mdpreview render -o "$previewDirectory\preview.html" "$previewDirectory\preview.md"
     if ($LASTEXITCODE -ne 0) { return }
     "`n$([char]27)[7m 確認 $([char]27)[0m"
     Get-Item -LiteralPath "$previewDirectory\preview.html" | Format-List FullName, Length
   }
   ```

   - `preview.html` のパスと、0 より大きい大きさが出ればよい
   - 表示されたファイルのパスを、この項の手順 3 で使う
   - エラーが出たら、中断して入力ファイルと書き出し先を確かめる

1. ブラウザで、書き出した HTML を開く。

   - Ctrl+O を押し、この項の手順 2 で確かめたファイルを開く
   - 本文に「書き出しの確認」と「本文です。」が出ればよい

1. 試験用のフォルダーを消す。

   ```powershell
   # この項の手順 2 で作った試験用のフォルダーを消す
   if ($previewDirectory) { Remove-Item -LiteralPath $previewDirectory -Recurse; $previewDirectory = $null }
   ```

   - 何も出なければよい
   - この項の手順 2 と同じ PowerShell 7 の窓で行う（別の窓では、消すフォルダーが分からない）

<details id="windows-11-の-mdpreview-で-markdown-を-html-に書き出す-の補足と注意点">
  <summary>Windows 11 の mdpreview で Markdown を HTML に書き出す の補足と注意点</summary>

**[Windows 11 の mdpreview で Markdown を HTML に書き出す](#windows-11-の-mdpreview-で-markdown-を-html-に書き出す)のリード**

- PowerShell の版によっては、パイプやリダイレクトが文字コードや改行を変える。`-o` ならアプリが UTF-8 の HTML を直接書く

**[Windows 11 の mdpreview で Markdown を HTML に書き出す](#windows-11-の-mdpreview-で-markdown-を-html-に書き出す)の手順 2**

- 自分の文書を使う形は `mdpreview render -o preview.html README.md`。`-o` を省くと標準出力に HTML を書く。書き出し先のフォルダーは先に用意する
- 知らせがあれば、標準エラーに種類ごとの件数を出す。例は `mdpreview: 参照: 2 件`、`mdpreview: 見つからないメディア: 1 件`。ほかの種類の名前は「図」「提案」「取り込み」「絵文字」と、件数が多くて省いた分の「省略した知らせ」
- `--theme system|light|dark` で配色、`--width fluid|fixed` で本文幅を選ぶ。既定は `system`（OS に従う）と `fluid`（全幅）
- `system` の暗い配色は画面だけに使い、印刷は明るい配色にする
- `--root <フォルダー>` でルートを指定できる。省略したときのルートの決め方はプレビューと同じ
- ルート内の相対パスの画像・動画・音声・リンクは、書き出し先から見た相対パスに直す。メディアのファイル自体は HTML に埋め込まない
- 書き出し先がルートの外のときは、相対パスを文書のフォルダーから見た形のままにし、そのことを標準エラーに知らせる（書き出し先から見た形にすると、ルートより上のフォルダーの名前が HTML に入るため）。リンクと画像を合わせるには、ルートの中へ書き出す
- 入力の Markdown と同じファイルは、書き出し先にできない
- 別の Markdown へのリンクもファイルへのリンクとして残る。リンク先の HTML をまとめて作る機能はない
- 公式スタイルを導入していても、書き出しには同梱のスタイルを使う。必要な KaTeX・mermaid と数式のフォントを埋め込む。数式も図も無ければスクリプトを入れない
- mermaid の図があると、ライブラリを埋め込むため HTML が大きくなる。数式と図を含む文書を書き出した例では約 6.2 MB

**[Windows 11 の mdpreview で Markdown を HTML に書き出す](#windows-11-の-mdpreview-で-markdown-を-html-に書き出す)の手順 3**

- サーバーを起こさず、`file://` から開く。HTML とメディアを別の場所へ移すときは、相対パスの関係を保つ

</details>

---
