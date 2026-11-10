// ルートからの相対パスの組み立て (lib/paths.mjs)。読み込みの関数に、ルートの外を指すパスを渡さないことを確かめる。
import assert from 'node:assert/strict';
import test from 'node:test';

import { includeBase, resolvePath } from '../lib/paths.mjs';
import { renderDocument } from '../lib/pipeline.mjs';

test('文書のフォルダーから解決し、/ で始まるものはルートから解決する', () => {
  assert.deepEqual(resolvePath('part.md', 'docs'), { path: 'docs/part.md', safe: true });
  assert.deepEqual(resolvePath('./sub/../part.md', 'docs'), { path: 'docs/part.md', safe: true });
  assert.deepEqual(resolvePath('../top.md', 'docs'), { path: 'top.md', safe: true });
  assert.deepEqual(resolvePath('/top.md', 'docs'), { path: 'top.md', safe: true });
  assert.deepEqual(resolvePath('part.md', ''), { path: 'part.md', safe: true });
  assert.equal(includeBase('docs/main.md'), 'docs');
  assert.equal(includeBase('main.md'), '.');
  assert.equal(includeBase('docs/main'), 'docs/main'); // 拡張子が無ければ、そのパス自身を基準にする (GitLab と同じ)
  assert.equal(includeBase(undefined), '');
});

test('ルートの外・絶対パス・OS で意味の変わるパスは、読み込ませない', () => {
  const unsafe = [
    '../../secret.md', '../..', 'a/../../../secret.md',
    'C:/secret.md', 'c:secret.md', '..\\secret.md', 'a\\b.md', '\\\\server\\share\\x.md', 'a\0b.md',
    // 点と空白だけの区切り。Windows は末尾の点と空白を捨てるので、親のフォルダーになる
    '.. /.. /secret.md', '.. /secret.md', 'a/.. /b.md', '.../x.md', '. ./x.md', '..  ./x.md', ' /x.md',
  ];
  for (const target of unsafe) assert.equal(resolvePath(target, 'docs').safe, false, JSON.stringify(target));
  for (const base of ['C:/docs', 'a\\b']) assert.equal(resolvePath('x.md', base).safe, false, base);
  assert.equal(resolvePath(null, 'docs').safe, false);
  // / で始まるパスの .. は、ルートで頭打ちになる (外へは出ない。GitLab の cleanpath と同じ)
  assert.deepEqual(resolvePath('/../secret.md', 'docs'), { path: 'secret.md', safe: true });
});

test('パーセントエンコードは解かず、文字どおりの名前として渡す', () => {
  // %2e%2e は「..」ではなく、その名前のフォルダー。解くのは URL を扱う側で、ここではない
  assert.deepEqual(resolvePath('%2e%2e/secret.md', 'docs'), { path: 'docs/%2e%2e/secret.md', safe: true });
  assert.deepEqual(resolvePath('a%2fb.md', ''), { path: 'a%2fb.md', safe: true });
});

test('include は、読み込みの関数に危ないパスを渡さない', () => {
  const asked = [];
  const context = { file: 'docs/main.md', dir: 'docs/', readInclude: (path) => { asked.push(path); return null; } };
  const targets = ['../../secret.md', '.. /.. /secret.md', 'C:/secret.md', '..\\secret.md', '/../x.md', '.../x.md', 'ok.md', '/top.md', '%2e%2e/x.md'];
  renderDocument(targets.map((target) => `::include{file=${target}}`).join('\n\n') + '\n', context);
  assert.deepEqual(asked, ['x.md', 'docs/ok.md', 'top.md', 'docs/%2e%2e/x.md']);
});

test('画像の有無の確かめにも、危ないパスを渡さない', () => {
  const asked = [];
  const context = { file: 'docs/main.md', dir: 'docs/', exists: (path) => { asked.push(path); return true; } };
  renderDocument('![a](../../x.png) ![b](%2e%2e%20/%2e%2e%20/y.png) ![c](ok.png) ![d](/top.png) ![e](https://example.com/z.png)\n', context);
  assert.deepEqual(asked.sort(), ['docs/ok.png', 'top.png']);
});
