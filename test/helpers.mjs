import assert from 'node:assert/strict';
import { mkdirSync, mkdtempSync, realpathSync, rmSync } from 'node:fs';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { execFile, spawn } from 'node:child_process';
import { createInterface } from 'node:readline';
import { promisify } from 'node:util';

export function temp(t) {
  const base = fileURLToPath(new URL('../.tests/', import.meta.url));
  mkdirSync(base, { recursive: true });
  const path = realpathSync.native(mkdtempSync(join(base, 'case-')));
  t.after(() => rmSync(path, { recursive: true, force: true }));
  return path;
}

const execFileAsync = promisify(execFile);
const pageTestRoot = fileURLToPath(new URL('../', import.meta.url));
const pageTestDirectory = join(pageTestRoot, '.tests');
const pageGoEnvironment = () => ({
  ...process.env,
  GOFLAGS: '-mod=readonly',
  GOPROXY: 'off',
  GOCACHE: join(pageTestDirectory, 'gocache'),
});
let pageCommands;

// ビルドはこの試験プロセスで一度だけ行う。Go が無い場合だけ、呼ぶ側に skip の理由を返す。
export function prepareGoPageTests() {
  return (pageCommands ??= (async () => {
    const options = { cwd: pageTestRoot, env: pageGoEnvironment(), windowsHide: true, timeout: 120000 };
    try {
      await execFileAsync('go', ['version'], options);
    } catch (error) {
      if (error.code === 'ENOENT') return { skip: 'Go が見つからない（ページ試験用サーバーのビルドに必要）' };
      throw error;
    }
    mkdirSync(pageTestDirectory, { recursive: true });
    const suffix = process.platform === 'win32' ? '.exe' : '';
    const server = join(pageTestDirectory, 'pagetest' + suffix);
    const command = join(pageTestDirectory, 'mdpreview-page-test' + suffix);
    await execFileAsync('go', ['build', '-o', server, './internal/pagetest'], options);
    await execFileAsync('go', ['build', '-o', command, './cmd/mdpreview'], options);
    return { server, command, skip: false };
  })());
}

export async function renderGoPageDocument({ file, root, output, embedImages = false }) {
  const commands = await prepareGoPageTests();
  assert.equal(commands.skip, false, '書き出しのコマンドが使える');
  // この入力には PlantUML がない。手元に導入済みのコマンドを探さず、製品の render を通す。
  await execFileAsync(commands.command, ['render', '--no-plantuml', '--root', root, ...(embedImages ? ['--embed-images'] : []), '-o', output, file], {
    cwd: pageTestRoot, env: pageGoEnvironment(), windowsHide: true, timeout: 90000,
  });
}

// 製品の --stdio を起こし、試験が Lua の代わりに JSON の行を送り、種類ごとに待つ。
export async function startGoStdio(t, { edit = true } = {}) {
  const commands = await prepareGoPageTests();
  assert.equal(commands.skip, false, 'Neovim の橋渡しのコマンドが使える');
  const child = spawn(commands.command, ['--stdio', '--no-plantuml', '--no-official-style'], {
    cwd: pageTestRoot, env: pageGoEnvironment(), windowsHide: true, stdio: ['pipe', 'pipe', 'pipe'],
  });
  const buffered = [], waiting = new Set();
  let stderr = '', failure, closing, exited = false;
  child.stderr.setEncoding('utf8');
  child.stderr.on('data', (value) => { stderr += value; });
  const fail = (error) => {
    failure ??= error;
    for (const entry of waiting) {
      clearTimeout(entry.timer);
      entry.reject(failure);
    }
    waiting.clear();
  };
  const lines = createInterface({ input: child.stdout });
  lines.on('line', (line) => {
    let value;
    try { value = JSON.parse(line); }
    catch (cause) { fail(new Error('Neovim の橋渡しが JSON 以外の行を返した', { cause })); return; }
    const entry = Array.from(waiting).find((entry) => entry.matches(value));
    if (entry) {
      waiting.delete(entry);
      clearTimeout(entry.timer);
      entry.resolve(value);
    } else buffered.push(value);
  });
  child.on('error', fail);
  child.stdin.on('error', (error) => { if (!closing) fail(error); });
  const finished = new Promise((resolve) => child.on('close', (code, signal) => {
    exited = true;
    if (!closing) fail(new Error(`Neovim の橋渡しが終了した（code=${code}, signal=${signal}）: ${stderr}`));
    resolve({ code, signal });
  }));
  const send = (value) => new Promise((resolve, reject) => {
    if (failure) return reject(failure);
    child.stdin.write(JSON.stringify(value) + '\n', (error) => error ? reject(error) : resolve());
  });
  const waitFor = (kind, timeout = 30000) => new Promise((resolve, reject) => {
    if (failure) return reject(failure);
    const matches = typeof kind === 'function' ? kind : (value) => value.type === kind;
    const index = buffered.findIndex(matches);
    if (index >= 0) return resolve(buffered.splice(index, 1)[0]);
    const entry = { matches, resolve, reject, timer: setTimeout(() => {
      waiting.delete(entry);
      reject(new Error('Neovim の橋渡しの応答が時間内に届かない: ' + kind));
    }, timeout) };
    waiting.add(entry);
  });
  const close = () => (closing ??= (async () => {
    child.stdin.end();
    const timer = setTimeout(() => { if (!exited) child.kill(); }, 5000);
    try {
      const result = await finished;
      assert.equal(result.code, 0, 'Neovim の橋渡しが正常終了する: ' + stderr);
      if (failure) throw failure;
    } finally {
      clearTimeout(timer);
      lines.close();
      fail(new Error('Neovim の橋渡しを閉じた'));
    }
  })());
  t.after(close);
  await send({ type: 'hello', protocol: 1, ...(edit ? { edit: 1 } : {}) });
  const ready = await waitFor('ready');
  return { ready, url: ready.url, send, waitFor, close };
}

// options の edit: 'file' と live: true は編集 API、review: true と live: true はコメント API も接続する。
export async function startGoPageServer(t, options = {}) {
  const commands = await prepareGoPageTests();
  assert.equal(commands.skip, false, 'ページ試験用サーバーが使える');
  const child = spawn(commands.server, [], {
    cwd: pageTestRoot, env: pageGoEnvironment(), windowsHide: true, stdio: ['pipe', 'pipe', 'pipe'],
  });
  const pending = new Map();
  const jumps = [];
  const launches = [];
  let sequence = 0;
  let stderr = '';
  let failure;
  let closing;
  let exited = false;
  child.stderr.setEncoding('utf8');
  child.stderr.on('data', (value) => { stderr += value; });
  const fail = (error) => {
    failure ??= error;
    for (const entry of pending.values()) {
      clearTimeout(entry.timer);
      entry.reject(failure);
    }
    pending.clear();
  };
  const lines = createInterface({ input: child.stdout });
  lines.on('line', (line) => {
    let value;
    try {
      value = JSON.parse(line);
    } catch (cause) {
      fail(new Error('ページ試験用サーバーが JSON 以外の行を返した', { cause }));
      return;
    }
    if (value.type === 'jump') {
      jumps.push({ line: value.line });
      return;
    }
    if (value.type === 'launch') {
      launches.push({ root: value.root });
      return;
    }
    const entry = pending.get(value.id);
    if (!entry) {
      fail(new Error('ページ試験用サーバーが対応しない応答を返した: ' + line));
      return;
    }
    pending.delete(value.id);
    clearTimeout(entry.timer);
    if (value.type === 'error') entry.reject(new Error(value.error));
    else entry.resolve(value);
  });
  child.on('error', fail);
  child.stdin.on('error', fail);
  const finished = new Promise((resolve) => child.on('close', (code, signal) => {
    exited = true;
    if (!closing || pending.size) {
      fail(new Error(`ページ試験用サーバーが終了した（code=${code}, signal=${signal}）: ${stderr}`));
    }
    resolve({ code, signal });
  }));
  const request = (op, fields = {}) => new Promise((resolve, reject) => {
    if (failure) return reject(failure);
    if (exited) return reject(new Error('ページ試験用サーバーは終了している'));
    const id = ++sequence;
    const timer = setTimeout(() => {
      fail(new Error('ページ試験用サーバーの応答が時間内に届かない: ' + op));
      child.kill();
    }, 30000);
    pending.set(id, { resolve, reject, timer });
    child.stdin.write(JSON.stringify({ ...fields, id, op }) + '\n', (error) => {
      if (error) fail(error);
    });
  });
  const close = (reason = 'exit') => (closing ??= (async () => {
    try {
      if (!exited && !failure) await request('close', { reason });
    } finally {
      child.stdin.end();
      const timer = setTimeout(() => child.kill(), 5000);
      try {
        const result = await finished;
        assert.equal(result.code, 0, 'ページ試験用サーバーが正常終了する: ' + stderr);
        if (failure) throw failure;
      } finally {
        clearTimeout(timer);
        lines.close();
      }
    }
  })());
  t.after(() => close());
  const ready = await request('start', options);
  assert.equal(ready.type, 'ready', 'ページ試験用サーバーの URL が届く');
  return {
    url: ready.url, port: ready.port, token: ready.token, jumps, launches, close,
    broadcast: (name, data) => request('broadcast', { name, data }),
    setDocument: (document) => request('document', { document }),
    sync: () => request('sync'),
  };
}
