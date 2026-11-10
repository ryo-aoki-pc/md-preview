// レビューで見つかった穴の再発を防ぐ試験 (閉じ込めの大文字小文字、読む前の大きさの検査、一覧の上限、配るライブラリのヘッダー)。
import assert from 'node:assert/strict';
import { mkdirSync, mkdtempSync, realpathSync, rmSync, symlinkSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import test from 'node:test';

import { listMarkdown } from '../lib/documents.mjs';
import { loadPage } from '../lib/page.mjs';
import { resolve } from '../lib/resolve.mjs';
import { createServer } from '../lib/server.mjs';
import { createSession } from '../lib/session.mjs';
import { get, until } from './helpers.mjs';

function temp(t) {
  const base = realpathSync.native(mkdtempSync(join(tmpdir(), 'mdp-hardening-')));
  t.after(() => rmSync(base, { recursive: true, force: true }));
  return base;
}

test('Windows の包含の判定は、小文字にすると長さが変わる文字で隣のフォルダーを内側と見誤らない', (t) => {
  // 「İ」(U+0130) は toLowerCase() で「i」と結合文字の 2 文字になる。ルートが repoİ のとき、隣の repoi̇ を
  // 同じ接頭辞と見てはいけない。
  const base = temp(t);
  const root = join(base, 'repoİ');
  const sibling = join(base, 'repoi̇');
  try {
    mkdirSync(root);
    mkdirSync(sibling);
    writeFileSync(join(sibling, 'secret.md'), '外のファイル');
    // フォルダーのリンク (Windows ではジャンクション) で、隣のフォルダーへ出る
    symlinkSync(sibling, join(root, 'link'), 'junction');
  } catch {
    return t.skip('この環境では、この名前のフォルダーかリンクを作れない');
  }
  writeFileSync(join(root, 'inside.md'), '内のファイル');
  const real = realpathSync.native(root);
  for (const isWin of [true, false]) {
    assert.equal(resolve(real, 'link/secret.md', isWin).status, 403, 'isWin=' + isWin);
    assert.equal(resolve(real, 'inside.md', isWin).status, 200, 'isWin=' + isWin);
  }
  // ASCII の大文字小文字の違いは、Windows では同じフォルダーとして扱う (ドライブ名の大文字小文字など)
  if (process.platform === 'win32') {
    assert.equal(resolve(real.toUpperCase().replace('İ', 'İ'), 'inside.md', true).status, 200);
  }
});

test('?doc= の文書は、読む前に大きさを見て、大きすぎるものを読まない', async (t) => {
  const root = temp(t);
  writeFileSync(join(root, 'small.md'), '# 小さい\n');
  writeFileSync(join(root, 'big.md'), 'あ'.repeat(200) + '\n');
  writeFileSync(join(root, 'main.md'), '# 主\n');
  const session = createSession({ root, maxBytes: 100 });
  t.after(() => session.close());
  const seen = { small: [], big: [] };
  const stops = [
    session.subscribeDocument('small.md', (name, data) => seen.small.push({ name, data })),
    session.subscribeDocument('big.md', (name, data) => seen.big.push({ name, data })),
  ];
  t.after(() => stops.forEach((stop) => stop()));
  await until(() => seen.small.some((e) => e.name === 'render') && seen.big.some((e) => e.name === 'render'));
  const small = seen.small.find((e) => e.name === 'render').data;
  const big = seen.big.find((e) => e.name === 'render').data;
  assert.match(small.html, /小さい/);
  assert.equal(big.error, '文書が大きすぎます。');
  assert.equal(big.html, undefined);
  assert.equal(big.markdown, ''); // 本文をページへ送らない
});

test('一覧は、1 つのフォルダーから読む名前の数にも上限がある', async (t) => {
  const root = temp(t);
  for (let i = 0; i < 30; i++) writeFileSync(join(root, `f${String(i).padStart(2, '0')}.md`), '');
  const all = await listMarkdown(root);
  assert.equal(all.files.length, 30);
  assert.equal(all.truncated, false);
  const capped = await listMarkdown(root, { maxEntries: 10 });
  assert.equal(capped.files.length, 10);
  assert.equal(capped.truncated, true);
});

test('配るライブラリの応答にも sandbox と共通のヘッダーが付く', async (t) => {
  const root = temp(t);
  const server = await createServer({ session: { root: () => root, last: () => ({ render: null, scroll: null }) }, page: loadPage() });
  t.after(() => server.close());
  for (const rel of ['_/lib/katex/katex.min.js', '_/lib/katex/katex.min.css', '_/lib/mermaid/mermaid.min.js', '_/lib/katex/fonts/KaTeX_Main-Regular.woff2']) {
    const response = await get(server, rel);
    assert.equal(response.status, 200, rel);
    assert.equal(response.headers['content-security-policy'], 'sandbox', rel);
    assert.equal(response.headers['x-content-type-options'], 'nosniff', rel);
    assert.equal(response.headers['referrer-policy'], 'no-referrer', rel);
  }
});
