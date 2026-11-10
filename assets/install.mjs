import { createHash, randomUUID } from 'node:crypto';
import { createReadStream } from 'node:fs';
import { mkdir, readFile, writeFile, rename, rm, lstat, realpath, readdir, open } from 'node:fs/promises';
import { dirname, join, resolve, posix } from 'node:path';
import { fileURLToPath } from 'node:url';
import { defaultAssetsDirectory } from './directory.mjs';
import { createGunzip } from 'node:zlib';

const defaultLock = fileURLToPath(new URL('./assets.lock.json', import.meta.url));
const digest = (bytes) => createHash('sha256').update(bytes).digest('hex');
const allowedMime = new Set([
  'text/css',
  'text/plain',
  'application/json',
  'font/woff2',
  'image/svg+xml',
  'image/png',
  'image/gif',
]);
const hex = /^[a-f0-9]{64}$/;

function fail(message) {
  throw new Error(message);
}
function relativePath(value) {
  if (
    typeof value !== 'string' ||
    !value ||
    value.includes('\\') ||
    value.includes(':') ||
    value.startsWith('/') ||
    value
      .split('/')
      .some(
        (part) =>
          !part ||
          part === '.' ||
          part === '..' ||
          /[\x00-\x20<>"|?*]/.test(part) ||
          /^(con|prn|aux|nul|com[1-9]|lpt[1-9])(?:\.|$)/i.test(part) ||
          /[. ]$/.test(part),
      )
  ) {
    fail('資産の相対パスが不正です。');
  }
  return value;
}
function checkedBytes(bytes, record, label) {
  if (bytes.length !== record.size || digest(bytes) !== record.sha256)
    fail(`${label} のサイズまたはSHA256が一致しません。`);
  return bytes;
}
function checkRecord(record) {
  if (!record || !hex.test(record.sha256) || !Number.isSafeInteger(record.size) || record.size < 0)
    fail('取得するファイルの固定値が不正です。');
}

export function parseLock(bytes) {
  const lock = JSON.parse(bytes.toString('utf8'));
  if (lock.schema !== 1 || !/^[0-9]+\.[0-9]+\.[0-9]+$/.test(lock.version) || !lock.files || !lock.styles)
    fail('資産ロックの形式が不正です。');
  checkRecord(lock.archive);
  if (lock.archive.registry) {
    if (
      lock.archive.registry !== 'https://registry.gitlab.com' ||
      lock.archive.repository !== 'gitlab-org/gitlab-foss/gitlab-assets-ce' ||
      !hex.test(lock.archive.manifest_sha256) ||
      !hex.test(lock.archive.index_sha256)
    )
      fail('CE公式レジストリの固定値が不正です。');
  } else {
    const url = new URL(lock.archive.url);
    // 置換ロックによる試験は、資格情報を使わないループバックHTTPでも行える。
    if (url.protocol !== 'https:' && !(url.protocol === 'http:' && ['127.0.0.1', '[::1]'].includes(url.hostname)))
      fail('取得元URLはHTTPSが必要です。');
    if (url.username || url.password) fail('取得元URLに認証情報を指定できません。');
  }
  const entries = Object.entries(lock.files);
  if (!entries.length || entries.length > 500) fail('資産の個数が不正です。');
  const names = new Set(),
    archiveNames = new Set();
  for (const [path, record] of entries) {
    relativePath(path);
    if (path === 'install.json' || names.has(path.toLowerCase())) fail('重複した資産名です。');
    names.add(path.toLowerCase());
    checkRecord(record);
    if (!allowedMime.has(record.mime) || !Array.isArray(record.licenses) || !record.licenses.length)
      fail(`${path} の種別またはライセンス対応がありません。`);
    if (Boolean(record.archive) === Boolean(record.url)) fail(`${path} の取得元が不正です。`);
    if (record.archive) {
      relativePath(record.archive);
      if (archiveNames.has(record.archive)) fail('アーカイブ内の同じファイルを重複して指定できません。');
      archiveNames.add(record.archive);
    } else {
      const url = new URL(record.url);
      if (url.protocol !== 'https:' && !(url.protocol === 'http:' && ['127.0.0.1', '[::1]'].includes(url.hostname)))
        fail('原文の取得元はHTTPSが必要です。');
      if (url.username || url.password) fail('原文の取得元URLに認証情報を指定できません。');
    }
    if (record.mime === 'text/css') {
      if (!hex.test(record.output_sha256) || !Number.isSafeInteger(record.output_size) || record.output_size < 0)
        fail(`${path} の変換後の固定値がありません。`);
    }
  }
  for (const [path, record] of entries) {
    if (entries.some(([other]) => other !== path && other.startsWith(`${path}/`)))
      fail('ファイルとディレクトリの名前が衝突しています。');
    for (const license of record.licenses)
      if (!lock.files[license] || !['text/plain', 'application/json'].includes(lock.files[license].mime))
        fail(`${path} のライセンス原文が固定されていません。`);
  }
  for (const theme of ['light', 'dark']) {
    if (
      !Array.isArray(lock.styles[theme]) ||
      !lock.styles[theme].length ||
      lock.styles[theme].some((path) => lock.files[path]?.mime !== 'text/css')
    )
      fail('テーマのCSS一覧が不正です。');
  }
  if (lock.files[lock.sprite]?.mime !== 'image/svg+xml') fail('SVGスプライトが固定されていません。');
  const lockSha256 = digest(bytes);
  return { lock, lockSha256, id: `${lock.version}-${lockSha256.slice(0, 12)}` };
}

// コメント・文字列の中を変更せず、CSSのurl()だけを読む。
export function rewriteCss(css, path, files, visit = () => {}) {
  let result = '',
    at = 0;
  const quotedEnd = (start) => {
    const quote = css[start];
    let end = start + 1;
    for (; end < css.length; end++) {
      if (css[end] === '\\') end++;
      else if (css[end] === quote) return end + 1;
    }
    fail(`${path} のCSS文字列が閉じていません（位置 ${start}）。`);
  };
  while (at < css.length) {
    if (css.startsWith('/*', at)) {
      const end = css.indexOf('*/', at + 2);
      if (end < 0) fail('CSSのコメントが閉じていません。');
      result += css.slice(at, end + 2);
      at = end + 2;
      continue;
    }
    if (css[at] === '\\') {
      result += css.slice(at, at + 2);
      at += 2;
      continue;
    }
    if (css[at] === '"' || css[at] === "'") {
      const end = quotedEnd(at);
      result += css.slice(at, end);
      at = end;
      continue;
    }
    const match = /^(url)\s*\(/i.exec(css.slice(at, at + 16));
    if (!match || (at && /[\w-]/.test(css[at - 1]))) {
      result += css[at++];
      continue;
    }
    let cursor = at + match[0].length;
    while (/\s/.test(css[cursor] || '') && cursor < css.length) cursor++;
    let value;
    if (css[cursor] === '"' || css[cursor] === "'") {
      const end = quotedEnd(cursor);
      value = css.slice(cursor + 1, end - 1);
      cursor = end;
    } else {
      const start = cursor;
      while (cursor < css.length && css[cursor] !== ')') {
        if (css[cursor] === '\\') cursor++;
        cursor++;
      }
      value = css.slice(start, cursor).trim();
    }
    while (cursor < css.length && /\s/.test(css[cursor])) cursor++;
    if (css[cursor] !== ')') fail('CSSのurl()が閉じていません。');
    const end = cursor + 1;
    value = value.replace(/\\([a-f0-9]{1,6}\s?|\r?\n|.)/gi, (_, escape) =>
      /^[a-f0-9]/i.test(escape) ? String.fromCodePoint(parseInt(escape.trim(), 16)) : /\n/.test(escape) ? '' : escape,
    );
    if (/^(data:|#)/i.test(value)) {
      result += css.slice(at, end);
      at = end;
      continue;
    }
    const source = files[path]?.archive?.replace(/^assets\//, '') || path;
    const url = new URL(value, `https://asset.invalid/${source}`);
    if (url.origin !== 'https://asset.invalid') fail(`${path} に外部資産への参照があります。`);
    const target = decodeURIComponent(url.pathname.replace(/^\/(?:assets\/)?/, ''));
    relativePath(target);
    visit(target);
    if (!files[target]) fail(`${path} が参照する資産がロックにありません: ${target}`);
    let relative = posix.relative(posix.dirname(path), target);
    if (!relative.startsWith('.')) relative = `./${relative}`;
    result += `url("${relative}${url.search}${url.hash}")`;
    at = end;
  }
  return result;
}

async function response(url, options, signal) {
  const res = await fetch(url, { ...options, signal });
  if (!res.ok) fail(`取得に失敗しました（HTTP ${res.status}）。`);
  return res;
}
async function smallDownload(record, signal, max = 16 * 1024 * 1024) {
  const res = await response(record.url, {}, signal);
  const chunks = [];
  let size = 0;
  for await (const chunk of res.body) {
    size += chunk.length;
    if (size > Math.min(record.size, max)) {
      await res.body.cancel().catch(() => {});
      fail('取得した原文が固定サイズを超えています。');
    }
    chunks.push(Buffer.from(chunk));
  }
  return checkedBytes(Buffer.concat(chunks), record, '原文');
}
async function registryLayer(archive, signal) {
  const auth = await response(
    `https://gitlab.com/jwt/auth?service=container_registry&scope=repository:${archive.repository}:pull`,
    {},
    signal,
  );
  // 公開pull専用の短命な応答だけを使い、利用者の環境変数や認証ファイルは読まない。
  const session = await auth.json();
  if (typeof session.token !== 'string') fail('公開レジストリの応答が不正です。');
  const headers = { Authorization: `Bearer ${session.token}`, Accept: 'application/vnd.oci.image.manifest.v1+json' };
  const url = `${archive.registry}/v2/${archive.repository}`;
  const indexResponse = await response(
    `${url}/manifests/sha256:${archive.index_sha256}`,
    {
      headers: { ...headers, Accept: 'application/vnd.oci.image.index.v1+json' },
    },
    signal,
  );
  const indexBytes = Buffer.from(await indexResponse.arrayBuffer());
  if (digest(indexBytes) !== archive.index_sha256) fail('公式indexのSHA256が一致しません。');
  const index = JSON.parse(indexBytes);
  if (
    !index.manifests?.some(
      (entry) =>
        entry.digest === `sha256:${archive.manifest_sha256}` &&
        entry.platform?.os === 'linux' &&
        entry.platform?.architecture === 'amd64',
    )
  )
    fail('公式indexの構成が固定値と異なります。');
  const res = await response(`${url}/manifests/sha256:${archive.manifest_sha256}`, { headers }, signal);
  const bytes = Buffer.from(await res.arrayBuffer());
  if (digest(bytes) !== archive.manifest_sha256) fail('公式manifestのSHA256が一致しません。');
  const manifest = JSON.parse(bytes);
  if (
    manifest.layers?.length !== 1 ||
    manifest.layers[0].mediaType !== 'application/vnd.oci.image.layer.v1.tar+gzip' ||
    manifest.layers[0].digest !== `sha256:${archive.sha256}` ||
    manifest.layers[0].size !== archive.size
  )
    fail('公式manifestのレイヤーが固定値と異なります。');
  return response(`${url}/blobs/sha256:${archive.sha256}`, { headers }, signal);
}
async function downloadArchive(archive, target, signal) {
  const res = archive.registry ? await registryLayer(archive, signal) : await response(archive.url, {}, signal);
  const handle = await open(target, 'wx');
  const hash = createHash('sha256');
  let size = 0;
  try {
    for await (const chunk of res.body) {
      signal?.throwIfAborted();
      size += chunk.length;
      if (size > archive.size) fail('資産アーカイブが固定サイズを超えています。');
      hash.update(chunk);
      await handle.writeFile(chunk);
    }
  } finally {
    await handle.close();
  }
  if (size !== archive.size || hash.digest('hex') !== archive.sha256)
    fail('資産アーカイブのサイズまたはSHA256が一致しません。');
}

// tarを一度だけ順に読み、固定した通常ファイル以外は書き出さない。展開用のシェルは使わない。
export async function extractSelected(archive, files, directory, signal) {
  const input = createReadStream(archive),
    stream = createGunzip();
  input.on('error', (error) => stream.destroy(error));
  input.pipe(stream);
  const iterator = stream[Symbol.asyncIterator]();
  let chunk = Buffer.alloc(0),
    offset = 0,
    consumed = 0;
  async function read(size, save = true) {
    const parts = [];
    let remaining = size;
    while (remaining) {
      signal?.throwIfAborted();
      if (offset === chunk.length) {
        const next = await iterator.next();
        if (next.done) fail('tarアーカイブが途中で終わっています。');
        chunk = next.value;
        offset = 0;
      }
      const length = Math.min(remaining, chunk.length - offset);
      if (save) parts.push(chunk.subarray(offset, offset + length));
      offset += length;
      remaining -= length;
      consumed += length;
      if (consumed > 16 * 1024 ** 3) fail('展開サイズの上限を超えています。');
    }
    return save ? Buffer.concat(parts, size) : null;
  }
  const text = (bytes) => bytes.toString('utf8').replace(/\0.*$/s, '');
  const numeric = (bytes) => {
    if (bytes[0] & 0x80) {
      let n = BigInt(bytes[0] & 0x7f);
      for (const b of bytes.subarray(1)) n = n * 256n + BigInt(b);
      if (n > BigInt(Number.MAX_SAFE_INTEGER)) fail('tarのサイズが大きすぎます。');
      return Number(n);
    }
    const value = text(bytes).trim();
    if (!/^[0-7]*$/.test(value)) fail('tarの数値が不正です。');
    return parseInt(value || '0', 8);
  };
  const selected = new Map(
    Object.entries(files)
      .filter(([, r]) => r.archive)
      .map(([path, record]) => [record.archive, { path, record }]),
  );
  const found = new Set();
  let attributes = {},
    global = {},
    longName;
  try {
    while (true) {
      const header = await read(512);
      if (header.every((byte) => byte === 0)) break;
      let sum = 0;
      for (let i = 0; i < 512; i++) sum += i >= 148 && i < 156 ? 32 : header[i];
      if (sum !== numeric(header.subarray(148, 156))) fail('tarヘッダーの検査値が一致しません。');
      let name = text(header.subarray(0, 100));
      const prefix = text(header.subarray(345, 500));
      if (prefix) name = `${prefix}/${name}`;
      const type = String.fromCharCode(header[156]);
      let size = numeric(header.subarray(124, 136));
      if (type === 'x' || type === 'g' || type === 'L') {
        if (size > 1024 * 1024) fail('tarの拡張ヘッダーが大きすぎます。');
        const data = await read(size);
        if (type === 'L') longName = text(data).replace(/\n$/, '');
        else {
          const fields = {};
          let cursor = 0;
          while (cursor < data.length) {
            const space = data.indexOf(32, cursor);
            const length = Number(data.subarray(cursor, space).toString());
            if (
              space < cursor ||
              !Number.isSafeInteger(length) ||
              length <= space - cursor + 1 ||
              cursor + length > data.length
            )
              fail('PAXヘッダーが不正です。');
            const entry = data.subarray(space + 1, cursor + length - 1).toString('utf8');
            const equals = entry.indexOf('=');
            if (equals < 1) fail('PAX属性が不正です。');
            fields[entry.slice(0, equals)] = entry.slice(equals + 1);
            cursor += length;
          }
          if (type === 'g') global = { ...global, ...fields };
          else attributes = fields;
        }
        await read((512 - (size % 512)) % 512, false);
        continue;
      }
      const attrs = { ...global, ...attributes };
      name = attrs.path || longName || name;
      if (attrs.size !== undefined) {
        size = Number(attrs.size);
        if (!Number.isSafeInteger(size) || size < 0) fail('PAXサイズが不正です。');
      }
      attributes = {};
      longName = undefined;
      const match = selected.get(name.replace(/^\.\//, ''));
      if (match) {
        if (!['0', '\0'].includes(type) || found.has(match.path))
          fail('固定した資産が通常ファイルでないか重複しています。');
        if (size !== match.record.size || size > 32 * 1024 * 1024) fail('固定した資産のサイズが一致しません。');
        const bytes = checkedBytes(await read(size), match.record, match.path);
        const target = join(directory, ...match.path.split('/'));
        await mkdir(dirname(target), { recursive: true });
        await writeFile(target, bytes, { flag: 'wx' });
        found.add(match.path);
      } else await read(size, false);
      await read((512 - (size % 512)) % 512, false);
    }
    // gzip末尾のCRC検証まで消費する。
    for await (const ignored of { [Symbol.asyncIterator]: () => iterator }) {
      signal?.throwIfAborted();
      consumed += ignored.length;
      if (consumed > 16 * 1024 ** 3) fail('展開サイズの上限を超えています。');
    }
    if (found.size !== selected.size) fail('固定した資産がアーカイブにありません。');
  } finally {
    input.destroy();
    stream.destroy();
  }
}

async function safeDirectory(parent, name) {
  const path = join(parent, name);
  await mkdir(path, { recursive: true });
  if (!(await lstat(path)).isDirectory() || (await realpath(path)) !== path)
    fail('導入先にシンボリックリンクがあります。');
  return path;
}
async function takeLock(root, id) {
  const path = join(root, `.lock-${id}`);
  for (let attempt = 0; attempt < 3; attempt++) {
    try {
      await mkdir(path);
      await writeFile(join(path, 'owner.json'), JSON.stringify({ pid: process.pid }));
      return async () => rm(path, { recursive: true, force: true });
    } catch (error) {
      if (error.code !== 'EEXIST') throw error;
      if (!(await lstat(path)).isDirectory() || (await realpath(path)) !== path) fail('導入ロックが不正です。');
      let owner;
      try {
        owner = JSON.parse(await readFile(join(path, 'owner.json'), 'utf8'));
      } catch {
        fail('別の導入処理が実行中か、導入ロックが残っています。');
      }
      if (!Number.isSafeInteger(owner.pid) || owner.pid < 1) fail('導入ロックが不正です。');
      try {
        process.kill(owner.pid, 0);
        fail('同じ版の導入処理が実行中です。');
      } catch (alive) {
        if (alive.code !== 'ESRCH') throw alive;
      }
      const stale = join(root, `.stale-${randomUUID()}`);
      try {
        await rename(path, stale);
        await rm(stale, { recursive: true, force: true });
      } catch (race) {
        if (race.code !== 'ENOENT') throw race;
      }
    }
  }
  fail('導入ロックを取得できませんでした。');
}
function outputRecord(record) {
  return { sha256: record.output_sha256 || record.sha256, size: record.output_size ?? record.size, mime: record.mime };
}
async function verifyInstallation(directory, info) {
  try {
    const stat = await lstat(directory);
    if (!stat.isDirectory() || stat.isSymbolicLink()) return false;
    const manifest = JSON.parse(await readFile(join(directory, 'install.json'), 'utf8'));
    const expected = Object.fromEntries(
      Object.entries(info.lock.files).map(([path, record]) => [path, outputRecord(record)]),
    );
    if (
      manifest.schema !== 1 ||
      manifest.id !== info.id ||
      manifest.version !== info.lock.version ||
      manifest.lock_sha256 !== info.lockSha256 ||
      JSON.stringify(manifest.files) !== JSON.stringify(expected) ||
      JSON.stringify(manifest.styles) !== JSON.stringify(info.lock.styles) ||
      manifest.sprite !== info.lock.sprite
    )
      return false;
    const walk = async (dir, prefix = '') => {
      for (const entry of await readdir(dir, { withFileTypes: true })) {
        const key = prefix + entry.name;
        if (entry.isSymbolicLink()) return false;
        if (entry.isDirectory()) {
          if (!(await walk(join(dir, entry.name), `${key}/`))) return false;
        } else {
          if (!entry.isFile()) return false;
          if (key === 'install.json') continue;
          const record = expected[key];
          if (!record) return false;
          checkedBytes(await readFile(join(dir, entry.name)), record, key);
        }
      }
      return true;
    };
    if (!(await walk(directory))) return false;
    for (const path of Object.keys(expected))
      if (!(await lstat(join(directory, ...path.split('/')))).isFile()) return false;
    return true;
  } catch {
    return false;
  }
}

export async function install({
  dataDir = defaultAssetsDirectory(),
  lockPath = defaultLock,
  signal,
  archivePath,
  log = () => {},
}) {
  if (!dataDir) fail('保存先が必要です。');
  const info = parseLock(await readFile(lockPath));
  await mkdir(resolve(dataDir), { recursive: true });
  const base = await realpath(resolve(dataDir));
  const root = base;
  const release = await takeLock(root, info.id);
  const target = join(root, info.id);
  let stage;
  try {
    // 強制終了で残った同じ導入IDの一時領域だけを、排他取得後に片付ける。
    for (const entry of await readdir(root, { withFileTypes: true })) {
      if (
        entry.isDirectory() &&
        entry.name.startsWith(`.stage-${info.id}-`) &&
        /^[a-f0-9-]{36}$/.test(entry.name.slice(`.stage-${info.id}-`.length))
      ) {
        const abandoned = join(root, entry.name);
        if ((await realpath(abandoned)) === abandoned) await rm(abandoned, { recursive: true, force: true });
      }
    }
    if (await verifyInstallation(target, info)) {
      log('導入済みの公式資産を確認しました。');
      return target;
    }
    stage = await safeDirectory(root, `.stage-${info.id}-${randomUUID()}`);
    const archive = join(stage, '.archive.tar.gz');
    if (archivePath) {
      // 実物の初回試験だけで使用する。CLIから既存ファイルの検査を省略できない。
      const bytesHash = createHash('sha256');
      let size = 0;
      for await (const bytes of createReadStream(archivePath)) {
        size += bytes.length;
        bytesHash.update(bytes);
      }
      if (size !== info.lock.archive.size || bytesHash.digest('hex') !== info.lock.archive.sha256)
        fail('試験アーカイブの固定値が一致しません。');
    } else {
      log(`公式資産を取得します（${info.lock.archive.size.toLocaleString('ja-JP')} バイト）。`);
      await downloadArchive(info.lock.archive, archive, signal);
    }
    await extractSelected(archivePath || archive, info.lock.files, stage, signal);
    if (!archivePath) await rm(archive);
    for (const [path, record] of Object.entries(info.lock.files)) {
      signal?.throwIfAborted();
      const destination = join(stage, ...path.split('/'));
      if (record.url) {
        await mkdir(dirname(destination), { recursive: true });
        await writeFile(destination, await smallDownload(record, signal), { flag: 'wx' });
      }
      if (record.mime === 'text/css') {
        const bytes = Buffer.from(rewriteCss(await readFile(destination, 'utf8'), path, info.lock.files));
        checkedBytes(bytes, outputRecord(record), path);
        await writeFile(destination, bytes);
      }
    }
    const manifest = {
      schema: 1,
      id: info.id,
      version: info.lock.version,
      lock_sha256: info.lockSha256,
      files: Object.fromEntries(Object.entries(info.lock.files).map(([path, record]) => [path, outputRecord(record)])),
      styles: info.lock.styles,
      sprite: info.lock.sprite,
    };
    await writeFile(join(stage, 'install.json'), JSON.stringify(manifest, null, 2) + '\n', { flag: 'wx' });
    if (!(await verifyInstallation(stage, info))) fail('導入後の検査に失敗しました。');
    signal?.throwIfAborted();
    let previous;
    try {
      await lstat(target);
      previous = join(root, `.previous-${info.id}-${randomUUID()}`);
      await rename(target, previous);
    } catch (error) {
      if (error.code !== 'ENOENT') throw error;
    }
    try {
      await rename(stage, target);
      stage = undefined;
    } catch (error) {
      if (previous) await rename(previous, target);
      throw error;
    }
    log(`公式資産を導入しました: ${target}`);
    return target;
  } finally {
    if (stage) await rm(stage, { recursive: true, force: true });
    await release();
  }
}

async function main() {
  const args = process.argv.slice(2);
  let dataDir, lockPath;
  for (let i = 0; i < args.length; i++) {
    if (args[i] === '--data-dir' && args[i + 1]) dataDir = args[++i];
    else if (args[i] === '--lock' && args[i + 1]) lockPath = args[++i];
    else fail('使い方: node assets/install.mjs [--data-dir <dir>] [--lock <path>]');
  }
  const controller = new AbortController();
  const stop = () => controller.abort(new Error('公式資産の導入を中断しました。'));
  process.once('SIGINT', stop);
  process.once('SIGTERM', stop);
  try {
    await install({ dataDir, lockPath, signal: controller.signal, log: console.log });
  } finally {
    process.removeListener('SIGINT', stop);
    process.removeListener('SIGTERM', stop);
  }
}
if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  main().catch((error) => {
    console.error(`GitLab公式資産: ${error.message}`);
    process.exitCode = 1;
  });
}
