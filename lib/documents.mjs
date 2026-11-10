import { lstat, opendir, stat } from 'node:fs/promises';
import { join, relative } from 'node:path';
import { resolve } from './resolve.mjs';

export const isMarkdown = (name) => /\.(?:md|markdown|mdown|mkd|mkdn)$/i.test(name);
export const encodeDocument = (name) => name.split('/').map(encodeURIComponent).join('/');

// 返す名前だけでなく、リンク先の実パスも既存の閉じ込めの検査を通す。
// maxEntries: 1 つのフォルダーから読む名前の数の上限。巨大なフォルダーの中身を、全部メモリーへ載せない。
export async function listMarkdown(root, { maxDepth = 20, maxFiles = 5000, maxEntries = 50000 } = {}) {
  const files = [];
  let truncated = false;
  if (!root) return { files, truncated };
  async function walk(dir, rel, depth) {
    const entries = [];
    try {
      const handle = await opendir(dir);
      for await (const entry of handle) {
        if (entries.length === maxEntries) {
          truncated = true;
          break; // for await を抜けると、フォルダーは閉じられる
        }
        entries.push(entry);
      }
    } catch {
      return;
    }
    entries.sort((a, b) => {
      const left = a.name + (a.isDirectory() ? '/' : ''),
        right = b.name + (b.isDirectory() ? '/' : '');
      return left < right ? -1 : left > right ? 1 : 0;
    });
    for (const entry of entries) {
      if (entry.name.startsWith('.') || entry.name.toLowerCase() === 'node_modules') continue;
      const name = rel + entry.name;
      const path = join(dir, entry.name);
      if (entry.isDirectory() && !entry.isSymbolicLink()) {
        // Windows のジャンクションも lstat で確かめて、フォルダーのリンクへは入らない。
        let info;
        try {
          info = await lstat(path);
        } catch {
          continue;
        }
        if (info.isSymbolicLink()) continue;
        if (depth >= maxDepth) {
          truncated = true;
          continue;
        }
        await walk(path, name + '/', depth + 1);
      } else if (isMarkdown(entry.name)) {
        const result = resolve(root, encodeDocument(name));
        if (result.status !== 200) continue;
        if (
          relative(root, result.path)
            .replaceAll('\\', '/')
            .split('/')
            .some((part) => part.startsWith('.') || part.toLowerCase() === 'node_modules')
        )
          continue;
        let info;
        try {
          info = await stat(result.path);
        } catch {
          continue;
        }
        if (!info.isFile()) continue;
        if (files.length === maxFiles) {
          truncated = true;
          return;
        }
        files.push(name);
      }
      if (files.length === maxFiles && truncated) return;
    }
  }
  await walk(root, '', 0);
  files.sort();
  return { files, truncated };
}
