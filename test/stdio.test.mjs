import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { EventEmitter } from 'node:events';
import { readFileSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { PassThrough, Readable, Writable } from 'node:stream';
import { fileURLToPath } from 'node:url';
import test from 'node:test';
import { main } from '../cli.mjs';
import { MAX_LINE_BYTES, readLines } from '../lib/stdio.mjs';
import { events, get, temp, until } from './helpers.mjs';

const cli = fileURLToPath(new URL('../cli.mjs', import.meta.url));
const version = JSON.parse(readFileSync(new URL('../package.json', import.meta.url), 'utf8')).version;

function app(t) {
  const child = spawn(process.execPath, [cli, '--stdio'], { cwd: fileURLToPath(new URL('../', import.meta.url)), windowsHide: true });
  const messages = [];
  const invalid = [];
  let pending = '';
  let stderr = '';
  child.stdout.setEncoding('utf8').on('data', (text) => {
    pending += text;
    let end;
    while ((end = pending.indexOf('\n')) !== -1) {
      const line = pending.slice(0, end);
      pending = pending.slice(end + 1);
      try { messages.push(JSON.parse(line)); } catch { invalid.push(line); }
    }
  });
  child.stderr.setEncoding('utf8').on('data', (text) => { stderr += text; });
  const done = new Promise((resolve, reject) => {
    child.once('error', reject);
    child.once('close', (code) => resolve(code));
  });
  t.after(async () => {
    if (child.exitCode === null) {
      child.stdin.end();
      const timer = setTimeout(() => child.kill(), 3000);
      await done;
      clearTimeout(timer);
    }
    assert.deepEqual(invalid, [], '標準出力は JSON の行だけ');
    assert.equal(pending, '', '最後の JSON の行にも改行がある');
    assert.equal(stderr, '', '通常の取り決めでは標準エラーを出さない');
  });
  return {
    child, messages, done,
    raw(value) { return new Promise((resolve, reject) => child.stdin.write(value, (error) => error ? reject(error) : resolve())); },
    send(value) { return this.raw(JSON.stringify(value) + '\n'); },
    async wait(type, predicate = () => true, after = 0) {
      await until(() => messages.slice(after).some((value) => value.type === type && predicate(value)), type + ' が届く', 10000);
      return messages.slice(after).find((value) => value.type === type && predicate(value));
    },
  };
}

async function ready(t, appearance) {
  const client = app(t);
  await client.send({ type: 'hello', protocol: 1, ...(appearance ? { appearance } : {}) });
  const message = await client.wait('ready');
  assert.equal(message.protocol, 1);
  assert.equal(message.version, version);
  const url = new URL(message.url);
  assert.equal(url.hostname, '127.0.0.1');
  assert.match(url.pathname, /^\/[0-9a-f]{32}\/$/);
  return { ...client, server: { url: message.url, port: Number(url.port), token: url.pathname.split('/')[1] } };
}

test('標準入出力で開始・本文・スクロール・タブ数・休止・再開・ジャンプ・終了をつなぐ', { timeout: 20000 }, async (t) => {
  const root = temp(t);
  const file = join(root, '文書.md');
  writeFileSync(file, '');
  const client = await ready(t, { theme: 'dark', width: 'fixed' });
  const config = /<script id="gp-config"[^>]*>(.*?)<\/script>/s.exec((await get(client.server)).body.toString())[1];
  assert.deepEqual(JSON.parse(config).appearance, { theme: 'dark', width: 'fixed' });
  assert.equal(JSON.parse(config).jump, true);
  await client.wait('clients', (value) => value.count === 0);
  const stream = await events(client.server);
  t.after(() => stream.close());
  await client.wait('clients', (value) => value.count === 1);
  const second = await events(client.server);
  await client.wait('clients', (value) => value.count === 2);
  const beforeClose = client.messages.length;
  second.close();
  await client.wait('clients', (value) => value.count === 1, beforeClose);
  await client.send({ type: 'doc', path: file, name: '文書.md', text: '# 最初\n', rev: 1 });
  await until(() => stream.received.some((value) => value.name === 'render' && value.data.rev === 1));
  await client.send({ type: 'scroll', line: 2, ratio: 0.5, lines: 10 });
  await until(() => stream.received.some((value) => value.name === 'scroll'));
  assert.deepEqual(stream.received.find((value) => value.name === 'scroll').data, { line: 2, ratio: 0.5, lines: 10 });
  await client.send({ type: 'pause' });
  await until(() => stream.received.some((value) => value.name === 'stop' && value.data.reason === 'stop'));
  await client.send({ type: 'doc', path: null, name: '捨てる.md', text: '# 捨てる', rev: 2 });
  await client.send({ type: 'scroll', line: 99, ratio: 1, lines: 100 });
  const barrier = client.messages.length;
  await client.send({ type: '試験の区切り' });
  await client.wait('error', () => true, barrier);
  assert.equal(stream.received.some((value) => value.name === 'render' && value.data.rev === 2), false);
  const pausedTab = await events(client.server);
  t.after(() => pausedTab.close());
  await until(() => pausedTab.received.some((value) => value.name === 'stop'));
  assert.equal(pausedTab.received.at(-1).data.reason, 'stop');
  assert.equal(pausedTab.received.some((value) => value.name === 'scroll' && value.data.line === 99), false);
  assert.equal((await get(client.server)).status, 200, '休止中も同じ URL で待ち受ける');
  await client.send({ type: 'resume' });
  await client.send({ type: 'doc', path: file, name: '文書.md', text: '# 再開\n', rev: 3 });
  await until(() => stream.received.some((value) => value.name === 'render' && value.data.rev === 3));
  const jump = await get(client.server, '_/jump', { method: 'POST', headers: { Origin: new URL(client.server.url).origin, 'Content-Type': 'application/json' }, body: '{"line":7}' });
  assert.equal(jump.status, 204);
  assert.deepEqual(await client.wait('jump'), { type: 'jump', path: file, line: 7 });
  await client.send({ type: 'doc', path: null, name: '無名', text: '# 無名', rev: 4 });
  await until(() => stream.received.some((value) => value.name === 'render' && value.data.rev === 4));
  const beforeJump = client.messages.length;
  await get(client.server, '_/jump', { method: 'POST', headers: { Origin: new URL(client.server.url).origin, 'Content-Type': 'application/json' }, body: '{"line":8}' });
  assert.deepEqual(await client.wait('jump', () => true, beforeJump), { type: 'jump', path: null, line: 8 });
  client.child.stdin.end();
  assert.equal(await client.done, 0);
  await until(() => stream.received.some((value) => value.name === 'stop' && value.data.reason === 'exit'));
  assert.equal(client.messages.filter((value) => value.type === 'ready').length, 1);
});

test('読めない行・知らない種類・型の誤りを捨て、その後も描画する', { timeout: 15000 }, async (t) => {
  const client = await ready(t);
  const stream = await events(client.server);
  t.after(() => stream.close());
  const bad = ['{broken\n', 'null\n', '[]\n', '{"type":"unknown"}\n', '{"type":"hello","protocol":1}\n',
    '{"type":"doc","path":"relative.md","name":"文書","text":"本文","rev":1}\n',
    '{"type":"doc","path":null,"name":0,"text":"本文","rev":1}\n',
    '{"type":"doc","path":null,"name":"文書","text":0,"rev":1}\n',
    '{"type":"doc","path":null,"name":"文書","text":"本文","rev":"1"}\n',
    '{"type":"scroll","line":0,"ratio":0,"lines":1}\n',
    '{"type":"scroll","line":1,"ratio":1.1,"lines":1}\n',
    '{"type":"scroll","line":1,"ratio":0,"lines":0}\n'];
  for (const line of bad) await client.raw(line);
  await client.raw(Buffer.from([0xff, 10]));
  await until(() => client.messages.filter((value) => value.type === 'error').length === bad.length + 1);
  for (const value of client.messages.filter((value) => value.type === 'error')) {
    assert.equal(value.fatal, undefined);
    assert.match(value.message, /^[^\r\n]+$/);
    assert.match(value.message, /[ぁ-んァ-ヶ一-龠]/);
  }
  await client.send({ type: 'doc', path: null, name: '復帰', text: '# 復帰', rev: 1 });
  await until(() => stream.received.some((value) => value.data.html?.includes('復帰')));
});

test('hello より前の行と異なる版は fatal で終了コード2になる', async (t) => {
  for (const first of [{ type: 'doc' }, { type: 'hello', protocol: 2 }, { type: 'hello', protocol: '1' }]) {
    await t.test(JSON.stringify(first), async (t) => {
      const client = app(t);
      await client.send(first);
      assert.equal(await client.done, 2);
      assert.equal(client.messages.length, 1);
      assert.equal(client.messages[0].type, 'error');
      assert.equal(client.messages[0].fatal, true);
    });
  }
});

test('配色の不正な値は既定になり、改行なしの最後の行と空の入力も閉じられる', async (t) => {
  const client = await ready(t, { theme: 'bad', width: 100 });
  const page = (await get(client.server)).body.toString();
  assert.match(page, /"appearance":\{"theme":"system","width":"fluid"\}/);
  client.child.stdin.end('{"type":"pause"}');
  assert.equal(await client.done, 0);
  const empty = app(t);
  empty.child.stdin.end();
  assert.equal(await empty.done, 0);
  assert.deepEqual(empty.messages, []);
});

test('5 MB の本文を JSON の1行で受け取り、SSE へ描画結果を返す', { timeout: 30000 }, async (t) => {
  const client = await ready(t);
  const stream = await events(client.server);
  t.after(() => stream.close());
  const text = 'a'.repeat(5 * 1024 * 1024);
  await client.send({ type: 'doc', path: null, name: '大きい.md', text, rev: 5 });
  await until(() => stream.received.some((value) => value.name === 'render' && value.data.rev === 5), '5 MB の描画結果が届く', 22000);
  const result = stream.received.find((value) => value.name === 'render').data;
  assert.equal(result.error, undefined);
  assert.ok(result.html.includes(text));
});

test('JSON の行と UTF-8 の文字が途中で分かれても受け取る', async (t) => {
  const client = app(t);
  await client.raw('{"type":"hel');
  await client.raw('lo","protocol":1}\n');
  const { url } = await client.wait('ready');
  const server = { url };
  const stream = await events(server);
  t.after(() => stream.close());
  const line = Buffer.from(JSON.stringify({ type: 'doc', path: null, name: '日本語', text: '# 分割した本文', rev: 1 }) + '\n');
  const split = line.indexOf(Buffer.from('日')) + 1;
  await client.raw(line.subarray(0, split));
  await client.raw(line.subarray(split, split + 1));
  await client.raw(line.subarray(split + 1));
  await until(() => stream.received.some((value) => value.data.html?.includes('分割した本文')));
});

test('96 MBを超えた行は改行まで捨て、次の行から続ける', { timeout: 30000 }, async (t) => {
  const client = await ready(t);
  const block = Buffer.alloc(1024 * 1024, 120);
  for (let i = 0; i < MAX_LINE_BYTES / block.length + 1; i++) await client.raw(block);
  await client.raw('\n');
  await client.wait('error', (value) => /96 MB/.test(value.message));
  const before = client.messages.length;
  await client.send({ type: 'scroll', line: 3, ratio: 0.5, lines: 5 });
  const stream = await events(client.server);
  t.after(() => stream.close());
  await until(() => stream.received.some((value) => value.name === 'scroll' && value.data.line === 3));
  assert.equal(client.messages.slice(before).some((value) => value.type === 'error'), false);
  assert.equal(client.messages.filter((value) => value.type === 'error').length, 1);
});

test('行のバイト数の境界と、小さい断片からの組み立てを確かめる', async () => {
  const received = [];
  let oversized = 0;
  const input = Readable.from([...Buffer.from('日本語\n1234567890\nok\n')].map((byte) => Buffer.from([byte])));
  await readLines(input, (line) => { received.push(line.toString()); }, () => { oversized++; }, 9);
  assert.deepEqual(received, ['日本語', 'ok']);
  assert.equal(oversized, 1);
});

test('標準出力が詰まっても HTTP と入力の処理を続け、入力の終了で閉じる', { timeout: 10000 }, async (t) => {
  const stdin = new PassThrough();
  const stdout = new Writable({ highWaterMark: 1, write() {} });
  const signals = new EventEmitter();
  let started;
  const ready = new Promise((resolve) => { started = resolve; });
  const result = main(['--stdio'], { stdin, stdout, signals, onStart: started });
  t.after(() => { stdin.destroy(); stdout.destroy(); });
  stdin.write('{"type":"hello","protocol":1}\n');
  const instance = await ready;
  assert.equal((await get(instance.server)).status, 200);
  stdin.write(JSON.stringify({ type: 'doc', path: null, name: '本文', text: '# 本文', rev: 1 }) + '\n');
  await until(() => instance.session.last().render?.rev === 1);
  // 出力キューの上限を超えるエラーも、本文や終了を止めない。
  stdin.write('{"type":"unknown"}\n'.repeat(25000));
  stdin.end();
  assert.equal(await result, 0);
});

test('標準出力のパイプが閉じても落ちず、標準入力の終了まで動く', async (t) => {
  const client = await ready(t);
  client.child.stdout.destroy();
  const stream = await events(client.server);
  t.after(() => stream.close());
  await client.send({ type: 'doc', path: null, name: '本文', text: '# つながっている', rev: 1 });
  await until(() => stream.received.some((value) => value.data.rev === 1));
  client.child.stdin.end();
  assert.equal(await client.done, 0);
});

test('休止は描画中の処理も取り消し、再開後の本文を待たせない', { timeout: 10000 }, async (t) => {
  const client = await ready(t);
  const stream = await events(client.server);
  t.after(() => stream.close());
  await client.send({ type: 'doc', path: null, name: '重い', text: '>'.repeat(200000) + ' x\n', rev: 1 });
  await client.send({ type: 'pause' });
  await until(() => stream.received.some((value) => value.name === 'stop'));
  await client.send({ type: 'resume' });
  await client.send({ type: 'doc', path: null, name: '再開', text: '# 再開した本文', rev: 2 });
  await until(() => stream.received.some((value) => value.name === 'render' && value.data.rev === 2));
  assert.equal(stream.received.some((value) => value.name === 'render' && value.data.rev === 1), false);
});

test('Neovim の文書が別のルートへ移ったら、ファイル文書のタブだけを読み直す', async (t) => {
  const { mkdirSync } = await import('node:fs');
  const root = temp(t);
  const one = join(root, 'one'), two = join(root, 'two');
  for (const dir of [one, two]) { mkdirSync(dir); mkdirSync(join(dir, '.git')); writeFileSync(join(dir, 'main.md'), ''); }
  writeFileSync(join(one, 'linked.md'), '# 最初のルート');
  const client = await ready(t);
  await client.send({ type: 'doc', path: join(one, 'main.md'), name: 'main.md', text: '# 編集中', rev: 1 });
  const active = await events(client.server);
  const linked = await events(client.server, { doc: 'linked.md' });
  t.after(() => { active.close(); linked.close(); });
  await until(() => linked.received.some((value) => value.data.html?.includes('最初のルート')));
  await client.send({ type: 'doc', path: join(two, 'main.md'), name: 'main.md', text: '# 別のルート', rev: 2 });
  await until(() => linked.received.some((value) => value.name === 'reload'));
  await until(() => active.received.some((value) => value.data.rev === 2));
  assert.equal(active.received.some((value) => value.name === 'reload'), false);
  const reopened = await events(client.server, { doc: 'linked.md' });
  t.after(() => reopened.close());
  await until(() => reopened.received.some((value) => value.name === 'render'));
  assert.equal(reopened.received[0].data.error, 'この文書は開けない');
});
