# md-preview の導入と更新（AlmaLinux 10・Windows 11）

- Markdown を GitLab の描画でプレビューする `mdpreview` コマンド（[md-preview](../README.md)）を入れる。本文は GitLab に送らず、手元で描く
- 使い方は [md-preview の使い方](usage.md) にある
- どの項も単独で通せる。項のリードの前提を先に満たす
- bash のブロックはブラケットペーストの効く端末に、PowerShell のブロックは PowerShell 7 に貼り、貼った後に Enter を押す。貼った後は、反転表示の「確認」から後ろの出力を箇条書きで確かめる
- PowerShell 7 の導入と実行ポリシーは、setup-notes の [アプリを入れる](https://github.com/ryo-aoki-pc/setup-notes/blob/main/docs/windows-setup.md#アプリを入れる)の手順 6〜9 に従う
- PowerShell の貼り方は [Windows Terminal で複数行を 1 つの入力として貼る](https://github.com/ryo-aoki-pc/setup-notes/blob/main/docs/shell-terminal.md#windows-terminal-で複数行を-1-つの入力として貼る)に従う

<details id="この文書の補足と注意点">
  <summary>この文書の補足と注意点</summary>

**選択した方針**

- clone は更新するソースを置く場所にし、実行するファイルと依存はリポジトリの外へ入れる
- 導入・更新には、どちらも `scripts/setup.mjs` を使う

**参照**

- [md-preview](../README.md)

</details>

## AlmaLinux 10 で使う

- GNOME にログインした自分のユーザーで行う。管理者の端末は使わない

### md-preview を導入・更新する

- md-preview を `~/repos/md-preview` に clone し、`mdpreview` コマンドを入れて確かめる。更新もこの項の手順を通す
- 前提: Node.js 22 以上（npm を含む）と git が入っている
- 実行場所: GNOME にログインした自分のユーザーの端末（bash）。`sudo` は使わない
- 更新では、手元の変更の確認を省かない

1. Node.js と git の版を確かめる。

   ```bash
   printf '\n\033[7m 確認 \033[0m\n'
   node --version
   git --version
   ```

   - Node.js は `v22.` 以上、git は `git version` と版が出ればよい
   - コマンドが無いか Node.js が 22 より古ければ、導入・更新してからこの項の手順 1 を貼り直す

1. clone があれば、手元の変更を確かめる。

   ```bash
   # 既存の clone の変更を確かめる
   if [ -e ~/repos/md-preview ]; then
     printf '\n\033[7m 確認 \033[0m\n'
     git -C ~/repos/md-preview status --short
   fi
   ```

   - 何も出なければ、この項の手順 3 へ進む
   - 変更やエラーが出たら、その内容を解決してからこの項の手順 2 を貼り直す

1. リポジトリを clone し、既にあれば更新する。

   ```bash
   # md-preview のソースを取得・更新する
   mkdir -p ~/repos &&
     if [ -e ~/repos/md-preview ]; then
       git -C ~/repos/md-preview pull --ff-only
     else
       git clone https://github.com/ryo-aoki-pc/md-preview.git ~/repos/md-preview
     fi
   ```

   - エラーなく終わればよい
   - `pull --ff-only` が失敗したら、中断して clone の状態を解決してからこの項の手順 2 へ戻る

1. コマンドを導入し、導入の状態を確かめる。

   ```bash
   (
     # mdpreview を導入する
     cd ~/repos/md-preview || exit
     printf '\n\033[7m 確認 \033[0m\n'
     node scripts/setup.mjs &&
       node scripts/setup.mjs --check
   )
   ```

   - 最後に `導入済み: mdpreview` と版、起動役のパスが出ればよい
   - `問題:` が出たら、案内に従って直してからこの項の手順 4 を貼り直す
   - `~/.local/bin` が PATH に無いときは、PATH に加えて端末を開き直し、この項の手順 4 を貼り直す
   - `ほかの導入が動いています` で止まったら、そのプロセスが終わるのを待ってこの項の手順 4 を貼り直す

1. 起動役から版を確かめる。

   ```bash
   mdpreview --version
   ```

   - この項の手順 4 の `導入済み: mdpreview` に続く版と同じ版が出ればよい
   - コマンドが無ければ PATH を直して端末を開き直し、この項の手順 5 を貼り直す

1. 小さな Markdown を開き、URL を確かめて止める。

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

   - `プレビュー: http://127.0.0.1:` で始まる URL が出ればよい
   - URL を確かめたら、同じ端末で Ctrl+C を押して止める
   - エラーが出たら、中断してこの項の手順 4 の導入状態を確かめる

<details id="md-preview-を導入更新する-の補足と注意点">
  <summary>md-preview を導入・更新する の補足と注意点</summary>

**[md-preview を導入・更新する](#md-preview-を導入更新する)の手順 3**

- 更新は `pull --ff-only` の後に、この項の手順 4〜6 の導入・確認を繰り返す

**[md-preview を導入・更新する](#md-preview-を導入更新する)の手順 4**

- 導入先は `${XDG_DATA_HOME:-~/.local/share}/md-preview`。空か絶対パスでない `XDG_DATA_HOME` は未設定として扱う
- 起動役 `mdpreview` は `~/.local/bin` に置く
- `--check` は何も変えず、写したファイル・依存・起動役・PATH を確かめる
- 一時フォルダーで組み立てて確かめてから入れ替える。導入の記録は導入先の `install.json` に置く

**[md-preview を導入・更新する](#md-preview-を導入更新する)の手順 6**

- `--no-open` はブラウザを開かずに起動する。終了すると試験用のフォルダーを消す
- 試験用の Markdown を専用のフォルダーに作るのは、そのフォルダーがルート（読める範囲）になるため

</details>

---

### md-preview の公式スタイルを取得する

- 任意で GitLab の公式の CSS・フォントを取得する。未導入のときは約 785 MB を取得し、約 4 MB を保存する。取得元へ通信する
- 前提: [md-preview を導入・更新する](#md-preview-を導入更新する)を通し、`mdpreview` が PATH にある
- 実行場所: GNOME にログインした自分のユーザーの端末（bash）。`sudo` は使わない
- アプリを更新して公式スタイルの版が変わったときも、この項の手順を通す

1. 公式スタイルを取得する。

   ```bash
   mdpreview install-style
   ```

   - `mdpreview: 公式資産を導入しました:` と保存先、または `mdpreview: 導入済みの公式資産を確認しました。` が出ればよい
   - エラーが出たら、通信先と保存先を確かめてからこの項の手順 1 を貼り直す

1. 公式スタイルを使えることを確かめる。

   ```bash
   mdpreview install-style --check
   ```

   - `mdpreview: 公式スタイルを利用できます` が出ればよい（終了コード 0）
   - `mdpreview: 公式スタイルが未導入または破損しています` が出たら、この項の手順 1 からやり直す（終了コード 1）

<details id="md-preview-の公式スタイルを取得する-の補足と注意点">
  <summary>md-preview の公式スタイルを取得する の補足と注意点</summary>

**[md-preview の公式スタイルを取得する](#md-preview-の公式スタイルを取得する)の手順 1**

- 保存先は `${XDG_DATA_HOME:-~/.local/share}/md-preview-assets`。空か絶対パスでない `XDG_DATA_HOME` は未設定として扱う。直下に版ごとのフォルダーを置く
- 取得元は `registry.gitlab.com` など。固定した版・ファイル・取得元は [assets/assets.lock.json](../assets/assets.lock.json) にある
- ライセンスの原文も保存先に置く。公式資産はリポジトリや書き出す HTML に含めて再配布せず、使う端末ごとに取得する
- 次に起こしたプレビューから公式の CSS とフォント（GitLab Sans）を使う。コードの配色はライト・ダークとも Dracula
- 保存先のフォルダーを消すと、同梱のスタイルでの表示に戻る
- `--dir` で保存先を変えた場合、`--check` にも同じ `--dir` を付ける。プレビューには `--assets-dir` でその保存先を指定する。`--no-official-style` で同梱のスタイルを選べる

**[md-preview の公式スタイルを取得する](#md-preview-の公式スタイルを取得する)の手順 2**

- `--check` は通信せず、導入済みのファイルを検査する。プレビューの表示と HTML への書き出しでも取得しない

</details>

---

## Windows 11 で使う

- この PC のデスクトップで、自分のユーザーが行う。管理者の窓は使わない

### Windows 11 で md-preview を導入・更新する

- md-preview を `%USERPROFILE%\repos\md-preview` に clone し、`mdpreview` コマンドを入れて確かめる。更新もこの項の手順を通す
- 前提: Node.js 22 以上（npm を含む）と git が入っている。PowerShell 7 の実行ポリシーは `RemoteSigned`（`Unrestricted`・`Bypass` でもよい）にしてある
- 実行場所: この PC のデスクトップの管理者ではない PowerShell 7。この項の手順 1 で開く
- 更新では、手元の変更の確認を省かない。入れ替えを妨げるプレビューがあれば、閉じてからやり直す

1. 管理者ではない PowerShell 7 を開く。

   - スタートメニューで「PowerShell」を探し、クリックして開く（「Windows PowerShell」ではない。「管理者として実行」にはしない）
   - 「PowerShell」が無ければ、[アプリを入れる](https://github.com/ryo-aoki-pc/setup-notes/blob/main/docs/windows-setup.md#アプリを入れる)の手順 6〜9 を通す

1. Node.js と git の版を確かめる。

   ```powershell
   "`n$([char]27)[7m 確認 $([char]27)[0m"
   node --version
   git --version
   ```

   - Node.js は `v22.` 以上、git は `git version` と版が出ればよい
   - コマンドが無いか Node.js が 22 より古ければ、導入・更新してからこの項の手順 2 を貼り直す

1. clone があれば、手元の変更を確かめる。

   ```powershell
   # 既存の clone の変更を確かめる
   if ([string]::IsNullOrWhiteSpace($env:USERPROFILE)) { throw "USERPROFILE が空です" }
   if (Test-Path -LiteralPath "$env:USERPROFILE\repos\md-preview") {
     "`n$([char]27)[7m 確認 $([char]27)[0m"
     git -C "$env:USERPROFILE\repos\md-preview" status --short
   }
   ```

   - 何も出なければ、この項の手順 4 へ進む
   - 変更やエラーが出たら、その内容を解決してからこの項の手順 3 を貼り直す

1. リポジトリを clone し、既にあれば更新する。

   ```powershell
   # md-preview のソースを取得・更新する
   if ([string]::IsNullOrWhiteSpace($env:USERPROFILE)) { throw "USERPROFILE が空です" }
   New-Item -ItemType Directory -Path "$env:USERPROFILE\repos" -Force -ErrorAction Stop | Out-Null
   if (Test-Path -LiteralPath "$env:USERPROFILE\repos\md-preview") {
     git -C "$env:USERPROFILE\repos\md-preview" pull --ff-only
   } else {
     git clone https://github.com/ryo-aoki-pc/md-preview.git "$env:USERPROFILE\repos\md-preview"
   }
   ```

   - エラーなく終わればよい
   - `pull --ff-only` が失敗したら、中断して clone の状態を解決してからこの項の手順 3 へ戻る

1. コマンドを導入し、導入の状態を確かめる。

   ```powershell
   & {
     # mdpreview を導入する
     if ([string]::IsNullOrWhiteSpace($env:USERPROFILE)) { throw "USERPROFILE が空です" }
     Push-Location -LiteralPath "$env:USERPROFILE\repos\md-preview" -ErrorAction Stop
     try {
       "`n$([char]27)[7m 確認 $([char]27)[0m"
       node scripts/setup.mjs
       if ($LASTEXITCODE -ne 0) { return }
       node scripts/setup.mjs --check
     } finally {
       Pop-Location
     }
   }
   ```

   - 最後に `導入済み: mdpreview` と版、起動役のパスが出ればよい
   - `問題:` が出たら、案内に従って直してからこの項の手順 5 を貼り直す
   - `%USERPROFILE%\.local\bin` が PATH に無いときは、PATH に加えて PowerShell 7 を開き直し、この項の手順 5 を貼り直す
   - `導入先を入れ替えられません` で止まったら、プレビューを閉じてこの項の手順 5 を貼り直す
   - `ほかの導入が動いています` で止まったら、そのプロセスが終わるのを待ってこの項の手順 5 を貼り直す

1. 起動役から版を確かめる。

   ```powershell
   mdpreview --version
   ```

   - この項の手順 5 の `導入済み: mdpreview` に続く版と同じ版が出ればよい
   - コマンドが無ければ PATH を直して PowerShell 7 を開き直し、この項の手順 6 を貼り直す

1. 小さな Markdown を開き、URL を確かめて止める。

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

   - `プレビュー: http://127.0.0.1:` で始まる URL が出ればよい
   - URL を確かめたら、同じ窓で Ctrl+C を押して止める
   - エラーが出たら、中断してこの項の手順 5 の導入状態を確かめる

<details id="windows-11-で-md-preview-を導入更新する-の補足と注意点">
  <summary>Windows 11 で md-preview を導入・更新する の補足と注意点</summary>

**[Windows 11 で md-preview を導入・更新する](#windows-11-で-md-preview-を導入更新する)の手順 4**

- 更新は `pull --ff-only` の後に、この項の手順 5〜7 の導入・確認を繰り返す

**[Windows 11 で md-preview を導入・更新する](#windows-11-で-md-preview-を導入更新する)の手順 5**

- 導入先は `%LOCALAPPDATA%\md-preview`。空の `LOCALAPPDATA` は `%USERPROFILE%\AppData\Local` として扱う
- 起動役は `%USERPROFILE%\.local\bin` の `mdpreview.ps1`（PowerShell）・`mdpreview.cmd`（cmd）・`mdpreview`（Git Bash）
- `--check` は何も変えず、写したファイル・依存・起動役・PATH を確かめる
- 一時フォルダーで組み立てて確かめてから入れ替える。導入の記録は導入先の `install.json` に置く
- `--install-dir` で ASCII 外の文字か `%` か `"` を含む独自のパスを指定した場合は、cmd 用を置かず、PowerShell と Git Bash 用を置く
- 終わると元の作業ディレクトリへ戻る

**[Windows 11 で md-preview を導入・更新する](#windows-11-で-md-preview-を導入更新する)の手順 7**

- `--no-open` はブラウザを開かずに起動する。終了すると試験用のフォルダーを消す
- 試験用の Markdown を専用のフォルダーに作るのは、そのフォルダーがルート（読める範囲）になるため

</details>

---

### Windows 11 で md-preview の公式スタイルを取得する

- 任意で GitLab の公式の CSS・フォントを取得する。未導入のときは約 785 MB を取得し、約 4 MB を保存する。取得元へ通信する
- 前提: [Windows 11 で md-preview を導入・更新する](#windows-11-で-md-preview-を導入更新する)を通し、`mdpreview` が PATH にある
- 実行場所: この PC のデスクトップの管理者ではない PowerShell 7。この項の手順 1 で開く
- アプリを更新して公式スタイルの版が変わったときも、この項の手順を通す

1. 管理者ではない PowerShell 7 を開く。

   - スタートメニューで「PowerShell」を探し、クリックして開く（「Windows PowerShell」ではない。「管理者として実行」にはしない）
   - 「PowerShell」が無ければ、[アプリを入れる](https://github.com/ryo-aoki-pc/setup-notes/blob/main/docs/windows-setup.md#アプリを入れる)の手順 6〜9 を通す

1. 公式スタイルを取得する。

   ```powershell
   mdpreview install-style
   ```

   - `mdpreview: 公式資産を導入しました:` と保存先、または `mdpreview: 導入済みの公式資産を確認しました。` が出ればよい
   - エラーが出たら、通信先と保存先を確かめてからこの項の手順 2 を貼り直す

1. 公式スタイルを使えることを確かめる。

   ```powershell
   mdpreview install-style --check
   ```

   - `mdpreview: 公式スタイルを利用できます` が出ればよい（終了コード 0）
   - `mdpreview: 公式スタイルが未導入または破損しています` が出たら、この項の手順 2 からやり直す（終了コード 1）

<details id="windows-11-で-md-preview-の公式スタイルを取得する-の補足と注意点">
  <summary>Windows 11 で md-preview の公式スタイルを取得する の補足と注意点</summary>

**[Windows 11 で md-preview の公式スタイルを取得する](#windows-11-で-md-preview-の公式スタイルを取得する)の手順 2**

- 保存先は `%LOCALAPPDATA%\md-preview-assets`。空の `LOCALAPPDATA` は `%USERPROFILE%\AppData\Local` として扱う。直下に版ごとのフォルダーを置く
- 取得元は `registry.gitlab.com` など。固定した版・ファイル・取得元は [assets/assets.lock.json](../assets/assets.lock.json) にある
- ライセンスの原文も保存先に置く。公式資産はリポジトリや書き出す HTML に含めて再配布せず、使う端末ごとに取得する
- 次に起こしたプレビューから公式の CSS とフォント（GitLab Sans）を使う。コードの配色はライト・ダークとも Dracula
- 保存先のフォルダーを消すと、同梱のスタイルでの表示に戻る
- `--dir` で保存先を変えた場合、`--check` にも同じ `--dir` を付ける。プレビューには `--assets-dir` でその保存先を指定する。`--no-official-style` で同梱のスタイルを選べる

**[Windows 11 で md-preview の公式スタイルを取得する](#windows-11-で-md-preview-の公式スタイルを取得する)の手順 3**

- `--check` は通信せず、導入済みのファイルを検査する。プレビューの表示と HTML への書き出しでも取得しない

</details>

---

### Windows 11 で PlantUML を入れる

- 任意で PlantUML を入れる。入れると、`plantuml` のコードブロックの図を、手元の `plantuml` コマンドで描いて表示する。scoop で Java・PlantUML・Graphviz を入れる（取得元へ通信する）
- 前提: scoop が入っている（setup-notes の [アプリを入れる](https://github.com/ryo-aoki-pc/setup-notes/blob/main/docs/windows-setup.md#アプリを入れる)の手順 2・3）。[Windows 11 で md-preview を導入・更新する](#windows-11-で-md-preview-を導入更新する)を通し、`mdpreview` が PATH にある
- 実行場所: この PC のデスクトップの管理者ではない PowerShell 7。この項の手順 1 で開く
- 入れた後に起こしたプレビューから効く。既に開いている窓とアプリ（Neovim など）は、開き直してから使う

1. 管理者ではない PowerShell 7 を開く。

   - スタートメニューで「PowerShell」を探し、クリックして開く（「Windows PowerShell」ではない。「管理者として実行」にはしない）
   - 「PowerShell」が無ければ、[アプリを入れる](https://github.com/ryo-aoki-pc/setup-notes/blob/main/docs/windows-setup.md#アプリを入れる)の手順 6〜9 を通す

1. scoop に、`java` と `extras` のバケットを足す。

   ```powershell
   # java と extras のバケットを、無いときだけ足す
   foreach ($bucket in 'java', 'extras') {
     if (-not (scoop bucket list | Where-Object Name -eq $bucket)) { scoop bucket add $bucket }
   }
   "`n$([char]27)[7m 確認 $([char]27)[0m"
   scoop bucket list
   ```

   - 一覧に `java` と `extras` の行が出ればよい

1. Java・PlantUML・Graphviz を入れる。

   ```powershell
   scoop install temurin-lts-jre plantuml graphviz
   ```

   - `temurin-lts-jre`・`plantuml`・`graphviz` が、それぞれ `was installed successfully!` になればよい
   - `is already installed.` と出たものは導入済みで、何も変えない

1. PlantUML・Java・Graphviz の版を確かめる。

   ```powershell
   "`n$([char]27)[7m 確認 $([char]27)[0m"
   plantuml -version | Select-Object -First 1
   java -version
   dot -V
   ```

   - `PlantUML version`・`openjdk version`・`dot - graphviz version` に続いて、それぞれの版が出ればよい
   - コマンドが無いと出たら、PowerShell 7 を開き直してこの項の手順 4 を貼り直す

1. 図を含む小さな Markdown を HTML に書き出し、図が入ったことを確かめる。

   ```powershell
   & {
     # 図を含む試験用の Markdown を、専用のフォルダーに作って書き出す
     $previewDirectory = Join-Path ([IO.Path]::GetTempPath()) ("md-preview-" + [guid]::NewGuid())
     $fence = '`' * 3
     try {
       New-Item -ItemType Directory -Path $previewDirectory -ErrorAction Stop | Out-Null
       Set-Content -LiteralPath "$previewDirectory\preview.md" -Value "# 図の確認`n`n${fence}plantuml`nAlice -> Bob: こんにちは`n$fence" -Encoding utf8 -ErrorAction Stop
       "`n$([char]27)[7m 確認 $([char]27)[0m"
       mdpreview render -o "$previewDirectory\preview.html" "$previewDirectory\preview.md"
       # 書き出した HTML に入った SVG の図を数える
       "SVG の図: " + ([regex]::Matches((Get-Content -LiteralPath "$previewDirectory\preview.html" -Raw), 'data:image/svg\+xml;base64,')).Count
     } finally {
       Remove-Item -LiteralPath $previewDirectory -Recurse -Force -ErrorAction SilentlyContinue
     }
   }
   ```

   - `SVG の図: 1` が出ればよい
   - `mdpreview: 図: 1 件` と `SVG の図: 0` が出たら、`plantuml` が見つかっていないか、md-preview が古い。この項の手順 4 と、[Windows 11 で md-preview を導入・更新する](#windows-11-で-md-preview-を導入更新する)の更新を確かめる
   - `mdpreview: PlantUML の図を描けませんでした（1 件）` が出たら、この項の手順 4 で Java が動くことを確かめる

<details id="windows-11-で-plantuml-を入れる-の補足と注意点">
  <summary>Windows 11 で PlantUML を入れる の補足と注意点</summary>

**[Windows 11 で PlantUML を入れる](#windows-11-で-plantuml-を入れる)の手順 3**

- Java は Eclipse Temurin の LTS の JRE。scoop が、利用者の環境変数 `JAVA_HOME` を設定し、Java の `bin` を PATH に足す
- Graphviz は、クラス図などの配置に使う。PlantUML 1.2026.9 では、Graphviz が無くてもクラス図を描けた
- 更新は、setup-notes の [scoop・winget・WSL を上げる](https://github.com/ryo-aoki-pc/setup-notes/blob/main/docs/windows-setup.md#scoopwingetwsl-を上げる)で、ほかの scoop のアプリと一緒に上がる
- `scoop uninstall plantuml` で外すと、図は原文のコードブロックの表示に戻る

**[Windows 11 で PlantUML を入れる](#windows-11-で-plantuml-を入れる)の手順 5**

- `mdpreview` は、PATH に `plantuml` があると、図の原文を標準入力で渡して SVG にする。本文と図の原文は外へ送らない
- 図の中から手元のファイルを取り込む記法（`!include` など）と、URL を読む記法は使えない（PlantUML の `SANDBOX` で止める）。PlantUML に同梱の標準ライブラリ（`!include <C4/C4_Container>` など）は使える
- 構文に誤りのある図は、PlantUML が誤りの場所を描いた図になる。SVG にならなかった図は、原文のコードブロックで表示する
- `--no-plantuml` を付けると、`plantuml` があっても使わない

</details>

---
