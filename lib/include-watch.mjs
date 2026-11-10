// include の文字どおりの相対名を、worker の locate と同じ resolve() で検査する。
import { statSync } from 'node:fs';
import { join } from 'node:path';
import { resolve } from './resolve.mjs';

function locate(root, name) {
  try {
    return resolve(root, name.split('/').map(encodeURIComponent).join('/'));
  } catch {
    return { status: 400 };
  }
}
export function includeWatchPath(root, name) {
  if (!root || typeof name !== 'string') return null;
  const parts = name.split('/').filter((part) => part && part !== '.');
  if (!parts.length) return null;
  const result = locate(root, name);
  if (result.status === 200) return result.path;
  if (result.status !== 404) return null;
  // 無いファイルは、最も近い存在する親まで実パスを調べる。外へ出るリンクの下は見張らない。
  for (let count = parts.length - 1; count > 0; count--) {
    const parent = locate(root, parts.slice(0, count).join('/'));
    if (parent.status === 404) continue;
    if (parent.status !== 200) return null;
    try {
      if (!statSync(parent.path).isDirectory()) return null;
      return join(parent.path, ...parts.slice(count));
    } catch {
      return null;
    }
  }
  try {
    return statSync(root).isDirectory() ? join(root, ...parts) : null;
  } catch {
    return null;
  }
}
