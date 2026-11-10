import assert from 'node:assert/strict';
import { mkdirSync, mkdtempSync, realpathSync, rmSync } from 'node:fs';
import { request } from 'node:http';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { setTimeout as delay } from 'node:timers/promises';
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

// 試験から起こすアプリが、この PC に導入済みの公式スタイルを読まないよう、既定の保存先のもとを空のフォルダーへ向ける
// (この試験のプロセスと、そこから起こす子プロセスだけに効く)。
export function isolateUserData() {
  const base = fileURLToPath(new URL('../.tests/', import.meta.url));
  mkdirSync(base, { recursive: true });
  const path = mkdtempSync(join(base, 'userdata-'));
  process.env.LOCALAPPDATA = path;
  process.env.XDG_DATA_HOME = path;
  process.on('exit', () => rmSync(path, { recursive: true, force: true }));
  return path;
}

export async function until(predicate, message = '条件が時間内に成立する', timeout = 6000) {
  const end = Date.now() + timeout;
  while (Date.now() < end) {
    if (await predicate()) return;
    await delay(20);
  }
  assert.fail(message);
}

// http.request はパスを正規化しないので、脱出を狙う要求もそのまま送れる。
export function get(server, rel = '', { method = 'GET', headers = {}, path, body } = {}) {
  return new Promise((resolve, reject) => {
    const req = request({ hostname: '127.0.0.1', port: server.port,
      path: path ?? '/' + server.token + '/' + rel, method, headers }, (res) => {
      const chunks = [];
      res.on('data', (chunk) => chunks.push(chunk));
      res.on('end', () => resolve({ status: res.statusCode, headers: res.headers, body: Buffer.concat(chunks) }));
      res.on('error', reject);
    });
    req.on('error', reject);
    req.end(body);
  });
}

export async function events(server, { doc } = {}) {
  const received = [];
  let buffer = '';
  let response;
  let req;
  await new Promise((resolve, reject) => {
    const url = new URL('_/events', server.url);
    if (doc !== undefined) url.searchParams.set('doc', doc);
    req = request(url, (res) => {
      response = res;
      res.setEncoding('utf8');
      res.on('data', (text) => {
        buffer += text;
        let end;
        while ((end = buffer.indexOf('\n\n')) !== -1) {
          const block = buffer.slice(0, end);
          buffer = buffer.slice(end + 2);
          const name = /^event: (.+)$/m.exec(block)?.[1];
          const data = /^data: (.+)$/m.exec(block)?.[1];
          if (name && data) received.push({ name, data: JSON.parse(data) });
        }
      });
      resolve();
    });
    req.on('error', reject);
    req.end();
  });
  return { received, close() { response.destroy(); req.destroy(); } };
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
  assert.equal(commands.skip, false, 'Go 版の書き出しコマンドが使える');
  // この入力には PlantUML がない。手元に導入済みのコマンドを探さず、製品の render を通す。
  await execFileAsync(commands.command, ['render', '--no-plantuml', '--root', root, '-o', output, file], {
    cwd: pageTestRoot, env: pageGoEnvironment(), windowsHide: true, timeout: 90000,
  });
}

export async function startGoPageServer(t, options = {}) {
  const commands = await prepareGoPageTests();
  assert.equal(commands.skip, false, 'Go 版のページ試験用サーバーが使える');
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
