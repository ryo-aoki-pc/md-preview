import assert from 'node:assert/strict';
import { mkdirSync, rmSync, symlinkSync } from 'node:fs';
import { join } from 'node:path';
import test from 'node:test';
import { EventEmitter } from 'node:events';
import { loadAssets, defaultAssetsDirectory } from '../lib/assets.mjs';
import { main, parseArgs } from '../cli.mjs';
import { createServer } from '../lib/server.mjs';
import { loadPage } from '../lib/page.mjs';
import { runStdio } from '../lib/stdio.mjs';
import { Readable } from 'node:stream';
import { get } from './helpers.mjs';
import { assetFixture } from './assets-fixture.mjs';

test('公式資産のバイナリー・版・サイズ・ハッシュ・一覧・通知を検査する', (t) => {
  const f = assetFixture(t),
    load = () => loadAssets(f.directory, f.lockPath);
  assert.equal(load(), null);
  f.save();
  assert.equal(load().id, f.id);
  assert.deepEqual(load().files.get('font.woff2').body, f.bodies['font.woff2']);
  assert.equal(load().files.get('font.woff2').mime, 'font/woff2');
  for (const change of [
    () => f.write('font.woff2', Buffer.from([97, 0, 99, 255, 10])),
    () => (f.manifest.files['../outside.css'] = { size: 0, sha256: '0'.repeat(64) }),
    () => (f.manifest.styles.light[0] = 'missing.css'),
    () => (f.manifest.lock_sha256 = '0'.repeat(64)),
    () => (f.manifest.version = 'other'),
    () => f.manifest.files['app.css'].size++,
    () => delete f.manifest.files['licenses/MIT.txt'],
  ]) {
    const previous = structuredClone(f.manifest);
    change();
    f.save();
    assert.equal(load(), null);
    Object.keys(f.manifest).forEach((key) => delete f.manifest[key]);
    Object.assign(f.manifest, previous);
    f.write('font.woff2', f.bodies['font.woff2']);
    f.save();
    assert.ok(load());
  }
  rmSync(join(f.bundle, 'licenses/MIT.txt'));
  assert.equal(load(), null);
});

test('公式資産も resolve の実パス検査で外へのリンクを拒む', (t) => {
  const f = assetFixture(t);
  f.save();
  rmSync(join(f.bundle, 'app.css'));
  try {
    symlinkSync(f.lockPath, join(f.bundle, 'app.css'));
  } catch (e) {
    if (e.code === 'EPERM') {
      t.skip('シンボリックリンクを作る権限がない');
      return;
    }
    throw e;
  }
  assert.equal(loadAssets(f.directory, f.lockPath), null);
});

test('公式資産の配信は許可リスト・MIME・sandbox・キャッシュを保つ', async (t) => {
  const f = assetFixture(t);
  f.save();
  const server = await createServer({
    session: { root: () => null, last: () => ({}) },
    page: loadPage(),
    assetsDir: f.directory,
    assetsLockPath: f.lockPath,
  });
  t.after(() => server.close());
  const path = '_/assets/' + f.id + '/app.css';
  const res = await get(server, path);
  assert.equal(res.status, 200);
  assert.equal(res.headers['content-type'], 'text/css; charset=utf-8');
  assert.equal(res.headers['content-security-policy'], 'sandbox');
  assert.match(res.headers['cache-control'], /private.*immutable/);
  assert.equal((await get(server, path, { headers: { 'If-None-Match': res.headers.etag } })).status, 304);
  assert.equal((await get(server, path, { method: 'HEAD' })).body.length, 0);
  for (const tail of ['unknown.css', 'install.json', '../lock.json', '%61pp.css'])
    assert.equal((await get(server, '_/assets/' + f.id + '/' + tail)).status, 404);
  assert.equal((await get(server, '_/assets/other/app.css')).status, 404);
  assert.ok((await get(server)).body.toString().includes(f.id));
});

test('公式スタイルの知らせは、何か導入されているのに使えないときだけ出す', async (t) => {
  const f = assetFixture(t); // 資産のファイルはあるが、install.json がまだ無い (使えない)
  const empty = join(f.directory, 'empty');
  mkdirSync(empty);
  const config = async (options) => {
    const server = await createServer({
      session: { root: () => null, last: () => ({}) },
      page: loadPage(),
      ...options,
    });
    try {
      const html = (await get(server)).body.toString();
      return JSON.parse(/<script id="gp-config"[^>]*>([\s\S]*?)<\/script>/.exec(html)[1]);
    } finally {
      await server.close();
    }
  };
  // 保存先を渡さない・保存先が無い・空: 既定の状態 (同梱のスタイル) なので、知らせない
  for (const options of [{}, { assetsDir: join(f.directory, 'none') }, { assetsDir: empty }]) {
    const value = await config(options);
    assert.equal(value.assets, null);
    assert.equal(value.notice, '');
  }
  const location = { assetsDir: f.directory, assetsLockPath: f.lockPath };
  const broken = await config(location);
  assert.equal(broken.assets, null);
  assert.match(broken.notice, /公式スタイルを使えません.*mdpreview install-style/);
  assert.equal((await config({ ...location, officialStyle: false })).notice, '');
  f.save();
  const valid = await config(location);
  assert.equal(valid.assets.id, f.id);
  assert.equal(valid.notice, '');
});

test('install-style --check は通信せず有効・欠落・破損を区別する', async (t) => {
  const f = assetFixture(t);
  const original = globalThis.fetch;
  globalThis.fetch = () => assert.fail('検査から通信しない');
  t.after(() => {
    globalThis.fetch = original;
  });
  for (const valid of [false, true, false]) {
    if (valid) f.save();
    else f.write('app.css', '破損');
    if (valid) f.write('app.css', f.bodies['app.css']);
    let out = '',
      err = '';
    const code = await main(['install-style', '--check', '--dir', f.directory], {
      stdout: { write: (s) => (out += s) },
      stderr: { write: (s) => (err += s) },
      assetsLockPath: f.lockPath,
      signals: new EventEmitter(),
    });
    assert.equal(code, valid ? 0 : 1);
    assert.equal(out, '');
    assert.match(err, /^mdpreview: [^\n]+\n$/);
  }
  assert.equal(parseArgs(['--stdio', '--assets-dir', f.directory, '--no-official-style']).officialStyle, false);
});

test('公式資産の既定の保存先は導入先と分ける', () => {
  assert.equal(defaultAssetsDirectory('linux', { XDG_DATA_HOME: '/data' }, '/home/test'), '/data/md-preview-assets');
  for (const xdg of ['', 'relative'])
    assert.equal(
      defaultAssetsDirectory('linux', { XDG_DATA_HOME: xdg }, '/home/test'),
      '/home/test/.local/share/md-preview-assets',
    );
  assert.equal(
    defaultAssetsDirectory('darwin', {}, '/home/test'),
    '/home/test/Library/Application Support/md-preview-assets',
  );
  assert.equal(
    defaultAssetsDirectory('win32', { LOCALAPPDATA: 'C:\\Data' }, 'C:\\Home'),
    'C:\\Data\\md-preview-assets',
  );
  assert.equal(
    defaultAssetsDirectory('win32', { USERPROFILE: 'C:\\Home' }, 'C:\\Home'),
    'C:\\Home\\AppData\\Local\\md-preview-assets',
  );
});

test('stdio でも同じ資産を検査し、版1の出力を保って公式スタイルを配る', async (t) => {
  const f = assetFixture(t);
  f.save();
  for (const officialStyle of [true, false]) {
    let out = '';
    const code = await runStdio({
      stdin: Readable.from(['{"type":"hello","protocol":1}\n']),
      stdout: { write: (s) => (out += s) },
      signals: new EventEmitter(),
      version: 'test',
      assetsDir: f.directory,
      assetsLockPath: f.lockPath,
      officialStyle,
      onStart: async ({ server }) => {
        const page = (await get(server)).body.toString();
        assert.equal(page.includes(f.id), officialStyle);
        assert.equal((await get(server, '_/assets/' + f.id + '/app.css')).status, officialStyle ? 200 : 404);
      },
    });
    assert.equal(code, 0);
    const messages = out
      .trim()
      .split('\n')
      .map((s) => JSON.parse(s));
    assert.equal(messages[0].type, 'ready');
    assert.equal(messages[0].protocol, 1);
    assert.equal(
      messages.some((m) => !['ready', 'clients'].includes(m.type)),
      false,
    );
  }
});
