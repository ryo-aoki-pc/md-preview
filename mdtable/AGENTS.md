# AGENTS.md

このリポジトリで作業するコーディングエージェント（Claude Code・Codex・Grok Build・Antigravity CLI）への指示。Claude Code は CLAUDE.md の `@AGENTS.md` で、Codex・Grok Build・Antigravity CLI はこのファイルを直接読む。

GLFM の文書にある表（パイプ表・HTML の表）を Excel で編集して書き戻す `mdtable` コマンド。セルの書き方と書き戻しの規則は [README.md](README.md)、導入と更新の手順は [docs/install.md](docs/install.md)、使い方の手順は [docs/usage.md](docs/usage.md)、設計の理由と変換の規則は [docs/reference/readme.md](docs/reference/readme.md)、検証記録は `repos/md-table-excel/docs/verification/readme.md`。

## 導入手順の正本

Windows 11 の導入・更新の手順は [docs/install.md](docs/install.md)、使い方の手順は [docs/usage.md](docs/usage.md) にある。setup-notes の手順書には項を置かず、「Git と開発の道具」のリードと「更新」から、これらの項へ案内している。項の見出しを変えたら、そこへのリンク（setup-notes の手順書と README、setup-notes-verification の検証記録）も直す。

## 構成

- `LICENSE` — md-table-excel の MIT ライセンス。`--licenses` の最初に出す
- `go.mod`・`go.sum` — Go のモジュールと依存
- `cmd/mdtable/` — コマンドの入口。処理は `internal/cli/`
- `tableedit/` — ほかの Go のプログラムから使う表の解析・セルと行・列の編集・原文の差し替えの公開の窓口
- `internal/blocks/`・`internal/inline/` — comrak と同じ規則のブロック解析と、コードスパン・生の HTML・数式などの走査
- `internal/htmltok/`・`internal/cellfromhtml/` — HTML の字句解析と、HTML のセルから Markdown への変換
- `internal/table/` — 文書の読み書き、表の検出・番号、パイプ表と HTML の表の解析・書き戻し
- `internal/cellsyntax/` — Excel のセルの Markdown と、パイプ表・HTML の表のセルの間の変換
- `internal/model/`・`internal/textwidth/` — 表のモデルとセルの文字の正規化、文字の表示幅
- `internal/changes/`・`internal/sync/` — 変更の要約とセルの一覧、書き出し・取り込み・書き戻した結果の確認
- `internal/verify/` — Excel に依存しない、書き戻した表と前後の Markdown の解釈の検査
- `internal/workbook/` — ブックの書き出しと読み込み。書き出しは自前の XML と ZIP、読み込みは excelize
- `internal/pycompat/` — 文字・差分・数値などの互換処理。`gen_tables.py`・`gen_testdata.py` は Unicode などの表と正解データを作り直すためのスクリプトで、そのときだけ Python 3 が要る
- `internal/version/version.go` — 版を置く所。`licenses/manifest.json` の md-table-excel の版も同じにする（テストが照合する）
- `licenses.go`・`licenses/` — 実行ファイルに同梱するライセンスの原文と一覧。`--licenses` で全文を出す
- `licenses_test.go`・`internal/cli/licenses_test.go` — 依存の版・原文と、ライセンスの表示の検査
- `testdata/` — テストの入力と正解。`documents/`（見本と乱数の文書）、`goldens.json.gz`（文書ごとのコマンドの出力の正解。版は `<VERSION>`）、`unit/`（単体テストの入力と期待値）、`workbooks.zip`（openpyxl で作って編集したブック）、`comrak_ast.mjs`（comrak と突き合わせるテストが使う）
  - 正解は Python 版 0.5.2 の出力から作った。`regenerate.py` などは作った方法の記録で、Python 版が無いので今は動かない。正解を直すときは、期待値を手で直し、理由をコミットに書く
- `scripts/build-release.sh` — Windows の amd64 向けの実行ファイル、ライセンス全文、sha256 の一覧を作る
- `.github/workflows/test.yml`・`release.yml` — Go の検査と配布物のビルド、タグによる Releases への配布
- `docs/install.md` — Windows 11 の導入・更新の手順
- `docs/usage.md` — Markdown の表を Excel と往復する手順と、ブックを Excel で編集するときの注意
- `docs/reference/readme.md` — 設計と変換の規則
- 非公開側の検証記録の `readme.md` — 検証記録と、Excel の実機での確認の手順
- `docs/verification/` — `multi-tables.md`・`excel-check.md`・`complex-cells.md` の見本の文書だけを置く

## テスト

```sh
gofmt -l .
go vet ./...
go test ./...
```

- リポジトリの直下で動かす。`gofmt -l .` は何も出ないこと
- Pull Request と `main` への push では、GitHub Actions が ubuntu-latest と windows-latest で検査し、Linux では `go test -race ./...` も動かす。取り込む前に `gh pr checks` で結果を見る
- comrak と突き合わせる Go のテストは、`MDTABLE_COMRAK_DIR` を設定したときだけ動く。`node_modules/comrak` を持つフォルダー（[md-format](https://github.com/ryo-aoki-pc/md-format) の導入先など）を指定する
- 手元の文書で無編集の往復を確かめる Go のテストは、`MDTABLE_CORPUS` を設定したときだけ動く。文書のフォルダーを指定する
- `bash scripts/build-release.sh <版> <出力のフォルダー>` で、CI と同じ配布物のビルドを確かめる
- ライセンスの検査は、`go.mod` の直接・間接の依存と原文の写しを照合する。依存を変えたら `licenses/manifest.json` と原文も確認する

## 書き方の規則

- Go は gofmt で整える。コードのコメントと利用者に出すメッセージは日本語で書く
- 移した元の関数名のコメントは記録として残す
- 版は `internal/version/version.go` の `Version` で決め、`licenses/manifest.json` の md-table-excel の版も合わせる。`v<版>` のタグを push すると、タグと版を照合して GitHub Releases に Windows の amd64 向けの実行ファイルを配布する
- エラーは日本語 1 行で出し、想定外の失敗のスタックは `MDTABLE_DEBUG=1` のときだけ出す
- 表の検出やブロックの規則を変えたら、Go のテストで comrak と突き合わせる。GitLab の描画に関わる変更は、GitLab の Markdown API（`/api/v4/markdown`）でも確かめ、検証記録に残す
- 変えていない表・行・セルを 1 文字も変えないことを、往復のテストで確かめる
- ソースは LF。文書（README・docs）は日本語。検証していないことを「動く」と書かない

## 文書の置き場所

- 導入・更新の手順は `docs/install.md`、使い方の手順は `docs/usage.md` に置き、README の「導入」の表からその項へ直接リンクする（文書の先頭へはリンクしない）。執筆規則は setup-notes の `docs/dev/writing-rules.md`
- `docs/extra/` は注意点。ロールバック（取り消し）の手順は書かない。`docs/reference/` は背景と規則。検証記録（環境・日付・対象コミット・実出力・未確認事項）は、Forgejo の非公開リポジトリ `setup-notes-verification` の `repos/md-table-excel/docs/verification/` に置く。テストのコマンドは上の「テスト」
- 検証記録を書く・直すときは、Forgejo の非公開リポジトリ `setup-notes-verification` を clone し、そちらへ Pull Request を作る。このリポジトリから検証記録へは URL で指さず、「`<検証記録のパス>` の「<見出し>」」と文字で書く（見出しを指さないときは、同じ名前の文書の記録なら「検証記録」とだけ書き、ほかの記録はパスを添える）
- 文書を移す・分ける・まとめるときは、移した経緯と、旧ファイル名・旧見出しとの対応を文書に書かない（経緯は git の履歴と Pull Request の本文に残す）。リンクを今の場所へ直す。中身が新しい場所への案内だけになったファイルは消す
- 残す検証記録の実施日・対象版・環境・出力・成功と失敗・未実施の範囲・記録したときの手順番号は書き換えない。再検証せずに検証範囲を広げない。記録の手順番号が今の手順と違い、本文を読むのに要るときだけ「記録の手順 | 今の手順」の表で結ぶ
- 導入先（実行ファイルの場所）を変えたら、`docs/install.md` の導入の項と README の「導入」を合わせて直す
