# AGENTS.md

このリポジトリで作業するコーディングエージェント（Claude Code・Codex・Grok Build・Antigravity CLI）への指示。Claude Code は CLAUDE.md の `@AGENTS.md` で、Codex・Grok Build・Antigravity CLI はこのファイルを直接読む。

利用者への回答・質問・報告は、常に日本語で書く（コードのコメントなどの言語は、このファイルのほかの決まりに従う）。

GLFM の文書にある表（パイプ表・HTML の表）を Excel で編集して書き戻す `mdtable` コマンド。セルの書き方と書き戻しの規則は [README.md](README.md)、設計の理由と変換の規則は [docs/reference/readme.md](docs/reference/readme.md)、検証記録は [非公開側の readme.md](https://github.com/ryo-aoki-pc/setup-notes-verification/blob/main/repos/md-table-excel/docs/verification/readme.md)。

## 導入手順の正本

導入・更新・使い方の手順は、setup-notes の [almalinux-setup.md](https://github.com/ryo-aoki-pc/setup-notes/blob/main/docs/almalinux-setup.md#mdtable) と [windows-setup.md](https://github.com/ryo-aoki-pc/setup-notes/blob/main/docs/windows-setup.md#mdtable) の「mdtable」とその子の項にある。このリポジトリには置かない。macOS の導入（README の 1 行）は、このリポジトリに置く。

## 構成

- `mdtable/blocks.py` — comrak（GitLab の解析器）と同じ規則のブロックの解析。行ごとのコンテナの接頭辞の位置も持つ
- `mdtable/inline.py` — コードスパン・生の HTML・数式などのひとかたまりの走査、行頭のエスケープ、強調が成り立つかの判定
- `mdtable/document.py` — 文書の読み書き（改行コード・BOM を保つ）、表の検出と番号、置き換え
- `mdtable/pipe_table.py` — パイプ表の解析・セルの変換・書き戻し（行とセルの再利用）
- `mdtable/html_table.py`・`htmltok.py`・`cell_from_html.py` — HTML の表の範囲・字句解析・格子・セルの変換・差し替えと作り直し
- `mdtable/cell_syntax.py` — Excel のセルの Markdown をパイプ表のセル・HTML の表のセルにする（とその逆）
- `mdtable/workbook.py` — ブックの書き出しと読み込み。`mdtable/sync.py` — 書き出し・取り込みの手順と、書き戻した結果の確かめ
- `mdtable/model.py` — 表のモデル（Excel と Markdown の間で受け渡す形）と、セルの文字の正規化。`mdtable/textwidth.py` — 文字の表示幅
- `mdtable/cli.py` — コマンド（list / export / import / edit）。`mdtable/__main__.py` は `python -m mdtable`、`mdtable/__init__.py` は版（`__version__`）
- `scripts/setup-mdtable.py` — リポジトリ外の仮想環境への導入。`pyproject.toml` — パッケージの定義（`readme = "README.md"` なので、README はパッケージの説明にもなる）
- `tests/` — unittest。`test_oracles.py` は環境変数を設定したときだけ、手元の文書と comrak で確かめる
- `docs/reference/readme.md` — 設計と変換の規則
- 非公開側の検証記録の `readme.md` — 検証記録と、Excel の実機での確認の手順
- `docs/verification/` — `multi-tables.md`（`tests/test_workbook.py` が読む）と `excel-check.md`（Excel の確認に使う）の見本の文書だけを置く

## テスト

```sh
python3 -B -m unittest discover -s tests -v
```

- Windows では `python`。openpyxl のある Python で、リポジトリの直下で動かす
- comrak と突き合わせるときは `MDTABLE_COMRAK_DIR` に `node_modules/comrak` を持つフォルダー（Markdown の整形器 [md-format](https://github.com/ryo-aoki-pc/md-format) を導入した導入先。`%LOCALAPPDATA%\md-format`・`${XDG_DATA_HOME:-~/.local/share}/md-format` など）を、手元の文書で無編集の往復を確かめるときは `MDTABLE_CORPUS` に文書のフォルダーを設定する

## 書き方の規則

- コードのコメントと利用者に出すメッセージは日本語。型ヒントは付けない。Python 3.9 で動く書き方にする（`match`・`X | Y`・`zip(strict=)` などは使わない）
- エラーは日本語 1 行で出し、Traceback は `MDTABLE_DEBUG=1` のときだけ出す
- 表の検出やブロックの規則を変えたら、`test_oracles.py` で comrak と突き合わせる。GitLab の描画に関わる変更は、GitLab の Markdown API（`/api/v4/markdown`）でも確かめ、検証記録に残す
- 変えていない表・行・セルを 1 文字も変えないことを崩さない（`tests/test_tables.py`・`test_workbook.py` の往復のテスト）
- ソースは LF。Windows の Python でファイルを書き換えるときは `newline=""` か `newline="\n"` を付ける
- 文書（README・docs）は日本語。検証していないことを「動く」と書かない

## 文書の置き場所

- 導入・利用・更新の手順は正本に置き、README の「導入」の表から正本の項へ直接リンクする（手順書の先頭やカテゴリの見出しへはリンクしない）。執筆規則は setup-notes の `docs/dev/writing-rules.md`
- `docs/extra/` は注意点。ロールバック（取り消し）の手順は書かない。`docs/reference/` は背景と規則。検証記録（環境・日付・対象コミット・実出力・未確認事項）は、非公開の `ryo-aoki-pc/setup-notes-verification` の `repos/md-table-excel/docs/verification/` に置く。テストのコマンドは上の「テスト」
- 検証記録を書く・直すときは、非公開の `setup-notes-verification` を clone し、そちらへ Pull Request を作る
- 文書を移す・分ける・まとめるときは、移した経緯と、旧ファイル名・旧見出しとの対応を文書に書かない（経緯は git の履歴と Pull Request の本文に残す）。リンクを今の場所へ直す。中身が新しい場所への案内だけになったファイルは消す
- 残す検証記録の実施日・対象版・環境・出力・成功と失敗・未実施の範囲・記録したときの手順番号は書き換えない。再検証せずに検証範囲を広げない。記録の手順番号が今の手順と違い、本文を読むのに要るときだけ「記録の手順 | 今の手順」の表で結ぶ
- 導入先（仮想環境・起動コマンドの場所）を変えたら、正本の「mdtable を導入・更新する」を合わせて直す

## 共同作業の規則

このリポジトリでは、Claude Code・Codex・Grok Build・Antigravity CLI が同じ規則で作業する。分担は人が決める。

- 起動された worktree（作業ディレクトリ）の中だけでファイルを変える。ほかの worktree のファイルは変えない
- 今のブランチにだけコミットする。`main` にはコミットも push もしない
- 頼まれた範囲のファイルだけを変える。範囲の外を変えるときは、変える前に理由を書いて確かめる
- 終わったら、テストとリンターを通してから、目的ごとにコミットする。通らなければコミットせずに、結果を報告する
- コミットしたら、今のブランチを push し、`main` への Pull Request を作る（既にあれば足す）。今のブランチに `main` を取り込むのは、頼まれたときと、Pull Request が競合したときだけ
- `main` への取り込み（マージ）は、エージェントが行ってよい。テストとリンターが通り、Pull Request が競合していなければ、`gh pr merge --squash` で Pull Request を取り込む（手元の `main` にはコミットも push もしない）。利用者が取り込まないよう頼んだときは取り込まず、そのことを報告する。取り込んだブランチには、その後コミットも push もせず、次の作業は新しいブランチで始める
  - リモートのブランチ: 取り込んだ Pull Request のブランチは、GitHub が自動で消す（リポジトリの Automatically delete head branches）。取り込む前に `gh api 'repos/{owner}/{repo}' --jq .delete_branch_on_merge` で確かめ、`false` なら `gh api -X PATCH 'repos/{owner}/{repo}' -F delete_branch_on_merge=true` で有効にする（`{owner}/{repo}` は gh が今のリポジトリで埋めるので、そのまま打つ）。取り込んだ後もリモートに残った自分のブランチは、`git push origin --delete <ブランチ>` で消す
  - 手元のブランチ: 自分が作ったブランチは、Pull Request が取り込まれたこと（`gh pr view <番号> --json state` が `MERGED`）と、どの worktree でも使っていないこと（`git worktree list`）を確かめてから、`git branch -D <ブランチ>` で消す（squash で取り込むので `git branch -d` は通らない）。今いるブランチは、新しいブランチへ移ってから消す。ほかの担当のブランチは消さない
  - Claude Code の worktree とブランチ: セッションの worktree は、Pull Request が閉じるとアプリがセッションをアーカイブして消す（Claude Code の設定の Auto-archive sessions when their pull request closes を有効にしておく。消えることはアプリの設定の説明による。コミットしていない変更がある worktree は残る）。残った `claude/` のブランチは、ほかの担当のブランチを消さない決まりの例外とする。Claude Code は、ほかのセッションが作ったものも、Pull Request が取り込まれたこと（`gh pr list --head <ブランチ> --state merged` に出る）と、どの worktree でも使っていないこと（`git worktree list`）を確かめてから、手元は `git branch -D <ブランチ>`、リモートに残っていれば `git push origin --delete <ブランチ>` で消す
- 秘密情報（`.env`・鍵・トークン・パスワード）を読まない・書かない・出力しない
- レビューを頼まれたら、ファイルを変えずに、指摘を「重大度・場所（ファイル:行）・理由・直し方」で挙げる
- ほかの担当の変更は、`git diff main...agent/codex` のように git で読む（ほかの worktree へ移らない）

## 既定の分担

利用者が Claude Code に、分担を指定せずにファイルを変える作業を頼んだときは、Claude Code が取りまとめて次の 4 つで分担する。「共同作業の規則」の「分担は人が決める」で、人が分担を決めていないときの既定にする。利用者がほかのエージェントに直接頼んだときは、この節は使わず「共同作業の規則」に従う。

- Claude Code だけで行うもの: 利用者の質問への答え、ファイルを変えない調べもの、1 つのファイルの数行で振る舞いを変えない直し（誤字・リンク・値の直しなど）
- Claude Code: 計画・実機での確かめ・ほかの 3 つへの依頼・指摘の確かめ・統合・コミットと Pull Request・取り込み
- Codex: 実装と下書き。Claude Code が用意した別の clone か worktree の中だけで書く。この分担で頼まれたときは、「共同作業の規則」のコミット・push・Pull Request・取り込みを行わず、変更を残して報告する（Claude Code が確かめ、自分の worktree に移してからコミットする）
- Grok Build: 差分の読み取り専用のレビュー。ファイルを変えない（Windows 11 では、Claude Code は `--always-approve` を付けずに頼む）
- Antigravity CLI: 差分の読み取り専用のレビューと、その作業のための調べもの。ファイルを変えない（Claude Code は、入力を写した使い捨てのフォルダーで 1 回だけ実行し、`--dangerously-skip-permissions`・`--mode` を付けない）
- Claude Code から頼まれたエージェントは、上の自分の役割の範囲で作業する
- レビューの指摘は、実物で確かめてから採る。採らなかった指摘は、理由を付けて報告する
- 使えないエージェント（入っていない・ログインが切れた）は飛ばし、そのことを報告する
- 頼み方は、Forgejo の非公開リポジトリ `setup-notes-private` の `docs/ai-agents.md` の「コーディングエージェントの共同作業: Windows 11 で既定で分担する」と、`docs/reference/coding-agents.md` の「既定の分担」
