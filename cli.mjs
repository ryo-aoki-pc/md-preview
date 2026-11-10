#!/usr/bin/env node
import { spawn } from 'node:child_process';
import { readFile, realpath, stat } from 'node:fs/promises';
import { readFileSync } from 'node:fs';
import { basename, resolve } from 'node:path';
import { pathToFileURL } from 'node:url';
import { createSession } from './lib/session.mjs';
import { createServer } from './lib/server.mjs';
import { loadPage } from './lib/page.mjs';
import { watchFile } from './lib/watch.mjs';
import { runStdio } from './lib/stdio.mjs';

const version = JSON.parse(readFileSync(new URL('./package.json', import.meta.url), 'utf8')).version;
const HELP =
  '使い方: mdpreview [--port <番号>] [--root <フォルダー>] [--no-open] [--browser <実行ファイル>] <file.md | フォルダー>\n       mdpreview --stdio [--port <番号>]\n       mdpreview --help | --version\n';

export function parseArgs(argv) {
  const options = { port: 0, open: true };
  let positional = false;
  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i];
    if (!positional && arg === '--') {
      positional = true;
      continue;
    }
    if (!positional && (arg === '--help' || arg === '--version')) {
      if (argv.length !== 1) throw new Error('--help と --version は単独で指定してください');
      return { action: arg.slice(2) };
    }
    if (!positional && arg === '--stdio') {
      options.stdio = true;
      continue;
    }
    if (!positional && arg === '--no-open') {
      options.open = false;
      continue;
    }
    if (!positional && ['--port', '--root', '--browser'].includes(arg)) {
      const value = argv[++i];
      if (!value || value.startsWith('--')) throw new Error(`${arg} の値を指定してください`);
      if (arg === '--port') {
        if (!/^\d+$/.test(value) || Number(value) > 65535)
          throw new Error('ポート番号は 0 から 65535 の整数で指定してください');
        options.port = Number(value);
      } else options[arg.slice(2)] = value;
      continue;
    }
    if (!positional && arg.startsWith('-')) throw new Error(`不明なオプションです: ${arg}`);
    if (options.file) throw new Error('文書は1つだけ指定してください');
    options.file = arg;
  }
  if (options.stdio) {
    if (options.file || !options.open || options.browser || options.root)
      throw new Error('--stdio は --port 以外のオプションやファイルと一緒に指定できません');
    return options;
  }
  if (!options.file) throw new Error('Markdown ファイルまたはフォルダーを指定してください');
  return options;
}

export function browserCommand(url, browser, platform = process.platform, env = process.env) {
  if (browser) return [browser, [url]];
  if (platform === 'win32') return ['rundll32', ['url.dll,FileProtocolHandler', url]];
  if (platform === 'darwin') return ['open', [url]];
  if (env.TERMUX_VERSION) return ['termux-open-url', [url]];
  return ['xdg-open', [url]];
}

export async function main(argv = process.argv.slice(2), io = {}) {
  const stdout = io.stdout || process.stdout;
  const stderr = io.stderr || process.stderr;
  const signals = io.signals || process;
  const report = (text) => stderr.write('mdpreview: ' + String(text).replace(/[\r\n]+/g, ' ') + '\n');
  let session;
  let server;
  let stopWatch;
  let shutdown;
  try {
    const options = parseArgs(argv);
    if (options.action) {
      stdout.write(options.action === 'help' ? HELP : version + '\n');
      return 0;
    }
    if (options.stdio)
      return await runStdio({
        stdin: io.stdin || process.stdin,
        stdout,
        signals,
        port: options.port,
        version,
        onStart: io.onStart,
      });
    const path = await realpath(resolve(options.file)).catch(() => {
      throw new Error('指定したファイルを読めません');
    });
    const info = await stat(path);
    const directory = info.isDirectory();
    if (!directory && !info.isFile()) throw new Error('ファイルまたはフォルダーを指定してください');
    if (directory && options.root) throw new Error('フォルダーと --root は一緒に指定できません');
    let root = directory ? path : undefined;
    if (options.root) {
      root = await realpath(resolve(options.root)).catch(() => {
        throw new Error('指定したルートが見つかりません');
      });
      if (!(await stat(root)).isDirectory()) throw new Error('ルートにはフォルダーを指定してください');
    }
    const read = async () => (await readFile(path, 'utf8')).replace(/^\uFEFF/, '');
    const text = directory ? null : await read();
    session = createSession({
      root,
      onRender: (data) => server?.broadcast('render', data),
      onScroll: (data) => server?.broadcast('scroll', data),
    });
    server = await createServer({ session, page: loadPage({ directory }), port: options.port });
    let rev = 1;
    if (!directory) session.setDocument({ path, name: basename(path), text, rev });
    let reading = false;
    let again = false;
    let ended = false;
    const update = async () => {
      if (ended) return;
      if (reading) {
        again = true;
        return;
      }
      reading = true;
      do {
        again = false;
        try {
          const next = await read();
          if (!ended) session.setDocument({ path, name: basename(path), text: next, rev: ++rev });
        } catch (error) {
          if (error.code !== 'ENOENT' && !ended) report('ファイルの変更を読めません');
        }
      } while (again && !ended);
      reading = false;
    };
    stopWatch = directory ? () => {} : watchFile(path, update);
    let finish;
    const done = new Promise((resolve) => {
      finish = resolve;
    });
    let stopping;
    const close = () => {
      if (stopping) return stopping;
      ended = true;
      stopWatch();
      signals.off('SIGINT', close);
      signals.off('SIGTERM', close);
      stopping = Promise.all([server.close('exit'), session.close()]).then(() => finish(0));
      return stopping;
    };
    shutdown = close;
    signals.on('SIGINT', close);
    signals.on('SIGTERM', close);
    stdout.write('プレビュー: ' + server.url + '\n');
    if (options.open) {
      const [command, args] = browserCommand(server.url, options.browser);
      let warned = false;
      const warn = () => {
        if (!warned) {
          warned = true;
          report('ブラウザを開けません。表示された URL を自分で開いてください');
        }
      };
      try {
        const child = (io.spawn || spawn)(command, args, { stdio: 'ignore', windowsHide: true });
        child.once('error', warn);
        child.once('exit', (code) => {
          if (code) warn();
        });
        child.unref();
      } catch {
        warn();
      }
    }
    await io.onStart?.({ url: server.url, server, session, close });
    return await done;
  } catch (error) {
    if (shutdown) await shutdown();
    stopWatch?.();
    if (server) await server.close('exit');
    if (session) await session.close();
    report(
      error.code === 'EADDRINUSE'
        ? '指定したポートは使用中です'
        : error.code
          ? 'ファイルまたはサーバーを開けません'
          : error.message,
    );
    return 2;
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href)
  process.exitCode = await main();
