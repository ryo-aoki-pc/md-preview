import assert from 'node:assert/strict';
import { mkdirSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import test from 'node:test';
import { createServer, CSP } from '../lib/server.mjs';
import { loadPage } from '../lib/page.mjs';
import { events, get, temp, until } from './helpers.mjs';

async function fixture(t, options = {}) {
  const root = temp(t);
  writeFileSync(join(root, 'image.png'), Buffer.from([0, 1, 2, 255]));
  writeFileSync(join(root, 'file.pdf'), 'pdf');
  writeFileSync(join(root, 'page.html'), '<script>alert(1)</script>');
  writeFileSync(join(root, 'project.txt'), 'project fixture');
  mkdirSync(join(root, 'directory'));
  mkdirSync(join(root, '.git'));
  writeFileSync(join(root, '.git', 'config'), 'hidden');
  const render = { file: '試験.md', dir: '', rev: 1, html: '<p>本文</p>', headings: [] };
  const scroll = { line: 3, ratio: 0.5, lines: 10 };
  const server = await createServer({
    session: { root: () => root, last: () => ({ render, scroll }) },
    page: loadPage(),
    ...options,
  });
  t.after(() => server.close());
  return { server, render, scroll };
}

test('HTTP は token・Host・取得元・メソッドを検査する', async (t) => {
  const { server } = await fixture(t);
  assert.match(server.token, /^[0-9a-f]{32}$/);
  for (const path of ['/', '/wrong/', '/' + server.token + 'x/'])
    assert.equal((await get(server, '', { path })).status, 404);
  const redirect = await get(server, '', { path: '/' + server.token });
  assert.equal(redirect.status, 301);
  assert.equal(redirect.headers.location, '/' + server.token + '/');
  // クエリは保ったまま送る
  const listRedirect = await get(server, '', { path: '/' + server.token + '?list' });
  assert.equal(listRedirect.status, 301);
  assert.equal(listRedirect.headers.location, '/' + server.token + '/?list');
  for (const host of ['example.invalid', 'localhost', `evil.invalid:${server.port}`, `127.0.0.1:${server.port + 1}`]) {
    assert.equal((await get(server, '', { headers: { Host: host } })).status, 403);
  }
  for (const site of ['same-site', 'cross-site', 'unknown'])
    assert.equal((await get(server, '', { headers: { 'Sec-Fetch-Site': site } })).status, 403);
  for (const site of ['none', 'same-origin'])
    assert.equal((await get(server, '', { headers: { 'Sec-Fetch-Site': site } })).status, 200);
  assert.equal((await get(server, '', { headers: { Host: `localhost:${server.port}` } })).status, 200);
  const post = await get(server, '', { method: 'POST' });
  assert.equal(post.status, 405);
  assert.equal(post.headers.allow, 'GET, HEAD');
  for (const response of [post, await get(server), await get(server, 'missing')]) {
    assert.equal(response.headers['x-content-type-options'], 'nosniff');
    assert.equal(response.headers['referrer-policy'], 'no-referrer');
    assert.equal(response.headers['access-control-allow-origin'], undefined);
  }
});

test('ページと組み込み資産に正しい CSP・MIME を付ける', async (t) => {
  const { server } = await fixture(t);
  const page = await get(server);
  assert.equal(page.headers['content-security-policy'], CSP);
  assert.equal(page.headers['cache-control'], 'no-store');
  assert.match(page.body.toString(), /"assets":null/);
  assert.doesNotMatch(page.body.toString(), /{{CONFIG}}|{{LIBS}}/);
  for (const [path, type] of [
    ['_/preview.js', 'text/javascript'],
    ['_/preview.css', 'text/css'],
    ['_/fallback.css', 'text/css'],
  ]) {
    const result = await get(server, path);
    assert.equal(result.status, 200);
    assert.ok(result.headers['content-type'].startsWith(type));
  }
  assert.equal((await get(server, '', { method: 'HEAD' })).body.length, 0);
});

test('依存は許可したファイルだけ配信し、ETag と HEAD を扱う', async (t) => {
  const { server } = await fixture(t);
  for (const [path, type] of [
    ['katex/katex.min.js', 'text/javascript'],
    ['katex/katex.min.css', 'text/css'],
    ['katex/fonts/KaTeX_Main-Regular.woff2', 'font/woff2'],
    ['mermaid/mermaid.min.js', 'text/javascript'],
  ]) {
    const result = await get(server, '_/lib/' + path);
    assert.equal(result.status, 200, path);
    assert.ok(result.body.length > 0, path);
    assert.ok(result.headers['content-type'].startsWith(type), path);
    assert.equal(result.headers['cache-control'], 'no-cache');
    const cached = await get(server, '_/lib/' + path, { headers: { 'If-None-Match': result.headers.etag } });
    assert.equal(cached.status, 304);
    const head = await get(server, '_/lib/' + path, { method: 'HEAD' });
    assert.equal(head.body.length, 0);
    assert.equal(Number(head.headers['content-length']), result.body.length);
  }
  for (const path of [
    'katex/../package.json',
    'katex/%2e%2e/package.json',
    'mermaid/package.json',
    'katex/package.json',
    'katex/fonts/../katex.min.js',
    'katex/fonts/a.css',
    'other.js',
  ]) {
    assert.equal((await get(server, '_/lib/' + path)).status, 404, path);
  }
});

test('ルートのファイルは sandbox・MIME・ETag を付け、脱出と一覧を拒否する', async (t) => {
  const { server } = await fixture(t);
  const image = await get(server, 'image.png');
  assert.equal(image.status, 200);
  assert.equal(image.headers['content-type'], 'image/png');
  assert.equal(image.headers['content-security-policy'], 'sandbox');
  assert.equal(image.headers['cache-control'], 'no-cache');
  assert.deepEqual([...image.body], [0, 1, 2, 255]);
  const cached = await get(server, 'image.png', { headers: { 'If-None-Match': image.headers.etag } });
  assert.equal(cached.status, 304);
  assert.equal(cached.body.length, 0);
  assert.equal(cached.headers['content-security-policy'], 'sandbox');
  const head = await get(server, 'image.png', { method: 'HEAD' });
  assert.equal(head.status, 200);
  assert.equal(head.body.length, 0);
  assert.equal(head.headers['content-length'], '4');
  assert.equal((await get(server, 'file.pdf')).headers['content-security-policy'], undefined);
  assert.equal((await get(server, 'page.html')).headers['content-security-policy'], 'sandbox');
  assert.equal((await get(server, 'project.txt')).body.toString(), 'project fixture');
  assert.equal((await get(server, 'directory')).status, 404);
  for (const path of ['../project.txt', '%2e%2e/project.txt', '.git/config', 'directory/.GIT/config'])
    assert.equal((await get(server, path)).status, 403, path);
  for (const path of ['a%00b', 'a%5cb', 'a%0Ab']) assert.equal((await get(server, path)).status, 400, path);
});

test('SSE は最後の内容・新しいイベント・終了を JSON で届ける', async (t) => {
  const { server, render, scroll } = await fixture(t);
  const stream = await events(server);
  t.after(() => stream.close());
  await until(() => stream.received.length === 2);
  assert.deepEqual(stream.received, [
    { name: 'render', data: render },
    { name: 'scroll', data: scroll },
  ]);
  assert.equal(server.clientCount(), 1);
  const next = { ...render, html: '<p>改行\n引用"</p>', rev: 2 };
  server.broadcast('render', next);
  server.broadcast('reload', {});
  await until(() => stream.received.some((entry) => entry.name === 'reload'));
  assert.deepEqual(stream.received[2], { name: 'render', data: next });
  const reopened = await events(server);
  t.after(() => reopened.close());
  await until(() => reopened.received.some((entry) => entry.name === 'render'));
  assert.deepEqual(reopened.received[0].data, next);
  await server.close('exit');
  await until(() => stream.received.some((entry) => entry.name === 'stop'));
  assert.deepEqual(stream.received.at(-1), { name: 'stop', data: { reason: 'exit' } });
  await until(() => server.clientCount() === 0);
});

test('SSE は8 MBを超える書き込みをためる相手を切る', async (t) => {
  const { server } = await fixture(t);
  const stream = await events(server);
  t.after(() => stream.close());
  await until(() => server.clientCount() === 1);
  server.broadcast('render', { html: 'x'.repeat(8 * 1024 * 1024) });
  await until(() => server.clientCount() === 0);
});

test('HTTP の解析段階で拒否する応答にも共通のヘッダーを付ける', async (t) => {
  const { createConnection } = await import('node:net');
  const { server } = await fixture(t);
  for (const [raw, status] of [
    [
      'POST /' +
        server.token +
        '/ HTTP/1.1\r\nHost: 127.0.0.1:' +
        server.port +
        '\r\nExpect: 100-continue\r\nConnection: close\r\n\r\n',
      405,
    ],
    [
      'GET /' +
        server.token +
        '/ HTTP/1.1\r\nHost: 127.0.0.1:' +
        server.port +
        '\r\nExpect: something\r\nConnection: close\r\n\r\n',
      200,
    ],
    ['CONNECT 127.0.0.1:80 HTTP/1.1\r\nHost: 127.0.0.1:' + server.port + '\r\nConnection: close\r\n\r\n', 405],
    ['GET / HTTP/1.1\r\nConnection: close\r\n\r\n', 400],
    ['GET / HTTP/1.1\r\nHost: 127.0.0.1:' + server.port + '\r\nBad Header: value\r\n\r\n', 400],
    ['GET / HTTP/1.1\r\nHost: 127.0.0.1:' + server.port + '\r\nX-Large: ' + 'x'.repeat(17000) + '\r\n\r\n', 431],
  ]) {
    const response = await new Promise((resolve, reject) => {
      const socket = createConnection({ host: '127.0.0.1', port: server.port });
      let text = '';
      socket.setEncoding('utf8');
      socket.setTimeout(3000, () => {
        socket.destroy();
        reject(new Error('HTTP の拒否が時間切れ'));
      });
      socket.on('connect', () => socket.write(raw));
      socket.on('data', (value) => {
        text += value;
      });
      socket.on('end', () => resolve(text));
      socket.on('error', reject);
    });
    assert.match(response, new RegExp('^HTTP/1.1 ' + status));
    assert.match(response, /X-Content-Type-Options: nosniff/i);
    assert.match(response, /Referrer-Policy: no-referrer/i);
  }
});

test('ジャンプは有効な POST だけを受け、Origin・型・大きさ・JSON・行番号を検査する', async (t) => {
  const jumped = [];
  const { server } = await fixture(t, { onJump: (value) => jumped.push(value) });
  const origin = new URL(server.url).origin;
  const headers = { Origin: origin, 'Content-Type': 'application/json' };
  const post = (body = '{"line":7}', changes = {}, rel = '_/jump') =>
    get(server, rel, { method: 'POST', headers: { ...headers, ...changes }, body });
  for (const allowedOrigin of [origin, 'http://localhost:' + server.port]) {
    assert.equal((await post('{"line":7}', { Origin: allowedOrigin })).status, 204);
  }
  assert.deepEqual(jumped, [{ line: 7 }, { line: 7 }]);
  const missingOrigin = await get(server, '_/jump', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: '{"line":7}',
  });
  assert.equal(missingOrigin.status, 403);
  for (const origin of [
    'null',
    'https://127.0.0.1:' + server.port,
    'http://example.invalid',
    'http://127.0.0.1:' + (server.port + 1),
  ]) {
    assert.equal((await post(undefined, { Origin: origin })).status, 403);
  }
  for (const type of ['text/plain', 'application/javascript', 'application/jsonx'])
    assert.equal((await post(undefined, { 'Content-Type': type })).status, 415);
  assert.equal(
    (await get(server, '_/jump', { method: 'POST', headers: { Origin: origin }, body: '{"line":7}' })).status,
    415,
  );
  assert.equal((await post(' '.repeat(1025))).status, 413);
  assert.equal((await post(' '.repeat(1014) + '{"line":7}')).status, 204, '本文がちょうど1 KBなら受け付ける');
  assert.equal((await post(' '.repeat(1015) + '{"line":7}')).status, 413);
  for (const body of [
    'broken',
    '{}',
    'null',
    '[]',
    '{"line":0}',
    '{"line":-1}',
    '{"line":1.5}',
    '{"line":"7"}',
    '{"line":1e100}',
  ]) {
    assert.equal((await post(body)).status, 400, body);
  }
  assert.equal((await post(Buffer.from([0xff]))).status, 400);
  assert.equal((await post(undefined, { 'Sec-Fetch-Site': 'cross-site' })).status, 403);
  assert.equal((await post(undefined, { Host: 'example.invalid' })).status, 403);
  assert.equal((await post('{"line":7}', {}, 'other')).status, 405);
  const put = await get(server, '_/jump', { method: 'PUT' });
  assert.equal(put.status, 405);
  assert.equal(put.headers.allow, 'GET, HEAD, POST');
  const other = await get(server, 'other', { method: 'PUT' });
  assert.equal(other.headers.allow, 'GET, HEAD');
  const response = await post();
  assert.equal(response.headers['x-content-type-options'], 'nosniff');
  assert.equal(response.headers['referrer-policy'], 'no-referrer');
  const plain = await fixture(t);
  const disabled = await get(plain.server, '_/jump', { method: 'POST', headers, body: '{"line":7}' });
  assert.equal(disabled.status, 405);
  assert.equal(disabled.headers.allow, 'GET, HEAD');
});

test('ジャンプの chunked 本文も、蓄積を1 KBで止める', async (t) => {
  const { request } = await import('node:http');
  const jumped = [];
  const { server } = await fixture(t, { onJump: (value) => jumped.push(value) });
  const response = await new Promise((resolve, reject) => {
    const req = request(
      server.url + '_/jump',
      { method: 'POST', headers: { Origin: new URL(server.url).origin, 'Content-Type': 'application/json' } },
      (res) => {
        res.resume();
        res.on('end', () => resolve(res.statusCode));
      },
    );
    req.on('error', reject);
    req.write(' '.repeat(600));
    req.write(' '.repeat(600));
    req.end('{"line":1}');
  });
  assert.equal(response, 413);
  assert.deepEqual(jumped, []);
});

test('SSE の接続数は増減のたびに通知し、遅い接続の切断も重複せず通知する', async (t) => {
  const counts = [];
  const { server } = await fixture(t, { onClientCount: (count) => counts.push(count) });
  const first = await events(server);
  const second = await events(server);
  t.after(() => {
    first.close();
    second.close();
  });
  await until(() => counts.length === 2);
  assert.deepEqual(counts, [1, 2]);
  first.close();
  await until(() => counts.length === 3);
  server.broadcast('render', { html: 'x'.repeat(8 * 1024 * 1024) });
  await until(() => server.clientCount() === 0);
  await until(() => counts.length === 4);
  assert.deepEqual(counts, [1, 2, 1, 0]);
});

async function navigationFixture(t, prepare, { releaseDelayMs = 5000, active = 'main.md' } = {}) {
  const { createSession } = await import('../lib/session.mjs');
  const root = temp(t);
  await prepare(root);
  let server;
  const session = createSession({
    root,
    releaseDelayMs,
    onRender: (data) => server?.broadcast('render', data),
    onScroll: (data) => server?.broadcast('scroll', data),
  });
  server = await createServer({ session, page: loadPage() });
  if (active) {
    writeFileSync(join(root, active), '# 保存済みの本文');
    session.setDocument({ path: join(root, active), text: '# 編集中の本文', rev: 100 });
  }
  t.after(async () => {
    await server.close();
    await session.close();
  });
  return { server, session, root };
}

test('文書を指定した SSE は、その文書だけを描き、スクロールを流さない', async (t) => {
  const { server, session, root } = await navigationFixture(
    t,
    (root) => writeFileSync(join(root, 'other.MD'), '# 別の文書'),
    { releaseDelayMs: 40 },
  );
  const active = await events(server);
  const linked = await events(server, { doc: 'other.MD' });
  const same = await events(server, { doc: 'main.md' });
  t.after(() => {
    active.close();
    linked.close();
    same.close();
  });
  await until(
    () =>
      active.received.some((value) => value.name === 'render') &&
      linked.received.some((value) => value.name === 'render') &&
      same.received.some((value) => value.name === 'render'),
  );
  assert.match(same.received[0].data.html, /編集中の本文/);
  assert.match(linked.received[0].data.html, /別の文書/);
  session.setScroll({ line: 7, ratio: 0.5, lines: 20 });
  await until(() => active.received.some((value) => value.name === 'scroll'));
  assert.equal(
    linked.received.some((value) => value.name === 'scroll'),
    false,
  );
  assert.equal(
    same.received.some((value) => value.name === 'scroll'),
    false,
  );
  writeFileSync(join(root, 'other.MD'), '# 保存し直した別の文書');
  await until(() => linked.received.some((value) => value.data.html?.includes('保存し直した別の文書')));
  assert.equal(
    active.received.some((value) => value.data.html?.includes('別の文書')),
    false,
  );
  assert.equal(
    same.received.some((value) => value.data.html?.includes('別の文書')),
    false,
  );
  linked.close();
  await until(() => session.documentCount() === 0);
});

test('拡張子・脱出・.git・無い文書は、同じ「開けない」の表示になる', async (t) => {
  const { server } = await navigationFixture(t, (root) => {
    writeFileSync(join(root, 'image.png'), 'image');
    mkdirSync(join(root, '.git'));
    writeFileSync(join(root, '.git', 'secret.md'), 'hidden');
  });
  for (const doc of ['image.png', '../other.md', '%2e%2e/other.md', '.git/secret.md', 'missing.md', '']) {
    const stream = await events(server, { doc });
    await until(() => stream.received.length > 0);
    assert.equal(stream.received[0].data.error, 'この文書は開けない', doc);
    assert.equal(Object.hasOwn(stream.received[0].data, 'status'), false);
    stream.close();
  }
});

test('文書のリンクがルートの外へ出ても、ファイルを読まず同じ誤りを返す', async (t) => {
  const { symlinkSync } = await import('node:fs');
  const base = temp(t);
  const root = join(base, 'root');
  mkdirSync(root);
  writeFileSync(join(base, 'secret.md'), '# 外の文書');
  try {
    symlinkSync(base, join(root, 'escape'), process.platform === 'win32' ? 'junction' : 'dir');
  } catch (error) {
    if (['EPERM', 'EACCES', 'ENOTSUP'].includes(error.code)) return t.skip('リンクを作成できない');
    throw error;
  }
  const { createSession } = await import('../lib/session.mjs');
  const session = createSession({ root });
  const server = await createServer({ session, page: loadPage() });
  t.after(async () => {
    await server.close();
    await session.close();
  });
  const stream = await events(server, { doc: 'escape/secret.md' });
  t.after(() => stream.close());
  await until(() => stream.received.length > 0);
  assert.equal(stream.received[0].data.error, 'この文書は開けない');
});

test('一覧は Markdown の拡張子だけを並べ、隠しファイル・依存・フォルダーのリンクを除く', async (t) => {
  const { server, root } = await navigationFixture(
    t,
    (root) => {
      for (const dir of ['docs', '.hidden', 'node_modules']) mkdirSync(join(root, dir));
      for (const [name, text] of Object.entries({
        'z.MD': '',
        'a.markdown': '',
        'b.mdown': '',
        'c.mkd': '',
        'd.MKDN': '',
        'docs/日本語.md': '',
        'docs/z.txt': '',
        '.hidden/a.md': '',
        '.file.md': '',
        'node_modules/a.md': '',
      }))
        writeFileSync(join(root, name), text);
    },
    { active: null },
  );
  const { symlinkSync } = await import('node:fs');
  try {
    symlinkSync(join(root, 'docs'), join(root, 'shortcut'), process.platform === 'win32' ? 'junction' : 'dir');
    symlinkSync(root, join(root, 'docs', 'loop'), process.platform === 'win32' ? 'junction' : 'dir');
  } catch (error) {
    if (!['EPERM', 'EACCES', 'ENOTSUP'].includes(error.code)) throw error;
    t.diagnostic('フォルダーのリンクは作成できないため、この環境では除外例を置けなかった');
  }
  const response = await get(server, '_/list');
  assert.equal(response.status, 200);
  assert.equal(response.headers['cache-control'], 'no-store');
  assert.match(response.headers['content-type'], /^application\/json/);
  assert.equal(response.headers['x-content-type-options'], 'nosniff');
  assert.equal(response.headers['referrer-policy'], 'no-referrer');
  assert.deepEqual(JSON.parse(response.body.toString()), {
    files: ['a.markdown', 'b.mdown', 'c.mkd', 'd.MKDN', 'docs/日本語.md', 'z.MD'],
    truncated: false,
  });
  const head = await get(server, '_/list', { method: 'HEAD' });
  assert.equal(head.body.length, 0);
});

test('一覧は深さ20までを返し、深い部分を省いたことを知らせる', async (t) => {
  const { server } = await navigationFixture(
    t,
    (root) => {
      let dir = root;
      for (let i = 1; i <= 21; i++) {
        dir = join(dir, 'd');
        mkdirSync(dir);
        writeFileSync(join(dir, 'level.md'), '# ' + i);
      }
    },
    { active: null },
  );
  const value = JSON.parse((await get(server, '_/list')).body.toString());
  assert.equal(value.files.length, 20);
  assert.equal(
    value.files.some((name) => name.split('/').length > 21),
    false,
  );
  assert.equal(value.truncated, true);
});

test('一覧は5000件までで打ち切り、ルートがないときは空にする', { timeout: 30000 }, async (t) => {
  const { server } = await navigationFixture(
    t,
    (root) => {
      for (let i = 0; i < 5001; i++) writeFileSync(join(root, String(i).padStart(4, '0') + '.md'), '');
    },
    { active: null },
  );
  const value = JSON.parse((await get(server, '_/list')).body.toString());
  assert.equal(value.files.length, 5000);
  assert.equal(value.truncated, true);
  assert.deepEqual([...value.files].sort(), value.files);
  const { createSession } = await import('../lib/session.mjs');
  const session = createSession();
  const empty = await createServer({ session, page: loadPage() });
  t.after(async () => {
    await empty.close();
    await session.close();
  });
  assert.deepEqual(JSON.parse((await get(empty, '_/list')).body.toString()), { files: [], truncated: false });
});

test('ファイル文書のジャンプは、今も編集中の文書である場合だけ有効にする', async (t) => {
  const { createSession } = await import('../lib/session.mjs');
  const root = temp(t);
  for (const name of ['a.md', 'b.md']) writeFileSync(join(root, name), '# 保存');
  const jumps = [];
  let server;
  const session = createSession({ root, onRender: (data) => server?.broadcast('render', data) });
  server = await createServer({ session, page: loadPage({ jump: true }), onJump: (data) => jumps.push(data) });
  t.after(async () => {
    await server.close();
    await session.close();
  });
  session.setDocument({ path: join(root, 'a.md'), text: '# 編集A', rev: 1 });
  const stream = await events(server, { doc: 'a.md' });
  t.after(() => stream.close());
  await until(() => stream.received.some((value) => value.name === 'render'));
  assert.equal(stream.received[0].data.currentDocument, true);
  const headers = { Origin: new URL(server.url).origin, 'Content-Type': 'application/json' };
  assert.equal((await get(server, '_/jump?doc=a.md', { method: 'POST', headers, body: '{"line":2}' })).status, 204);
  session.setDocument({ path: join(root, 'b.md'), text: '# 編集B', rev: 2 });
  assert.equal((await get(server, '_/jump?doc=a.md', { method: 'POST', headers, body: '{"line":3}' })).status, 403);
  await until(() => stream.received.some((value) => value.data.currentDocument === false));
  assert.deepEqual(jumps, [{ line: 2 }]);
});

test('include と知らせを SSE へ送り、取り込み先の更新で同じ本文を描き直す', async (t) => {
  const { createSession } = await import('../lib/session.mjs');
  const root = temp(t);
  writeFileSync(join(root, 'part.md'), '最初の取り込み');
  let server;
  const session = createSession({ root, onRender: (data) => server?.broadcast('render', data) });
  server = await createServer({ session, page: loadPage() });
  t.after(async () => {
    await server.close();
    await session.close();
  });
  const stream = await events(server);
  t.after(() => stream.close());
  session.setDocument({
    path: join(root, 'main.md'),
    text: '::include{file=part.md}\n\n~~~plantuml\n図の原文\n~~~',
    rev: 7,
  });
  await until(() => stream.received.some((value) => value.name === 'render'));
  const first = stream.received.find((value) => value.name === 'render').data;
  assert.equal(first.includes[0].ok, true);
  assert.equal(
    first.notices.some((value) => value.kind === 'diagram'),
    true,
  );
  writeFileSync(join(root, 'part.md'), '取り込みだけを変更');
  await until(() => stream.received.some((value) => value.data.html?.includes('取り込みだけを変更')));
  assert.equal(stream.received.at(-1).data.rev, 7);
});
