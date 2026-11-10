# md-preview の導入と更新（AlmaLinux 10・Windows 11）

- Markdown を GitLab の描画でプレビューする `mdpreview` コマンド（[md-preview](../README.md)）を入れる。本文は GitLab に送らず、手元で描く
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
     # 試験用の Markdown を開く
     preview_file=$(mktemp --suffix=.md) || exit
     trap 'rm -f -- "$preview_file"' EXIT
     printf '# プレビューの確認\n\n本文です。\n' > "$preview_file" || exit
     printf '\n\033[7m 確認 \033[0m\n'
     mdpreview --no-open "$preview_file"
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

- `--no-open` はブラウザを開かずに起動する。終了すると試験用の一時ファイルを消す

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
     # 試験用の Markdown を開く
     $previewFile = Join-Path ([IO.Path]::GetTempPath()) ("md-preview-" + [guid]::NewGuid() + ".md")
     try {
       Set-Content -LiteralPath $previewFile -Value "# プレビューの確認`n`n本文です。" -Encoding utf8 -ErrorAction Stop
       "`n$([char]27)[7m 確認 $([char]27)[0m"
       mdpreview --no-open $previewFile
     } finally {
       Remove-Item -LiteralPath $previewFile -Force -ErrorAction SilentlyContinue
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

- `--no-open` はブラウザを開かずに起動する。終了すると試験用の一時ファイルを消す

</details>

---
