// ブラウザ用のライブラリ (page/lib/ の KaTeX・mermaid の写し) を読む。
// 名前は lock.json にあるものだけ、中身は lock.json の大きさと sha256 に合うものだけを返す
// (置いてあるファイルが差し替わっていたら、配らない・埋め込まない)。
import { createHash } from 'node:crypto';
import { readFileSync } from 'node:fs';

const root = new URL('../page/lib/', import.meta.url);
const lock = JSON.parse(readFileSync(new URL('lock.json', root), 'utf8'));
const records = new Map();
for (const [name, record] of Object.entries(lock.packages)) {
  if (!['katex', 'mermaid'].includes(name) || record.name !== name)
    throw new Error('ライブラリの固定情報が正しくありません');
  for (const [file, info] of Object.entries(record.files ?? {})) {
    if (
      !/^[a-z0-9_.-]+(?:\/[a-z0-9_.-]+)*$/i.test(file) ||
      file.split('/').some((part) => part === '.' || part === '..')
    )
      throw new Error('ライブラリのファイル名が正しくありません');
    if (!/^[a-f0-9]{64}$/.test(info?.sha256) || !Number.isSafeInteger(info?.size) || info.size < 0)
      throw new Error('ライブラリの固定情報が正しくありません');
    records.set(name + '/' + file, { path: new URL(name + '/' + file, root), sha256: info.sha256, size: info.size });
  }
}

const bodies = new Map();

// name は「katex/katex.min.js」の形。無い名前・読めないファイル・中身が合わないファイルは null。
export function readLibrary(name) {
  const record = records.get(name);
  if (!record) return null;
  let body = bodies.get(name);
  if (!body) {
    try {
      body = readFileSync(record.path);
    } catch {
      return null;
    }
    if (body.length !== record.size || createHash('sha256').update(body).digest('hex') !== record.sha256) return null;
    bodies.set(name, body);
  }
  return { body, sha256: record.sha256 };
}
