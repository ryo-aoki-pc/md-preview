# mdtable のロールバック

[手順書（AlmaLinux 10）](https://github.com/ryo-aoki-pc/setup-notes/blob/main/docs/almalinux-setup.md#mdtable-を導入更新する)・[手順書（Windows 11）](https://github.com/ryo-aoki-pc/setup-notes/blob/main/docs/windows-setup.md#mdtable-を導入更新する)・[検証記録](../verification/readme.md)・[参考資料](../reference/readme.md)

- setup-notes の「mdtable を導入・更新する」で入れたものを消す。消すのは、起動コマンド・専用の仮想環境・clone の 3 つ
- Python・git と、`~/.local/bin` を PATH に足した設定は、ほかのコマンドも使うので残す
- `setup-mdtable.py` に `--install-dir`・`--bin-dir` を付けて別の場所に入れたなら、その場所に読み替える
- 「この節の手順 N」はこの文書の手順を指す
- macOS（未検証）は、起動コマンドが `~/.local/bin/mdtable`、仮想環境が `~/Library/Application Support/md-table-excel`

## ロールバック

- 対象: AlmaLinux 10
- 実行場所: GNOME にログインした自分のユーザーの端末（bash。`sudo` は使わない）

> [!CAUTION]
> **この節の手順 3 は、`~/md-table-excel` を消す**。手元で変えて commit・push していないものは取り戻せない。この節の手順 2 で確かめてから貼る。

1. 起動コマンドと専用の仮想環境を消し、消えたか確かめる。

   ```bash
   d="${XDG_DATA_HOME:-}"; case "$d" in /*) ;; *) d="$HOME/.local/share" ;; esac
   rm -f ~/.local/bin/mdtable
   rm -rf "$d/md-table-excel"
   printf '\n\033[7m 確認 \033[0m\n'
   command -v mdtable
   ls -d ~/.local/bin/mdtable "$d/md-table-excel"
   ```

   - `command -v` が何も出さず、`ls` が 2 つとも `No such file or directory` を出せばよい
   - 仮想環境の場所は `setup-mdtable.py` と同じ決め方 (`XDG_DATA_HOME` が絶対パスのときだけ使う) にしてある
   - pip のキャッシュ（`~/.cache/pip`）は、ほかの pip も使うので残す

1. clone に、手元だけの変更が無いか確かめる。

   ```bash
   printf '\n\033[7m 確認 \033[0m\n'
   git -C ~/md-table-excel status --short
   git -C ~/md-table-excel log --branches --not --remotes --oneline
   git -C ~/md-table-excel stash list
   ```

   - 3 つとも何も出さなければ、手元だけの変更は無い（コミットしていない変更、どのブランチにも push していないコミット、stash が無い）
   - 何か出たら、要るものを別の場所へ写してから、この節の手順 3 へ進む

1. clone を消す（取り戻せない）。

   ```bash
   rm -rf ~/md-table-excel
   printf '\n\033[7m 確認 \033[0m\n'
   ls -d ~/md-table-excel
   ```

   - `No such file or directory` と出ればよい

---

## Windows 11 のロールバック

- 対象: Windows 11
- 実行場所: この PC のデスクトップの管理者ではない Windows PowerShell（5.1）。この節の手順 1 で開く

> [!CAUTION]
> **この節の手順 4 は、`%USERPROFILE%\md-table-excel` を消す**。手元で変えて commit・push していないものは取り戻せない。この節の手順 3 で確かめてから貼る。

1. 管理者ではない Windows PowerShell（5.1）を開く。

   - スタートメニューで「Windows PowerShell」を探し、クリックして開く（「管理者として実行」にはしない。PowerShell 7 の「PowerShell」ではない）

1. 起動コマンドと専用の仮想環境を消し、消えたか確かめる。

   ```powershell
   Remove-Item -LiteralPath "$env:USERPROFILE\.local\bin\mdtable.exe" -ErrorAction SilentlyContinue
   Remove-Item -LiteralPath "$env:LOCALAPPDATA\md-table-excel" -Recurse -Force -ErrorAction SilentlyContinue
   "`n$([char]27)[7m 確認 $([char]27)[0m"
   Get-Command mdtable -All -ErrorAction SilentlyContinue | Format-Table Source
   Test-Path -LiteralPath "$env:USERPROFILE\.local\bin\mdtable.exe", "$env:LOCALAPPDATA\md-table-excel"
   ```

   - `Get-Command` が何も出さず、`Test-Path` が `False` を 2 つ出せばよい
   - `True` が出たら、動いている mdtable（と、それが開いた Excel のブック）を閉じてから貼り直す
   - scoop で入れた Python は、ほかでも使うので残す（消すなら `scoop uninstall python`）

1. clone に、手元だけの変更が無いか確かめる。

   ```powershell
   "`n$([char]27)[7m 確認 $([char]27)[0m"
   git -C "$env:USERPROFILE\md-table-excel" status --short
   git -C "$env:USERPROFILE\md-table-excel" log --branches --not --remotes --oneline
   git -C "$env:USERPROFILE\md-table-excel" stash list
   ```

   - 3 つとも何も出さなければ、手元だけの変更は無い
   - 何か出たら、要るものを別の場所へ写してから、この節の手順 4 へ進む

1. clone を消す（取り戻せない）。

   ```powershell
   Remove-Item -LiteralPath "$env:USERPROFILE\md-table-excel" -Recurse -Force
   "`n$([char]27)[7m 確認 $([char]27)[0m"
   Test-Path -LiteralPath "$env:USERPROFILE\md-table-excel"
   ```

   - `False` と出ればよい

---
