import assert from 'node:assert/strict';
import { EventEmitter } from 'node:events';
import { readFileSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { spawn } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import test from 'node:test';
import { main, parseArgs, browserCommand } from '../cli.mjs';
import { events, temp, until } from './helpers.mjs';

function output() {
  const io = { out: '', err: '', signals: new EventEmitter() };
  io.stdout = { write: (text) => { io.out += text; } };
  io.stderr = { write: (text) => { io.err += text; } };
  return io;
}

test('引数の誤りとファイルの誤りは終了コード2と日本語1行を返す', async (t) => {
  const root = temp(t);
  const file = join(root, 'file.md');
  writeFileSync(file, '# 文書');
  for (const argv of [[], ['--unknown'], ['--port'], ['--port', '-1', file], ['--port', '65536', file],
    ['--port', '3.1', file], [file, file], ['--browser'], ['--root'], ['--version', file], ['--root', root, root],
    ['--stdio', file], ['--stdio', '--no-open'], ['--no-open', '--stdio'], ['--stdio', '--browser', 'browser'], ['--stdio', '--root', root], [join(root, 'missing.md')], ['--root', file, file], ['--root', join(root, 'missing'), file]]) {
    const io = output();
    assert.equal(await main(argv, io), 2, JSON.stringify(argv));
    assert.match(io.err, /^mdpreview: [^\r\n]+\n$/);
    assert.match(io.err, /[ぁ-んァ-ヶ一-龠]/);
    assert.equal(io.out, '');
  }
  assert.equal(parseArgs(['--stdio', '--port', '0']).stdio, true);
  assert.equal(parseArgs(['--', '-file.md']).file, '-file.md');
  assert.equal(parseArgs(['--port', '0', file]).port, 0);
});

test('--help と --version はサーバーを起こさず終了する', async () => {
  for (const option of ['--help', '--version']) {
    const io = output();
    assert.equal(await main([option], io), 0);
    assert.equal(io.err, '');
    if (option === '--help') assert.match(io.out, /^使い方: mdpreview/);
    else assert.equal(io.out.trim(), JSON.parse(readFileSync(new URL('../package.json', import.meta.url), 'utf8')).version);
  }
});

test('--no-open で URL を出し、変更を配信し、SIGINT 相当で終了する', async (t) => {
  const root = temp(t);
  const file = join(root, '文書.md');
  writeFileSync(file, '\uFEFF# 最初の本文\n');
  const io = output();
  io.spawn = () => assert.fail('--no-open ではブラウザを起動しない');
  let started;
  const ready = new Promise((resolve) => { started = resolve; });
  io.onStart = started;
  const done = main(['--no-open', '--root', root, file], io);
  const app = await ready;
  t.after(() => app.close());
  assert.equal(io.out, 'プレビュー: ' + app.url + '\n');
  assert.equal(io.err, '');
  const stream = await events(app.server);
  t.after(() => stream.close());
  await until(() => stream.received.some((entry) => entry.name === 'render'));
  assert.match(stream.received[0].data.html, /最初の本文/);
  assert.doesNotMatch(stream.received[0].data.html, /\uFEFF/);
  writeFileSync(file, '# 更新した本文\n');
  await until(() => stream.received.some((entry) => entry.data.html?.includes('更新した本文')));
  io.signals.emit('SIGINT');
  assert.equal(await done, 0);
  await until(() => stream.received.some((entry) => entry.name === 'stop'));
  assert.deepEqual(stream.received.at(-1), { name: 'stop', data: { reason: 'exit' } });
  assert.equal(io.signals.listenerCount('SIGINT'), 0);
  assert.equal(io.signals.listenerCount('SIGTERM'), 0);
});

test('ブラウザ起動の引数はシェルを通さず、失敗しても表示を続ける', async (t) => {
  const root = temp(t);
  const file = join(root, 'file.md');
  writeFileSync(file, '# 文書');
  const io = output();
  const calls = [];
  io.spawn = (command, args, options) => {
    calls.push({ command, args, options });
    const child = new EventEmitter();
    child.unref = () => {};
    queueMicrotask(() => child.emit('error', new Error('試験用の起動失敗')));
    return child;
  };
  io.onStart = ({ close }) => { queueMicrotask(close); };
  assert.equal(await main(['--browser', 'browser executable', file], io), 0);
  assert.equal(calls[0].command, 'browser executable');
  assert.equal(calls[0].args.length, 1);
  assert.match(calls[0].args[0], /^http:\/\/127\.0\.0\.1:/);
  assert.equal(calls[0].options.shell, undefined);
  assert.match(io.err, /^mdpreview: ブラウザを開けません。[^\n]+\n$/);
  const url = 'http://127.0.0.1:1/token/';
  assert.deepEqual(browserCommand(url, null, 'win32', {}), ['rundll32', ['url.dll,FileProtocolHandler', url]]);
  assert.deepEqual(browserCommand(url, null, 'darwin', {}), ['open', [url]]);
  assert.deepEqual(browserCommand(url, null, 'linux', { TERMUX_VERSION: '1' }), ['termux-open-url', [url]]);
  assert.deepEqual(browserCommand(url, null, 'linux', {}), ['xdg-open', [url]]);
});

test('実行入口は使い方の誤りを終了コード2で返す', async () => {
  const result = await new Promise((resolve, reject) => {
    const child = spawn(process.execPath, [fileURLToPath(new URL('../cli.mjs', import.meta.url)), '--invalid'], { windowsHide: true });
    let stderr = '';
    child.stderr.setEncoding('utf8').on('data', (text) => { stderr += text; });
    child.on('error', reject);
    child.on('exit', (code) => resolve({ code, stderr }));
  });
  assert.equal(result.code, 2);
  assert.match(result.stderr, /^mdpreview: [^\n]+\n$/);
});

test('フォルダーを渡すと、そのルートの一覧を同じ URL で開く', async (t) => {
  const { get } = await import('./helpers.mjs');
  const root = temp(t);
  writeFileSync(join(root, 'README.md'), '# フォルダーの文書');
  const io = output();
  io.onStart = async ({ server, session, close }) => {
    assert.equal(session.root(), root);
    assert.equal(session.hasDocument(), false);
    const html = (await get(server)).body.toString();
    assert.match(html, /"directory":true/);
    assert.match(html, /"list":true/);
    assert.match(html, /"hasDocument":false/);
    const result = await get(server, '_/list');
    assert.deepEqual(JSON.parse(result.body.toString()), { files: ['README.md'], truncated: false });
    await close();
  };
  assert.equal(await main(['--no-open', root], io), 0);
  assert.match(io.out, /^プレビュー: http:\/\/127\.0\.0\.1:/);
  assert.equal(io.err, '');
});
