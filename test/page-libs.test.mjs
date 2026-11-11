// 更新の試験は .tests/ の使い捨ての Git リポジトリと合成の npm パッケージだけを使う。
import assert from 'node:assert/strict';
import { execFileSync, spawnSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import test from 'node:test';
import { fileURLToPath } from 'node:url';
import { updatePageLibraries } from '../tools/update-page-libs.mjs';
import { temp } from './helpers.mjs';

function fixture(t) {
  const root = temp(t);
  const clone = join(root, 'sticky-source');
  const save = (file, bytes) => {
    mkdirSync(dirname(file), { recursive: true });
    writeFileSync(file, bytes);
  };
  const git = (...args) => execFileSync('git', ['-C', clone, '-c', 'core.autocrlf=false', '-c', 'user.name=Fixture',
    '-c', 'user.email=fixture@example.invalid', '-c', 'commit.gpgsign=false', ...args],
  { windowsHide: true, stdio: ['ignore', 'pipe', 'pipe'] }).toString('utf8').trim();
  mkdirSync(clone);
  git('init');
  const commit = () => {
    git('add', 'src/sticky-table.js', 'LICENSE', 'package.json');
    git('commit', '-m', '試験用の原文');
    return git('rev-parse', 'HEAD');
  };
  const original = Buffer.from('// 合成の原文\r\n(() => {})();\r\n');
  save(join(clone, 'src/sticky-table.js'), original);
  save(join(clone, 'LICENSE'), '合成のライセンス\r\n');
  save(join(clone, 'package.json'), JSON.stringify({ name: 'md-table-sticky', version: '0.1.0' }));
  const head = commit();
  const packages = {};
  for (const name of ['katex', 'mermaid']) {
    const pkg = { name, version: '1.0.0' };
    const module = join(root, 'node_modules', name);
    packages['node_modules/' + name] = { version: pkg.version, integrity: 'sha512-fixture-' + name };
    save(join(module, 'package.json'), JSON.stringify(pkg));
    save(join(module, 'LICENSE'), '合成のライセンス\n');
    if (name === 'katex') {
      save(join(module, 'dist/katex.min.js'), '/* 数式 */');
      save(join(module, 'dist/katex.min.css'), '/* スタイル */');
      save(join(module, 'dist/fonts/font.woff2'), Buffer.from([1, 2, 3]));
      save(join(module, 'dist/fonts/font.ttf'), Buffer.from([4, 5, 6]));
    } else save(join(module, 'dist/mermaid.min.js'), '/* 図 */');
  }
  save(join(root, 'package-lock.json'), JSON.stringify({ packages }));
  return { root, clone, save, commit, original, head, lockPath: join(root, 'page/lib/lock.json'),
    stickyPath: join(root, 'page/lib/md-table-sticky/sticky-table.js') };
}

test('きれいな HEAD の版・コミット・原文のバイトを写し、固定したコミットとも比べる', (t) => {
  const f = fixture(t);
  const lock = updatePageLibraries({ root: f.root, mdTableSticky: f.clone });
  const record = lock.packages['md-table-sticky'];
  assert.equal(record.version, '0.1.0');
  assert.equal(record.source.commit, f.head);
  assert.equal(Object.hasOwn(record, 'integrity'), false);
  assert.deepEqual(readFileSync(f.stickyPath), f.original);
  assert.deepEqual(readFileSync(join(f.root, 'page/lib/md-table-sticky/LICENSE')),
    readFileSync(join(f.clone, 'LICENSE')));
  updatePageLibraries({ root: f.root, check: true, mdTableSticky: f.clone });
  updatePageLibraries({ root: f.root, modules: join(f.root, 'absent'), check: true });
  // HEAD と作業ツリーが変わっても、check は保存したコミットを参照する。
  f.save(join(f.clone, 'src/sticky-table.js'), '// 次の原文\n');
  f.commit();
  f.save(join(f.clone, 'LICENSE'), '未コミットの変更');
  updatePageLibraries({ root: f.root, check: true, mdTableSticky: f.clone });
  const preserved = JSON.stringify(record);
  updatePageLibraries({ root: f.root });
  assert.equal(JSON.stringify(JSON.parse(readFileSync(f.lockPath, 'utf8')).packages['md-table-sticky']), preserved);
  assert.deepEqual(readFileSync(f.stickyPath), f.original);
});

test('変更や未追跡ファイルのある写し元は、配布先を書き換える前に拒む', (t) => {
  const f = fixture(t);
  updatePageLibraries({ root: f.root, mdTableSticky: f.clone });
  const lock = readFileSync(f.lockPath);
  f.save(join(f.clone, 'src/sticky-table.js'), '// 未コミットの変更\n');
  assert.throws(() => updatePageLibraries({ root: f.root, mdTableSticky: f.clone }), /作業ツリーに変更/);
  assert.deepEqual(readFileSync(f.lockPath), lock);
  assert.deepEqual(readFileSync(f.stickyPath), f.original);
  f.save(join(f.clone, 'src/sticky-table.js'), f.original);
  f.save(join(f.clone, 'untracked.txt'), '未追跡');
  assert.throws(() => updatePageLibraries({ root: f.root, mdTableSticky: f.clone }), /作業ツリーに変更/);
  assert.deepEqual(readFileSync(f.lockPath), lock);
});

test('改変・固定情報にないファイル・Git の原文との違いを見逃さない', (t) => {
  const f = fixture(t);
  const lock = updatePageLibraries({ root: f.root, mdTableSticky: f.clone });
  const modified = Buffer.from('// 原文とは違う写し\n');
  f.save(f.stickyPath, modified);
  assert.throws(() => updatePageLibraries({ root: f.root, check: true }), /ハッシュまたは大きさ/);
  lock.packages['md-table-sticky'].files['sticky-table.js'] = {
    sha256: createHash('sha256').update(modified).digest('hex'), size: modified.length,
  };
  f.save(f.lockPath, JSON.stringify(lock));
  updatePageLibraries({ root: f.root, check: true });
  assert.throws(() => updatePageLibraries({ root: f.root, check: true, mdTableSticky: f.clone }), /原文と写しが一致しません/);
  f.save(join(f.root, 'page/lib/md-table-sticky/extra.js'), '/* 一覧にない */');
  assert.throws(() => updatePageLibraries({ root: f.root, check: true }), /固定情報にないファイル/);
});

test('コマンドの引数の誤りは使い方を出して 2 で終わり、検査は 0 で終わる', () => {
  const tool = fileURLToPath(new URL('../tools/update-page-libs.mjs', import.meta.url));
  const run = (...args) => spawnSync(process.execPath, [tool, ...args], { encoding: 'utf8', windowsHide: true });
  for (const args of [['--invalid'], ['--check', '--check'], ['--md-table-sticky'], ['--md-table-sticky', '--check']]) {
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
