import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { createHash } from 'node:crypto';
import {
  cpSync,
  existsSync,
  mkdirSync,
  readdirSync,
  readFileSync,
  symlinkSync,
  unlinkSync,
  writeFileSync,
} from 'node:fs';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import test from 'node:test';
import { updatePageLibraries } from '../tools/update-page-libs.mjs';
import { temp } from './helpers.mjs';

const repo = fileURLToPath(new URL('../', import.meta.url));
const libraryRoot = join(repo, 'page', 'lib');
const lock = JSON.parse(readFileSync(join(libraryRoot, 'lock.json'), 'utf8'));
const npmLock = JSON.parse(readFileSync(join(repo, 'package-lock.json'), 'utf8'));
const sha256 = (bytes) => createHash('sha256').update(bytes).digest('hex');

test('ブラウザ用の全ファイルは、固定情報の sha256・大きさと npm の版・integrity に一致する', () => {
  assert.equal(lock.version, 1);
  assert.deepEqual(Object.keys(lock.packages).sort(), ['katex', 'mermaid']);
  for (const [name, pkg] of Object.entries(lock.packages)) {
    assert.equal(pkg.name, name);
    assert.equal(pkg.version, npmLock.packages['node_modules/' + name].version);
    assert.equal(pkg.integrity, npmLock.packages['node_modules/' + name].integrity);
    for (const [file, record] of Object.entries(pkg.files)) {
      const bytes = readFileSync(join(libraryRoot, name, file));
      assert.equal(bytes.length, record.size, name + '/' + file);
      assert.equal(sha256(bytes), record.sha256, name + '/' + file);
    }
    assert.ok(Object.hasOwn(pkg.files, 'LICENSE'));
  }
  assert.ok(Object.keys(lock.packages.katex.files).some((name) => name.endsWith('.woff2')));
  assert.equal(
    Object.keys(lock.packages.katex.files).some((name) => /\.(woff|ttf)$/.test(name)),
    false,
  );
});

test('導入済みの原文と写しを比べ、更新道具の --check も同じ結果になる', () => {
  assert.deepEqual(updatePageLibraries({ check: true }), lock);
  for (const [name, pkg] of Object.entries(lock.packages)) {
    for (const file of Object.keys(pkg.files)) {
      const source = join(repo, 'node_modules', name, ...(file === 'LICENSE' ? [file] : ['dist', file]));
      assert.deepEqual(readFileSync(join(libraryRoot, name, file)), readFileSync(source));
    }
  }
});

function copied(t) {
  const root = temp(t);
  mkdirSync(join(root, 'page'));
  cpSync(libraryRoot, join(root, 'page', 'lib'), { recursive: true });
  writeFileSync(join(root, 'package-lock.json'), readFileSync(join(repo, 'package-lock.json')));
  return root;
}

test('node_modules がなくても写しを検査し、改変・不足・未登録のファイルを見つける', (t) => {
  const root = copied(t);
  assert.equal(existsSync(join(root, 'node_modules')), false);
  assert.deepEqual(updatePageLibraries({ root, check: true }), lock);
  const file = join(root, 'page', 'lib', 'katex', 'katex.min.js');
  const bytes = readFileSync(file);
  writeFileSync(file, Buffer.concat([bytes, Buffer.from('\n')]));
  assert.throws(() => updatePageLibraries({ root, check: true }), /ハッシュ|大きさ/);
  writeFileSync(file, bytes);
  unlinkSync(file);
  assert.throws(() => updatePageLibraries({ root, check: true }), { code: 'ENOENT' });
  writeFileSync(file, bytes);
  writeFileSync(join(root, 'page', 'lib', 'katex', 'unlisted.js'), '追加のファイル');
  assert.throws(() => updatePageLibraries({ root, check: true }), /固定情報にないファイル/);
});

test('更新道具は node_modules から写しとライセンスを作り直せる', (t) => {
  const root = temp(t);
  writeFileSync(join(root, 'package-lock.json'), readFileSync(join(repo, 'package-lock.json')));
  const updated = updatePageLibraries({ root, modules: join(repo, 'node_modules') });
  assert.deepEqual(updated, lock);
  assert.deepEqual(updatePageLibraries({ root, check: true }), lock);
});

test('更新道具は、置き場所の途中にリンクがあれば止め、リンクの先を変えない', (t) => {
  const root = temp(t),
    elsewhere = temp(t);
  writeFileSync(join(root, 'package-lock.json'), readFileSync(join(repo, 'package-lock.json')));
  mkdirSync(join(elsewhere, 'katex'));
  writeFileSync(join(elsewhere, 'katex', 'keep.txt'), '利用者のファイル');
  mkdirSync(join(root, 'page'));
  try {
    symlinkSync(elsewhere, join(root, 'page', 'lib'), 'junction');
  } catch (error) {
    if (error.code === 'EPERM') return t.skip('この環境ではフォルダーへのリンクを作れない');
    throw error;
  }
  assert.throws(() => updatePageLibraries({ root, modules: join(repo, 'node_modules') }), /リンク/);
  assert.deepEqual(readdirSync(elsewhere), ['katex']);
  assert.deepEqual(readdirSync(join(elsewhere, 'katex')), ['keep.txt']);
  assert.equal(readFileSync(join(elsewhere, 'katex', 'keep.txt'), 'utf8'), '利用者のファイル');
});

test('開発用依存を解決できない隔離コピーでも起動し、固定情報にあるファイルだけ配る', { timeout: 15000 }, async (t) => {
  const root = copied(t);
  mkdirSync(join(root, 'lib'));
  mkdirSync(join(root, 'assets'));
  writeFileSync(join(root, 'assets', 'directory.mjs'), readFileSync(join(repo, 'assets', 'directory.mjs')));
  for (const file of ['server.mjs', 'documents.mjs', 'resolve.mjs', 'assets.mjs', 'page-libs.mjs'])
    writeFileSync(join(root, 'lib', file), readFileSync(join(repo, 'lib', file)));
  writeFileSync(join(root, 'page', 'lib', 'katex', 'unlisted.js'), '配ってはいけないファイル');
  // 固定情報にあるファイルでも、中身が差し替わっていたら配らない。
  const tampered = join(root, 'page', 'lib', 'katex', 'fonts', 'KaTeX_AMS-Regular.woff2');
  writeFileSync(tampered, Buffer.concat([readFileSync(tampered), Buffer.from([0])]));
  // 親フォルダーの node_modules へも逃げて解決できないようにする。
  const script = [
    "import Module from 'node:module';",
    'const original = Module._resolveFilename;',
    "Module._resolveFilename = function(id, ...args) { if (/^(?:katex|mermaid)(?:\\/|$)/.test(id)) { const error = new Error('開発用依存は導入していません'); error.code = 'MODULE_NOT_FOUND'; throw error; } return Reflect.apply(original, this, [id, ...args]); };",
    "const {createServer} = await import('./lib/server.mjs');",
    "const server = await createServer({session:{root:()=>null,last:()=>({})},page:{html:'隔離したページ'}});",
    "try { const paths=['katex/katex.min.js','katex/katex.min.css','katex/fonts/KaTeX_Main-Regular.woff2','mermaid/mermaid.min.js','katex/LICENSE','mermaid/LICENSE','katex/unlisted.js','katex/fonts/KaTeX_Main-Regular.woff','katex/fonts/KaTeX_Main-Regular.ttf','lock.json','katex/../package.json','katex/fonts/KaTeX_AMS-Regular.woff2']; const results=[]; for(const path of paths){const response=await fetch(server.url+'_/lib/'+path);results.push({path,status:response.status,size:(await response.arrayBuffer()).byteLength});} process.stdout.write(JSON.stringify(results));} finally {await server.close();}",
  ].join('\n');
  const result = await new Promise((resolve, reject) => {
    const child = spawn(process.execPath, ['--input-type=module', '-e', script], { cwd: root, windowsHide: true });
    let stdout = '',
      stderr = '';
    child.stdout.on('data', (bytes) => {
      stdout += bytes;
    });
    child.stderr.on('data', (bytes) => {
      stderr += bytes;
    });
    child.on('error', reject);
    child.on('close', (code) => resolve({ code, stdout, stderr }));
    t.after(() => child.kill());
  });
  assert.equal(result.code, 0, result.stderr);
  const responses = JSON.parse(result.stdout);
  for (const entry of responses.slice(0, 6)) {
    assert.equal(entry.status, 200, entry.path);
    assert.ok(entry.size > 0);
  }
  for (const entry of responses.slice(6)) assert.equal(entry.status, 404, entry.path);
});
