// 導入済みの資産だけを読む。通信せず、検査したバイト列だけをサーバーへ渡す。
import { createHash } from 'node:crypto';
import { lstatSync, readdirSync, readFileSync, realpathSync, statSync } from 'node:fs';
import { join } from 'node:path';
import { isDeepStrictEqual } from 'node:util';
import { defaultAssetsDirectory } from '../assets/directory.mjs';
import { resolve } from './resolve.mjs';
export { defaultAssetsDirectory } from '../assets/directory.mjs';

const digest = (bytes) => createHash('sha256').update(bytes).digest('hex');
const MIME = {
  css: 'text/css; charset=utf-8',
  woff2: 'font/woff2',
  woff: 'font/woff',
  ttf: 'font/ttf',
  eot: 'application/vnd.ms-fontobject',
  svg: 'image/svg+xml',
  png: 'image/png',
  jpg: 'image/jpeg',
  jpeg: 'image/jpeg',
  gif: 'image/gif',
  webp: 'image/webp',
  avif: 'image/avif',
  ico: 'image/x-icon',
  txt: 'text/plain; charset=utf-8',
  md: 'text/plain; charset=utf-8',
  json: 'application/json; charset=utf-8',
};
const record = (value) => value && typeof value === 'object' && !Array.isArray(value);
const check = (condition) => {
  if (!condition) throw new Error('公式資産の検査に失敗しました');
};

export function loadAssets(
  directory = defaultAssetsDirectory(),
  lockPath = new URL('../assets/assets.lock.json', import.meta.url),
) {
  try {
    const bytes = readFileSync(lockPath);
    const lock = JSON.parse(bytes);
    check(typeof lock.version === 'string' && /^[\w.-]+$/.test(lock.version) && record(lock.files));
    const hash = digest(bytes),
      id = lock.version + '-' + hash.slice(0, 12);
    // 版のフォルダーそのものがリンクなら使わない (取得の処理の検査と同じ)。
    const folder = lstatSync(join(directory, id));
    check(folder.isDirectory() && !folder.isSymbolicLink());
    const root = realpathSync.native(join(directory, id));
    const manifestPath = resolve(root, 'install.json');
    check(manifestPath.status === 200);
    const manifest = JSON.parse(readFileSync(manifestPath.path));
    check(
      manifest.schema === 1 && manifest.id === id && manifest.lock_sha256 === hash && manifest.version === lock.version,
    );
    check(record(manifest.files) && isDeepStrictEqual(lock.styles, manifest.styles) && lock.sprite === manifest.sprite);
    const files = new Map();
    for (const [path, item] of Object.entries(manifest.files)) {
      check(
        /^[\w./@-]+$/.test(path) &&
          !path.startsWith('/') &&
          !path.split('/').some((part) => !part || ['.', '..', '.git'].includes(part.toLowerCase())),
      );
      const expected = Object.hasOwn(lock.files, path) && lock.files[path];
      check(
        record(item) &&
          record(expected) &&
          item.sha256 === (expected.output_sha256 || expected.sha256) &&
          item.size === (expected.output_size ?? expected.size),
      );
      const ext = path.split('.').at(-1).toLowerCase();
      const mime = Object.hasOwn(MIME, ext) && MIME[ext];
      check(mime && /^[a-f\d]{64}$/i.test(item.sha256) && Number.isSafeInteger(item.size) && item.size >= 0);
      const target = resolve(root, path);
      check(target.status === 200 && statSync(target.path).isFile() && statSync(target.path).size === item.size);
      const body = readFileSync(target.path);
      check(body.length === item.size && digest(body) === item.sha256);
      files.set(path, { body, sha256: item.sha256, mime });
    }
    check(Object.keys(lock.files).length > 0 && Object.keys(lock.files).every((path) => files.has(path)));
    for (const theme of ['light', 'dark'])
      check(
        Array.isArray(manifest.styles?.[theme]) &&
          manifest.styles[theme].length >= 4 &&
          manifest.styles[theme].every((path) => files.get(path)?.mime === MIME.css),
      );
    check(typeof manifest.sprite === 'string' && files.has(manifest.sprite));
    return { id, version: lock.version, files, styles: manifest.styles, sprite: manifest.sprite };
  } catch {
    return null;
  }
}

// 保存先に何かあるのに使えない (壊れている・この版のものでない) ときだけ知らせる。
// 何も無いのは既定の状態 (同梱のスタイルで表示する) なので、知らせない。
export function assetsNotice(loaded, directory) {
  if (loaded || !directory) return '';
  // 「何かある」と見るのは、版のフォルダー (<版>-<ロックのハッシュの先頭 12 桁>) だけ。
  // OS が置くファイル (desktop.ini など) や、取得の途中の作業用フォルダーでは知らせない。
  try {
    const versions = readdirSync(directory, { withFileTypes: true }).filter(
      (entry) => entry.isDirectory() && /^[\w][\w.-]*-[a-f0-9]{12}$/.test(entry.name),
    );
    if (!versions.length) return '';
  } catch {
    return '';
  }
  return '公式スタイルを使えません（壊れているか、この版のものではありません）。mdpreview install-style で取得し直せます';
}

export function assetsPageConfig(loaded, directory) {
  return {
    assets: loaded ? { id: loaded.id, version: loaded.version, styles: loaded.styles, sprite: loaded.sprite } : null,
    notice: assetsNotice(loaded, directory),
  };
}
