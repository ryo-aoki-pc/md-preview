import { realpathSync } from 'node:fs';

const DEVICES = new Set(['con', 'prn', 'aux', 'nul', 'conin$', 'conout$']);

// Lua の gsub と同じく、不完全な % は残し、有効な %HH だけをバイトへ戻す。
function unescapePath(raw) {
  const bytes = Buffer.from(raw);
  const out = [];
  for (let i = 0; i < bytes.length; i++) {
    const hex = bytes.subarray(i + 1, i + 3).toString('latin1');
    if (bytes[i] === 37 && /^[0-9a-f]{2}$/i.test(hex)) {
      out.push(Number.parseInt(hex, 16));
      i += 2;
    } else out.push(bytes[i]);
  }
  return Buffer.from(out).toString('utf8');
}

// _ref/lua/server.lua の M.resolve と同じ順番で検査する。root は realpath 済み。
export function resolve(root, raw, isWin = process.platform === 'win32') {
  if (raw.includes('%00')) return { status: 400 };
  const path = unescapePath(raw);
  if (/[\x00-\x1f\x7f]/.test(path) || path.includes('\\') || (isWin && path.includes(':'))) {
    return { status: 400 };
  }
  const segs = [];
  for (const seg of path.split('/').filter(Boolean)) {
    if (seg === '..') return { status: 403 };
    if (seg === '.') continue;
    if (isWin) {
      const stem = seg.split('.')[0].toLowerCase();
      if (/[. ]$/.test(seg) || DEVICES.has(stem) || /^(?:com|lpt)\d$/.test(stem)) {
        return { status: 400 };
      }
    }
    if (seg.toLowerCase() === '.git') return { status: 403 };
    segs.push(seg);
  }
  if (!segs.length) return { status: 404 };
  const base = root.replaceAll('\\', '/').replace(/\/+$/, '');
  let real;
  try {
    real = realpathSync.native(base + '/' + segs.join('/')).replaceAll('\\', '/');
  } catch {
    return { status: 404 };
  }
  const a = isWin ? real.toLowerCase() : real;
  const b = isWin ? (base + '/').toLowerCase() : base + '/';
  if (!a.startsWith(b)) return { status: 403 };
  if (
    real
      .slice(b.length)
      .split('/')
      .some((seg) => seg.toLowerCase() === '.git')
  )
    return { status: 403 };
  return { status: 200, path: real };
}
