# mdtable の導入と更新（AlmaLinux 10・Windows 11）

- GLFM の Markdown の表を、Excel のブックにして編集し、Markdown へ戻す（[md-table-excel](../README.md)）
- 使い方は [mdtable の使い方](usage.md) にある
- どの項も単独で通せる。項のリードの前提を先に満たす
- bash のブロックはブラケットペーストの効く端末に、PowerShell のブロックは PowerShell 7 に貼り、貼った後に Enter を押す。貼り方と、反転表示の「確認」から後ろの出力の読み方は、setup-notes の [AlmaLinux 10 のセットアップ](https://github.com/ryo-aoki-pc/setup-notes/blob/main/docs/almalinux-setup.md) と [Windows 11 のセットアップ](https://github.com/ryo-aoki-pc/setup-notes/blob/main/docs/windows-setup.md) の冒頭に従う
- macOS の導入は [README の「導入」](../README.md#導入) にある

<details id="この文書の補足と注意点">
  <summary>この文書の補足と注意点</summary>

**参照**

- 検証記録（setup-notes-verification の `docs/verification/almalinux-setup.md`）
- 検証記録（setup-notes-verification の `docs/verification/windows-setup.md`）

</details>

## AlmaLinux 10 で使う

- GNOME にログインした自分のユーザーで行う。前提の setup-notes の項は、各項のリードのリンクから開く

### mdtable を導入・更新する

- `mdtable` を `~/md-table-excel` に clone し、専用の仮想環境に入れて確かめる。更新も、この項の手順 1 で行う
- 前提: Python 3.9 以上と git が入っている（git は[Homebrew](https://github.com/ryo-aoki-pc/setup-notes/blob/main/docs/almalinux-setup.md#homebrew)の手順 1 で入る）
- 実行場所: GNOME にログインした自分のユーザーの端末（bash）。`sudo` は、ensurepip で止まったときの `python3-pip` の導入だけで使う。更新では、`~/md-table-excel` の手元の変更の確認を省かない

1. リポジトリを clone し、導入して確かめる。

   ```bash
   # md-table-excel のソースを取得する
   git clone https://github.com/ryo-aoki-pc/md-table-excel.git ~/md-table-excel
   # コマンドを導入して設定を検査する
   (
     cd ~/md-table-excel &&
       python3 scripts/setup-mdtable.py &&
       python3 scripts/setup-mdtable.py --check
   )
   ```

   - ensurepip で止まったら `sudo dnf install python3-pip` を行う
   - `~/.local/bin` が PATH に無いときは、案内に従って加える
   - 更新時は `git -C ~/md-table-excel pull --ff-only` し、この項の手順 1 の `(`〜`)` の導入・確認を再実行する

<details id="mdtable-を導入更新する-の補足と注意点">
  <summary>mdtable を導入・更新する の補足と注意点</summary>

**[mdtable を導入・更新する](#mdtable-を導入更新する)の手順 1**

- 依存はリポジトリ外の専用仮想環境へ入る

- `--check` は何も変更せず状態を確かめる

- 起動コマンドは `~/.local/bin`。PATH に無いときは案内に従って加える

</details>

---

## Windows 11 で使う

- この PC のデスクトップで、自分のユーザーが行う。前提の setup-notes の項は、各項のリードのリンクから開く

### Windows 11 で mdtable を導入・更新する

- Markdown と Excel を往復する mdtable を入れ、導入状態を確かめる
- 前提: [アプリを入れる](https://github.com/ryo-aoki-pc/setup-notes/blob/main/docs/windows-setup.md#アプリを入れる)の手順 2・3 と[Git Bash と WezTerm の設定](https://github.com/ryo-aoki-pc/setup-notes/blob/main/docs/shell-terminal.md#git-bash-と-wezterm-の設定)を通してある
- 実行場所: この PC のデスクトップの管理者ではない PowerShell 7。この項の手順 1 で開く
- Python 3.9 以上が要る。この項の手順 2 で、無ければ scoop で入れる
- 更新時は `git -C "$env:USERPROFILE\md-table-excel" status` で既存の変更を確かめてから進む

1. 管理者ではない PowerShell 7 を開く。

   - スタートメニューで「PowerShell」を探し、クリックして開く（「Windows PowerShell」ではない。「管理者として実行」にはしない）
   - 「PowerShell」が無ければ、[アプリを入れる](https://github.com/ryo-aoki-pc/setup-notes/blob/main/docs/windows-setup.md#アプリを入れる)の手順 6〜9 を通す

1. Python が無ければ scoop で入れ、版を確かめる。

   ```powershell
   # Python を導入する
   if (-not (Get-Command python -ErrorAction SilentlyContinue)) { scoop install python }
   "`n$([char]27)[7m 確認 $([char]27)[0m"
   python --version
   ```

   - `Python 3.9` 以上の版が出ればよい
   - 3.9 より古い版やエラーが出たら、その Python を更新してから貼り直す

1. リポジトリを clone し、導入して確かめる。

   ```powershell
   # md-table-excel のリポジトリを取得する
   git clone https://github.com/ryo-aoki-pc/md-table-excel.git "$env:USERPROFILE\md-table-excel"
   & {
     # mdtable を導入する
     Push-Location -LiteralPath "$env:USERPROFILE\md-table-excel" -ErrorAction Stop
     try {
       python scripts/setup-mdtable.py
       "`n$([char]27)[7m 確認 $([char]27)[0m"
       python scripts/setup-mdtable.py --check
     } finally {
       Pop-Location
     }
   }
   ```

   - 起動コマンドは `%USERPROFILE%\.local\bin`。PATH に無いときは案内に従って加える
   - 更新時は `git -C "$env:USERPROFILE\md-table-excel" pull --ff-only` を実行し、この項の手順 3 の `& { ... }` 部分（導入・確認）を貼り直す。`git clone` は再実行しない

<details id="windows-11-で-mdtable-を導入更新する-の補足と注意点">
  <summary>Windows 11 で mdtable を導入・更新する の補足と注意点</summary>

**[Windows 11 で mdtable を導入・更新する](#windows-11-で-mdtable-を導入更新する)の手順 3**

- 依存はリポジトリ外の専用仮想環境へ入る

- `--check` は何も変更せず状態を確かめる

- 終わると元の作業ディレクトリへ戻る

</details>

---
