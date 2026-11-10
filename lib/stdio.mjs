import { isUtf8 } from 'node:buffer';
import { isAbsolute } from 'node:path';
import { createSession } from './session.mjs';
import { createServer } from './server.mjs';
import { loadPage } from './page.mjs';

export const MAX_LINE_BYTES = 96 * 1024 * 1024;
const MAX_OUTPUT_BYTES = 1024 * 1024;

// 小さく分かれて届いても、断片の配列を無制限に増やさない。
export async function readLines(input, onLine, onOversize, maxBytes = MAX_LINE_BYTES) {
  let buffer = Buffer.allocUnsafe(Math.min(65536, maxBytes));
  let used = 0;
  let dropping = false;
  const reset = () => {
    buffer = Buffer.allocUnsafe(Math.min(65536, maxBytes));
    used = 0;
  };
  for await (const value of input) {
    const chunk = Buffer.isBuffer(value) ? value : Buffer.from(value);
    let start = 0;
    while (start < chunk.length) {
      const end = chunk.indexOf(10, start);
      const stop = end < 0 ? chunk.length : end;
      if (!dropping) {
        const size = stop - start;
        if (used + size > maxBytes) {
          reset();
          dropping = true;
          if ((await onOversize()) === false) return;
        } else {
          if (buffer.length < used + size) {
            const next = Buffer.allocUnsafe(Math.min(maxBytes, Math.max(used + size, buffer.length * 2)));
            buffer.copy(next, 0, 0, used);
            buffer = next;
          }
          chunk.copy(buffer, used, start, stop);
          used += size;
        }
      }
      if (end < 0) break;
      if (dropping) dropping = false;
      else {
        const line = buffer.subarray(0, used);
        reset();
        if ((await onLine(line)) === false) return;
      }
      start = end + 1;
    }
  }
  // 最後の改行だけが省かれた行も、readline と同じく受け付ける。
  if (!dropping && used) await onLine(buffer.subarray(0, used));
}

// 標準出力へ JSON の行を書く。出すのは ready・clients・jump・error だけで、どれも短い。
// 相手 (Neovim) が読まずに溜まり続けたときは、上限を超えた分を捨てる (溜めてメモリーを使い切らない)。
// Windows では、パイプへの書き込みは同期で、パイプが一杯のまま相手が読まないと止まる。Neovim は標準出力を
// 読み続けるので、そのための別のプロセスは持たない。
function jsonOutput(output) {
  let failed = false;
  output.on?.('error', () => {
    failed = true;
  });
  return {
    send(value) {
      if (failed || output.destroyed || output.writableLength > MAX_OUTPUT_BYTES) return;
      try {
        output.write(JSON.stringify(value) + '\n');
      } catch {
        failed = true;
      }
    },
    // 書き終わるのを少しだけ待つ (読まれないまま残っていても、終了は止めない)。
    finish() {
      return new Promise((resolve) => {
        if (failed || output.destroyed || !output.writableLength) return resolve();
        const timer = setTimeout(resolve, 200);
        output.once('drain', () => {
          clearTimeout(timer);
          resolve();
        });
      });
    },
  };
}

const object = (value) => value !== null && typeof value === 'object' && !Array.isArray(value);
const lineNumber = (value) => Number.isSafeInteger(value) && value >= 1;

export async function runStdio({
  stdin,
  stdout,
  signals,
  port,
  version,
  onStart,
  assetsDir,
  officialStyle,
  assetsLockPath,
  plantuml = null,
}) {
  const output = jsonOutput(stdout);
  let session;
  let server;
  let greeted = false;
  let paused = false;
  let closing = false;
  let currentPath = null;
  let code = 0;
  const close = () => {
    closing = true;
    stdin.destroy?.();
  };
  const error = (message, fatal = false) => {
    output.send({ type: 'error', ...(fatal ? { fatal: true } : {}), message });
    if (fatal) {
      code = 2;
      closing = true;
    }
    return !fatal;
  };
  signals.on('SIGINT', close);
  signals.on('SIGTERM', close);

  async function message(line) {
    if (closing) return false;
    let value;
    try {
      if (!isUtf8(line)) throw new Error();
      value = JSON.parse(line.toString('utf8'));
    } catch {
      return error('JSON の行を読めません', !greeted);
    }
    if (!object(value)) return error('JSON のオブジェクトを送ってください', !greeted);
    if (value.type === 'hello' && value.protocol !== 1)
      return error('対応していない取り決めの版です。protocol は1にしてください', true);
    if (!greeted) {
      if (value.type !== 'hello') return error('最初に hello を送ってください', true);
      const appearance = object(value.appearance) ? value.appearance : {};
      const theme = ['system', 'light', 'dark'].includes(appearance.theme) ? appearance.theme : 'system';
      const width = ['fluid', 'fixed'].includes(appearance.width) ? appearance.width : 'fluid';
      session = createSession({
        plantuml: !!plantuml,
        onRender: (data) => {
          if (!paused && !closing) server?.broadcast('render', data);
        },
        onScroll: (data) => {
          if (!paused && !closing) server?.broadcast('scroll', data);
        },
      });
      try {
        server = await createServer({
          session,
          plantuml,
          page: loadPage({ theme, width, jump: true }),
          port,
          assetsDir,
          assetsLockPath,
          officialStyle,
          onJump: ({ line }) => output.send({ type: 'jump', path: currentPath, line }),
          onClientCount: (count) => output.send({ type: 'clients', count }),
        });
      } catch {
        return error('プレビューのサーバーを起動できません', true);
      }
      if (closing) return false;
      greeted = true;
      output.send({ type: 'ready', protocol: 1, version, url: server.url });
      output.send({ type: 'clients', count: server.clientCount() });
      await onStart?.({ url: server.url, server, session, close });
      return !closing;
    }
    switch (value.type) {
      case 'doc':
        if (paused) return true;
        if (
          !(
            value.path === null ||
            (typeof value.path === 'string' && isAbsolute(value.path) && !value.path.includes('\0'))
          ) ||
          typeof value.name !== 'string' ||
          typeof value.text !== 'string' ||
          !Number.isFinite(value.rev)
        ) {
          return error('doc の path・name・text・rev の型または値が正しくありません');
        }
        try {
          session.setDocument(value);
          currentPath = value.path;
        } catch {
          return error('文書のパスまたはフォルダーを使えません');
        }
        return true;
      case 'scroll':
        if (paused) return true;
        if (
          !lineNumber(value.line) ||
          !lineNumber(value.lines) ||
          !Number.isFinite(value.ratio) ||
          value.ratio < 0 ||
          value.ratio > 1
        ) {
          return error('scroll の line・ratio・lines の型または値が正しくありません');
        }
        session.setScroll(value);
        return true;
      case 'pause':
        paused = true;
        // 休止前の描画が、resume の後に遅れて届かないように取り消す。
        session.cancelRender();
        server.broadcast('stop', { reason: 'stop' });
        return true;
      case 'resume':
        paused = false;
        return true;
      case 'hello':
        return error('hello は最初に1回だけ送ってください');
      default:
        return error('知らない行の種類です。type を確かめてください');
    }
  }

  try {
    await readLines(stdin, message, () => error('1行の大きさが上限の96 MBを超えています', !greeted));
  } catch {
    if (!closing) error('標準入力を読めません', true);
  } finally {
    closing = true;
    signals.off('SIGINT', close);
    signals.off('SIGTERM', close);
    stdin.destroy?.();
    await Promise.all([server?.close('exit'), session?.close()]);
    await output.finish();
  }
  return code;
}
