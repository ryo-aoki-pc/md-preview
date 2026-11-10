#!/usr/bin/env node
// mdpreview を導入する (Node.js 22 以降)。
//
// リポジトリの外の導入先 (Linux は ${XDG_DATA_HOME:-~/.local/share}/md-preview、Windows は %LOCALAPPDATA%\md-preview) に
// プレビューのアプリを写して依存を npm ci で入れ、起動役 (mdpreview) を ~/.local/bin (Windows は %USERPROFILE%\.local\bin) に置く。
// 隣の作業用フォルダで入れて確かめてから、今の導入と入れ替える (途中で失敗しても、今の導入を壊さない)。
// リポジトリを更新したら再実行する。--check は何も変えずに、導入の状態を確かめる。
import { spawnSync } from 'node:child_process';
import fs from 'node:fs';
import { createHash } from 'node:crypto';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const repository = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const windows = process.platform === 'win32';
const minimumNode = 22;

const usage = `使い方: node scripts/setup.mjs [--check] [--install-dir <フォルダ>] [--bin-dir <フォルダ>]

mdpreview を導入する。--check は何も変えずに、導入の状態を確かめる。
  --install-dir <フォルダ>  プレビューのアプリと依存を置くフォルダ（既定: ${defaultInstallDirectory()}）
  --bin-dir <フォルダ>      起動役 mdpreview を置くフォルダ（既定: ${defaultBinDirectory()}）
`;

function defaultInstallDirectory() {
  let base;
  if (windows) {
    base = process.env.LOCALAPPDATA || path.join(os.homedir(), 'AppData', 'Local');
  } else if (process.platform === 'darwin') {
    base = path.join(os.homedir(), 'Library', 'Application Support');
  } else {
    const xdg = process.env.XDG_DATA_HOME;
    base = xdg && path.isAbsolute(xdg) ? xdg : path.join(os.homedir(), '.local', 'share');
  }
  return path.join(base, 'md-preview');
}

function defaultBinDirectory() {
  return path.join(os.homedir(), '.local', 'bin');
}

function parseArguments(argv) {
  const options = { installDir: defaultInstallDirectory(), binDir: defaultBinDirectory(), check: false, help: false };
  for (let index = 0; index < argv.length; index++) {
    const argument = argv[index];
    if (argument === '--check') options.check = true;
    else if (argument === '--help' || argument === '-h') options.help = true;
    else if (argument === '--install-dir' || argument === '--bin-dir') {
      const value = argv[++index];
      if (!value || value.startsWith('--')) throw new Error(`${argument} の値がありません`);
      options[argument === '--install-dir' ? 'installDir' : 'binDir'] = path.resolve(value);
    } else {
      throw new Error(`知らない引数です: ${argument}`);
    }
  }
  return options;
}

// child が parent そのものか、その下か。".." で始まる名前のフォルダ (..backup など) は下にあるとみなす。
function inside(child, parent) {
  const relative = path.relative(path.resolve(parent), path.resolve(child));
  return relative === '' || (relative.split(/[\\/]/u)[0] !== '..' && !path.isAbsolute(relative));
}

function onPath(directory) {
  const target = path.resolve(directory);
  const same = (entry) => (windows ? entry.toLowerCase() === target.toLowerCase() : entry === target);
  return (process.env.PATH || '').split(path.delimiter).some((entry) => entry && same(path.resolve(entry)));
}

function readJson(file) {
  try {
    return JSON.parse(fs.readFileSync(file, 'utf8'));
  } catch {
    return null;
  }
}

function repositoryVersion() {
  return readJson(path.join(repository, 'package.json'))?.version ?? null;
}

// clone のコミット (git が無い・clone でないときは null)。
function repositoryCommit() {
  const result = spawnSync('git', ['-C', repository, 'rev-parse', 'HEAD'], { encoding: 'utf8' });
  return result.status === 0 ? result.stdout.trim() : null;
}

// 実行に使うファイルだけを写す。ソース・試験・手順書は含めない。
// 公式スタイルの保存先 md-preview-assets は導入先と同じ親に置く別のフォルダーで、ここでは触らない。
function sourceFiles() {
  const entries = [
    'cli.mjs',
    'lib',
    'page',
    'engine/glfm_markdown.wasm',
    'engine/engine.lock.json',
    'engine/licenses',
    'package.json',
    'package-lock.json',
  ];
  if (fs.existsSync(path.join(repository, 'assets'))) entries.push('assets');
  const files = [];
  function collect(name) {
    const info = fs.lstatSync(path.join(repository, name));
    if (info.isDirectory()) {
      for (const child of fs.readdirSync(path.join(repository, name)).sort()) collect(path.join(name, child));
    } else if (info.isFile()) files.push(name);
    else throw new Error('配布するファイルにリンクや特殊なファイルは使えません: ' + name);
  }
  for (const entry of entries) collect(entry);
  return files;
}

function hash(file) {
  return createHash('sha256').update(fs.readFileSync(file)).digest('hex');
}

// npm は npm-cli.js を探して node で動かす（シェルを通さない）。
// MD_PREVIEW_TEST_NPM_CLI は試験用（本物の npm の代わりに動かすスクリプト）。

function npmCli() {
  if (process.env.MD_PREVIEW_TEST_NPM_CLI) return process.env.MD_PREVIEW_TEST_NPM_CLI;
  const candidates = [];
  if (process.env.npm_execpath?.endsWith('npm-cli.js')) candidates.push(process.env.npm_execpath);
  const nodeDirectory = path.dirname(fs.realpathSync(process.execPath));
  candidates.push(
    path.join(nodeDirectory, 'node_modules', 'npm', 'bin', 'npm-cli.js'),
    path.join(nodeDirectory, '..', 'lib', 'node_modules', 'npm', 'bin', 'npm-cli.js'),
  );
  for (const entry of (process.env.PATH || '').split(path.delimiter)) {
    if (!entry) continue;
    for (const name of windows ? ['npm.cmd', 'npm'] : ['npm']) {
      let real;
      try {
        real = fs.realpathSync(path.join(entry, name));
      } catch {
        continue;
      }
      if (real.endsWith('.js')) candidates.push(real);
      candidates.push(
        path.join(path.dirname(real), 'node_modules', 'npm', 'bin', 'npm-cli.js'),
        path.join(path.dirname(real), '..', 'lib', 'node_modules', 'npm', 'bin', 'npm-cli.js'),
      );
    }
  }
  const found = candidates.find((candidate) => fs.existsSync(candidate));
  if (!found) throw new Error('npm (npm-cli.js) が見つかりません。Node.js の npm を入れてください');
  return found;
}

function nodeMajor() {
  return Number(process.versions.node.split('.')[0]);
}

// 起動役の中身。Linux と Git Bash は sh。Windows の PowerShell は mdpreview.ps1、cmd は mdpreview.cmd。
// PowerShell は同じフォルダの .ps1 を .cmd より先に選ぶ。.cmd を通すと、cmd.exe が引数の & や %名前% を解釈してしまう
// (PowerShell から mdpreview.cmd に a&b.md を渡すと、b.md をコマンドとして動かそうとする) ので、PowerShell では .ps1 を使う。
// .ps1 は、setup-notes の手順で実行ポリシーを RemoteSigned にした PowerShell で動く。
// --stdio は標準入力を使うため、$input による文字列化を挟まず、Node.js にそのまま引き継ぐ。
// $MyInvocation.ExpectingInput の分岐は、リダイレクトもパイプと見なして文字を変えてしまうため置かない。
function launchers(installDir, binDir) {
  const preview = path.join(installDir, 'cli.mjs');
  const sh = (file) => `#!/bin/sh\nexec node '${file.replaceAll("'", "'\\''")}' "$@"\n`;
  if (!windows) return [{ file: path.join(binDir, 'mdpreview'), text: sh(preview), mode: 0o755 }];
  const localAppData = process.env.LOCALAPPDATA;
  const underLocalAppData = localAppData && inside(preview, localAppData);
  const relative = underLocalAppData ? path.relative(localAppData, preview) : '';
  // PowerShell 5.1 は BOM の無い .ps1 を ANSI で読むので、BOM を付ける。
  const psPath = underLocalAppData
    ? `(Join-Path $env:LOCALAPPDATA '${relative.replaceAll("'", "''")}')`
    : `'${preview.replaceAll("'", "''")}'`;
  const result = [
    {
      file: path.join(binDir, 'mdpreview.ps1'),
      text:
        '\ufeff# mdpreview の起動役 (PowerShell)。md-preview の導入スクリプト (scripts/setup.mjs) が置いた。\r\n' +
        `$preview = ${psPath}\r\n` +
        '& node $preview @args\r\n' +
        'exit $LASTEXITCODE\r\n',
    },
    { file: path.join(binDir, 'mdpreview'), text: sh(preview.replaceAll('\\', '/')), mode: 0o755 },
  ];
  // cmd は .cmd を OEM のコードページで読み、% を展開するので、ASCII でないパスや % と " を含むパスは書かない
  // (そのときは .cmd を置かない。cmd からは使えないが、PowerShell と Git Bash からは使える)。
  const cmdPath = underLocalAppData ? path.join('%LOCALAPPDATA%', relative) : preview;
  if (!/[^\x20-\x7e]|[%"]/u.test(underLocalAppData ? relative : preview)) {
    result.push({
      file: path.join(binDir, 'mdpreview.cmd'),
      text: `@echo off\r\nnode "${cmdPath}" %*\r\nexit /b %ERRORLEVEL%\r\n`,
    });
  }
  return result;
}

function run(command, args, options) {
  const result = spawnSync(command, args, { encoding: 'utf8', ...options });
  if (result.error) throw result.error;
  return result;
}

// 依存が揃っていること、CLI の版、小さな文書を描けることを確かめる（サーバーは起こさない）。
function selfTest(directory, version) {
  const dependencies = readJson(path.join(directory, 'package.json'))?.dependencies ?? {};
  for (const name of Object.keys(dependencies)) {
    if (!fs.existsSync(path.join(directory, 'node_modules', name))) {
      throw new Error('導入した mdpreview が動きません: 依存 ' + name + ' がありません');
    }
  }
  const shown = run(process.execPath, [path.join(directory, 'cli.mjs'), '--version'], { cwd: directory });
  if (shown.status !== 0 || shown.stdout.trim() !== version) {
    throw new Error('導入した mdpreview が動きません: ' + (shown.stderr || shown.stdout).trim());
  }
  // 描画エンジン (.wasm) と依存を実際に読み、小さな文書を描く。
  const probe =
    "import { renderDocument } from './lib/pipeline.mjs';" +
    "const { html } = renderDocument('# 確認\\n\\n- [x] 済\\n');" +
    "if (!html.includes('task-list-item')) { console.error('描画の結果が違います'); process.exit(1); }";
  const rendered = run(process.execPath, ['--input-type=module', '--eval', probe], { cwd: directory });
  if (rendered.status !== 0) {
    throw new Error('導入した mdpreview で描画できません: ' + (rendered.stderr || rendered.stdout).trim());
  }
}

function remove(directory) {
  fs.rmSync(directory, { recursive: true, force: true, maxRetries: 3 });
}

function processAlive(pid) {
  try {
    process.kill(pid, 0);
    return true;
  } catch (error) {
    return error.code === 'EPERM';
  }
}

// 途中で止まった導入の後片づけ。導入先が無く、退避した前の導入 (<導入先>.old-<番号>) があれば、いちばん新しいものを戻す。
// 残った作業用フォルダ (.new-*) と退避 (.old-*) は消す。
function recover(installDir) {
  const parent = path.dirname(installDir);
  const base = path.basename(installDir);
  const leftovers = fs
    .readdirSync(parent)
    .filter((name) => name.startsWith(`${base}.old-`) || name.startsWith(`${base}.new-`))
    .map((name) => path.join(parent, name));
  const previous = leftovers
    .filter((full) => path.basename(full).startsWith(`${base}.old-`))
    .sort((a, b) => fs.statSync(b).mtimeMs - fs.statSync(a).mtimeMs);
  if (!fs.existsSync(installDir) && previous.length) {
    fs.renameSync(previous[0], installDir);
    console.log(`途中で止まった導入から、前の導入を戻しました: ${installDir}`);
  }
  for (const full of leftovers) remove(full);
}

// 同時に 2 つの導入を動かさないための鍵 (<導入先>.lock)。中に導入しているプロセスの番号を書く。そのプロセスがもう無い
// (途中で止まった導入の) 鍵は、後片づけをしてから取り直す。番号を書く前に止まった鍵は、10 分たてば止まったものとみなす。
function acquireLock(installDir) {
  const lock = `${installDir}.lock`;
  for (let attempt = 0; attempt < 2; attempt++) {
    try {
      fs.mkdirSync(lock);
      fs.writeFileSync(path.join(lock, 'pid'), String(process.pid));
      return lock;
    } catch (error) {
      if (error.code !== 'EEXIST') throw error;
    }
    let owner = 0;
    try {
      owner = Number(fs.readFileSync(path.join(lock, 'pid'), 'utf8'));
    } catch {
      // 番号を書く前の鍵。
    }
    const stale = owner ? !processAlive(owner) : Date.now() - fs.statSync(lock).mtimeMs > 10 * 60 * 1000;
    if (!stale) {
      throw new Error(
        `ほかの導入が動いています${owner ? `（プロセス ${owner}）` : ''}。終わるのを待ってからやり直してください: ${lock}`,
      );
    }
    console.log(`途中で止まった導入の鍵を片づけます: ${lock}`);
    recover(installDir);
    remove(lock);
  }
  throw new Error(`導入の鍵を取れません: ${lock}`);
}

// 起動役は隣の一時ファイルに書いてから置き換える (今の起動役がリンクでも、リンクの先を書き換えない。途中で止まっても、
// 起動役が欠けない)。
function writeLauncher({ file, text, mode }) {
  const temporary = `${file}.tmp-${process.pid}`;
  try {
    fs.writeFileSync(temporary, text, { flag: 'w' });
    if (mode) fs.chmodSync(temporary, mode);
    fs.renameSync(temporary, file);
  } catch (error) {
    fs.rmSync(temporary, { force: true });
    throw error;
  }
}

// 起動役のリンクはリンクとして退避し、リンクの先を読み書きしない。
function launcherState(file) {
  try {
    const info = fs.lstatSync(file);
    if (info.isSymbolicLink()) return { link: fs.readlinkSync(file) };
    return { text: fs.readFileSync(file), mode: info.mode };
  } catch (error) {
    if (error.code === 'ENOENT') return null;
    throw error;
  }
}

function restoreLauncher({ file, before }) {
  fs.rmSync(file, { force: true });
  if (before?.link) fs.symlinkSync(before.link, file);
  else if (before) fs.writeFileSync(file, before.text, { mode: before.mode });
}

// まだ無い末尾のフォルダーも、既にある親の実体からたどる（リンクを通った clone 内も拒む）。
function realLocation(directory) {
  let parent = directory;
  const suffix = [];
  while (!fs.existsSync(parent)) {
    suffix.unshift(path.basename(parent));
    const next = path.dirname(parent);
    if (next === parent) break;
    parent = next;
  }
  return path.resolve(fs.realpathSync(parent), ...suffix);
}

function install({ installDir, binDir }) {
  const realRepository = fs.realpathSync(repository);
  const realInstall = realLocation(installDir);
  const realBin = realLocation(binDir);
  if (
    inside(installDir, repository) ||
    inside(binDir, repository) ||
    inside(realInstall, realRepository) ||
    inside(realBin, realRepository)
  ) {
    throw new Error('--install-dir と --bin-dir はリポジトリの外を指定してください（clone の中に依存を置かない）');
  }
  // 導入先は入れ替えるので、clone を中に含むフォルダは使えない (clone ごと退避して消してしまう)。
  if (inside(repository, installDir) || inside(realRepository, realInstall))
    throw new Error('--install-dir に、clone を含むフォルダは指定できません');
  if (nodeMajor() < minimumNode) throw new Error(`Node.js ${minimumNode} 以上が必要です（今は ${process.version}）`);
  const version = repositoryVersion();
  if (!version) throw new Error(`package.json を読めません: ${repository}`);
  const npm = npmCli();
  const planned = launchers(installDir, binDir);
  fs.mkdirSync(path.dirname(installDir), { recursive: true });
  // 同時に 2 つの導入を動かさない (作業用フォルダと入れ替えがぶつかる)。
  const lock = acquireLock(installDir);
  const staging = `${installDir}.new-${process.pid}`;
  const previous = `${installDir}.old-${process.pid}`;
  let switched = false;
  const changedLaunchers = [];
  try {
    remove(staging);
    fs.mkdirSync(staging);
    for (const name of sourceFiles()) {
      fs.mkdirSync(path.dirname(path.join(staging, name)), { recursive: true });
      fs.copyFileSync(path.join(repository, name), path.join(staging, name));
    }
    console.log(`依存を入れます: ${staging}`);
    const installed = run(process.execPath, [npm, 'ci', '--omit=dev', '--ignore-scripts', '--no-audit', '--no-fund'], {
      cwd: staging,
      stdio: 'inherit',
    });
    if (installed.status !== 0) throw new Error('npm ci に失敗しました（上の出力を確かめてください）');
    selfTest(staging, version);
    fs.writeFileSync(
      path.join(staging, 'install.json'),
      JSON.stringify(
        {
          version,
          installedAt: new Date().toISOString(),
          commit: repositoryCommit(),
          source: repository,
          node: process.version,
        },
        null,
        2,
      ) + '\n',
    );
    // 今の導入を脇へ移してから入れ替える。移せなければ (Windows で使われていれば) 何も変えずに止める。
    if (fs.existsSync(installDir)) {
      try {
        fs.renameSync(installDir, previous);
      } catch (error) {
        if (['EBUSY', 'EPERM', 'EACCES'].includes(error.code)) {
          throw new Error(
            `導入先を入れ替えられません（${error.code}）。プレビューを閉じてからやり直してください: ${installDir}`,
          );
        }
        throw error;
      }
    }
    try {
      fs.renameSync(staging, installDir);
    } catch (error) {
      if (fs.existsSync(previous)) fs.renameSync(previous, installDir);
      throw error;
    }
    switched = true;
    // 起動役も、鍵を持っている間に置く。失敗したら先に置いた起動役と前の導入を戻す。
    fs.mkdirSync(binDir, { recursive: true });
    for (const launcher of planned) {
      const before = launcherState(launcher.file);
      writeLauncher(launcher);
      changedLaunchers.push({ ...launcher, before });
      console.log(`起動役を置きました: ${launcher.file}`);
    }
    if (windows && !planned.some(({ file }) => file.endsWith('.cmd'))) {
      console.log(
        `注意: 導入先のパスに ASCII でない文字か % か " があるので、cmd 用の mdpreview.cmd は置きません（PowerShell と Git Bash からは使えます）`,
      );
    }
    try {
      remove(previous);
    } catch {
      console.log(`注意: 前の導入を消せませんでした。後で消してください: ${previous}`);
    }
  } catch (error) {
    if (switched) {
      try {
        remove(installDir);
        if (fs.existsSync(previous)) fs.renameSync(previous, installDir);
      } finally {
        for (const launcher of changedLaunchers.reverse()) restoreLauncher(launcher);
      }
    }
    throw error;
  } finally {
    // 片づけが失敗しても、元の誤りを消さず、鍵は外す。
    try {
      remove(staging);
    } catch {
      console.log(`注意: 作業用フォルダを消せませんでした。後で消してください: ${staging}`);
    }
    fs.rmSync(lock, { recursive: true, force: true });
  }
}

function check({ installDir, binDir }) {
  const problems = [];
  if (nodeMajor() < minimumNode) problems.push(`Node.js ${minimumNode} 以上が必要です（今は ${process.version}）`);
  const installed = readJson(path.join(installDir, 'install.json'));
  const expected = { version: repositoryVersion(), commit: repositoryCommit() };
  const label = (value) => `${value?.version ?? '?'}${value?.commit ? ` / ${value.commit.slice(0, 12)}` : ''}`;
  if (!installed) {
    problems.push(`導入されていません: ${installDir}`);
  } else {
    if (installed.version !== expected.version || (expected.commit && installed.commit !== expected.commit)) {
      problems.push(`導入した版（${label(installed)}）が clone（${label(expected)}）と違います。導入し直してください`);
    }
    for (const name of sourceFiles()) {
      const file = path.join(installDir, name);
      if (!fs.existsSync(file)) problems.push('導入先に ' + name + ' がありません。導入し直してください');
      else {
        try {
          if (hash(file) !== hash(path.join(repository, name))) {
            problems.push('導入先の ' + name + ' が clone と違います。導入し直してください');
          }
        } catch {
          problems.push('導入先の ' + name + ' を読めません。導入し直してください');
        }
      }
    }
    try {
      selfTest(installDir, expected.version);
    } catch (error) {
      problems.push(error.message.replace(/\s*[\r\n]+\s*/gu, ' '));
    }
  }
  let planned = [];
  try {
    planned = launchers(installDir, binDir);
  } catch (error) {
    problems.push(error.message);
  }
  for (const { file, text } of planned) {
    let actual = null;
    try {
      actual = fs.readFileSync(file, 'utf8');
    } catch {
      // 無いときは下で知らせる。
    }
    if (actual === null) problems.push(`起動役がありません: ${file}`);
    else if (actual !== text) problems.push(`起動役の中身が導入先と合いません。導入し直してください: ${file}`);
  }
  if (!onPath(binDir))
    problems.push(`${binDir} が PATH にありません。PATH に足すか、起動役をフルパスで動かしてください`);
  for (const problem of problems) console.log(`問題: ${problem}`);
  if (!problems.length)
    console.log(`導入済み: mdpreview ${label(installed)}（${planned.map(({ file }) => file).join('・')}）`);
  return problems.length ? 1 : 0;
}

function main() {
  try {
    const options = parseArguments(process.argv.slice(2));
    if (options.help) {
      process.stdout.write(usage);
      return 0;
    }
    if (options.check) return check(options);
    install(options);
    return check(options);
  } catch (error) {
    console.error(`md-preview の導入に失敗しました: ${error.message.replace(/\s*[\r\n]+\s*/gu, ' ')}`);
    return 1;
  }
}

process.exitCode = main();
