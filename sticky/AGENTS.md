# AGENTS.md

このリポジトリで作業するコーディングエージェント（Claude Code・Codex・Grok Build・Antigravity CLI）への指示。Claude Code は CLAUDE.md の `@AGENTS.md` で、Codex・Grok Build・Antigravity CLI はこのファイルを直接読む。

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
