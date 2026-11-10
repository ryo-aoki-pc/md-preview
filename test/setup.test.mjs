import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import {
  cpSync,
  existsSync,
  lstatSync,
  mkdirSync,
  readFileSync,
  readdirSync,
  realpathSync,
  renameSync,
  rmSync,
  symlinkSync,
  writeFileSync,
} from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import test from 'node:test';
import { temp } from './helpers.mjs';

// 手本と同じく、偽の npm が既存の依存へのリンクを置く。ネットワークは使わない。
// 試験用の clone と導入先を .tests/ に並べ、この作業ツリーの外には書かない。
const repository = fileURLToPath(new URL('../', import.meta.url));
const windows = process.platform === 'win32';
const version = JSON.parse(readFileSync(path.join(repository, 'package.json'), 'utf8')).version;
const fakeNpm = [
  "import fs from 'node:fs';",
  "import path from 'node:path';",
  "fs.writeFileSync('npm-args.json', JSON.stringify(process.argv.slice(2)));",
  "if (process.env.MD_PREVIEW_TEST_NPM_FAIL) { console.error('偽の npm の失敗'); process.exit(1); }",
  "if (!process.env.MD_PREVIEW_TEST_NO_DEPS) fs.symlinkSync(process.env.MD_PREVIEW_TEST_NODE_MODULES, path.join(process.cwd(), 'node_modules'), 'junction');",
].join('\n');

function workspace(t, { assets = false } = {}) {
  const root = temp(t);
  const clone = path.join(root, 'clone');
  mkdirSync(path.join(clone, 'scripts'), { recursive: true });
  cpSync(path.join(repository, 'scripts/setup.mjs'), path.join(clone, 'scripts/setup.mjs'));
  for (const name of [
    'cli.mjs',
    'lib',
    'page',
    'engine/glfm_markdown.wasm',
    'engine/engine.lock.json',
    'engine/licenses',
    'package.json',
    'package-lock.json',
  ]) {
    mkdirSync(path.dirname(path.join(clone, name)), { recursive: true });
    cpSync(path.join(repository, name), path.join(clone, name), { recursive: true });
  }
  // 配布しないものも試験用 clone に置いて、選んだファイルだけを写すことを確かめる。
  for (const name of [
    'engine/src/lib.rs',
    'engine/build.sh',
    'test/test.mjs',
    'tools/tool.mjs',
    'docs/install.md',
    '_ref/reference.txt',
  ]) {
    mkdirSync(path.dirname(path.join(clone, name)), { recursive: true });
    writeFileSync(path.join(clone, name), '配布しない\n');
  }
  if (assets) {
    mkdirSync(path.join(clone, 'assets/nested'), { recursive: true });
    writeFileSync(path.join(clone, 'assets/nested/style.css'), 'body {}\n');
  }
  const npm = path.join(root, 'fake-npm.mjs');
  writeFileSync(npm, fakeNpm);
  const installDir = path.join(root, 'share/md-preview');
  const binDir = path.join(root, 'bin');
  const env = {
    ...process.env,
    MD_PREVIEW_TEST_NPM_CLI: npm,
    MD_PREVIEW_TEST_NODE_MODULES: path.join(repository, 'node_modules'),
    LOCALAPPDATA: path.join(root, 'local-app-data'),
    PATH: binDir + path.delimiter + process.env.PATH,
  };
  delete env.MD_PREVIEW_TEST_NPM_FAIL;
  delete env.MD_PREVIEW_TEST_NO_DEPS;
  return { root, clone, installDir, binDir, env };
}

function setup(space, ...extra) {
  const child = spawnSync(
    process.execPath,
    [
      path.join(space.clone, 'scripts/setup.mjs'),
      '--install-dir',
      space.installDir,
      '--bin-dir',
      space.binDir,
      ...extra,
    ],
    { cwd: space.clone, env: space.env, encoding: 'utf8', timeout: 60000 },
  );
  assert.ifError(child.error);
  return child;
}

function success(child) {
  assert.equal(child.status, 0, child.stdout + child.stderr);
}

function installed(space, name) {
  return path.join(space.installDir, name);
}
function read(file) {
  return readFileSync(file, 'utf8');
}
function leftovers(space) {
  return readdirSync(path.dirname(space.installDir)).filter((name) => name !== 'md-preview');
}

function snapshot(directory) {
  const result = {};
  function visit(folder) {
    for (const name of readdirSync(folder)) {
      const file = path.join(folder, name);
      const info = lstatSync(file);
      // 依存のリンクの先には入らない。
      if (info.isDirectory() && !info.isSymbolicLink()) visit(file);
      else result[path.relative(directory, file)] = { mtime: info.mtimeMs, size: info.size };
    }
  }
  visit(directory);
  return result;
}

test('導入: 実行時のファイルと依存だけを写し、起動役と記録を置いて --check が通る', (t) => {
  const space = workspace(t);
  const child = setup(space);
  success(child);
  assert.match(child.stdout, /導入済み: mdpreview /u);
  const record = JSON.parse(read(installed(space, 'install.json')));
  assert.equal(record.version, version);
  assert.equal(record.source, path.resolve(space.clone));
  assert.equal(record.node, process.version);
  assert.ok(Number.isFinite(Date.parse(record.installedAt)));
  const head = spawnSync('git', ['-C', space.clone, 'rev-parse', 'HEAD'], { encoding: 'utf8' });
  assert.equal(record.commit, head.status === 0 ? head.stdout.trim() : null);
  for (const name of [
    'cli.mjs',
    'lib/pipeline.mjs',
    'page/index.html',
    'engine/glfm_markdown.wasm',
    'engine/engine.lock.json',
    'engine/licenses',
    'package.json',
    'package-lock.json',
  ]) {
    assert.ok(existsSync(installed(space, name)), name);
  }
  for (const name of ['engine/src', 'engine/build.sh', 'test', 'tools', 'docs', '_ref', 'scripts', 'assets']) {
    assert.equal(existsSync(installed(space, name)), false, name);
  }
  assert.deepEqual(JSON.parse(read(installed(space, 'npm-args.json'))), [
    'ci',
    '--omit=dev',
    '--ignore-scripts',
    '--no-audit',
    '--no-fund',
  ]);
  assert.deepEqual(leftovers(space), []);
  const before = snapshot(space.root);
  const check = setup(space, '--check');
  success(check);
  assert.match(check.stdout, /導入済み: mdpreview /u);
  assert.deepEqual(snapshot(space.root), before, '--check はファイルを書き換えない');
  const launcher = path.join(space.binDir, windows ? 'mdpreview.cmd' : 'mdpreview');
  const launched = windows
    ? spawnSync('cmd.exe', ['/d', '/s', '/c', '"' + launcher + '" --version'], {
        env: space.env,
        encoding: 'utf8',
        windowsVerbatimArguments: true,
      })
    : spawnSync(launcher, ['--version'], { env: space.env, encoding: 'utf8' });
  assert.ifError(launched.error);
  assert.equal(launched.status, 0, launched.stderr);
  assert.equal(launched.stdout.trim(), version);
  assert.match(read(path.join(space.binDir, 'mdpreview')), /exec node '.+cli\.mjs' "\$@"/u);
  if (windows) {
    assert.ok(read(launcher).includes('node "' + installed(space, 'cli.mjs') + '" %*'));
    const ps = read(path.join(space.binDir, 'mdpreview.ps1'));
    assert.equal(ps.charCodeAt(0), 0xfeff);
    assert.match(ps, /& node \$preview @args/u);
    assert.doesNotMatch(ps, /ExpectingInput|\$input \|/u);
  } else assert.ok(lstatSync(launcher).mode & 0o111);
});

test('任意の assets は写し、導入先の隣の md-preview-assets は触らない', (t) => {
  const space = workspace(t, { assets: true });
  const assets = path.join(path.dirname(space.installDir), 'md-preview-assets');
  mkdirSync(assets, { recursive: true });
  writeFileSync(path.join(assets, 'marker'), '任意の公式スタイル');
  success(setup(space));
  assert.equal(read(installed(space, 'assets/nested/style.css')), 'body {}\n');
  success(setup(space));
  assert.equal(read(path.join(assets, 'marker')), '任意の公式スタイル');
});

test('再導入は前のファイルを入れ替え、作業用フォルダーと鍵を残さない', (t) => {
  const space = workspace(t);
  success(setup(space));
  writeFileSync(installed(space, 'stale.txt'), '前の導入');
  success(setup(space));
  assert.equal(existsSync(installed(space, 'stale.txt')), false);
  assert.deepEqual(leftovers(space), []);
});

test('--check はファイルの欠落とハッシュの違いを知らせる', (t) => {
  const space = workspace(t, { assets: true });
  success(setup(space));
  rmSync(installed(space, 'lib/pipeline.mjs'));
  writeFileSync(installed(space, 'assets/nested/style.css'), '変わった内容');
  const check = setup(space, '--check');
  assert.equal(check.status, 1);
  assert.match(check.stdout, /問題: 導入先に lib[\\/]pipeline\.mjs がありません/u);
  assert.match(check.stdout, /問題: 導入先の assets[\\/]nested[\\/]style\.css が clone と違います/u);
});

test('--check は依存を読めないことを CLI の --version で知らせる', (t) => {
  const space = workspace(t);
  success(setup(space));
  // junction そのものだけを外し、clone の node_modules は残す。
  rmSync(installed(space, 'node_modules'), { recursive: true });
  const check = setup(space, '--check');
  assert.equal(check.status, 1);
  assert.match(check.stdout, /問題: 導入した mdpreview が動きません/u);
  assert.ok(existsSync(path.join(repository, 'node_modules')));
});

test('描画エンジンが壊れていれば、入れ替える前に止め、前の導入を残す', (t) => {
  const space = workspace(t);
  success(setup(space));
  writeFileSync(installed(space, 'marker'), '前の導入');
  writeFileSync(path.join(space.clone, 'engine/glfm_markdown.wasm'), '壊れた .wasm');
  const failed = setup(space);
  assert.equal(failed.status, 1);
  assert.match(failed.stderr, /md-preview の導入に失敗しました: 導入した mdpreview で描画できません/u);
  assert.equal(read(installed(space, 'marker')), '前の導入');
  assert.deepEqual(leftovers(space), []);
});

test('--check は起動役の欠落・書き換え・PATH を知らせ、何も作らない', (t) => {
  const space = workspace(t);
  const missing = setup({ ...space, env: { ...space.env, PATH: process.env.PATH } }, '--check');
  assert.equal(missing.status, 1);
  assert.match(missing.stdout, /問題: 導入されていません/u);
  assert.match(missing.stdout, /問題: 起動役がありません/u);
  assert.match(missing.stdout, /PATH にありません/u);
  assert.equal(existsSync(path.dirname(space.installDir)), false);
  assert.equal(existsSync(space.binDir), false);
  success(setup(space));
  writeFileSync(path.join(space.binDir, 'mdpreview'), '違う起動役');
  const changed = setup(space, '--check');
  assert.equal(changed.status, 1);
  assert.match(changed.stdout, /起動役の中身が導入先と合いません/u);
});

for (const [title, env] of [
  ['npm の失敗', { MD_PREVIEW_TEST_NPM_FAIL: '1' }],
  ['導入後の動作確認の失敗', { MD_PREVIEW_TEST_NO_DEPS: '1' }],
])
  test(title + 'では、前の導入と起動役を残す', (t) => {
    const space = workspace(t);
    success(setup(space));
    writeFileSync(installed(space, 'marker'), '前の導入');
    const record = read(installed(space, 'install.json'));
    const launcher = read(path.join(space.binDir, 'mdpreview'));
    const failed = setup({ ...space, env: { ...space.env, ...env } });
    assert.equal(failed.status, 1);
    assert.match(failed.stderr, /md-preview の導入に失敗しました:/u);
    assert.equal(
      failed.stderr.split(/\r?\n/u).filter((line) => line.startsWith('md-preview の導入に失敗しました:')).length,
      1,
      '自分の失敗は日本語 1 行で知らせる',
    );
    assert.equal(read(installed(space, 'install.json')), record);
    assert.equal(read(installed(space, 'marker')), '前の導入');
    assert.equal(read(path.join(space.binDir, 'mdpreview')), launcher);
    assert.deepEqual(leftovers(space), []);
  });

test('起動役を置けないときも、入れ替えた前の導入と起動役を戻す', (t) => {
  const space = workspace(t);
  success(setup(space));
  writeFileSync(installed(space, 'marker'), '前の導入');
  const launcher = path.join(space.binDir, windows ? 'mdpreview.cmd' : 'mdpreview');
  rmSync(launcher);
  mkdirSync(launcher);
  const before = windows ? read(path.join(space.binDir, 'mdpreview.ps1')) : null;
  const failed = setup(space);
  assert.equal(failed.status, 1);
  assert.equal(read(installed(space, 'marker')), '前の導入');
  assert.ok(lstatSync(launcher).isDirectory());
  if (windows) assert.equal(read(path.join(space.binDir, 'mdpreview.ps1')), before);
  assert.deepEqual(leftovers(space), []);
});

test('ほかの導入の鍵があれば、何も変えずに止める', (t) => {
  const space = workspace(t);
  mkdirSync(space.installDir + '.lock', { recursive: true });
  writeFileSync(path.join(space.installDir + '.lock', 'pid'), String(process.pid));
  const blocked = setup(space);
  assert.equal(blocked.status, 1);
  assert.match(blocked.stderr, /ほかの導入が動いています（プロセス \d+）/u);
  assert.equal(existsSync(space.installDir), false);
  assert.equal(existsSync(space.binDir), false);
});

test('途中で止まった導入の鍵と退避を回収し、失敗した再導入でも前の導入を残す', (t) => {
  const space = workspace(t);
  success(setup(space));
  writeFileSync(installed(space, 'marker'), '前の導入');
  renameSync(space.installDir, space.installDir + '.old-99999');
  mkdirSync(space.installDir + '.lock');
  writeFileSync(path.join(space.installDir + '.lock', 'pid'), '2147483646');
  mkdirSync(space.installDir + '.new-99999');
  const child = setup({ ...space, env: { ...space.env, MD_PREVIEW_TEST_NPM_FAIL: '1' } });
  assert.equal(child.status, 1);
  assert.match(child.stdout, /途中で止まった導入から、前の導入を戻しました/u);
  assert.equal(read(installed(space, 'marker')), '前の導入');
  assert.deepEqual(leftovers(space), []);
  success(setup(space));
});

test('clone の中・clone を含む親・clone 内の起動役の指定を拒む', (t) => {
  const space = workspace(t);
  for (const installDir of [path.join(space.clone, 'installed'), path.join(space.clone, '..backup')]) {
    const child = setup({ ...space, installDir });
    assert.equal(child.status, 1);
    assert.match(child.stderr, /リポジトリの外/u);
    assert.equal(existsSync(installDir), false);
  }
  const ancestor = setup({ ...space, installDir: space.root });
  assert.equal(ancestor.status, 1);
  assert.match(ancestor.stderr, /clone を含むフォルダ/u);
  const bin = setup({ ...space, binDir: path.join(space.clone, 'bin') });
  assert.equal(bin.status, 1);
  assert.match(bin.stderr, /リポジトリの外/u);
  assert.ok(existsSync(path.join(space.clone, 'cli.mjs')));
});

test('リンクで clone 内へ向かう導入先も拒む', (t) => {
  const space = workspace(t);
  const link = path.join(space.root, 'link');
  try {
    symlinkSync(space.clone, link, 'junction');
  } catch (error) {
    if (error.code === 'EPERM') return t.skip('この環境ではフォルダーへのリンクを作れない');
    throw error;
  }
  const child = setup({ ...space, installDir: path.join(link, 'installed') });
  assert.equal(child.status, 1);
  assert.match(child.stderr, /リポジトリの外/u);
});

test('起動役がリンクでも、リンク先を書き換えずに起動役そのものを置く', (t) => {
  const space = workspace(t);
  mkdirSync(space.binDir, { recursive: true });
  const target = path.join(space.root, 'target.txt');
  writeFileSync(target, 'リンクの先');
  const launcher = path.join(space.binDir, windows ? 'mdpreview.cmd' : 'mdpreview');
  try {
    symlinkSync(target, launcher, 'file');
  } catch (error) {
    if (error.code === 'EPERM') return t.skip('この環境ではファイルへのリンクを作れない');
    throw error;
  }
  success(setup(space));
  assert.equal(read(target), 'リンクの先');
  assert.equal(lstatSync(launcher).isSymbolicLink(), false);
});

for (const args of [
  ['--unknown'],
  ['--install-dir'],
  ['--bin-dir'],
  ['--install-dir', '--check'],
  ['--bin-dir', '--check'],
]) {
  test('引数の誤りを 1 行で知らせる: ' + args.join(' '), (t) => {
    const space = workspace(t);
    const child = setup(space, ...args);
    assert.equal(child.status, 1);
    assert.match(child.stderr, /md-preview の導入に失敗しました:/u);
    assert.equal(child.stderr.trim().split(/\r?\n/u).length, 1);
    assert.equal(existsSync(space.installDir), false);
    assert.equal(existsSync(space.binDir), false);
  });
}

test('--help は既定の導入先と起動役を知らせ、空の環境変数は未設定として扱う', (t) => {
  const space = workspace(t);
  const env = { ...space.env, LOCALAPPDATA: '', XDG_DATA_HOME: '' };
  const child = setup({ ...space, env }, '--help');
  success(child);
  const base = windows
    ? path.join(os.homedir(), 'AppData/Local')
    : process.platform === 'darwin'
      ? path.join(os.homedir(), 'Library/Application Support')
      : path.join(os.homedir(), '.local/share');
  assert.ok(child.stdout.includes(path.join(base, 'md-preview')));
  assert.ok(child.stdout.includes(path.join(os.homedir(), '.local/bin')));
  assert.equal(existsSync(space.installDir), false);
});

test(
  'Windows の既定の導入先は LOCALAPPDATA を参照する起動役にする',
  { skip: windows ? false : 'Windows の起動役だけの試験' },
  (t) => {
    const space = workspace(t);
    const child = spawnSync(
      process.execPath,
      [path.join(space.clone, 'scripts/setup.mjs'), '--bin-dir', space.binDir],
      { env: space.env, encoding: 'utf8', timeout: 60000 },
    );
    success(child);
    assert.match(read(path.join(space.binDir, 'mdpreview.ps1')), /Join-Path \$env:LOCALAPPDATA/u);
    assert.match(read(path.join(space.binDir, 'mdpreview.cmd')), /%LOCALAPPDATA%/u);
  },
);

test(
  'Windows の ASCII 外の独自パスでは手本と同じく cmd 用を省き、PowerShell と Git Bash 用を置く',
  { skip: windows ? false : 'Windows のコードページに関する試験' },
  (t) => {
    const space = workspace(t);
    const installDir = path.join(space.root, '導入先/md-preview');
    const child = setup({ ...space, installDir });
    success(child);
    assert.match(child.stdout, /mdpreview\.cmd は置きません/u);
    assert.equal(existsSync(path.join(space.binDir, 'mdpreview.cmd')), false);
    assert.ok(read(path.join(space.binDir, 'mdpreview.ps1')).includes(path.join(installDir, 'cli.mjs')));
    assert.ok(existsSync(path.join(space.binDir, 'mdpreview')));
  },
);

test(
  'PowerShell の起動役は標準入力のバイトと引数と終了コードを引き継ぐ',
  { skip: windows ? false : 'Windows の PowerShell の起動役だけの試験' },
  async (t) => {
    const space = workspace(t);
    success(setup(space));
    writeFileSync(
      installed(space, 'cli.mjs'),
      [
        "import fs from 'node:fs';",
        "console.log(JSON.stringify({ args: process.argv.slice(2), input: fs.readFileSync(0).toString('base64') }));",
        'process.exitCode = 7;',
      ].join('\n'),
    );
    const launcher = path.join(space.binDir, 'mdpreview.ps1');
    const input = Buffer.from('標準入力\u0000\r\n{"type":"hello","protocol":1}\n');
    for (const shell of ['pwsh.exe', 'powershell.exe'])
      await t.test(shell, (t) => {
        const child = spawnSync(
          shell,
          ['-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', launcher, '--stdio', 'a&b %TEMP% x.md'],
          { env: space.env, input, encoding: 'utf8', timeout: 15000 },
        );
        if (child.error?.code === 'ENOENT') return t.skip(shell + ' が無い');
        assert.ifError(child.error);
        assert.equal(child.status, 7, child.stdout + child.stderr);
        assert.deepEqual(JSON.parse(child.stdout), {
          args: ['--stdio', 'a&b %TEMP% x.md'],
          input: input.toString('base64'),
        });
      });
  },
);
