import assert from 'node:assert/strict';
import { mkdirSync, mkdtempSync, realpathSync, rmSync } from 'node:fs';
import { request } from 'node:http';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { setTimeout as delay } from 'node:timers/promises';

export function temp(t) {
  const base = fileURLToPath(new URL('../.tests/', import.meta.url));
  mkdirSync(base, { recursive: true });
  const path = realpathSync.native(mkdtempSync(join(base, 'case-')));
  t.after(() => rmSync(path, { recursive: true, force: true }));
  return path;
}

export async function until(predicate, message = '条件が時間内に成立する', timeout = 6000) {
  const end = Date.now() + timeout;
  while (Date.now() < end) {
    if (await predicate()) return;
    await delay(20);
  }
  assert.fail(message);
}

// http.request はパスを正規化しないので、脱出を狙う要求もそのまま送れる。
export function get(server, rel = '', { method = 'GET', headers = {}, path, body } = {}) {
  return new Promise((resolve, reject) => {
    const req = request({ hostname: '127.0.0.1', port: server.port,
      path: path ?? '/' + server.token + '/' + rel, method, headers }, (res) => {
      const chunks = [];
      res.on('data', (chunk) => chunks.push(chunk));
      res.on('end', () => resolve({ status: res.statusCode, headers: res.headers, body: Buffer.concat(chunks) }));
      res.on('error', reject);
    });
    req.on('error', reject);
    req.end(body);
  });
}

export async function events(server, { doc } = {}) {
  const received = [];
  let buffer = '';
  let response;
  let req;
  await new Promise((resolve, reject) => {
    const url = new URL('_/events', server.url);
    if (doc !== undefined) url.searchParams.set('doc', doc);
    req = request(url, (res) => {
      response = res;
      res.setEncoding('utf8');
      res.on('data', (text) => {
        buffer += text;
        let end;
        while ((end = buffer.indexOf('\n\n')) !== -1) {
          const block = buffer.slice(0, end);
          buffer = buffer.slice(end + 2);
          const name = /^event: (.+)$/m.exec(block)?.[1];
          const data = /^data: (.+)$/m.exec(block)?.[1];
          if (name && data) received.push({ name, data: JSON.parse(data) });
        }
      });
      resolve();
    });
    req.on('error', reject);
    req.end();
  });
  return { received, close() { response.destroy(); req.destroy(); } };
}
