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

export async function renderGoPageDocument({ file, root, output }) {
  const commands = await prepareGoPageTests();
  assert.equal(commands.skip, false, '書き出しのコマンドが使える');
  // この入力には PlantUML がない。手元に導入済みのコマンドを探さず、製品の render を通す。
  await execFileAsync(commands.command, ['render', '--no-plantuml', '--root', root, '-o', output, file], {
    cwd: pageTestRoot, env: pageGoEnvironment(), windowsHide: true, timeout: 90000,
  });
}

export async function startGoPageServer(t, options = {}) {
  const commands = await prepareGoPageTests();
  assert.equal(commands.skip, false, 'ページ試験用サーバーが使える');
  const child = spawn(commands.server, [], {
    cwd: pageTestRoot, env: pageGoEnvironment(), windowsHide: true, stdio: ['pipe', 'pipe', 'pipe'],
  });
  const pending = new Map();
  const jumps = [];
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
    url: ready.url, port: ready.port, token: ready.token, jumps, close,
    broadcast: (name, data) => request('broadcast', { name, data }),
    setDocument: (document) => request('document', { document }),
    sync: () => request('sync'),
  };
}
