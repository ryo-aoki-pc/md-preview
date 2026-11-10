import { isUtf8 } from 'node:buffer';
import { randomBytes } from 'node:crypto';
import { createReadStream, statSync } from 'node:fs';
import { createServer as httpServer, ServerResponse, STATUS_CODES } from 'node:http';
import { extname } from 'node:path';
import { resolve } from './resolve.mjs';
import { listMarkdown } from './documents.mjs';
import { loadAssets, assetsPageConfig } from './assets.mjs';
import { readLibrary } from './page-libs.mjs';

export const CSP =
  "default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; font-src 'self' data:; img-src * data: blob:; media-src *; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'";
const MAX_QUEUE = 8 * 1024 * 1024;
const COMMON = { 'X-Content-Type-Options': 'nosniff', 'Referrer-Policy': 'no-referrer' };
const MIME = {
  png: 'image/png',
  jpg: 'image/jpeg',
  jpeg: 'image/jpeg',
  gif: 'image/gif',
  webp: 'image/webp',
  avif: 'image/avif',
  svg: 'image/svg+xml',
  bmp: 'image/bmp',
  ico: 'image/x-icon',
  mp4: 'video/mp4',
  webm: 'video/webm',
  mov: 'video/quicktime',
  mp3: 'audio/mpeg',
  ogg: 'audio/ogg',
  wav: 'audio/wav',
  pdf: 'application/pdf',
  txt: 'text/plain; charset=utf-8',
  md: 'text/plain; charset=utf-8',
  csv: 'text/plain; charset=utf-8',
};
const event = (name, data) => `event: ${name}\ndata: ${JSON.stringify(data)}\n\n`;

// Lua 版のプロセス内でのポート再利用は行わない。単体コマンドごとに起動し、固定したい場合は port を指定する。
export async function createServer({
  session,
  page,
  port = 0,
  assetsDir,
  assetsLockPath,
  officialStyle = true,
  onJump,
  onClientCount = () => {},
  token = randomBytes(16).toString('hex'),
}) {
  if (!/^[0-9a-f]{32}$/.test(token)) throw new Error('token は16バイトの16進文字列にしてください');
  // 公式スタイルは、保存先を渡されたときだけ使う (入口の cli.mjs が既定の保存先を渡す。試験は、この PC の保存先を読まない)。
  const assets = officialStyle && assetsDir ? loadAssets(assetsDir, assetsLockPath) : null;
  const clients = new Set();
  const routes = new Map();
  const remembered = {};
  let closing;
  let boundPort;

  function removeClient(client) {
    routes.get(client)?.release?.();
    routes.delete(client);
    if (clients.delete(client)) onClientCount(clients.size);
  }

  const offRoot = session.onRootChange?.(() => {
    delete remembered.render;
    delete remembered.scroll;
  });
  function parameters(req) {
    const mark = req.url.indexOf('?');
    return new URLSearchParams(mark < 0 ? '' : req.url.slice(mark + 1));
  }
  function pageHtml(req) {
    const params = parameters(req);
    const match = /(<script id="gp-config"[^>]*>)([\s\S]*?)(<\/script>)/.exec(page.html);
    if (!match) return page.html;
    const config = JSON.parse(match[2]);
    Object.assign(config, assetsPageConfig(assets, officialStyle ? assetsDir : null));
    config.hasDocument = session.hasDocument ? session.hasDocument() : !!session.last().render;
    config.list = params.has('list') || (config.directory && !params.has('doc'));
    config.currentDocument = !params.has('doc') || !!session.isCurrentDocument?.(params.get('doc'));
    const json = JSON.stringify(config).replaceAll('<', '\\u003c');
    return page.html.replace(match[0], () => match[1] + json + match[3]);
  }

  function send(req, res, status, headers = {}, body = status >= 400 ? STATUS_CODES[status] : '') {
    if (res.destroyed) return;
    const value = Buffer.isBuffer(body) ? body : Buffer.from(body);
    const out = { ...COMMON, Connection: 'close', ...headers };
    if (status !== 304) out['Content-Length'] = value.length;
    if (value.length && !out['Content-Type']) out['Content-Type'] = 'text/plain; charset=utf-8';
    res.writeHead(status, out);
    res.end(req.method === 'HEAD' || status === 304 ? undefined : value);
  }

  function file(req, res, path, mime, sandbox) {
    let st;
    try {
      st = statSync(path, { bigint: true });
    } catch {
      return send(req, res, 404);
    }
    if (!st.isFile()) return send(req, res, 404);
    const headers = {
      'Content-Type': mime,
      'Cache-Control': 'no-cache',
      ETag: `W/"${st.size}-${st.mtimeNs / 1000000000n}-${st.mtimeNs % 1000000000n}"`,
    };
    if (sandbox) headers['Content-Security-Policy'] = 'sandbox';
    if (req.headers['if-none-match'] === headers.ETag) return send(req, res, 304, headers);
    if (req.method === 'HEAD') {
      res.writeHead(200, { ...COMMON, Connection: 'close', ...headers, 'Content-Length': String(st.size) });
      return res.end();
    }
    const stream = createReadStream(path, { highWaterMark: 64 * 1024 });
    stream.once('open', () => {
      res.writeHead(200, { ...COMMON, Connection: 'close', ...headers, 'Content-Length': String(st.size) });
      stream.pipe(res);
    });
    stream.on('error', () => (res.headersSent ? res.destroy() : send(req, res, 404)));
    res.once('close', () => stream.destroy());
  }

  // 許可リストは置いてある lock.json だけ。CSS の原文に残る woff / ttf は写さず、要求には404を返す。
  // 配るライブラリの URL を直接開かれても、この origin で動かないよう sandbox を付ける
  // (<script>・<link>・フォントとして読むときには効かない)。
  // 配るのは、lock.json の大きさと sha256 に合うバイト列だけ (置いてあるファイルが差し替わっていたら 404)。
  function library(req, res, rel) {
    const found = readLibrary(rel.slice('_/lib/'.length));
    if (!found) return send(req, res, 404);
    const headers = {
      'Content-Type':
        { '.js': 'text/javascript; charset=utf-8', '.css': 'text/css; charset=utf-8', '.woff2': 'font/woff2' }[
          extname(rel)
        ] || 'text/plain; charset=utf-8',
      'Cache-Control': 'no-cache',
      ETag: '"' + found.sha256 + '"',
      'Content-Security-Policy': 'sandbox',
    };
    return send(req, res, req.headers['if-none-match'] === headers.ETag ? 304 : 200, headers, found.body);
  }

  function write(client, text) {
    if (client.destroyed || client.writableLength + Buffer.byteLength(text) > MAX_QUEUE) {
      removeClient(client);
      client.destroy();
      return;
    }
    client.write(text);
  }

  // ダブルクリックから戻る入口だけを POST で受ける。token に加えて Origin も確かめる。
  function jump(req, res) {
    const origin = req.headers.origin;
    if (origin !== 'http://127.0.0.1:' + boundPort && origin !== 'http://localhost:' + boundPort) {
      return send(req, res, 403);
    }
    if (!/^application\/json(?:\s*;[^\r\n]*)?$/i.test(req.headers['content-type'] || '')) return send(req, res, 415);
    const length = Number(req.headers['content-length']);
    if (Number.isFinite(length) && length > 1024) return send(req, res, 413);
    let bytes = 0;
    const parts = [];
    let rejected = false;
    req.on('data', (part) => {
      if (rejected) return;
      bytes += part.length;
      if (bytes > 1024) {
        rejected = true;
        parts.length = 0;
        send(req, res, 413);
      } else parts.push(part);
    });
    req.on('error', () => {
      rejected = true;
    });
    req.on('end', () => {
      if (rejected || res.destroyed) return;
      let value;
      try {
        const body = Buffer.concat(parts);
        if (!isUtf8(body)) throw new Error();
        value = JSON.parse(body.toString('utf8'));
      } catch {
        return send(req, res, 400);
      }
      if (
        !value ||
        typeof value !== 'object' ||
        Array.isArray(value) ||
        !Number.isSafeInteger(value.line) ||
        value.line < 1
      )
        return send(req, res, 400);
      try {
        const params = parameters(req);
        if (params.has('doc') && !session.isCurrentDocument?.(params.get('doc'))) return send(req, res, 403);
        onJump({ line: value.line });
        send(req, res, 204, {}, '');
      } catch {
        send(req, res, 500);
      }
    });
  }

  // Lua 版の手書きの解析に代えて Node のパーサーを使う。16 KB の上限は、終端が届いた要求にも適用する。
  const handle = (req, res) => {
    try {
      if (!req.headers.host) return send(req, res, 400);
      if (req.headers.host !== `127.0.0.1:${boundPort}` && req.headers.host !== `localhost:${boundPort}`)
        return send(req, res, 403);
      const site = req.headers['sec-fetch-site'];
      if (site && site !== 'same-origin' && site !== 'none') return send(req, res, 403);
      // URL は正規化しない。.. や %2e%2e を resolve に渡して拒否する。
      const path = req.url.split(/[?#]/, 1)[0];
      const base = '/' + token;
      const jumpRoute = typeof onJump === 'function' && path === base + '/_/jump';
      const allow = jumpRoute ? 'GET, HEAD, POST' : 'GET, HEAD';
      if (req.method === 'POST' && jumpRoute) return jump(req, res);
      if (req.method !== 'GET' && req.method !== 'HEAD') return send(req, res, 405, { Allow: allow });
      // 末尾の / が無い URL は、クエリ (?list・?doc=) を保ったまま / 付きへ送る。
      if (path === base) {
        const query = req.url.includes('?') ? req.url.slice(req.url.indexOf('?')) : '';
        return send(req, res, 301, { Location: base + '/' + query }, '');
      }
      if (!path.startsWith(base + '/')) return send(req, res, 404);
      const rel = path.slice(base.length + 1);
      if (rel === '')
        return send(
          req,
          res,
          200,
          { 'Content-Type': 'text/html; charset=utf-8', 'Cache-Control': 'no-store', 'Content-Security-Policy': CSP },
          pageHtml(req),
        );
      const builtins = {
        '_/preview.js': ['js', 'text/javascript; charset=utf-8'],
        '_/preview.css': ['css', 'text/css; charset=utf-8'],
        '_/fallback.css': ['fallback', 'text/css; charset=utf-8'],
      };
      if (Object.hasOwn(builtins, rel)) {
        const [key, mime] = builtins[rel];
        return send(req, res, 200, { 'Content-Type': mime, 'Cache-Control': 'no-store' }, page[key] || '');
      }
      if (rel.startsWith('_/lib/')) return library(req, res, rel);
      if (rel.startsWith('_/assets/')) {
        const prefix = assets && '_/assets/' + assets.id + '/';
        const item = prefix && rel.startsWith(prefix) && assets.files.get(rel.slice(prefix.length));
        if (!item) return send(req, res, 404);
        const headers = {
          'Content-Type': item.mime,
          'Cache-Control': 'private, max-age=31536000, immutable',
          ETag: '"' + item.sha256 + '"',
          'Content-Security-Policy': 'sandbox',
        };
        return send(req, res, req.headers['if-none-match'] === headers.ETag ? 304 : 200, headers, item.body);
      }
      if (rel === '_/list') {
        return listMarkdown(session.root()).then(
          (data) =>
            send(
              req,
              res,
              200,
              { 'Content-Type': 'application/json; charset=utf-8', 'Cache-Control': 'no-store' },
              JSON.stringify(data),
            ),
          () => send(req, res, 500),
        );
      }
      if (rel === '_/events') {
        const headers = { 'Content-Type': 'text/event-stream; charset=utf-8', 'Cache-Control': 'no-store' };
        if (req.method === 'HEAD') return send(req, res, 200, headers, '');
        // Lua 版の接続終端による区切りに代わり、Node が SSE を chunked で送る。イベントの内容は同じ。
        res.writeHead(200, { ...COMMON, Connection: 'close', ...headers });
        const params = parameters(req);
        const doc = params.has('doc') ? params.get('doc') : null;
        routes.set(res, { doc, release: null });
        clients.add(res);
        onClientCount(clients.size);
        res.on('close', () => removeClient(res));
        write(res, 'retry: 1000\n\n');
        if (doc !== null) {
          if (session.subscribeDocument) {
            const release = session.subscribeDocument(doc, (name, data) =>
              write(
                res,
                event(name, name === 'render' ? { ...data, currentDocument: !!session.isCurrentDocument(doc) } : data),
              ),
            );
            if (clients.has(res)) routes.get(res).release = release;
            else release();
          } else
            write(
              res,
              event('render', { file: doc, dir: '', rev: 0, error: 'この文書は開けない', markdown: '', headings: [] }),
            );
        } else {
          const last = { ...session.last(), ...remembered };
          for (const name of ['render', 'scroll', 'stop']) if (last[name]) write(res, event(name, last[name]));
        }
        return;
      }
      const root = session.root();
      if (!root) return send(req, res, 404);
      const result = resolve(root, rel);
      if (result.status !== 200) return send(req, res, result.status);
      const ext = extname(result.path).slice(1).toLowerCase();
      // 拡張子で表を引くときに、継承した名前 (constructor など) を拾わない。
      file(req, res, result.path, Object.hasOwn(MIME, ext) ? MIME[ext] : 'application/octet-stream', ext !== 'pdf');
    } catch {
      if (res.headersSent) res.destroy();
      else send(req, res, 500);
    }
  };
  // Node が Host のない要求を先に返すと共通ヘッダーが付かないため、handle で Lua 版と同じ400を返す。
  const server = httpServer({ maxHeaderSize: 16 * 1024, requireHostHeader: false }, handle);
  // Lua 版と同じく Expect を特別扱いせず、100や417の自動応答を経由しない。
  server.on('checkContinue', handle);
  server.on('checkExpectation', handle);
  // CONNECT も Host を調べてから405にする。Node の既定の切断処理へは流さない。
  server.on('connect', (req, socket) => {
    const res = new ServerResponse(req);
    res.assignSocket(socket);
    res.once('finish', () => socket.end());
    handle(req, res);
  });
  // Node の HTTP パーサーが拒む応答にも、共通の守りのヘッダーを付ける。
  server.on('clientError', (error, socket) => {
    const code = error.code === 'HPE_HEADER_OVERFLOW' ? 431 : 400;
    if (socket.writable)
      socket.end(
        `HTTP/1.1 ${code} ${STATUS_CODES[code]}\r\nConnection: close\r\nX-Content-Type-Options: nosniff\r\nReferrer-Policy: no-referrer\r\nContent-Length: 0\r\n\r\n`,
      );
  });
  await new Promise((resolve, reject) => {
    server.once('error', reject);
    server.listen(port, '127.0.0.1', () => {
      server.off('error', reject);
      resolve();
    });
  });
  boundPort = server.address().port;
  return {
    url: `http://127.0.0.1:${boundPort}/${token}/`,
    port: boundPort,
    token,
    broadcast(name, data) {
      if (closing) return;
      if (!['render', 'scroll', 'stop', 'reload'].includes(name)) throw new Error('未対応のイベントです');
      if (name === 'render' || name === 'scroll') remembered[name] = data;
      if (name === 'stop') remembered.stop = data;
      if (name === 'render') delete remembered.stop;
      const text = event(name, data);
      for (const client of clients) {
        if ((name === 'render' || name === 'scroll') && routes.get(client)?.doc !== null) continue;
        write(client, text);
      }
    },
    clientCount: () => clients.size,
    close(reason = 'stop') {
      if (closing) return closing;
      offRoot?.();
      for (const client of clients) {
        write(client, event('stop', { reason: reason === 'exit' ? 'exit' : 'stop' }));
        client.end();
      }
      closing = new Promise((resolve) => {
        // Node の接続が残り続けるのを防ぐため、stop の書き込み後は最大250ミリ秒だけ待つ。
        const force = setTimeout(() => server.closeAllConnections(), 250);
        server.close(() => {
          clearTimeout(force);
          resolve();
        });
        server.closeIdleConnections();
      });
      return closing;
    },
  };
}
