# mdtable の導入と更新（Windows 11）

- GLFM の文書の表を Excel で編集して書き戻す `mdtable` コマンド（[md-table-excel](../README.md)）を入れる
- 使い方は [mdtable の使い方](usage.md) にある
- どの項も単独で通せる。項のリードの前提を先に満たす
- PowerShell のブロックは PowerShell 7 に貼り、貼った後に Enter を押す。貼った後は、反転表示の「確認」から後ろの出力を箇条書きで確かめる
- PowerShell 7 の導入と実行ポリシーは、setup-notes の [アプリを入れる](https://github.com/ryo-aoki-pc/setup-notes/blob/main/docs/windows-setup.md#アプリを入れる)の手順 6〜9 に従う
- PowerShell の貼り方は [Windows Terminal で複数行を 1 つの入力として貼る](https://github.com/ryo-aoki-pc/setup-notes/blob/main/docs/shell-terminal.md#windows-terminal-で複数行を-1-つの入力として貼る)に従う

<details id="この文書の補足と注意点">
  <summary>この文書の補足と注意点</summary>

**選択した方針**

- 実行ファイルを 1 つ置くだけにする。取得したファイルは sha256 で確かめる

**参照**

- [md-table-excel](../README.md)
- 検証記録（setup-notes-verification の `repos/md-table-excel/docs/verification/readme.md`）

</details>

## Windows 11 で使う

- この PC のデスクトップで、自分のユーザーが行う。管理者の窓は使わない

### Windows 11 で mdtable を導入・更新する

- GitHub Releases の実行ファイルを取得し、`mdtable` コマンドを入れて確かめる。更新もこの項の手順を通す
- 前提: x86-64（amd64）の Windows 11 と PowerShell 7。ブックを編集するときは、この PC に Excel が要る
- 実行場所: この PC のデスクトップの管理者ではない PowerShell 7。この項の手順 1 で開く。この項の手順 2〜4 は同じ窓で続ける
- 更新するときは、先に動いている `mdtable` を終了する。`edit` で開いたブックは保存して閉じる

1. 管理者ではない PowerShell 7 を開く。

   - スタートメニューで「PowerShell」を探し、クリックして開く（「Windows PowerShell」ではない。「管理者として実行」にはしない）
   - 「PowerShell」が無ければ、[アプリを入れる](https://github.com/ryo-aoki-pc/setup-notes/blob/main/docs/windows-setup.md#アプリを入れる)の手順 6〜9 を通す

1. 一時フォルダーに実行ファイルと `SHA256SUMS` を取得する。

   ```powershell
   # 配布ファイルと sha256 の一覧を、TEMP の専用のフォルダーへ取得する
   if ([string]::IsNullOrWhiteSpace($env:TEMP)) { throw "TEMP が空です" }
   $downloadDirectory = Join-Path $env:TEMP ("mdtable-download-" + [guid]::NewGuid())
   New-Item -ItemType Directory -Path $downloadDirectory -ErrorAction Stop | Out-Null
   $downloadName = 'mdtable-windows-amd64.exe'
   $downloadFile = Join-Path $downloadDirectory $downloadName
   Invoke-WebRequest -Uri "https://github.com/ryo-aoki-pc/md-table-excel/releases/latest/download/$downloadName" -OutFile $downloadFile -ErrorAction Stop
   Invoke-WebRequest -Uri 'https://github.com/ryo-aoki-pc/md-table-excel/releases/latest/download/SHA256SUMS' -OutFile "$downloadDirectory\SHA256SUMS" -ErrorAction Stop
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

   - `mdtable-windows-amd64.exe : sha256 が一致しました` が出ればよい
   - 一致しない場合やエラーが出た場合は、この項の手順 4 へ進まず、この項の手順 2 からやり直す

1. 実行ファイルを置き、一時フォルダーを消す。

   ```powershell
   # mdtable.exe を置き、取得に使ったフォルダーを片付ける
   if ([string]::IsNullOrWhiteSpace($env:USERPROFILE)) { throw "USERPROFILE が空です" }
   $mdtableBin = Join-Path $env:USERPROFILE '.local\bin'
   New-Item -ItemType Directory -Path $mdtableBin -Force -ErrorAction Stop | Out-Null
   Move-Item -LiteralPath $downloadFile -Destination (Join-Path $mdtableBin 'mdtable.exe') -Force -ErrorAction Stop
   Remove-Item -LiteralPath "$downloadDirectory\SHA256SUMS" -ErrorAction Stop
   Remove-Item -LiteralPath $downloadDirectory -ErrorAction Stop
   ```

   - エラーなく終わればよい
   - 使用中で置き換えられないときは、動いている `mdtable` を終了し、この項の手順 4 を貼り直す

1. `%USERPROFILE%\.local\bin` が PATH に無ければ、ユーザーの PATH に加えて PowerShell 7 を開き直す。

   - スタートメニューで「環境変数を編集」を探し、「環境変数」を開く
   - 自分のユーザーの `Path` の「編集」→「新規」で `%USERPROFILE%\.local\bin` を加える
   - 既にあれば変更せず、この項の手順 6 へ進む

1. コマンドの版と場所を確かめる。

   ```powershell
   "`n$([char]27)[7m 確認 $([char]27)[0m"
   mdtable --version
   where.exe mdtable
   ```

   - `mdtable 0.6.0` などが出ればよい。GitHub Releases の取得した版と比べる
   - `where.exe mdtable` の最初が `%USERPROFILE%\.local\bin\mdtable.exe` ならよい
   - コマンドが無ければ、この項の手順 5 で PATH を確かめ、PowerShell 7 を開き直してこの項の手順 6 を貼り直す
   - ほかの場所が先に出るときは、ユーザーの `Path` で `%USERPROFILE%\.local\bin` をその場所より上へ移し、PowerShell 7 を開き直してこの項の手順 6 を貼り直す

<details id="windows-11-で-mdtable-を導入更新する-の補足と注意点">
  <summary>Windows 11 で mdtable を導入・更新する の補足と注意点</summary>

**[Windows 11 で mdtable を導入・更新する](#windows-11-で-mdtable-を導入更新する)のリード**

- 導入も更新も、この項の手順 1〜6 を通す

**[Windows 11 で mdtable を導入・更新する](#windows-11-で-mdtable-を導入更新する)の手順 4**

- 置き場所は `%USERPROFILE%\.local\bin\mdtable.exe`
- 同梱しているもののライセンスの原文は `mdtable --licenses` で出す。GitHub Releases の `THIRD-PARTY-LICENSES.txt` も同じ内容

</details>

---
