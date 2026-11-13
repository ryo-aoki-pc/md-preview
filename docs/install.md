# md-preview の導入と更新（AlmaLinux 10・Windows 11）

- Markdown を GitLab の描画でプレビューする `mdpreview` コマンド（[md-preview](../README.md)）を入れる。本文は GitLab に送らず、手元で描く
- 使い方は [md-preview の使い方](usage.md) にある
- どの項も単独で通せる。項のリードの前提を先に満たす
- bash のブロックはブラケットペーストの効く端末に、PowerShell のブロックは PowerShell 7 に貼り、貼った後に Enter を押す。貼った後は、反転表示の「確認」から後ろの出力を箇条書きで確かめる
- [PowerShell 7](https://learn.microsoft.com/en-us/powershell/scripting/install/install-powershell-on-windows)が入っていて、実行ポリシーが `RemoteSigned`（`Unrestricted`・`Bypass` でもよい）であることを確かめてある
- PowerShell の貼り方は [Windows Terminal で複数行を 1 つの入力として貼る](https://github.com/ryo-aoki-pc/setup-notes/blob/main/docs/shell-terminal.md#windows-terminal-で複数行を-1-つの入力として貼る)に従う

<details id="この文書の補足と注意点">
  <summary>この文書の補足と注意点</summary>

**選択した方針**

- 実行ファイルを 1 つ置くだけにする。ほかに入れるものは無い
- 取得したファイルは sha256 で確かめる

**参照**

- [md-preview](../README.md)

</details>

## AlmaLinux 10 で使う

- GNOME にログインした自分のユーザーで行う。管理者の端末は使わない

### md-preview を導入・更新する

- GitHub Releases の実行ファイルを取得し、`mdpreview` コマンドを入れて確かめる。更新もこの項の手順を通す
- 前提: `curl`・`sha256sum`・`install` が入っている。対応する CPU は x86-64 と AArch64
- 実行場所: GNOME にログインした自分のユーザーの端末（bash）。`sudo` は使わない。この項の手順 1〜4 は、同じ端末で続けて行う（前の手順で決めた名前とフォルダーを使う）

1. CPU を確かめ、取得するファイルの名前を決める。

   ```bash
   # CPU に合う配布ファイルを選ぶ
   preview_download_name=$(
     case "$(uname -m)" in
       x86_64) printf 'mdpreview-linux-amd64\n' ;;
       aarch64) printf 'mdpreview-linux-arm64\n' ;;
       *) printf '対象外の CPU です。\n' >&2; exit 1 ;;
     esac
   ) &&
     printf '\n\033[7m 確認 \033[0m\n' &&
     printf '%s\n' "$preview_download_name"
   ```

   - `uname -m` が `x86_64` なら `mdpreview-linux-amd64`、`aarch64` なら `mdpreview-linux-arm64` が出ればよい
   - `対象外の CPU です。` が出たら、この項はここで止める

1. 一時フォルダーに実行ファイルと `SHA256SUMS` を取得する。

   ```bash
   # 配布ファイルと sha256 の一覧を、専用のフォルダーへ取得する
   preview_download_dir=$(mktemp -d) &&
     curl -fL -o "$preview_download_dir/$preview_download_name" "https://github.com/ryo-aoki-pc/md-tools/releases/latest/download/$preview_download_name" &&
     curl -fL -o "$preview_download_dir/SHA256SUMS" 'https://github.com/ryo-aoki-pc/md-tools/releases/latest/download/SHA256SUMS'
   ```

   - エラーなく終わればよい
   - 取得に失敗したら、この項の手順 3 へ進まず、通信先を確かめてこの項の手順 2 を貼り直す

1. 取得した実行ファイルの sha256 を確かめる。

   ```bash
   (
     cd -- "$preview_download_dir" || exit
     printf '\n\033[7m 確認 \033[0m\n'
     sha256sum -c --ignore-missing SHA256SUMS
   )
   ```

   - `<取得するファイルの名前>: OK` が出ればよい
   - `FAILED` やエラーが出たら、取得したファイルを使わず、この項の手順 2 からやり直す

1. 実行ファイルを置き、一時フォルダーを消す。

   ```bash
   # コマンドを置き、取得に使ったファイルを片付ける
   mkdir -p ~/.local/bin &&
     install -m 755 "$preview_download_dir/$preview_download_name" ~/.local/bin/mdpreview &&
     rm -- "$preview_download_dir/$preview_download_name" "$preview_download_dir/SHA256SUMS" &&
     rmdir -- "$preview_download_dir"
   ```

   - エラーなく終わればよい
   - 動いているプレビューがあっても置き換えられる。次に起こすプレビューから、取得した版を使う

1. コマンドの版を確かめる。

   ```bash
   printf '\n\033[7m 確認 \033[0m\n'
   mdpreview --version
   ```

   - `0.18.0` などの版が出ればよい。GitHub Releases の最新の版と比べる
   - `~/.local/bin` が PATH に無いときは、PATH に加えて端末を開き直し、この項の手順 5 を貼り直す
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
   - エラーが出たら、中断してこの項の手順 5 の版と PATH を確かめる

<details id="md-preview-を導入更新する-の補足と注意点">
  <summary>md-preview を導入・更新する の補足と注意点</summary>

**[md-preview を導入・更新する](#md-preview-を導入更新する)のリード**

- 導入も更新も、この項の手順 1〜6 を通す

**[md-preview を導入・更新する](#md-preview-を導入更新する)の手順 4**

- 置き場所は `~/.local/bin/mdpreview`。外すときはこのファイルを消す
- 公式スタイルも外すときは、[公式スタイルの取得](#md-preview-の公式スタイルを取得する)に書いた保存先の `md-preview-assets` のフォルダーを消す
- 同梱しているもののライセンスの原文は `mdpreview --licenses` で出す

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

- GitHub Releases の実行ファイルを取得し、`mdpreview` コマンドを入れて確かめる。更新もこの項の手順を通す
- 前提: x86-64（amd64）の Windows 11 と PowerShell 7
- 実行場所: この PC のデスクトップの管理者ではない PowerShell 7。この項の手順 1 で開く。この項の手順 2〜4 は、同じ窓で続けて行う（前の手順で決めた名前とフォルダーを使う）
- 更新するときは、先に動いているプレビューを止める。Neovim から使っているときは Neovim を閉じる

1. 管理者ではない PowerShell 7 を開く。

   - スタートメニューで「PowerShell」を探し、クリックして開く（「Windows PowerShell」ではない。「管理者として実行」にはしない）
   - 「PowerShell」が無ければ、[PowerShell 7](https://learn.microsoft.com/en-us/powershell/scripting/install/install-powershell-on-windows)を入れる

1. 一時フォルダーに実行ファイルと `SHA256SUMS` を取得する。

   ```powershell
   # 配布ファイルと sha256 の一覧を、TEMP の専用のフォルダーへ取得する
   if ([string]::IsNullOrWhiteSpace($env:TEMP)) { throw "TEMP が空です" }
   $downloadDirectory = Join-Path $env:TEMP ("md-preview-download-" + [guid]::NewGuid())
   New-Item -ItemType Directory -Path $downloadDirectory -ErrorAction Stop | Out-Null
   $downloadName = 'mdpreview-windows-amd64.exe'
   $downloadFile = Join-Path $downloadDirectory $downloadName
   Invoke-WebRequest -Uri "https://github.com/ryo-aoki-pc/md-tools/releases/latest/download/$downloadName" -OutFile $downloadFile -ErrorAction Stop
   Invoke-WebRequest -Uri 'https://github.com/ryo-aoki-pc/md-tools/releases/latest/download/SHA256SUMS' -OutFile "$downloadDirectory\SHA256SUMS" -ErrorAction Stop
   ```

   - エラーなく終わればよい
   - 取得に失敗したら、この項の手順 3 へ進まず、通信先を確かめてこの項の手順 2 を貼り直す

1. 取得した実行ファイルの sha256 を比べる。

   ```powershell
   # 一覧の同じファイル名の行と、大文字・小文字を区別せずに比べる
   $sumPattern = '^[0-9a-fA-F]{64}\s+\*?' + [regex]::Escape($downloadName) + '$'
   $sumLines = @(Get-Content -LiteralPath "$downloadDirectory\SHA256SUMS" -ErrorAction Stop | Where-Object { $_ -match $sumPattern })
   if ($sumLines.Count -ne 1) {
     Remove-Item -LiteralPath $downloadFile -Force -ErrorAction Stop
     throw "SHA256SUMS に対象のファイルの行が 1 つありません。取得をやり直してください"
   }
   $expectedHash = ($sumLines[0] -split '\s+')[0]
   $actualHash = (Get-FileHash -LiteralPath $downloadFile -Algorithm SHA256 -ErrorAction Stop).Hash
   if ($actualHash -ine $expectedHash) {
     Remove-Item -LiteralPath $downloadFile -Force -ErrorAction Stop
     throw "sha256 が一致しません。取得した実行ファイルを削除しました"
   }
   "`n$([char]27)[7m 確認 $([char]27)[0m"
   "$downloadName : sha256 が一致しました"
   ```

   - `mdpreview-windows-amd64.exe : sha256 が一致しました` が出ればよい
   - 一致しない場合やエラーが出た場合は、この項の手順 4 へ進まず、この項の手順 2 からやり直す

1. 動いているプレビューを止め、実行ファイルを置く。

   - プレビューを起こした窓で Ctrl+C を押す。Neovim から使っているときは Neovim を閉じる

   ```powershell
   # mdpreview.exe を置き、取得に使ったフォルダーを片付ける
   if ([string]::IsNullOrWhiteSpace($env:USERPROFILE)) { throw "USERPROFILE が空です" }
   $previewBin = Join-Path $env:USERPROFILE '.local\bin'
   New-Item -ItemType Directory -Path $previewBin -Force -ErrorAction Stop | Out-Null
   Move-Item -LiteralPath $downloadFile -Destination (Join-Path $previewBin 'mdpreview.exe') -Force -ErrorAction Stop
   Remove-Item -LiteralPath "$downloadDirectory\SHA256SUMS" -ErrorAction Stop
   Remove-Item -LiteralPath $downloadDirectory -ErrorAction Stop
   ```

   - エラーなく終わればよい
   - 動いているプレビューがあると置き換えられない。プレビューを止め、Neovim を閉じてから、この項の手順 4 を貼り直す

1. コマンドの版を確かめる。

   ```powershell
   "`n$([char]27)[7m 確認 $([char]27)[0m"
   mdpreview --version
   ```

   - `0.18.0` などの版が出ればよい。GitHub Releases の最新の版と比べる
   - `%USERPROFILE%\.local\bin` が PATH に無いときは、PATH に加えて PowerShell 7 を開き直し、この項の手順 5 を貼り直す
   - コマンドが無ければ PATH を直して PowerShell 7 を開き直し、この項の手順 5 を貼り直す

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
   - エラーが出たら、中断してこの項の手順 5 の版と PATH を確かめる

<details id="windows-11-で-md-preview-を導入更新する-の補足と注意点">
  <summary>Windows 11 で md-preview を導入・更新する の補足と注意点</summary>

**[Windows 11 で md-preview を導入・更新する](#windows-11-で-md-preview-を導入更新する)のリード**

- 導入も更新も、この項の手順 1〜6 を通す

**[Windows 11 で md-preview を導入・更新する](#windows-11-で-md-preview-を導入更新する)の手順 4**

- 置き場所は `%USERPROFILE%\.local\bin\mdpreview.exe`。外すときは、プレビューを止めてこのファイルを消す
- 公式スタイルも外すときは、[公式スタイルの取得](#windows-11-で-md-preview-の公式スタイルを取得する)に書いた保存先の `md-preview-assets` のフォルダーを消す
- 同梱しているもののライセンスの原文は `mdpreview --licenses` で出す

**[Windows 11 で md-preview を導入・更新する](#windows-11-で-md-preview-を導入更新する)の手順 6**

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
   - 「PowerShell」が無ければ、[PowerShell 7](https://learn.microsoft.com/en-us/powershell/scripting/install/install-powershell-on-windows)を入れる

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
- 前提: [scoop](https://scoop.sh/)が入っている。[Windows 11 で md-preview を導入・更新する](#windows-11-で-md-preview-を導入更新する)を通し、`mdpreview` が PATH にある
- 実行場所: この PC のデスクトップの管理者ではない PowerShell 7。この項の手順 1 で開く
- 入れた後に起こしたプレビューから効く。既に開いている窓とアプリ（Neovim など）は、開き直してから使う

1. 管理者ではない PowerShell 7 を開く。

   - スタートメニューで「PowerShell」を探し、クリックして開く（「Windows PowerShell」ではない。「管理者として実行」にはしない）
   - 「PowerShell」が無ければ、[PowerShell 7](https://learn.microsoft.com/en-us/powershell/scripting/install/install-powershell-on-windows)を入れる

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
