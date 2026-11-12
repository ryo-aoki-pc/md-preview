// ブラウザ用の写しと固定情報を更新する。実行時のアプリは node_modules のこれらの依存を使わない。
import { createHash } from 'node:crypto';
import { existsSync, lstatSync, mkdirSync, readdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { dirname, join, relative, resolve } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const repoRoot = fileURLToPath(new URL('../', import.meta.url));
const names = ['katex', 'mermaid'];
const digest = (bytes) => ({ sha256: createHash('sha256').update(bytes).digest('hex'), size: bytes.length });
const validFile = (name, file) =>
  file === 'LICENSE' ||
  (name === 'katex'
    ? ['katex.min.js', 'katex.min.css'].includes(file) || /^fonts\/[a-z0-9_-]+\.woff2$/i.test(file)
    : name === 'mermaid' && file === 'mermaid.min.js');

function validateRecord(name, record) {
  if (
    record?.name !== name || typeof record.version !== 'string' || !record.version ||
    typeof record.integrity !== 'string'
  )
    throw new Error('パッケージの固定情報が正しくありません: ' + name);
  for (const required of [
    'LICENSE',
    ...(name === 'katex' ? ['katex.min.js', 'katex.min.css'] : ['mermaid.min.js']),
  ])
    if (!Object.hasOwn(record.files || {}, required)) throw new Error('必要な写しがありません: ' + name + '/' + required);
  for (const file of Object.keys(record.files))
    if (!validFile(name, file)) throw new Error('許可していない写しです: ' + name + '/' + file);
}

function sourceFiles(modules, name) {
  const root = join(modules, name);
  const pkg = JSON.parse(readFileSync(join(root, 'package.json'), 'utf8'));
  const files = new Map([['LICENSE', readFileSync(join(root, 'LICENSE'))]]);
  if (name === 'katex') {
    for (const file of ['katex.min.js', 'katex.min.css']) files.set(file, readFileSync(join(root, 'dist', file)));
    // CSS は原文のまま。今のブラウザは先頭の woff2 を選ぶため、woff / ttf は写さない。
    for (const font of readdirSync(join(root, 'dist', 'fonts')).sort()) {
      if (/^[a-z0-9_-]+\.woff2$/i.test(font))
        files.set('fonts/' + font, readFileSync(join(root, 'dist', 'fonts', font)));
    }
  } else files.set('mermaid.min.js', readFileSync(join(root, 'dist', 'mermaid.min.js')));
  return { pkg, files };
}

function actualFiles(root, prefix = '') {
  const files = [];
  for (const entry of readdirSync(root, { withFileTypes: true })) {
    const file = prefix + entry.name;
    if (entry.isSymbolicLink()) throw new Error('写しの中にリンクがあります: ' + file);
    if (entry.isDirectory()) files.push(...actualFiles(join(root, entry.name), file + '/'));
    else if (entry.isFile()) files.push(file);
  }
  return files.sort();
}

export function updatePageLibraries({
  root = repoRoot, modules = join(root, 'node_modules'), check = false,
} = {}) {
  const libraryRoot = resolve(root, 'page', 'lib');
  const lockPath = join(libraryRoot, 'lock.json');
  const packageLockPath = join(root, 'package-lock.json');
  const npmLock = existsSync(packageLockPath) ? JSON.parse(readFileSync(packageLockPath, 'utf8')) : null;
  if (existsSync(lockPath)) {
    const lock = JSON.parse(readFileSync(lockPath, 'utf8'));
    if (lock.version !== 1 || JSON.stringify(Object.keys(lock.packages || {}).sort()) !== JSON.stringify(names))
      throw new Error('ライブラリの固定情報の形が正しくありません');
  }
  if (check) {
    if (lstatSync(lockPath).isSymbolicLink()) throw new Error('固定情報がリンクです');
    const present = actualFiles(libraryRoot);
    const lock = JSON.parse(readFileSync(lockPath, 'utf8'));
    const expected = ['lock.json'];
    for (const name of names) {
      const record = lock.packages[name];
      validateRecord(name, record);
      const npm = npmLock?.packages?.['node_modules/' + name];
      if (npm && (record.version !== npm.version || record.integrity !== npm.integrity))
        throw new Error('npm の固定情報と一致しません: ' + name);
      for (const [file, info] of Object.entries(record.files || {})) {
        const actual = digest(readFileSync(join(libraryRoot, name, file)));
        if (actual.sha256 !== info.sha256 || actual.size !== info.size)
          throw new Error('写しのハッシュまたは大きさが一致しません: ' + name + '/' + file);
        expected.push(name + '/' + file);
      }
      if (existsSync(join(modules, name, 'package.json'))) {
        const source = sourceFiles(modules, name);
        if (source.pkg.name !== record.name || source.pkg.version !== record.version)
          throw new Error('導入済みの版と一致しません: ' + name);
        if (JSON.stringify([...source.files.keys()].sort()) !== JSON.stringify(Object.keys(record.files).sort()))
          throw new Error('導入済みのファイルの一覧と一致しません: ' + name);
        for (const [file, bytes] of source.files)
          if (!bytes.equals(readFileSync(join(libraryRoot, name, file))))
            throw new Error('導入済みの原文と写しが一致しません: ' + name + '/' + file);
      }
    }
    if (JSON.stringify(present) !== JSON.stringify(expected.sort()))
      throw new Error('固定情報にないファイルがあります');
    return lock;
  }
  const lock = { version: 1, packages: {} };
  const sources = names.map((name) => {
    const source = sourceFiles(modules, name);
    const npm = npmLock?.packages?.['node_modules/' + name];
    if (!npm || source.pkg.name !== name || source.pkg.version !== npm.version || !npm.integrity)
      throw new Error('導入済みのパッケージと npm の固定情報が一致しません: ' + name);
    return { name, source, npm };
  });
  // page や page/lib がリンクだと、下の作成・削除・書き込みがリンクの先へ行くので、先に確かめて止める。
  for (const part of [resolve(root, 'page'), libraryRoot])
    if (existsSync(part) && lstatSync(part).isSymbolicLink()) throw new Error('写しの置き場所の途中にリンクがあります');
  mkdirSync(libraryRoot, { recursive: true });
  for (const { name, source, npm } of sources) {
    const target = resolve(libraryRoot, name);
    // 消すのは、この道具が管理するフォルダーだけ。絶対パスと包含関係を先に確かめる。
    if (relative(libraryRoot, target) !== name) throw new Error('写しの置き場所が正しくありません');
    if (existsSync(target) && lstatSync(target).isSymbolicLink()) throw new Error('写しの置き場所がリンクです');
    rmSync(target, { recursive: true, force: true });
    const record = {
      name, version: source.pkg.version,
      integrity: npm.integrity, files: {},
    };
    for (const [file, bytes] of [...source.files].sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0))) {
      const destination = join(target, file);
      mkdirSync(dirname(destination), { recursive: true });
      writeFileSync(destination, bytes);
      record.files[file] = digest(bytes);
    }
    lock.packages[name] = record;
  }
  lock.packages = Object.fromEntries(names.map((name) => [name, lock.packages[name]]));
  writeFileSync(lockPath, JSON.stringify(lock, null, 2) + '\n');
  return lock;
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  const args = process.argv.slice(2);
  let check = false, invalid = false;
  for (let i = 0; i < args.length; i++) {
    if (args[i] === '--check' && !check) check = true;
    else invalid = true;
  }
  if (invalid) {
    process.stderr.write('使い方: node tools/update-page-libs.mjs [--check]\n');
    process.exitCode = 2;
  } else {
    try {
      updatePageLibraries({ check });
      process.stdout.write(
        check
          ? 'ブラウザ用ライブラリの写しは一致しています\n'
          : 'ブラウザ用ライブラリの写しと固定情報を更新しました\n',
      );
    } catch (error) {
      process.stderr.write(
        'update-page-libs: ' +
          (error.code ? '必要なファイルを読めません（' + error.code + '）' : String(error.message)).replace(
            /[\r\n]+/g,
            ' ',
          ) +
          '\n',
      );
      process.exitCode = 1;
    }
  }
}
