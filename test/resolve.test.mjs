import assert from 'node:assert/strict';
import { mkdirSync, realpathSync, symlinkSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import test from 'node:test';
import { resolve } from '../lib/resolve.mjs';
import { temp } from './helpers.mjs';

for (const isWin of [false, true]) {
  test(`パス検査（${isWin ? 'Windows' : 'POSIX'} の分岐）`, async (t) => {
    const root = temp(t);
    mkdirSync(join(root, 'docs'));
    writeFileSync(join(root, 'docs', '画像 a.png'), 'image');
    const valid = ['docs/画像 a.png', 'docs/%E7%94%BB%E5%83%8F%20a.png', './docs//画像 a.png', '/docs/画像 a.png'];
    for (const raw of valid) {
      assert.deepEqual(resolve(root, raw, isWin), { status: 200, path: realpathSync.native(join(root, 'docs', '画像 a.png')).replaceAll('\\', '/') }, raw);
    }
    for (const raw of ['', '.', '/', 'missing']) assert.equal(resolve(root, raw, isWin).status, 404, raw);
    for (const raw of ['%00', 'docs/a%00.png', 'a\0', 'a\x01', 'a\x1f', 'a\x7f', 'a%0A', 'a%7F', 'a\\b', 'a%5cb']) {
      assert.equal(resolve(root, raw, isWin).status, 400, raw);
    }
    for (const raw of ['..', '../x', 'docs/../x', '%2e%2e/x', 'docs/%2E%2e/x', '.git/config', 'docs/.GIT/config', '%2egit/config']) {
      assert.equal(resolve(root, raw, isWin).status, 403, raw);
    }
    const windowsOnly = ['x:y', 'C:/file', 'file.', 'file ', 'docs. /x', 'CON', 'con.txt', 'PRN', 'aux.dat', 'NUL', 'conin$', 'conout$.txt', 'COM0', 'COM9.txt', 'LPT0', 'lpt9.log'];
    for (const raw of windowsOnly) assert.equal(resolve(root, raw, isWin).status, isWin ? 400 : 404, raw);
    for (const raw of ['COM10', 'lpt10', 'console']) assert.equal(resolve(root, raw, isWin).status, 404, raw);
    writeFileSync(join(root, 'percent%Ʊ'), 'unicode');
    assert.equal(resolve(root, 'percent%Ʊ', isWin).status, 200);
    // Lua と同じく、不完全なエスケープを不正要求とはせず、実ファイルを探す。
    writeFileSync(join(root, 'percent%ZZ'), 'value');
    assert.equal(resolve(root, 'percent%ZZ', isWin).status, 200);
  });
}

test('シンボリックリンクの実パスもルートと .git で検査する', (t) => {
  const base = temp(t);
  const root = join(base, 'root');
  mkdirSync(root);
  mkdirSync(join(root, '.git'));
  writeFileSync(join(base, 'outside.txt'), 'outside');
  writeFileSync(join(root, '.git', 'config'), 'git');
  writeFileSync(join(root, 'safe.txt'), 'safe');
  try {
    symlinkSync(base, join(root, 'outside'), process.platform === 'win32' ? 'junction' : 'dir');
    symlinkSync(join(root, '.git'), join(root, 'hidden'), process.platform === 'win32' ? 'junction' : 'dir');
    symlinkSync(root, join(root, 'inside'), process.platform === 'win32' ? 'junction' : 'dir');
  } catch (error) {
    if (['EPERM', 'EACCES', 'ENOTSUP'].includes(error.code)) return t.skip('シンボリックリンクを作る権限または機能がない');
    throw error;
  }
  for (const isWin of [false, true]) {
    assert.equal(resolve(realpathSync.native(root), 'outside/outside.txt', isWin).status, 403);
    assert.equal(resolve(realpathSync.native(root), 'hidden/config', isWin).status, 403);
    assert.equal(resolve(realpathSync.native(root), 'inside/safe.txt', isWin).status, 200);
  }
});
