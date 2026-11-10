import test from 'node:test';
import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { createServer } from 'node:http';
import { gzipSync } from 'node:zlib';
import { mkdtemp, readFile, writeFile, mkdir, readdir, rm } from 'node:fs/promises';
import { temp } from './helpers.mjs';
import { join } from 'node:path';
import { install, parseLock, rewriteCss } from '../assets/install.mjs';

const sha256 = (bytes) => createHash('sha256').update(bytes).digest('hex');
const record = (bytes, mime, extra = {}) => ({
  sha256: sha256(bytes),
  size: bytes.length,
  mime,
  licenses: ['licenses/source.txt'],
  ...extra,
});

function archive(entries) {
  const chunks = [];
  for (const [name, data, type = '0'] of entries) {
    const bytes = Buffer.from(data);
    const header = Buffer.alloc(512);
    header.write(name, 0, 100, 'utf8');
    header.write('0000644\0', 100);
    header.write('0000000\0', 108);
    header.write('0000000\0', 116);
    header.write(bytes.length.toString(8).padStart(11, '0') + '\0', 124);
    header.write('00000000000\0', 136);
    header.fill(32, 148, 156);
    header.write(type, 156);
    header.write('ustar\0', 257);
    header.write('00', 263);
    const sum = header.reduce((value, byte) => value + byte, 0);
    header.write(sum.toString(8).padStart(6, '0') + '\0 ', 148);
    chunks.push(header, bytes, Buffer.alloc((512 - (bytes.length % 512)) % 512));
  }
  return gzipSync(Buffer.concat([...chunks, Buffer.alloc(1024)]));
}

async function fixture(t, options = {}) {
  const dir = temp(t);
  const data = join(dir, 'data');
  const font = Buffer.from([0, 255, 9, 10, 13, 128]);
  const sprite = Buffer.from('<svg/>');
  const license = Buffer.from('試験用の原文\n');
  const css = Buffer.from('/* url(/assets/ignored) */ .x{src:url(/assets/fonts/a.woff2);mask:url("./icons.svg#one")}');
  const paths = [
    ['assets/main.css', css],
    ['assets/fonts/a.woff2', font],
    ['assets/icons.svg', sprite],
  ];
  const blob = options.archive || archive(paths);
  let requests = 0;
  let hold;
  const server = createServer(async (req, res) => {
    requests++;
    if (req.url === '/archive') {
      if (options.hold) {
        res.writeHead(200);
        res.write(blob.subarray(0, 8));
        hold = res;
        return;
      }
      res.end(blob);
    } else if (req.url === '/license') res.end(license);
    else {
      res.statusCode = 404;
      res.end();
    }
  });
  await new Promise((done) => server.listen(0, '127.0.0.1', done));
  const origin = `http://127.0.0.1:${server.address().port}`;
  const files = {
    'main.css': record(css, 'text/css', { archive: 'assets/main.css' }),
    'fonts/a.woff2': record(font, 'font/woff2', { archive: 'assets/fonts/a.woff2' }),
    'icons.svg': record(sprite, 'image/svg+xml', { archive: 'assets/icons.svg' }),
    'licenses/source.txt': record(license, 'text/plain', { url: origin + '/license' }),
  };
  const output = Buffer.from(rewriteCss(css.toString(), 'main.css', files));
  files['main.css'].output_sha256 = sha256(output);
  files['main.css'].output_size = output.length;
  const lock = {
    schema: 1,
    version: '1.2.3',
    archive: { url: origin + '/archive', size: blob.length, sha256: sha256(blob) },
    files,
    styles: { light: ['main.css'], dark: ['main.css'] },
    sprite: 'icons.svg',
  };
  const lockPath = join(dir, 'lock.json');
  const save = async () => writeFile(lockPath, JSON.stringify(lock, null, 2) + '\n');
  await save();
  t.after(async () => {
    hold?.destroy();
    server.closeAllConnections();
    await new Promise((done) => server.close(done));
    await rm(dir, { recursive: true, force: true });
  });
  return {
    dir,
    data,
    lock,
    lockPath,
    save,
    blob,
    get requests() {
      return requests;
    },
    get hold() {
      return hold;
    },
    server,
  };
}

test('取得・CSS相対化・原文保存と、通信しない再検査', async (t) => {
  const f = await fixture(t);
  const target = await install({ dataDir: f.data, lockPath: f.lockPath });
  const manifest = JSON.parse(await readFile(join(target, 'install.json')));
  assert.equal(manifest.id, parseLock(await readFile(f.lockPath)).id);
  assert.match(await readFile(join(target, 'main.css'), 'utf8'), /url\("\.\/fonts\/a.woff2"\)/);
  assert.deepEqual(await readFile(join(target, 'fonts/a.woff2')), Buffer.from([0, 255, 9, 10, 13, 128]));
  assert.equal(await readFile(join(target, 'licenses/source.txt'), 'utf8'), '試験用の原文\n');
  assert.equal((await readdir(target)).includes('.archive.tar.gz'), false);
  const before = f.requests;
  assert.equal(await install({ dataDir: f.data, lockPath: f.lockPath }), target);
  assert.equal(f.requests, before);
});

test('公式レジストリのindex・manifest・レイヤーを順に照合する', async (t) => {
  const f = await fixture(t);
  const manifest = Buffer.from(
    JSON.stringify({
      schemaVersion: 2,
      layers: [
        {
          mediaType: 'application/vnd.oci.image.layer.v1.tar+gzip',
          digest: `sha256:${sha256(f.blob)}`,
          size: f.blob.length,
        },
      ],
    }),
  );
  const index = Buffer.from(
    JSON.stringify({
      manifests: [{ digest: `sha256:${sha256(manifest)}`, platform: { os: 'linux', architecture: 'amd64' } }],
    }),
  );
  const originalFetch = globalThis.fetch;
  const visited = [];
  globalThis.fetch = async (url, options) => {
    if (String(url).startsWith('https://gitlab.com/jwt/auth?'))
      return new Response(JSON.stringify({ token: 'public-fixture' }));
    if (!String(url).startsWith('https://registry.gitlab.com/')) return originalFetch(url, options);
    visited.push(String(url));
    if (String(url).includes(`/manifests/sha256:${sha256(index)}`)) return new Response(index);
    if (String(url).includes(`/manifests/sha256:${sha256(manifest)}`)) return new Response(manifest);
    if (String(url).includes('/blobs/')) return new Response(f.blob);
    return new Response(index);
  };
  t.after(() => {
    globalThis.fetch = originalFetch;
  });
  f.lock.archive = {
    registry: 'https://registry.gitlab.com',
    repository: 'gitlab-org/gitlab-foss/gitlab-assets-ce',
    index_sha256: sha256(index),
    manifest_sha256: sha256(manifest),
    sha256: sha256(f.blob),
    size: f.blob.length,
  };
  await f.save();
  const target = await install({ dataDir: f.data, lockPath: f.lockPath });
  assert.equal(visited.length, 3);
  assert.ok(visited[2].includes('/blobs/'));
  f.lock.version = '1.2.4';
  f.lock.archive.index_sha256 = '0'.repeat(64);
  await f.save();
  await assert.rejects(install({ dataDir: f.data, lockPath: f.lockPath }), /indexのSHA256/);
  assert.ok(await readFile(join(target, 'install.json')));
});

test('破損した同じ版を再導入し、以前の内容は退避する', async (t) => {
  const f = await fixture(t);
  const target = await install({ dataDir: f.data, lockPath: f.lockPath });
  await writeFile(join(target, 'fonts/a.woff2'), '破損');
  await install({ dataDir: f.data, lockPath: f.lockPath });
  assert.equal((await readFile(join(target, 'fonts/a.woff2'))).length, 6);
  assert.ok((await readdir(f.data)).some((name) => name.startsWith('.previous-')));
});

test('版更新は旧版を残す', async (t) => {
  const f = await fixture(t);
  const old = await install({ dataDir: f.data, lockPath: f.lockPath });
  f.lock.version = '1.2.4';
  await f.save();
  const current = await install({ dataDir: f.data, lockPath: f.lockPath });
  assert.notEqual(old, current);
  assert.equal(JSON.parse(await readFile(join(old, 'install.json'))).version, '1.2.3');
});

test('アーカイブ不一致で完了印を作らず旧版とデータを保つ', async (t) => {
  const f = await fixture(t);
  const old = await install({ dataDir: f.data, lockPath: f.lockPath });
  f.lock.version = '1.2.4';
  f.lock.archive.sha256 = '0'.repeat(64);
  await f.save();
  await assert.rejects(install({ dataDir: f.data, lockPath: f.lockPath }), /SHA256/);
  assert.equal(JSON.parse(await readFile(join(old, 'install.json'))).version, '1.2.3');
  assert.deepEqual(await readdir(f.data), [old.split(/[\\/]/).at(-1)]);
});

test('選択したsymlinkと重複メンバーを拒む', async (t) => {
  for (const type of ['2', 'duplicate'])
    await t.test(type, async (t) => {
      const css = '/* url(/assets/ignored) */ .x{src:url(/assets/fonts/a.woff2);mask:url("./icons.svg#one")}';
      const entries = [
        ['assets/main.css', css, type === '2' ? '2' : '0'],
        ['assets/fonts/a.woff2', Buffer.from([0, 255, 9, 10, 13, 128])],
        ['assets/icons.svg', '<svg/>'],
      ];
      if (type === 'duplicate') entries.push(entries[0]);
      const f = await fixture(t, { archive: archive(entries) });
      await assert.rejects(install({ dataDir: f.data, lockPath: f.lockPath }), /通常ファイル|重複/);
      assert.deepEqual(await readdir(f.data), []);
    });
});

test('中断と同時導入の保護', async (t) => {
  const f = await fixture(t, { hold: true });
  const controller = new AbortController();
  const first = install({ dataDir: f.data, lockPath: f.lockPath, signal: controller.signal });
  const firstRejected = assert.rejects(first, /中断|abort/i);
  for (let i = 0; !f.hold && i < 100; i++) await new Promise((done) => setTimeout(done, 10));
  assert.ok(f.hold);
  await assert.rejects(install({ dataDir: f.data, lockPath: f.lockPath }), /実行中/);
  controller.abort(new Error('中断'));
  await firstRejected;
  assert.deepEqual(await readdir(f.data), []);
});

test('原文の不一致・パス逸脱・不明な原文対応を拒む', async (t) => {
  const f = await fixture(t);
  f.lock.files['licenses/source.txt'].sha256 = '0'.repeat(64);
  await f.save();
  await assert.rejects(install({ dataDir: f.data, lockPath: f.lockPath }), /原文/);
  f.lock.files['../escape.css'] = f.lock.files['main.css'];
  assert.throws(() => parseLock(Buffer.from(JSON.stringify(f.lock))), /相対パス/);
  delete f.lock.files['../escape.css'];
  f.lock.files['main.css'].licenses = ['unknown.txt'];
  assert.throws(() => parseLock(Buffer.from(JSON.stringify(f.lock))), /原文が固定/);
});

test('tarの欠落・gzipの破損を完了扱いにしない', async (t) => {
  const css = '/* url(/assets/ignored) */ .x{src:url(/assets/fonts/a.woff2);mask:url("./icons.svg#one")}';
  const entries = [
    ['assets/main.css', css],
    ['assets/fonts/a.woff2', Buffer.from([0, 255, 9, 10, 13, 128])],
    ['assets/icons.svg', '<svg/>'],
  ];
  for (const [name, bytes] of [
    ['欠落', archive(entries.slice(0, 1))],
    ['破損', archive(entries).subarray(0, -4)],
  ])
    await t.test(name, async (t) => {
      const f = await fixture(t, { archive: bytes });
      await assert.rejects(install({ dataDir: f.data, lockPath: f.lockPath }));
      assert.deepEqual(await readdir(f.data), []);
    });
});

test('PAXのpath属性を読み、選択外のパスは一切展開しない', async (t) => {
  const css = '/* url(/assets/ignored) */ .x{src:url(/assets/fonts/a.woff2);mask:url("./icons.svg#one")}';
  const value = 'path=assets/main.css\n';
  let length = Buffer.byteLength(value) + 2;
  while (Buffer.byteLength(`${length} ${value}`) !== length) length = Buffer.byteLength(`${length} ${value}`);
  const f = await fixture(t, {
    archive: archive([
      ['PaxHeader', `${length} ${value}`, 'x'],
      ['placeholder', css],
      ['../../outside.txt', '展開しない'],
      ['assets/fonts/a.woff2', Buffer.from([0, 255, 9, 10, 13, 128])],
      ['assets/icons.svg', '<svg/>'],
    ]),
  });
  const target = await install({ dataDir: f.data, lockPath: f.lockPath });
  assert.match(await readFile(join(target, 'main.css'), 'utf8'), /fonts\/a.woff2/);
  await assert.rejects(readFile(join(f.dir, 'outside.txt')), { code: 'ENOENT' });
});

test('終了済みPIDのロックと同じIDの中断領域を回収する', async (t) => {
  const f = await fixture(t);
  const info = parseLock(await readFile(f.lockPath));
  const root = f.data;
  const lock = join(root, `.lock-${info.id}`);
  const stage = join(root, `.stage-${info.id}-00000000-0000-0000-0000-000000000000`);
  await mkdir(lock, { recursive: true });
  await writeFile(join(lock, 'owner.json'), JSON.stringify({ pid: 1073741823 }));
  await mkdir(stage);
  await writeFile(join(stage, '.archive.tar.gz'), '中断した取得');
  await install({ dataDir: f.data, lockPath: f.lockPath });
  assert.deepEqual(await readdir(root), [info.id]);
});

test('CSSはコメント・文字列・dataとescape済みセレクターを保ち、未固定参照を拒む', () => {
  const css = `.gl-\\[\\'x\\'\\] { content:'url(/assets/no)'; /* url(x) */ mask:url('data:image/svg+xml,<svg/>'); src:url(../../fonts/a.woff2#id) }`;
  const files = { 'nested/themes/a.css': { archive: 'assets/nested/themes/a.css' }, 'fonts/a.woff2': {} };
  const output = rewriteCss(css, 'nested/themes/a.css', files);
  assert.match(output, /content:'url\(\/assets\/no\)'/);
  assert.match(output, /\/\* url\(x\) \*\//);
  assert.match(output, /url\("\.\.\/\.\.\/fonts\/a.woff2#id"\)/);
  assert.throws(() => rewriteCss('a{src:url(/assets/missing)}', 'a.css', {}), /ロックにありません/);
  assert.throws(() => rewriteCss('a{src:url(https://example.com/a)}', 'a.css', {}), /外部資産/);
});
