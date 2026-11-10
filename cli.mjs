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
import { defaultAssetsDirectory, loadAssets } from './lib/assets.mjs';
import { install } from './assets/install.mjs';
import { exportDocument } from './lib/export.mjs';
import { createPlantumlRenderer, findPlantuml } from './lib/plantuml.mjs';

const version = JSON.parse(readFileSync(new URL('./package.json', import.meta.url), 'utf8')).version;
const HELP =
  '使い方: mdpreview [--port <番号>] [--root <フォルダー>] [--no-open] [--browser <実行ファイル>] [--assets-dir <保存先>] [--no-official-style] [--no-plantuml] <file.md | フォルダー>\n       mdpreview --stdio [--port <番号>] [--assets-dir <保存先>] [--no-official-style] [--no-plantuml]\n       mdpreview install-style [--dir <保存先>] [--check]\n       mdpreview render [--root <フォルダー>] [--theme system|light|dark] [--width fluid|fixed] [--no-plantuml] [-o <out.html>] <file.md>\n       mdpreview --help | --version\nPATH に plantuml があれば、PlantUML の図を手元で描きます。\n--no-plantuml を付けると、原文のコードブロックのまま表示します。\n';

export function parseArgs(argv) {
  if (argv[0] === 'render') {
    const result = { command: 'render', theme: 'system', width: 'fluid' };
    let positional = false;
    for (let i = 1; i < argv.length; i++) {
      const arg = argv[i];
      if (!positional && arg === '--') {
        positional = true;
        continue;
      }
      if (!positional && arg === '--no-plantuml') {
        result.plantuml = false;
        continue;
      }
      if (!positional && ['--root', '--theme', '--width', '-o'].includes(arg)) {
        const value = argv[++i];
        if (!value || value.startsWith('-')) throw new Error(`${arg} の値を指定してください`);
        result[arg === '-o' ? 'output' : arg.slice(2)] = value;
      } else {
        if (!positional && arg.startsWith('-')) throw new Error(`不明なオプションです: ${arg}`);
        if (result.file) throw new Error('文書は1つだけ指定してください');
        result.file = arg;
      }
    }
    if (!result.file) throw new Error('Markdown ファイルを指定してください');
    if (!['system', 'light', 'dark'].includes(result.theme))
      throw new Error('配色は system・light・dark で指定してください');
    if (!['fluid', 'fixed'].includes(result.width)) throw new Error('本文幅は fluid・fixed で指定してください');
    return result;
  }
  if (argv[0] === 'install-style') {
    const result = { command: 'install-style' };
    for (let i = 1; i < argv.length; i++) {
      if (argv[i] === '--check') result.check = true;
      else if (argv[i] === '--dir' && argv[i + 1] && !argv[i + 1].startsWith('-')) result.dir = argv[++i];
      else throw new Error('使い方: mdpreview install-style [--dir <保存先>] [--check]');
    }
    return result;
  }
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
    if (!positional && arg === '--no-official-style') {
      options.officialStyle = false;
      continue;
    }
    if (!positional && arg === '--no-plantuml') {
      options.plantuml = false;
      continue;
    }
    if (!positional && ['--port', '--root', '--browser', '--assets-dir'].includes(arg)) {
      const value = argv[++i];
      if (!value || value.startsWith('--')) throw new Error(`${arg} の値を指定してください`);
      if (arg === '--port') {
        if (!/^\d+$/.test(value) || Number(value) > 65535)
          throw new Error('ポート番号は 0 から 65535 の整数で指定してください');
        options.port = Number(value);
      } else options[arg === '--assets-dir' ? 'assetsDir' : arg.slice(2)] = value;
      continue;
    }
    if (!positional && arg.startsWith('-')) throw new Error(`不明なオプションです: ${arg}`);
    if (options.file) throw new Error('文書は1つだけ指定してください');
    options.file = arg;
  }
  if (options.stdio) {
    if (options.file || !options.open || options.browser || options.root)
      throw new Error(
        '--stdio は --port・--assets-dir・--no-official-style・--no-plantuml 以外のオプションやファイルと一緒に指定できません',
      );
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
  let plantuml;
  try {
    const options = parseArgs(argv);
    if (!options.action && options.command !== 'install-style' && options.plantuml !== false) {
      const command = findPlantuml();
      if (command) plantuml = createPlantumlRenderer({ command });
    }
    if (options.command === 'render') {
      try {
        const result = await exportDocument({ ...options, plantuml });
        if (!options.output) stdout.write(result.html);
        const labels = {
          reference: '参照',
          diagram: '図',
          suggestion: '提案',
          include: '取り込み',
          emoji: '絵文字',
          missing: '見つからないメディア',
          more: '省略した知らせ',
        };
        for (const warning of result.warnings) report(warning);
        for (const [kind, count] of result.notices) report(`${labels[kind] || '表示の違い'}: ${count} 件`);
        return 0;
      } catch (error) {
        report(error.message);
        return 1;
      }
    }
    if (options.command === 'install-style') {
      if (options.check) {
        const loaded = loadAssets(options.dir, io.assetsLockPath);
        report(loaded ? '公式スタイルを利用できます' : '公式スタイルが未導入または破損しています');
        return loaded ? 0 : 1;
      }
      report(
        'GitLab の公式の CSS・フォント・ライセンスを導入します（未導入のときは、registry.gitlab.com などから約 785 MB を取得します）',
      );
      const controller = new AbortController();
      const stop = () => controller.abort(new Error('中断しました'));
      signals.once('SIGINT', stop);
      signals.once('SIGTERM', stop);
      try {
        // 途中の知らせ (導入済みの確認・取得の開始・導入先) も、標準エラーに 1 行ずつ出す。
        await install({ dataDir: options.dir, lockPath: io.assetsLockPath, signal: controller.signal, log: report });
        return 0;
      } catch (error) {
        if (controller.signal.aborted) report('公式スタイルの導入を中断しました');
        else report('公式スタイルの導入に失敗しました: ' + String(error?.message || error).split(/\r?\n/)[0]);
        return 1;
      } finally {
        signals.off('SIGINT', stop);
        signals.off('SIGTERM', stop);
      }
    }
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
        assetsDir: options.assetsDir || defaultAssetsDirectory(),
        officialStyle: options.officialStyle,
        plantuml,
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
      plantuml: !!plantuml,
      onRender: (data) => server?.broadcast('render', data),
      onScroll: (data) => server?.broadcast('scroll', data),
    });
    server = await createServer({
      session,
      plantuml,
      page: loadPage({ directory }),
      port: options.port,
      assetsDir: options.assetsDir || defaultAssetsDirectory(),
      officialStyle: options.officialStyle,
    });
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
  } finally {
    await plantuml?.close();
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href)
  process.exitCode = await main();
