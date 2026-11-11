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
- Neovim からは [LazyVimStarter](https://github.com/ryo-aoki-pc/LazyVimStarter) の `<leader>cp` で使う。未保存の内容の表示、カーソルに合わせたスクロール、本文のダブルクリックでエディタの行へ移る操作、プレビューの画面の「編集」で直した内容のバッファへの反映を扱う
- Neovim の導入は、setup-notes の [LazyVim の GitLab プレビューを導入する](https://github.com/ryo-aoki-pc/setup-notes/blob/main/docs/neovim.md#lazyvim-の-gitlab-プレビューを導入する)・[Windows 11 で LazyVim の GitLab プレビューを導入する](https://github.com/ryo-aoki-pc/setup-notes/blob/main/docs/neovim.md#windows-11-で-lazyvim-の-gitlab-プレビューを導入する)に従う
- 外へ通信する場面は [README](../README.md#外へ通信する場面) にある
- `mdpreview --licenses` で、同梱しているもののライセンスの原文を出す。一覧は [licenses/manifest.json](../licenses/manifest.json) にある
- PlantUML のコードブロックは、`plantuml` コマンドが PATH にあれば、手元で SVG にして表示する（Windows 11 の導入は [Windows 11 で PlantUML を入れる](install.md#windows-11-で-plantuml-を入れる)）。コマンドが無い場合、`--no-plantuml` を付けた場合、原文が 256 KiB を超える場合は、コードブロックのまま表示し、「GitLab との違い」の「図」で知らせる
- `--no-plantuml` はプレビュー・`--stdio`・`render` で使える。SVG にならなかった図は、原文のコードブロックで表示する。`render` では描けた SVG を HTML の画像に埋め、描けなかった図の件数を標準エラーへ知らせる。図の原文は外へ送らず、図の中から手元のファイルや URL を読む記法も止める
- Neovim から使うとき（ダブルクリックで行へ移れるとき）は、図のダブルクリックでエディタの行へ移り、図のクリックでは SVG を新しいタブで開かない。単体のプレビューでは、図のクリックで SVG を新しいタブで開く

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
   - 「表を固定」で、表のそばにツールバーを出す。もう一度押すと外す
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

### mdpreview のプレビューで表を編集する

- パイプの表と結合セルのある HTML の表、段落の原文、タスクのチェックボックスを、プレビューの画面で直してファイルに書く
- 前提: [md-preview を導入・更新する](install.md#md-preview-を導入更新する)を通し、`mdpreview` が PATH にある
- 実行場所: GNOME にログインした自分のユーザーの端末（bash）。`sudo` は使わない

1. 試験用の Markdown を作り、編集を有効にしてプレビューを起こす。

   ```bash
   (
     # 試験用の Markdown を、専用のフォルダーに作って開く。止めたときに、書き込まれた内容を出してから消す
     preview_dir=$(mktemp -d) || exit
     trap 'printf "\n\033[7m 保存された内容 \033[0m\n"; cat -- "$preview_dir/edit.md"; rm -rf -- "$preview_dir"' EXIT
     printf '%s\n' \
       '# 表の編集の確認' '' \
       '編集する段落。' '' '- [ ] 作業' '' \
       '| 名前 | 個数 |' '| --- | --- |' '| りんご | 1 |' '| みかん | 2 |' '' \
       '<table>' '<tbody>' \
       '<tr><td rowspan="2">結合したセル</td><td>右上</td></tr>' \
       '<tr><td>右下</td></tr>' \
       '</tbody>' '</table>' > "$preview_dir/edit.md" || exit
     printf '\n\033[7m 確認 \033[0m\n'
     mdpreview --edit --no-open "$preview_dir/edit.md"
   )
   ```

   - `プレビュー: http://127.0.0.1:<ポート>/<32 桁の 16 進>/` の形の URL が出ればよい
   - エラーが出たら、中断して [導入の手順](install.md#md-preview-を導入更新する)で導入状態を確かめる

1. ブラウザで、この項の手順 1 に出た URL を開き、操作欄の「編集」を押す。

   - 表が 2 つ出て、「編集」が押された状態になればよい
   - 上の表の「りんご」をクリックする。そのセルに枠が付き、画面の下に操作欄が出ればよい

1. セルを直し、行を足して、元に戻す。

   - 「りんご」をダブルクリックし、「青りんご」に直して Enter を押す。表の文字が「青りんご」に変わればよい
   - 「青りんご」をクリックし、画面の下の「下に行を挿入」を押す。その下に空の行が足されればよい
   - 「元に戻す」を押す。足した行が消えればよい

1. 段落の原文を直し、タスクのチェックボックスを切り替える。

   - 「編集する段落。」をダブルクリックし、「直した段落。」に直して Ctrl+Enter を押す。段落が「直した段落。」に変わればよい
   - 「作業」のチェックボックスをクリックする。チェックが付けばよい

1. 「表を固定」で全幅表示にしたまま、セルを直す。

   - 操作欄の「表を固定」を押し、下の表のツールバーの「全幅で表示」を押す
   - 「右上」をダブルクリックし、「右上（変更）」に直して Enter を押す。入力欄が全幅表示の上に出て、確定の後も全幅表示のままならよい
   - Esc を 2 回押す。1 回目で選択の枠が消え、2 回目で全幅表示が閉じればよい

1. プレビューを起こした端末で、Ctrl+C を押して止める。

   - 「保存された内容」の下に出る Markdown に、「青りんご」・「右上（変更）」・「直した段落。」・`- [x] 作業` が入っていればよい
   - コマンドの入力に戻ればよい

<details id="mdpreview-のプレビューで表を編集する-の補足と注意点">
  <summary>mdpreview のプレビューで表を編集する の補足と注意点</summary>

**[mdpreview のプレビューで表を編集する](#mdpreview-のプレビューで表を編集する)の手順 1**

- 自分の文書には `mdpreview --edit README.md`、フォルダーには `mdpreview --edit docs` を使う。フォルダーでは一覧から文書を選ぶ。`--edit` がなければ編集ボタンも書き込みもない
- Neovim から使うとき（LazyVimStarter の `<leader>cp`）は、`--edit` は要らない。「編集」で直した内容は Neovim のバッファへ入り、ファイルには書かない（保存は `:w`、取り消しは `u`。画面に「元に戻す」「やり直し」は出ない）。編集できるのは、Neovim で開いている文書だけ。Neovim の側でバッファを変えた直後や、変更できないバッファでは、編集を断って理由を知らせる。`vim.g.gitlab_preview_edit = false` で無効にできる
- 画面の編集は直ちにファイルへ書く。BOM とファイルの権限を保ち、隣の一時ファイルから入れ替えるため、ハードリンクは切れ、同じ実体を指していた別名には反映しない
- 取り込んだ include 先・JSON から作った表・入れ子の表・対応を確かめられない表は編集できない。UTF-8 でないファイル・単独の CR で行の対応がずれる文書・16 MiB を超える文書も対象外
- セル用 Markdown を同じ描画に書き戻せない内容、壊れた HTML の原文、表の外へはみ出す貼り付けなどは書かず、理由を知らせる。ファイルを外で変えた場合も上書きしない
- 取り消しの履歴は文書ごとに200手・全体で32 MiBまで。プレビューを終了すると失われる。外で変わったファイルには古い履歴を適用しない

**[mdpreview のプレビューで表を編集する](#mdpreview-のプレビューで表を編集する)の手順 3・4**

| 状態 | 操作 | 動き |
| --- | --- | --- |
| 選択中 | 矢印 | 結合セルを一つとして隣へ移る |
| 選択中 | Tab / Shift+Tab | 次 / 前のセルへ移る。行の端では行をまたぐ |
| 選択中 | Enter・F2・ダブルクリック | 今の文字を残して編集する |
| 選択中 | 文字入力・単一の文字列の貼り付け | 今の文字を置き換える編集を始める |
| 選択中 | Delete・Backspace | セルを空にして書く |
| 選択中 | Ctrl+Z / Ctrl+Y・Ctrl+Shift+Z | 元に戻す / やり直す |
| 選択中 | Ctrl+C | セルの文字（セル用 Markdown）をコピーする |
| 選択中 | Esc | 選択をやめる。全幅表示中なら、もう一度押すと全幅表示を閉じる |
| 編集中 | Enter / Shift+Enter | 確定して下 / 上へ移る |
| 編集中 | Tab / Shift+Tab | 確定して次 / 前へ移る |
| 編集中 | Alt+Enter | セル内に改行を入れる |
| 編集中 | Esc | 入力を取り消して選択へ戻る。全幅表示は続く |
| HTML の原文を編集中 | Enter / Ctrl+Enter | 改行 / 確定 |
| ブロックの原文を編集中 | Enter / Ctrl+Enter（macOS は Cmd+Enter も） | 改行 / 確定 |
| ブロックの原文を編集中 | Esc / 入力欄の外のクリック | 取り消し / 確定。選択は残さない |
| タスクのチェックボックス | クリック / フォーカス中の Space | チェックを切り替える。`[~]` は対象外 |

- 入力欄に出るのは、セル用 Markdown（`**太字**` などの記法のまま。セル内の改行は改行）。パイプの表では、縦棒は `\|`、改行は `<br>` にして書く
- タブや改行を含む貼り付け（Excel でコピーした範囲など）は、選んだセルを左上にして複数のセルへ入れる。引用符で囲んだ改行・タブ・二重の引用符にも対応する。貼り付ける値もセル用 Markdown として読む（`a*b*c` は強調になる）。編集中の貼り付けは入力欄に入る
- 行・列の操作は画面下の操作欄を使う。できない操作は無効になり、マウスを置くと理由が出る。削除の確認窓は出ない。「元に戻す」で戻す
- HTML の表では「原文を編集」を使える。セル用 Markdown に直すと形が変わるセルは、通常の編集でも原文を開く
- 表の外は、段落・見出し・リストの項目・引用・コードブロックをダブルクリックすると Markdown の原文を編集できる（マウスを載せると、直す対象に破線の枠が出る）。リストの項目は、項目の文字を押すと子のリストも含む原文、項目の中の段落を押すとその段落だけになる
- 編集モードの間は、原文を直せる所のリンクを押しても移動しない。リンクで移るときは、「編集」をもう一度押して編集モードを出る（Ctrl などを押しながらのクリックは止めないので、新しいタブでは開ける）
- 脚注・目次・include 先・front matter・数式と図は画面から編集できない。原文の取得や書き込みを断ったときは、理由を知らせる。Ctrl・Shift・Alt を押しながらのダブルクリックでは、編集を始めない
- 編集モードと表の固定は再読み込み後もそのタブに覚える。実際の IME の操作は未検証で、変換のイベントを合成したブラウザ試験だけを行っている

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
   - 「表を固定」で、表のそばにツールバーを出す。もう一度押すと外す
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

### Windows 11 の mdpreview のプレビューで表を編集する

- パイプの表と結合セルのある HTML の表、段落の原文、タスクのチェックボックスを、プレビューの画面で直してファイルに書く
- 前提: [Windows 11 で md-preview を導入・更新する](install.md#windows-11-で-md-preview-を導入更新する)を通し、`mdpreview` が PATH にある
- 実行場所: この PC のデスクトップの管理者ではない PowerShell 7。この項の手順 1 で開く

1. 管理者ではない PowerShell 7 を開く。

   - スタートメニューで「PowerShell」を探し、クリックして開く（「Windows PowerShell」ではない。「管理者として実行」にはしない）
   - 「PowerShell」が無ければ、[アプリを入れる](https://github.com/ryo-aoki-pc/setup-notes/blob/main/docs/windows-setup.md#アプリを入れる)の手順 6〜9 を通す

1. 試験用の Markdown を作り、編集を有効にしてプレビューを起こす。

   ```powershell
   & {
     # 試験用の Markdown を、専用のフォルダーに作って開く。止めたときに、書き込まれた内容を出してから消す
     $previewDirectory = Join-Path ([IO.Path]::GetTempPath()) ("md-preview-" + [guid]::NewGuid())
     try {
       New-Item -ItemType Directory -Path $previewDirectory -ErrorAction Stop | Out-Null
       $lines = @(
         '# 表の編集の確認', '',
         '編集する段落。', '', '- [ ] 作業', '',
         '| 名前 | 個数 |', '| --- | --- |', '| りんご | 1 |', '| みかん | 2 |', '',
         '<table>', '<tbody>',
         '<tr><td rowspan="2">結合したセル</td><td>右上</td></tr>',
         '<tr><td>右下</td></tr>',
         '</tbody>', '</table>'
       )
       Set-Content -LiteralPath "$previewDirectory\edit.md" -Value $lines -Encoding utf8 -ErrorAction Stop
       "`n$([char]27)[7m 確認 $([char]27)[0m"
       mdpreview --edit --no-open "$previewDirectory\edit.md"
     } finally {
       if (Test-Path -LiteralPath "$previewDirectory\edit.md") {
         Write-Host "`n$([char]27)[7m 保存された内容 $([char]27)[0m"
         Write-Host (Get-Content -LiteralPath "$previewDirectory\edit.md" -Raw -Encoding utf8)
       }
       Remove-Item -LiteralPath $previewDirectory -Recurse -Force -ErrorAction SilentlyContinue
     }
   }
   ```

   - `プレビュー: http://127.0.0.1:<ポート>/<32 桁の 16 進>/` の形の URL が出ればよい
   - エラーが出たら、中断して [導入の手順](install.md#windows-11-で-md-preview-を導入更新する)で導入状態を確かめる

1. ブラウザで、この項の手順 2 に出た URL を開き、操作欄の「編集」を押す。

   - 表が 2 つ出て、「編集」が押された状態になればよい
   - 上の表の「りんご」をクリックする。そのセルに枠が付き、画面の下に操作欄が出ればよい

1. セルを直し、行を足して、元に戻す。

   - 「りんご」をダブルクリックし、「青りんご」に直して Enter を押す。表の文字が「青りんご」に変わればよい
   - 「青りんご」をクリックし、画面の下の「下に行を挿入」を押す。その下に空の行が足されればよい
   - 「元に戻す」を押す。足した行が消えればよい

1. 段落の原文を直し、タスクのチェックボックスを切り替える。

   - 「編集する段落。」をダブルクリックし、「直した段落。」に直して Ctrl+Enter を押す。段落が「直した段落。」に変わればよい
   - 「作業」のチェックボックスをクリックする。チェックが付けばよい

1. 「表を固定」で全幅表示にしたまま、セルを直す。

   - 操作欄の「表を固定」を押し、下の表のツールバーの「全幅で表示」を押す
   - 「右上」をダブルクリックし、「右上（変更）」に直して Enter を押す。入力欄が全幅表示の上に出て、確定の後も全幅表示のままならよい
   - Esc を 2 回押す。1 回目で選択の枠が消え、2 回目で全幅表示が閉じればよい

1. プレビューを起こした PowerShell 7 で、Ctrl+C を押して止める。

   - 「保存された内容」の下に出る Markdown に、「青りんご」・「右上（変更）」・「直した段落。」・`- [x] 作業` が入っていればよい
   - コマンドの入力に戻ればよい

<details id="windows-11-の-mdpreview-のプレビューで表を編集する-の補足と注意点">
  <summary>Windows 11 の mdpreview のプレビューで表を編集する の補足と注意点</summary>

**[Windows 11 の mdpreview のプレビューで表を編集する](#windows-11-の-mdpreview-のプレビューで表を編集する)の手順 2**

- 自分の文書には `mdpreview --edit README.md`、フォルダーには `mdpreview --edit docs` を使う。フォルダーでは一覧から文書を選ぶ。`--edit` がなければ編集ボタンも書き込みもない
- Neovim から使うとき（LazyVimStarter の `<leader>cp`）は、`--edit` は要らない。「編集」で直した内容は Neovim のバッファへ入り、ファイルには書かない（保存は `:w`、取り消しは `u`。画面に「元に戻す」「やり直し」は出ない）。編集できるのは、Neovim で開いている文書だけ。Neovim の側でバッファを変えた直後や、変更できないバッファでは、編集を断って理由を知らせる。`vim.g.gitlab_preview_edit = false` で無効にできる
- 画面の編集は直ちにファイルへ書く。BOM とファイルの権限を保ち、隣の一時ファイルから入れ替えるため、ハードリンクは切れ、同じ実体を指していた別名には反映しない
- include 先・JSON の表・入れ子の表・原文と対応を確かめられない表は編集できない。UTF-8 でないファイル・単独の CR で行の対応がずれる文書・16 MiB を超える文書も対象外
- セル用 Markdown を同じ描画に書き戻せない内容、壊れた HTML の原文、表の外へはみ出す貼り付けなどは書かず、理由を知らせる。外で変わったファイルも上書きしない
- 取り消しの履歴は文書ごとに200手・全体で32 MiBまで。終了すると失われ、外で変わったファイルには適用しない

**[Windows 11 の mdpreview のプレビューで表を編集する](#windows-11-の-mdpreview-のプレビューで表を編集する)の手順 4・5**

| 状態 | 操作 | 動き |
| --- | --- | --- |
| 選択中 | 矢印 | 結合セルを一つとして隣へ移る |
| 選択中 | Tab / Shift+Tab | 次 / 前のセルへ移る。行の端では行をまたぐ |
| 選択中 | Enter・F2・ダブルクリック | 今の文字を残して編集する |
| 選択中 | 文字入力・単一の文字列の貼り付け | 今の文字を置き換える編集を始める |
| 選択中 | Delete・Backspace | セルを空にして書く |
| 選択中 | Ctrl+Z / Ctrl+Y・Ctrl+Shift+Z | 元に戻す / やり直す |
| 選択中 | Ctrl+C | セルの文字（セル用 Markdown）をコピーする |
| 選択中 | Esc | 選択をやめる。全幅表示中なら、もう一度押すと全幅表示を閉じる |
| 編集中 | Enter / Shift+Enter | 確定して下 / 上へ移る |
| 編集中 | Tab / Shift+Tab | 確定して次 / 前へ移る |
| 編集中 | Alt+Enter | セル内に改行を入れる |
| 編集中 | Esc | 入力を取り消して選択へ戻る。全幅表示は続く |
| HTML の原文を編集中 | Enter / Ctrl+Enter | 改行 / 確定 |
| ブロックの原文を編集中 | Enter / Ctrl+Enter（macOS は Cmd+Enter も） | 改行 / 確定 |
| ブロックの原文を編集中 | Esc / 入力欄の外のクリック | 取り消し / 確定。選択は残さない |
| タスクのチェックボックス | クリック / フォーカス中の Space | チェックを切り替える。`[~]` は対象外 |

- 入力欄に出るのは、セル用 Markdown（`**太字**` などの記法のまま。セル内の改行は改行）。パイプの表では、縦棒は `\|`、改行は `<br>` にして書く
- タブや改行を含む貼り付け（Excel でコピーした範囲など）は、選んだセルを左上にして複数のセルへ入れる。引用符で囲んだ改行・タブ・二重の引用符にも対応する。貼り付ける値もセル用 Markdown として読む（`a*b*c` は強調になる）。編集中の貼り付けは入力欄に入る
- 行・列の操作は画面下の操作欄を使う。できない操作は無効になり、マウスを置くと理由が出る。削除の確認窓は出ない。「元に戻す」で戻す
- HTML の表では「原文を編集」を使える。セル用 Markdown に直すと形が変わるセルは、通常の編集でも原文を開く
- 表の外は、段落・見出し・リストの項目・引用・コードブロックをダブルクリックすると Markdown の原文を編集できる（マウスを載せると、直す対象に破線の枠が出る）。リストの項目は、項目の文字を押すと子のリストも含む原文、項目の中の段落を押すとその段落だけになる
- 編集モードの間は、原文を直せる所のリンクを押しても移動しない。リンクで移るときは、「編集」をもう一度押して編集モードを出る（Ctrl などを押しながらのクリックは止めないので、新しいタブでは開ける）
- 脚注・目次・include 先・front matter・数式と図は画面から編集できない。原文の取得や書き込みを断ったときは、理由を知らせる。Ctrl・Shift・Alt を押しながらのダブルクリックでは、編集を始めない
- 編集モードと表の固定は再読み込み後もそのタブに覚える。実際の IME の操作は未検証で、変換のイベントを合成したブラウザ試験だけを行っている

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
