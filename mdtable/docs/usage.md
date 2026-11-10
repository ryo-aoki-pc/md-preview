# mdtable の使い方（AlmaLinux 10・Windows 11）

- GLFM の Markdown の表を、Excel のブックにして編集し、Markdown へ戻す（[md-table-excel](../README.md)）
- 導入と更新は [mdtable の導入と更新](install.md) にある
- どの項も単独で通せる。項のリードの前提を先に満たす
- bash のブロックはブラケットペーストの効く端末に、PowerShell のブロックは PowerShell 7 に貼り、貼った後に Enter を押す。貼り方と、反転表示の「確認」から後ろの出力の読み方は、setup-notes の [AlmaLinux 10 のセットアップ](https://github.com/ryo-aoki-pc/setup-notes/blob/main/docs/almalinux-setup.md) と [Windows 11 のセットアップ](https://github.com/ryo-aoki-pc/setup-notes/blob/main/docs/windows-setup.md) の冒頭に従う

<details id="この文書の補足と注意点">
  <summary>この文書の補足と注意点</summary>

**参照**

- 検証記録（setup-notes-verification の `docs/verification/almalinux-setup.md`）
- 検証記録（setup-notes-verification の `docs/verification/windows-setup.md`）

</details>

## AlmaLinux 10 で使う

- GNOME にログインした自分のユーザーで行う。前提の setup-notes の項は、各項のリードのリンクから開く

### mdtable のブックを Excel で編集するときの注意

- セルは文字列の書式（`@`）になっている。書式をクリアしたセルや、別のブック・Web から「すべて貼り付け」したセルでは、Excel が入力を数値や日付に変えることがある（`1/2` → 日付、`3.10` → 3.1 など）。日付になったセルがあると取り込みは止まる
- 貼り付けは「値のみ」か、セルの編集中（F2・数式バー）に行う
- オートコレクト（`(c)` → `©` など）は入力を変える。コードを書くなら Excel の [ファイル] → [オプション] で切る
- 見出し行の直下に行を挿入すると、見出しの書式が写る（HTML の表では `<th>` になる）。[挿入オプション] で「下と同じ書式を適用」を選ぶ
- [セルを結合して中央揃え] は中央揃えも付く。結合だけなら [セルを結合]
- 「テーブルとして書式設定」（Ctrl+T）は使わない
- セルの一部だけの書式（文字の色・斜体など）は書き戻さない。強調は `**` などで書く
- セル全体の太字・斜体・取り消し線・下線、ハイパーリンク（Ctrl+K）、コメント（メモ）も書き戻さず、取り込みで警告する。リンクは `[文字](URL)` の形で書く
- 行・列を消すときは、Excel で行・列ごと削除する。中身だけを消すと空の行・列が残り、末尾の空の行・列が増えたら取り込みで警告する
- シート名の変更・並べ替えはよい。シートを消した表は変更なしとして扱う。足したシートは読まない
- 表から離れたセルに書いた値は無視する（警告を出す）
- フォントは、書き出す PC に HackGen Console NF が入っていればそれ、無ければ BIZ UDゴシック（`\` が `¥` に見える）。環境変数 `MDTABLE_FONT` にフォント名を入れると、そのフォントにする

---

### mdtable で Markdown の表を Excel と往復する

- Markdown の表を Excel のブックに書き出し、Excel で編集したブックの変更を Markdown に書き戻す
- 前提: [mdtable を導入・更新する](install.md#mdtable-を導入更新する)を通し、`mdtable` が PATH にある。ブックの編集には、Excel のある端末を使う
- 実行場所: GNOME にログインした自分のユーザーの端末（bash。`sudo` は使わない）の、編集する文書のあるディレクトリ。ブックの編集は、Excel のある端末
- `README.md` と `README.tables.xlsx` は、対象の文書名とブック名に置き換える

> [!CAUTION]
> この項の手順 1 の後で Excel を編集する前に、[mdtable のブックを Excel で編集するときの注意](#mdtable-のブックを-excel-で編集するときの注意)を必ず読む。日付化・小数の桁落ちなどで、元の文字列を失うことがある。

1. Markdown の表を Excel ブックへ書き出す。

   ```bash
   mdtable export README.md
   ```

   - 同じフォルダーに `README.tables.xlsx` ができる。保存先は `-o` で変えられる
   - 既存のブックは `--force` が無ければ上書きしない
   - **次の手順は、Excel のある端末でブックを編集し、元のフォルダーへ戻してから貼る**

1. 取り込む差分を確かめる。

   ```bash
   mdtable import README.tables.xlsx --dry-run
   ```

   - 差分だけが表示され、Markdown は変わらない
   - **次の手順は、差分の内容を確かめてから貼る**

1. ブックの変更を Markdown に書き戻す。

   ```bash
   mdtable import README.tables.xlsx
   ```

   - ブックに記録した Markdown に書き戻す
   - ブックで変えた表を、書き出した後に Markdown でも変えていたら、衝突として書き戻さない。**`--force` はブックの内容で上書きする**
   - 取り込み済みのブックは「適用済み」になる。取り込み後のブックは消してよい

<details id="mdtable-で-markdown-の表を-excel-と往復する-の補足と注意点">
  <summary>mdtable で Markdown の表を Excel と往復する の補足と注意点</summary>

**[mdtable で Markdown の表を Excel と往復する](#mdtable-で-markdown-の表を-excel-と往復する)の手順 3**

- ブックに記録した Markdown に書き戻す。書き戻す先は `--md` で変えられる
- `import` に `--keep-kind` を付けると、パイプ表を HTML の表に変える必要があるときは、理由とセルの位置を出して文書全体の書き戻しを止める
- `import` に `--strict-text` を付けると、数値・真偽値・数式・エラー値になったセルがあるときも、日付と同じく文書全体の書き戻しを止める。`--force` とは同時に指定できない

</details>

---

## Windows 11 で使う

- この PC のデスクトップで、自分のユーザーが行う。前提の setup-notes の項は、各項のリードのリンクから開く

### Windows 11 で mdtable のブックを Excel で編集するときの注意

- セルは文字列書式（`@`）。書式を消したり「すべて貼り付け」したりすると、`1/2` が日付・`3.10` が 3.1 などに変わることがある。日付のセルがあると取り込みは止まる
- 貼り付けは「値のみ」か、セルの編集中（F2・数式バー）に行う
- オートコレクト（`(c)` → `©` など）は入力を変える。コードを書くなら Excel の [ファイル] → [オプション] で切る
- 見出し行の直下に行を挿入すると、見出しの書式が写る（HTML の表では `<th>` になる）。[挿入オプション] で「下と同じ書式を適用」を選ぶ
- [セルを結合して中央揃え] は中央揃えも付く。結合だけなら [セルを結合]
- 「テーブルとして書式設定」（Ctrl+T）は使わない
- セルの一部だけの書式（文字の色・斜体など）は書き戻さない。強調は `**` などで書く
- セル全体の太字・斜体・取り消し線・下線、ハイパーリンク（Ctrl+K）、コメント（メモ）も書き戻さず、取り込みで警告する。リンクは `[文字](URL)` の形で書く
- 行・列を消すときは、Excel で行・列ごと削除する。中身だけを消すと空の行・列が残り、末尾の空の行・列が増えたら取り込みで警告する
- シート名の変更・並べ替えはよい。シートを消した表は変更なしとして扱う。足したシートは読まない
- 表から離れたセルに書いた値は無視する（警告を出す）
- フォントは、書き出す PC に HackGen Console NF が入っていればそれ、無ければ BIZ UDゴシック（`\` が `¥` に見える）。環境変数 `MDTABLE_FONT` にフォント名を入れると、そのフォントにする

---

### Windows 11 の mdtable で Markdown の表を Excel と往復する

- 自分の Markdown の表を Excel で編集し、Markdown に書き戻す
- 前提: [Windows 11 で mdtable を導入・更新する](install.md#windows-11-で-mdtable-を導入更新する)を通してある。Excel が入っている。対象の Markdown を編集できる
- 実行場所: この PC のデスクトップの管理者ではない PowerShell 7。この項の手順 1 で開く。Excel の編集は、この PC のデスクトップで行う
- 例の `README.md` と `README.tables.xlsx` は、対象の文書名と対応するブック名に置き換える

> [!CAUTION]
> この項の手順 4・6 で Excel を編集する前に、[Windows 11 で mdtable のブックを Excel で編集するときの注意](#windows-11-で-mdtable-のブックを-excel-で編集するときの注意)を必ず読む。日付化・小数の桁落ちなどで、元の文字列を失うことがある。

1. 管理者ではない PowerShell 7 を開く。

   - スタートメニューで「PowerShell」を探し、クリックして開く（「Windows PowerShell」ではない。「管理者として実行」にはしない）
   - 「PowerShell」が無ければ、[アプリを入れる](https://github.com/ryo-aoki-pc/setup-notes/blob/main/docs/windows-setup.md#アプリを入れる)の手順 6〜9 を通す

1. 作業場所の変数を設定する（`MDTABLE_DIR` は必ず値を入れる）。

   ```powershell
   $MDTABLE_DIR = ''                     # ← 編集する自分の Markdown があるディレクトリの絶対パス。<MDTABLE_DIR>
   ```

   ```powershell
   'MDTABLE_DIR = {0}' -f $MDTABLE_DIR
   ```

   - 最後に値を読み戻して確かめる
   - **新しい PowerShell を開いたら**、この項の手順 2 を貼り直してから先へ進む

1. 対象の Markdown があるディレクトリへ移る。

   ```powershell
   & {
     # Markdown の作業フォルダーに移動する
     if (-not $MDTABLE_DIR) { Write-Error '中断: この項の手順 2 の MDTABLE_DIR が空のまま'; return }
     Set-Location -LiteralPath $MDTABLE_DIR -ErrorAction Stop
     "`n$([char]27)[7m 確認 $([char]27)[0m"
     Get-Location
   }
   ```

   - 対象の Markdown がある場所が出ればよい。エラーが出たら先へ進まず、この項の手順 2 の場所を確かめる

1. 表を一覧にし、Excel で開いて編集する。

   ```powershell
   # Markdown の表を一覧にして編集する
   mdtable list README.md
   mdtable edit README.md
   ```

   - `list` に表の一覧が出ればよい
   - `edit` は一時ブックを Excel で開く。保存して Excel を閉じてから Enter を押すと書き戻す
   - `--table 3` は 3 番目の表、`--table L120` は 120 行目を含む表を選ぶ
   - 書き戻せないときや Ctrl+C で止めたときはブックを残し、取り込み直すコマンドを表示する
   - **次の手順は、Excel を閉じて Enter を押し、プロンプトに戻ってから貼る**

1. ブックをファイルとして残すときだけ、Markdown から書き出す。

   ```powershell
   mdtable export README.md
   ```

   - Markdown と同じフォルダーに `README.tables.xlsx` ができる（`-o` で変更）。既存のブックは `--force` 無しでは上書きしない

1. ブックをファイルとして残すときだけ、Excel で開いて編集する。

   - エクスプローラーで `README.tables.xlsx` を開き、表を編集する
   - この項のリードから案内した、Excel で編集するときの注意に従う
   - **次の手順は、保存して Excel を閉じてから、この項の手順 1 の PowerShell に貼る**

1. ブックをファイルとして残すときだけ、取り込む前に差分を確かめる。

   ```powershell
   mdtable import README.tables.xlsx --dry-run
   ```

   - Markdown に書き戻す差分だけが出る
   - 差分が意図した内容と違えば、この項の手順 8 は貼らず、Markdown とブックを見直す
   - **次の手順は、差分を確かめてから貼る**

1. ブックをファイルとして残すときだけ、確かめた差分を Markdown に取り込む。

   ```powershell
   mdtable import README.tables.xlsx
   ```

   - ブックに記録した Markdown に書き戻す（`--md` で変更）
   - ブックで変えた表を、書き出した後に Markdown でも変えていたら、衝突として書き戻さない（`--force` を付けると、ブックの内容で Markdown を上書きする）
   - 同じブックを再度取り込むと「適用済み」になる。取り込み後はブックを消してよい

<details id="windows-11-の-mdtable-で-markdown-の表を-excel-と往復する-の補足と注意点">
  <summary>Windows 11 の mdtable で Markdown の表を Excel と往復する の補足と注意点</summary>

**[Windows 11 の mdtable で Markdown の表を Excel と往復する](#windows-11-の-mdtable-で-markdown-の表を-excel-と往復する)の手順 4**

- `list` は表の番号・行・種類・大きさ・場所・直前の見出しを出す
- `edit` に `--keep-kind` を付けると、パイプ表を HTML の表に変える必要があるときは、理由とセルの位置を出して文書全体の書き戻しを止め、ブックを残す
- `edit` に `--strict-text` を付けると、数値・真偽値・数式・エラー値になったセルがあるときも、日付と同じく文書全体の書き戻しを止め、ブックを残す

**[Windows 11 の mdtable で Markdown の表を Excel と往復する](#windows-11-の-mdtable-で-markdown-の表を-excel-と往復する)の手順 8**

- `import` に `--keep-kind` を付けると、パイプ表を HTML の表に変える必要があるときは、理由とセルの位置を出して文書全体の書き戻しを止める
- `import` に `--strict-text` を付けると、数値・真偽値・数式・エラー値になったセルがあるときも、日付と同じく文書全体の書き戻しを止める。`--force` とは同時に指定できない

</details>

---
