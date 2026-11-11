# AGENTS.md

このリポジトリで作業するコーディングエージェント（Claude Code・Codex・Grok Build・Antigravity CLI）への指示。Claude Code は CLAUDE.md の `@AGENTS.md` で、Codex・Grok Build・Antigravity CLI はこのファイルを直接読む。

GLFM の文書にある表（パイプ表・HTML の表）を Excel で編集して書き戻す `mdtable` コマンド。セルの書き方と書き戻しの規則は [README.md](README.md)、導入と更新の手順は [docs/install.md](docs/install.md)、使い方の手順は [docs/usage.md](docs/usage.md)、設計の理由と変換の規則は [docs/reference/readme.md](docs/reference/readme.md)、検証記録は `repos/md-table-excel/docs/verification/readme.md`。

利用者向けの導入は Python 版を使う。

## 導入手順の正本

AlmaLinux 10・Windows 11 の導入・更新の手順は [docs/install.md](docs/install.md)、使い方の手順は [docs/usage.md](docs/usage.md) にある。macOS の導入（README の 1 行）も、このリポジトリに置く。setup-notes の手順書には項を置かず、「Git と開発の道具」のリードと「更新」から、これらの項へ案内している。項の見出しを変えたら、そこへのリンク（setup-notes の手順書と README、setup-notes-verification の検証記録）も直す。

## 構成

- `mdtable/blocks.py` — comrak（GitLab の解析器）と同じ規則のブロックの解析。行ごとのコンテナの接頭辞の位置も持つ
- `mdtable/inline.py` — コードスパン・生の HTML・数式などのひとかたまりの走査、行頭のエスケープ、強調が成り立つかの判定
- `mdtable/document.py` — 文書の読み書き（改行コード・BOM を保つ）、表の検出と番号、置き換え
- `mdtable/pipe_table.py` — パイプ表の解析・セルの変換・書き戻し（行とセルの再利用）
- `mdtable/html_table.py`・`htmltok.py`・`cell_from_html.py` — HTML の表の範囲・字句解析・格子・セルの変換・差し替えと作り直し
- `mdtable/cell_syntax.py` — Excel のセルの Markdown をパイプ表のセル・HTML の表のセルにする（とその逆）
- `mdtable/workbook.py` — ブックの書き出しと読み込み。`mdtable/sync.py` — 書き出し・取り込みの手順と、書き戻した結果の確かめ
- `mdtable/changes.py` — 元の表と書き戻すモデルの突き合わせ、変更の要約とセルの一覧
- `mdtable/model.py` — 表のモデル（Excel と Markdown の間で受け渡す形）と、セルの文字の正規化。`mdtable/textwidth.py` — 文字の表示幅
- `mdtable/cli.py` — コマンド（list / export / import / edit / convert）。`mdtable/__main__.py` は `python -m mdtable`、`mdtable/__init__.py` は版（`__version__`）
- `go.mod`・`go.sum` — Go 版のモジュールと依存
- `cmd/mdtable/` — Go 版の入口（`mdtable/__main__.py` に相当）。コマンドの処理は `internal/cli/`（`mdtable/cli.py`）
- `internal/blocks/`・`internal/inline/` — `mdtable/blocks.py`・`mdtable/inline.py` の Go 版
- `internal/htmltok/`・`internal/cellfromhtml/` — `mdtable/htmltok.py`・`mdtable/cell_from_html.py` の Go 版
- `internal/table/` — `mdtable/document.py`・`mdtable/pipe_table.py`・`mdtable/html_table.py` をまとめた Go 版
- `internal/cellsyntax/` — `mdtable/cell_syntax.py` の Go 版
- `internal/model/`・`internal/textwidth/` — `mdtable/model.py`・`mdtable/textwidth.py` の Go 版
- `internal/changes/`・`internal/sync/` — `mdtable/changes.py`・`mdtable/sync.py` の Go 版
- `internal/workbook/` — `mdtable/workbook.py` の Go 版。書き出しは自前の XML と ZIP、読み込みは excelize
- `internal/pycompat/` — Python の標準ライブラリと同じ結果を返す関数群。Unicode・HTML の表は `internal/pycompat/gen_tables.py` で作る
- `scripts/compare-go.py` — Python 版と Go 版の list・convert の終了コード・出力・文書のバイト列を突き合わせる
- `scripts/compare-go-workbook.py` — export したブックの性質と、編集したブックの import を Python 版と突き合わせる（openpyxl が要る）
- `scripts/compare-go-excel.py` — 本物の Excel で保存・編集したブックを突き合わせる（Windows と Excel が要る）
- `scripts/fuzz-docs.py` — 乱数の文書を作る（出力先・文書数・乱数の種を指定）
- `scripts/setup-mdtable.py` — リポジトリ外の仮想環境への導入。`pyproject.toml` — パッケージの定義（`readme = "README.md"` なので、README はパッケージの説明にもなる）
- `tests/` — unittest。`test_oracles.py` は環境変数を設定したときだけ、手元の文書と comrak で確かめる
- `.github/workflows/test.yml` — GitHub Actions で Python・Go のテストと list・convert の突き合わせを動かす
- `docs/install.md` — AlmaLinux 10・Windows 11 の導入・更新の手順
- `docs/usage.md` — Markdown の表を Excel と往復する手順と、ブックを Excel で編集するときの注意
- `docs/reference/readme.md` — 設計と変換の規則
- 非公開側の検証記録の `readme.md` — 検証記録と、Excel の実機での確認の手順
- `docs/verification/` — `multi-tables.md`（`tests/test_workbook.py` が読む）、`excel-check.md`（Excel の確認に使う）、`complex-cells.md`（セルの中に複雑な Markdown がある表の、`convert` と Excel の往復の確認に使う）の見本の文書だけを置く

## テスト

```sh
python3 -B -m unittest discover -s tests -v
```

- Windows では `python`。openpyxl のある Python で、リポジトリの直下で動かす
- Pull Request と `main` への push では、GitHub Actions（`.github/workflows/test.yml`）が、ubuntu と windows の Python 3.9 と最新で同じテストを動かす。取り込む前に `gh pr checks` で結果を見る
- comrak と突き合わせるときは `MDTABLE_COMRAK_DIR` に `node_modules/comrak` を持つフォルダー（Markdown の整形器 [md-format](https://github.com/ryo-aoki-pc/md-format) を導入した導入先。`%LOCALAPPDATA%\md-format`・`${XDG_DATA_HOME:-~/.local/share}/md-format` など）を、手元の文書で無編集の往復を確かめるときは `MDTABLE_CORPUS` に文書のフォルダーを設定する

```sh
gofmt -l .
go vet ./...
go test ./...
```

- Go の検査もリポジトリの直下で動かす。`gofmt -l .` は何も出ないこと。CI は ubuntu-latest と windows-latest で検査し、Linux では `go test -race ./...` も動かす
- `go build -o <実行ファイル> ./cmd/mdtable` で Go 版を作り、`python -B scripts/compare-go.py --go <実行ファイル> --py-root . --out <結果のフォルダー> <文書かフォルダー> ...` で list・convert を突き合わせる。openpyxl は要らない
- `python -B scripts/fuzz-docs.py <出力先> 150 1` で乱数の文書を作り、突き合わせに足せる。CI は `docs/verification` とこの 150 文書を両 OS で使う
- `python -B scripts/compare-go-workbook.py --go <実行ファイル> --py-root . --out <結果のフォルダー> <文書かフォルダー> ...` でブックの性質・交差 import・編集後の import を突き合わせる
- `scripts/compare-go-excel.py` は Windows と Excel のある PC で動かす。引数は `python -B scripts/compare-go-excel.py --help` で見る

## 書き方の規則

- コードのコメントと利用者に出すメッセージは日本語。型ヒントは付けない。Python 3.9 で動く書き方にする（`match`・`X | Y`・`zip(strict=)` などは使わない）
- Go は gofmt で整える。コメントと利用者に出すメッセージは日本語で書く
- Go の関数の前には、元の Python の関数名をコメントで書く
- Python 版を変えたら Go 版も同じに変え、突き合わせで違い 0 を確かめる
- 意図した違いは、セルの `_x005F_` の読み方（Go はその文字のまま読む）と、argparse が出す使い方の文だけ
- 版を変えるときは、`mdtable/__init__.py` の `__version__` と、`internal/version/version.go` の `Version` をそろえる
- エラーは日本語 1 行で出し、Traceback は `MDTABLE_DEBUG=1` のときだけ出す
- 表の検出やブロックの規則を変えたら、`test_oracles.py` で comrak と突き合わせる。GitLab の描画に関わる変更は、GitLab の Markdown API（`/api/v4/markdown`）でも確かめ、検証記録に残す
- 変えていない表・行・セルを 1 文字も変えないことを崩さない（`tests/test_tables.py`・`test_workbook.py` の往復のテスト）
- ソースは LF。Windows の Python でファイルを書き換えるときは `newline=""` か `newline="\n"` を付ける
- 文書（README・docs）は日本語。検証していないことを「動く」と書かない

## 文書の置き場所

- 導入・更新の手順は `docs/install.md`、使い方の手順は `docs/usage.md` に置き、README の「導入」の表からその項へ直接リンクする（文書の先頭へはリンクしない）。執筆規則は setup-notes の `docs/dev/writing-rules.md`
- `docs/extra/` は注意点。ロールバック（取り消し）の手順は書かない。`docs/reference/` は背景と規則。検証記録（環境・日付・対象コミット・実出力・未確認事項）は、Forgejo の非公開リポジトリ `setup-notes-verification` の `repos/md-table-excel/docs/verification/` に置く。テストのコマンドは上の「テスト」
- 検証記録を書く・直すときは、Forgejo の非公開リポジトリ `setup-notes-verification` を clone し、そちらへ Pull Request を作る。このリポジトリから検証記録へは URL で指さず、「`<検証記録のパス>` の「<見出し>」」と文字で書く（見出しを指さないときは、同じ名前の文書の記録なら「検証記録」とだけ書き、ほかの記録はパスを添える）
- 文書を移す・分ける・まとめるときは、移した経緯と、旧ファイル名・旧見出しとの対応を文書に書かない（経緯は git の履歴と Pull Request の本文に残す）。リンクを今の場所へ直す。中身が新しい場所への案内だけになったファイルは消す
- 残す検証記録の実施日・対象版・環境・出力・成功と失敗・未実施の範囲・記録したときの手順番号は書き換えない。再検証せずに検証範囲を広げない。記録の手順番号が今の手順と違い、本文を読むのに要るときだけ「記録の手順 | 今の手順」の表で結ぶ
- 導入先（仮想環境・起動コマンドの場所）を変えたら、`docs/install.md` の 2 つの導入の項と README の「導入」を合わせて直す
