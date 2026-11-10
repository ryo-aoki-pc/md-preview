// worker が renderDocument に渡す、ファイルの読み込み (::include) と有無の確かめ (lib/worker.mjs の fileContext)。
// ルートの内側の実ファイルだけを読むことと、セッションを通して結果がページへ届くことを確かめる。
import assert from 'node:assert/strict';
import { mkdirSync, mkdtempSync, realpathSync, rmSync, symlinkSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import test from 'node:test';

import { createSession } from '../lib/session.mjs';
import { fileContext } from '../lib/worker.mjs';

function tree(t) {
  const base = realpathSync.native(mkdtempSync(join(tmpdir(), 'mdp-worker-')));
  t.after(() => rmSync(base, { recursive: true, force: true }));
  const root = join(base, 'root');
  mkdirSync(join(root, 'docs'), { recursive: true });
  mkdirSync(join(root, '.git'));
  writeFileSync(join(root, '.git', 'config'), 'secret');
  writeFileSync(join(base, 'outside.md'), '外のファイル');
  writeFileSync(join(root, 'top.md'), '﻿ルートのファイル\n');
  writeFileSync(join(root, 'docs', 'part.md'), '## 取り込んだ見出し\n\n本文 <script>alert(1)</script>\n');
  writeFileSync(join(root, 'docs', 'a%2fb.md'), '名前にパーセントのあるファイル\n');
  writeFileSync(join(root, 'docs', 'bin.dat'), Buffer.from([0x41, 0x00, 0x42]));
  writeFileSync(join(root, 'docs', 'latin1.txt'), Buffer.from([0xe9, 0xe8]));
  writeFileSync(join(root, 'docs', 'a.png'), Buffer.from([0x89, 0x50]));
  return { base, root };
}

test('ルートの内側のファイルだけを読む', (t) => {
  const { root } = tree(t);
  const context = fileContext({ root, file: 'docs/main.md', dir: 'docs/' });
  assert.equal(context.readInclude('docs/part.md'), '## 取り込んだ見出し\n\n本文 <script>alert(1)</script>\n');
  assert.equal(context.readInclude('top.md'), 'ルートのファイル\n'); // BOM は消す
  assert.equal(context.readInclude('docs/a%2fb.md'), '名前にパーセントのあるファイル\n'); // % を解かない
  assert.equal(context.readInclude('docs/none.md'), null);
  assert.equal(context.readInclude('docs'), null); // フォルダー
  assert.deepEqual(context.readInclude('docs/bin.dat'), { error: 'not readable' });
  assert.deepEqual(context.readInclude('docs/latin1.txt'), { error: 'not readable' });
  // 外へ出るパスと .git は、渡されても読まない (renderDocument は渡さないが、ここでも拒む)
  for (const path of ['../outside.md', 'docs/../../outside.md', '.git/config', 'docs/../.git/config', '%2e%2e/outside.md', 'docs\\part.md']) {
    assert.equal(context.readInclude(path), null, path);
    assert.equal(context.exists(path), false, path);
  }
  // 先頭の / は、ルートからの意味になるだけ (外へは出ない)
  assert.equal(context.readInclude('/top.md'), 'ルートのファイル\n');
  assert.equal(context.exists('docs/a.png'), true);
  assert.equal(context.exists('docs/b.png'), false);
  assert.equal(context.exists('docs'), false);
});

test('リンクでルートの外へ出るファイルは読まない', (t) => {
  const { base, root } = tree(t);
  try {
    symlinkSync(join(base, 'outside.md'), join(root, 'docs', 'link.md'));
  } catch {
    return t.skip('この環境ではシンボリックリンクを作れない');
  }
  const context = fileContext({ root, file: 'docs/main.md', dir: 'docs/' });
  assert.equal(context.readInclude('docs/link.md'), null);
  assert.equal(context.exists('docs/link.md'), false);
});

test('10 MB を超えるファイルは、大きすぎるとして読まない', (t) => {
  const { root } = tree(t);
  writeFileSync(join(root, 'big.md'), Buffer.alloc(10 * 1024 * 1024 + 1, 0x61));
  writeFileSync(join(root, 'edge.md'), Buffer.alloc(10 * 1024 * 1024, 0x61));
  const context = fileContext({ root, file: 'main.md', dir: '' });
  assert.deepEqual(context.readInclude('big.md'), { error: 'too large' });
  assert.equal(context.readInclude('edge.md').length, 10 * 1024 * 1024);
});

test('ルートの無い文書 (無名) では、読み込みも有無の確かめもしない', () => {
  const context = fileContext({ root: null, file: '[No Name]', dir: '' });
  assert.equal(context.readInclude, undefined);
  assert.equal(context.exists, undefined);
});

function rendered(t, document) {
  return new Promise((resolve, reject) => {
    const session = createSession({ onRender: (data) => resolve(data) });
    t.after(() => session.close());
    try {
      session.setDocument(document);
    } catch (error) {
      reject(error);
    }
  });
}

test('セッションを通すと、include が展開され、消毒され、知らせが届く', async (t) => {
  const { root } = tree(t);
  const path = join(root, 'docs', 'main.md');
  const text = '# 題\n\n::include{file=part.md}\n\n::include{file=../../outside.md}\n\n![ある](a.png) ![無い](b.png)\n\n課題 #12\n';
  writeFileSync(path, text);
  const data = await rendered(t, { path, name: 'main.md', text, rev: 1 });
  assert.equal(data.file, 'docs/main.md');
  assert.match(data.html, /取り込んだ見出し/);
  assert.doesNotMatch(data.html, /<script|外のファイル/);
  assert.match(data.html, /Error including .*not found/);
  assert.deepEqual(
    data.notices.map((notice) => notice.kind + ':' + notice.line),
    ['include:5', 'missing:7', 'reference:9'],
  );
});

test('無名の文書は、include を展開せずに描く', async (t) => {
  const data = await rendered(t, { path: null, name: '[No Name]', text: '::include{file=part.md}\n\n![x](a.png)\n', rev: 1 });
  assert.match(data.html, /::include\{file=part\.md\}/);
  assert.deepEqual(data.notices.map((notice) => notice.kind), ['include']);
});

test('入れ子が深すぎる文書は、その理由と原文を届ける', async (t) => {
  const text = '<div>'.repeat(20000) + 'x\n';
  const data = await rendered(t, { path: null, name: '[No Name]', text, rev: 1 });
  assert.equal(data.html, undefined);
  assert.equal(data.error, '入れ子が深すぎるため、描画できません。原文を表示します。');
  assert.equal(data.markdown, text);
});
