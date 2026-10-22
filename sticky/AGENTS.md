# AGENTS.md

このリポジトリで作業するコーディングエージェント（Claude Code・Codex・Grok Build・Antigravity CLI）への指示。Claude Code は CLAUDE.md の `@AGENTS.md` で、Codex・Grok Build・Antigravity CLI はこのファイルを直接読む。

利用者への回答・質問・報告は、常に日本語で書く（コードのコメントなどの言語は、このファイルのほかの決まりに従う）。

GitLab の Markdown の表で、先頭行・先頭列を固定してスクロールし、セルの折り返し・横スクロール・全幅表示を切り替える `md-table-sticky` のブックマークレット。使い方は [README.md](README.md)、設計は [docs/reference/design.md](docs/reference/design.md)、検証記録は [非公開側の readme.md](https://github.com/ryo-aoki-pc/setup-notes-verification/blob/main/repos/md-table-sticky/docs/verification/readme.md)。

## 導入手順の正本

導入・更新・使い方の手順は、setup-notes の [windows-setup.md](https://github.com/ryo-aoki-pc/setup-notes/blob/main/docs/windows-setup.md#md-table-sticky) の「md-table-sticky」の項にある。README の登録のしかたと正本の手順を合わせる（正本は setup-notes の Pull Request で直す）。

## 構成

- `src/sticky-table.js` — 中核の IIFE。格子と固定する範囲の純粋な関数、DOM への当て方、見張り、元へ戻す処理
- `tools/build-bookmarklet.mjs` — ブックマークレットの組み立てと `--check`
- `dist/bookmarklet.txt`・`dist/bookmarklet.html` — URL と登録用のページの生成物
- `test/grid.test.mjs` — 格子と固定する範囲の試験
- `test/build.test.mjs` — 組み立ての制約、生成物の一致、構文の試験
- `test/browser.test.mjs` — Edge・Chromium を CDP で動かし、生成物でスクロールと元への戻しを確かめる試験
- `test/fixtures/sample.md`・`test/fixtures/sample.gitlab.html`・`test/fixtures/gen-sample.mjs` — 確かめ用の Markdown、GitLab の API が描いた HTML、Markdown の生成の道具
- `test/fixtures/gitlab.css`・`test/fixtures/mimic-master.js`・`test/fixtures/mimic-19.5.js` — GitLab の CSS と版ごとの表の置き換えをまねるページの材料
- `tools/render-gitlab.mjs` — `GITLAB_TOKEN` を使い、gitlab.com の Markdown API で fixture の HTML を描き直す道具
- `README.md`・`docs/reference/design.md` — 使い方と設計
- `AGENTS.md`・`CLAUDE.md` — エージェントへの指示と Claude Code の入口
- `package.json`・`.gitignore` — Node.js のスクリプトと、リポジトリへ含めないもの
- 検証記録は、非公開の `ryo-aoki-pc/setup-notes-verification` の `repos/md-table-sticky/docs/verification/` に置く。このリポジトリには利用者の GitLab の URL・ホスト名を書かない

## テスト

```sh
npm test
for f in src/*.js tools/*.mjs test/*.mjs test/fixtures/*.mjs test/fixtures/*.js; do node --check "$f"; done
git diff --check
```

- Node.js 22 以上。依存のパッケージは無い
- ブラウザーの試験は Edge か Chromium が要る。`MTS_BROWSER` で実行ファイルを指定できる。見つからないときは、理由を出して飛ばす

## 書き方の規則

- コメントと利用者に出す文は日本語。まわりのコードの書き方に合わせる
- `src/sticky-table.js` では、ブロックコメント（`/* */`）、行の途中の `//` コメント、複数行のテンプレートリテラルを使わない。コメントは行全体の `//` だけにする。組み立てのための決まりで、試験でも確かめる
- `src/` を変えたら `npm run build` で `dist/` を作り直してコミットする
- ブックマークレットは外部への通信と外部のスクリプトの読み込みをしない
- GitLab の描画に関わる変更は、`tools/render-gitlab.mjs` と gitlab.com の実際の画面で確かめ、検証記録に残す
- ソースは LF。文書は日本語。検証していないことを「動く」と書かない
- 検証記録の過去の節は書き換えず、新しい節を足す。検証記録は非公開の setup-notes-verification の `repos/md-table-sticky/docs/verification/` に置く

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
- Antigravity CLI: 差分の読み取り専用のレビューと、その作業のための調べもの。ファイルを変えない（Claude Code は、入力をコピーした使い捨てのフォルダーで 1 回だけ実行し、`--dangerously-skip-permissions`・`--mode` を付けない）
- Claude Code から頼まれたエージェントは、上の自分の役割の範囲で作業する
- レビューの指摘は、実物で確かめてから採る。採らなかった指摘は、理由を付けて報告する
- 使えないエージェント（入っていない・ログインが切れた）は飛ばし、そのことを報告する
- 頼み方は、[setup-notes](https://github.com/ryo-aoki-pc/setup-notes) の `docs/ai-agents.md` の「コーディングエージェントの共同作業: Windows 11 で既定で分担する」と、`docs/reference/coding-agents.md` の「既定の分担」
