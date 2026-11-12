// 更新の試験は .tests/ の合成の npm パッケージだけを使う。
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import test from 'node:test';
import { fileURLToPath } from 'node:url';
import { updatePageLibraries } from '../tools/update-page-libs.mjs';
import { temp } from './helpers.mjs';

function fixture(t) {
  const root = temp(t);
  const save = (file, bytes) => {
    mkdirSync(dirname(file), { recursive: true });
    writeFileSync(file, bytes);
  };
  const original = Buffer.from('// 合成の数式\r\n(() => {})();\r\n');
  const packages = {};
  for (const name of ['katex', 'mermaid']) {
    const pkg = { name, version: '1.0.0' };
    const module = join(root, 'node_modules', name);
    packages['node_modules/' + name] = { version: pkg.version, integrity: 'sha512-fixture-' + name };
    save(join(module, 'package.json'), JSON.stringify(pkg));
    save(join(module, 'LICENSE'), '合成のライセンス\r\n');
    if (name === 'katex') {
      save(join(module, 'dist/katex.min.js'), original);
      save(join(module, 'dist/katex.min.css'), '/* スタイル */');
      save(join(module, 'dist/fonts/font.woff2'), Buffer.from([1, 2, 3]));
      save(join(module, 'dist/fonts/font.ttf'), Buffer.from([4, 5, 6]));
    } else save(join(module, 'dist/mermaid.min.js'), '/* 図 */');
  }
  save(join(root, 'package-lock.json'), JSON.stringify({ packages }));
  return { root, save, original, lockPath: join(root, 'page/lib/lock.json'),
    copiedPath: join(root, 'page/lib/katex/katex.min.js') };
}

test('npm の版・integrity・原文のバイトを写し、開発依存がなくても検査できる', (t) => {
  const f = fixture(t);
  const lock = updatePageLibraries({ root: f.root });
  assert.deepEqual(Object.keys(lock.packages), ['katex', 'mermaid']);
  for (const name of ['katex', 'mermaid']) {
    assert.equal(lock.packages[name].version, '1.0.0');
    assert.equal(lock.packages[name].integrity, 'sha512-fixture-' + name);
    assert.deepEqual(readFileSync(join(f.root, 'page/lib', name, 'LICENSE')),
      readFileSync(join(f.root, 'node_modules', name, 'LICENSE')));
  }
  assert.deepEqual(readFileSync(f.copiedPath), f.original);
  assert.deepEqual(readFileSync(join(f.root, 'page/lib/katex/fonts/font.woff2')), Buffer.from([1, 2, 3]));
  assert.equal(existsSync(join(f.root, 'page/lib/katex/fonts/font.ttf')), false);
  updatePageLibraries({ root: f.root, check: true });
  updatePageLibraries({ root: f.root, modules: join(f.root, 'absent'), check: true });
  updatePageLibraries({ root: f.root });
  assert.deepEqual(JSON.parse(readFileSync(f.lockPath, 'utf8')), lock);
});

test('npm の固定情報と導入済みの版の違いは、配布先を書き換える前に拒む', (t) => {
  const f = fixture(t);
  updatePageLibraries({ root: f.root });
  const lock = readFileSync(f.lockPath);
  f.save(join(f.root, 'node_modules/katex/package.json'), JSON.stringify({ name: 'katex', version: '2.0.0' }));
  assert.throws(() => updatePageLibraries({ root: f.root }), /npm の固定情報が一致しません/);
  assert.throws(() => updatePageLibraries({ root: f.root, check: true }), /導入済みの版と一致しません/);
  assert.deepEqual(readFileSync(f.lockPath), lock);
  assert.deepEqual(readFileSync(f.copiedPath), f.original);
  f.save(join(f.root, 'node_modules/katex/package.json'), JSON.stringify({ name: 'katex', version: '1.0.0' }));
  const npmLock = JSON.parse(readFileSync(join(f.root, 'package-lock.json'), 'utf8'));
  npmLock.packages['node_modules/katex'].integrity = 'sha512-different';
  f.save(join(f.root, 'package-lock.json'), JSON.stringify(npmLock));
  assert.throws(() => updatePageLibraries({ root: f.root, check: true }), /npm の固定情報と一致しません/);
});

test('改変・固定情報にないファイル・npm の原文との違いを見逃さない', (t) => {
  const f = fixture(t);
  const lock = updatePageLibraries({ root: f.root });
  const modified = Buffer.from('// 原文とは違う写し\n');
  f.save(f.copiedPath, modified);
  assert.throws(() => updatePageLibraries({ root: f.root, check: true }), /ハッシュまたは大きさ/);
  lock.packages.katex.files['katex.min.js'] = {
    sha256: createHash('sha256').update(modified).digest('hex'), size: modified.length,
  };
  f.save(f.lockPath, JSON.stringify(lock));
  updatePageLibraries({ root: f.root, modules: join(f.root, 'absent'), check: true });
  assert.throws(() => updatePageLibraries({ root: f.root, check: true }), /原文と写しが一致しません/);
  updatePageLibraries({ root: f.root });
  f.save(join(f.root, 'page/lib/katex/extra.js'), '/* 一覧にない */');
  assert.throws(() => updatePageLibraries({ root: f.root, check: true }), /固定情報にないファイル/);
});

test('lock.json に md-table-sticky があれば検査も更新も拒む', (t) => {
  const f = fixture(t);
  const lock = updatePageLibraries({ root: f.root });
  lock.packages['md-table-sticky'] = { name: 'md-table-sticky', version: '0.1.0', files: {} };
  f.save(f.lockPath, JSON.stringify(lock));
  const originalLock = readFileSync(f.lockPath);
  for (const check of [false, true]) {
    assert.throws(() => updatePageLibraries({ root: f.root, check }), /固定情報の形が正しくありません/);
    assert.deepEqual(readFileSync(f.lockPath), originalLock);
    assert.deepEqual(readFileSync(f.copiedPath), f.original);
  }
});

test('コマンドの引数の誤りは使い方を出して 2 で終わり、検査は 0 で終わる', () => {
  const tool = fileURLToPath(new URL('../tools/update-page-libs.mjs', import.meta.url));
  const run = (...args) => spawnSync(process.execPath, [tool, ...args], { encoding: 'utf8', windowsHide: true });
  for (const args of [['--invalid'], ['--check', '--check'], ['--md-table-sticky'], ['--md-table-sticky', 'clone']]) {
    const result = run(...args);
    assert.equal(result.status, 2, args.join(' '));
    assert.match(result.stderr, /使い方/);
    assert.equal(result.stdout, '');
  }
  // リポジトリに置いた写しそのものを、開発依存が無くても検査できる。
  const checked = run('--check');
  assert.equal(checked.status, 0, checked.stderr);
  assert.match(checked.stdout, /一致しています/);
});
